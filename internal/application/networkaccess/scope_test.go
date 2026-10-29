package networkaccess

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type scopeAuthorizerFunc func(context.Context, domainaccess.Request) (domainaccess.Decision, error)

func (f scopeAuthorizerFunc) Authorize(ctx context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	return f(ctx, request)
}

func allowTestNetworkScope() ScopeAuthorizer {
	return scopeAuthorizerFunc(func(context.Context, domainaccess.Request) (domainaccess.Decision, error) {
		return domainaccess.Decision{Allowed: true}, nil
	})
}

type scopeStore struct {
	Store
	sites   []domain.Site
	device  domain.Device
	session domain.Session
	updates int
	reads   int
}

func (s *scopeStore) ListSites(_ context.Context, filter domain.SiteFilter) ([]domain.Site, error) {
	s.reads++
	if filter.Offset >= len(s.sites) {
		return []domain.Site{}, nil
	}
	end := min(filter.Offset+filter.Limit, len(s.sites))
	return s.sites[filter.Offset:end], nil
}

func (s *scopeStore) GetSite(_ context.Context, id string) (domain.Site, error) {
	s.reads++
	for _, item := range s.sites {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.Site{}, apperrors.ErrNotFound
}

func (s *scopeStore) GetDevice(context.Context, string) (domain.Device, error) {
	return s.device, nil
}

func (s *scopeStore) UpdateDevice(_ context.Context, _ string, _ domain.DeviceInput, _ time.Time) (domain.Device, error) {
	s.updates++
	return s.device, nil
}

func (s *scopeStore) GetSession(context.Context, string) (domain.Session, error) {
	return s.session, nil
}

func (s *scopeStore) GetSessionCommand(context.Context, string) (domain.SessionCommand, error) {
	return domain.SessionCommand{ID: "command-1", SessionID: s.session.ID}, nil
}

func newScopeService(store *scopeStore, keys ...string) (*Service, domainidentity.Principal) {
	permissions := appaccess.NewPermissionResolver(enrollmentRoleReader{"network-operator": keys})
	principal := domainidentity.Principal{UserID: "operator-1", Roles: []string{"network-operator"}}
	scope := scopeAuthorizerFunc(func(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
		return domainaccess.Decision{Allowed: request.Resource.Labels["network.siteId"] == "site-allowed"}, nil
	})
	return &Service{store: store, permissions: permissions, scope: scope}, principal
}

func TestNetworkListsApplyScopeBeforePublicLimit(t *testing.T) {
	store := &scopeStore{}
	for i := range 200 {
		store.sites = append(store.sites, domain.Site{ID: fmt.Sprintf("site-hidden-%d", i)})
	}
	store.sites = append(store.sites, domain.Site{ID: "site-allowed"})
	service, principal := newScopeService(store, appaccess.PermNetworkAccessSitesView)
	items, err := service.ListSites(context.Background(), principal, domain.SiteFilter{Limit: 1})
	if err != nil || len(items) != 1 || items[0].ID != "site-allowed" || store.reads != 2 {
		t.Fatalf("items=%v reads=%d error=%v", items, store.reads, err)
	}
}

func TestNetworkGetEnforcesRBACAndObjectScope(t *testing.T) {
	store := &scopeStore{sites: []domain.Site{{ID: "site-hidden"}}}
	service, principal := newScopeService(store, appaccess.PermNetworkAccessSitesView)
	if _, err := service.GetSite(context.Background(), principal, "site-hidden"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("out-of-scope get error=%v", err)
	}
	service, principal = newScopeService(store)
	store.reads = 0
	if _, err := service.GetSite(context.Background(), principal, "site-hidden"); !errors.Is(err, apperrors.ErrAccessDenied) || store.reads != 0 {
		t.Fatalf("RBAC error=%v reads=%d", err, store.reads)
	}
}

func TestDeviceMoveRequiresOldAndNewSiteScopes(t *testing.T) {
	for _, test := range []struct{ name, oldSite, newSite string }{
		{"source denied", "site-hidden", "site-allowed"},
		{"destination denied", "site-allowed", "site-hidden"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &scopeStore{device: domain.Device{ID: "device-1", OwnerUserID: "member-1", SiteID: test.oldSite}}
			service, principal := newScopeService(store, appaccess.PermNetworkAccessEndpointDevicesUpdate)
			_, err := service.UpdateDevice(context.Background(), principal, "device-1", domain.DeviceInput{Name: "Test device", SiteID: test.newSite, Status: domain.DeviceStatusActive, PostureStatus: domain.PostureCompliant, DeviceType: domain.DeviceTypeDesktop, OwnershipType: domain.DeviceOwnershipCompany})
			if !errors.Is(err, apperrors.ErrAccessDenied) || store.updates != 0 {
				t.Fatalf("error=%v updates=%d", err, store.updates)
			}
		})
	}
}

func TestNASCommandReadAndActionCannotBypassParentScope(t *testing.T) {
	store := &scopeStore{session: domain.Session{ID: "session-1", SiteID: "site-hidden", DeviceID: "device-1", SubjectID: "member-1"}}
	service, principal := newScopeService(store, appaccess.PermNetworkAccessSitesView, appaccess.PermNetworkAccessSitesUpdate)
	command, err := service.GetSessionCommand(context.Background(), principal, "command-1")
	if !errors.Is(err, apperrors.ErrAccessDenied) || command.ID != "" {
		t.Fatalf("command=%v error=%v", command, err)
	}
	if _, err := service.PlanSessionAction(context.Background(), principal, "session-1", domain.SessionActionInput{}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("plan error=%v", err)
	}
	if _, err := service.ExecuteSessionAction(context.Background(), principal, "session-1", domain.SessionActionInput{}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("execute error=%v", err)
	}
}

func TestScopeFailureReturnsNoPartialList(t *testing.T) {
	store := &scopeStore{sites: []domain.Site{{ID: "site-allowed"}, {ID: "site-unavailable"}}}
	service, principal := newScopeService(store, appaccess.PermNetworkAccessSitesView)
	want := errors.New("scope backend unavailable")
	service.scope = scopeAuthorizerFunc(func(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
		if request.Resource.Name == "site-unavailable" {
			return domainaccess.Decision{}, want
		}
		return domainaccess.Decision{Allowed: true}, nil
	})
	items, err := service.ListSites(context.Background(), principal, domain.SiteFilter{Limit: 2})
	if !errors.Is(err, want) || items != nil {
		t.Fatalf("items=%v error=%v", items, err)
	}
}
