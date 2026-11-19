package templateupdate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	appservice "github.com/opensoha/soha/internal/application/app"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type impactApplicationRepository struct {
	domainapp.Repository
	items    []domainapp.App
	filters  []domainapp.Filter
	getIDs   []string
	failPage int
	err      error
}

func (r *impactApplicationRepository) List(_ context.Context, filter domainapp.Filter) ([]domainapp.App, error) {
	r.filters = append(r.filters, filter)
	if len(r.filters) == r.failPage {
		return nil, r.err
	}
	start := 0
	if filter.After != nil {
		start = slices.IndexFunc(r.items, func(item domainapp.App) bool { return item.ID == filter.After.ID }) + 1
		if start == 0 {
			return nil, fmt.Errorf("unknown cursor: %+v", filter.After)
		}
	}
	return r.items[start:min(start+filter.Limit, len(r.items))], nil
}

func (r *impactApplicationRepository) Get(_ context.Context, id string) (domainapp.App, error) {
	r.getIDs = append(r.getIDs, id)
	for _, item := range r.items {
		if item.ID == id {
			return item, nil
		}
	}
	return domainapp.App{}, apperrors.ErrNotFound
}

type impactApplicationAuthorizer struct {
	denyCollection bool
	requests       []domainaccess.Request
}

func (a *impactApplicationAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	a.requests = append(a.requests, request)
	allowed := !a.denyCollection && (request.Delivery.ApplicationID == "" || request.Delivery.BusinessLineID == "visible")
	return domainaccess.Decision{Allowed: allowed}, nil
}

type impactApplicationAudit struct{ entries []domainaudit.Entry }

func (a *impactApplicationAudit) Record(_ context.Context, entry domainaudit.Entry) error {
	a.entries = append(a.entries, entry)
	return nil
}

func impactApplicationFixture(hidden, visible int) (*Service, *impactApplicationRepository, *impactApplicationAuthorizer, *impactApplicationAudit) {
	repo := &impactApplicationRepository{}
	for _, group := range []struct {
		name  string
		count int
	}{{"hidden", hidden}, {"visible", visible}} {
		for index := 0; index < group.count; index++ {
			repo.items = append(repo.items, domainapp.App{
				ID: fmt.Sprintf("%s-%04d", group.name, index), Group: group.name,
				Name: "same name", BusinessLineID: group.name,
			})
		}
	}
	authorizer := &impactApplicationAuthorizer{}
	audit := &impactApplicationAudit{}
	applications := appservice.New(repo, nil, authorizer, audit, nil)
	return &Service{deps: Dependencies{Applications: applications}}, repo, authorizer, audit
}

func TestBindingApplicationsCountsAccessibleAppsAcrossRawPages(t *testing.T) {
	for _, test := range []struct {
		name    string
		hidden  int
		visible int
		pages   int
	}{
		{name: "accessible after raw 501", hidden: 550, visible: 3, pages: 6},
		{name: "empty", pages: 1},
		{name: "only hidden", hidden: 1100, pages: 12},
		{name: "exact limit", hidden: 550, visible: 500, pages: 11},
		{name: "501 accessible", hidden: 550, visible: 501, pages: 11},
		{name: "stop at 501 accessible", hidden: 550, visible: 2000, pages: 11},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo, authorizer, audit := impactApplicationFixture(test.hidden, test.visible)
			items, err := service.bindingApplications(t.Context(), domainidentity.Principal{UserID: "reader"}, "")
			if test.visible > 500 {
				if !errors.Is(err, apperrors.ErrInvalidArgument) || !strings.Contains(err.Error(), "500 accessible applications") || items != nil {
					t.Fatalf("expected narrowing error, got %d items, %v", len(items), err)
				}
			} else {
				if err != nil || len(items) != test.visible {
					t.Fatalf("accessible apps = %d, err = %v, want %d", len(items), err, test.visible)
				}
				for index, item := range items {
					if item.ID != fmt.Sprintf("visible-%04d", index) {
						t.Fatalf("unexpected app or order at %d: %+v", index, item)
					}
				}
			}
			if len(repo.filters) != test.pages || len(repo.getIDs) != 0 {
				t.Fatalf("reads = %d list / %d get, want %d list", len(repo.filters), len(repo.getIDs), test.pages)
			}
			for page, filter := range repo.filters {
				if filter.Limit != 100 || filter.Search != "" {
					t.Fatalf("unbounded or changed filter: %+v", filter)
				}
				if page == 0 {
					if filter.After != nil {
						t.Fatalf("first page cursor = %+v", filter.After)
					}
				} else {
					last := repo.items[page*100-1]
					want := domainapp.ListCursor{Group: last.Group, Name: last.Name, ID: last.ID}
					if filter.After == nil || *filter.After != want {
						t.Fatalf("page %d cursor = %+v, want %+v", page, filter.After, want)
					}
				}
			}
			if len(audit.entries) != 1 || audit.entries[0].ActorID != "reader" || audit.entries[0].Action != "list" || audit.entries[0].Result != "success" {
				t.Fatalf("list audit = %+v", audit.entries)
			}
			if got, want := len(authorizer.requests), 1+test.hidden+min(test.visible, 501); got != want {
				t.Fatalf("authorization checks = %d, want %d", got, want)
			}
		})
	}
}

func TestBindingApplicationsNarrowsToNamedApplication(t *testing.T) {
	service, repo, _, audit := impactApplicationFixture(550, 501)
	items, err := service.bindingApplications(t.Context(), domainidentity.Principal{UserID: "reader"}, "visible-0500")
	if err != nil || len(items) != 1 || items[0].ID != "visible-0500" || len(repo.filters) != 0 || !slices.Equal(repo.getIDs, []string{"visible-0500"}) {
		t.Fatalf("named lookup = %+v, %v; queries = %+v, %+v", items, err, repo.filters, repo.getIDs)
	}
	if len(audit.entries) != 1 || audit.entries[0].Action != "view" {
		t.Fatalf("named lookup audit = %+v", audit.entries)
	}
	if _, err := service.bindingApplications(t.Context(), domainidentity.Principal{}, "hidden-0000"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("hidden named application = %v", err)
	}
	if _, err := service.bindingApplications(t.Context(), domainidentity.Principal{}, "missing"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("missing named application = %v", err)
	}
}

func TestBindingApplicationsReturnsReadAndAuthorizationErrors(t *testing.T) {
	t.Run("later page error", func(t *testing.T) {
		service, repo, _, audit := impactApplicationFixture(0, 200)
		repo.failPage, repo.err = 2, errors.New("database unavailable")
		items, err := service.bindingApplications(t.Context(), domainidentity.Principal{}, "")
		if !errors.Is(err, repo.err) || items != nil || len(audit.entries) != 0 {
			t.Fatalf("page error = %+v, %v; audit = %+v", items, err, audit.entries)
		}
	})
	t.Run("collection denied", func(t *testing.T) {
		service, repo, authorizer, audit := impactApplicationFixture(0, 1)
		authorizer.denyCollection = true
		if _, err := service.bindingApplications(t.Context(), domainidentity.Principal{}, ""); !errors.Is(err, apperrors.ErrAccessDenied) || len(repo.filters) != 0 || len(audit.entries) != 0 {
			t.Fatalf("collection denial = %v; queries = %+v; audit = %+v", err, repo.filters, audit.entries)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		service, repo, _, audit := impactApplicationFixture(0, 1)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := service.bindingApplications(ctx, domainidentity.Principal{}, ""); !errors.Is(err, context.Canceled) || len(repo.filters) != 0 || len(audit.entries) != 0 {
			t.Fatalf("cancelled read = %v; queries = %+v; audit = %+v", err, repo.filters, audit.entries)
		}
	})
}
