package delivery

import (
	"context"
	"errors"
	"reflect"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type versionedDraftApplication struct {
	stubApplicationReader
	writes int
	input  domainapp.UpsertInput
}

func (s *versionedDraftApplication) Update(_ context.Context, _ domainidentity.Principal, _ string, input domainapp.UpsertInput) (domainapp.App, error) {
	if input.ExpectedVersion == nil || *input.ExpectedVersion != s.app.Version {
		return domainapp.App{}, apperrors.ErrConflict
	}
	s.writes++
	s.input = input
	s.app.Version++
	return s.app, nil
}

func TestDeliveryDraftKeepsCapturedApplicationVersion(t *testing.T) {
	ctx := context.Background()
	apps := &versionedDraftApplication{stubApplicationReader: stubApplicationReader{app: domainapp.App{ID: "app-1", Key: "api", Name: "API", Version: 4, RepositoryIDs: []string{"repo-1"}}}}
	repo := &draftRepository{}
	service := New(apps, stubCatalogReader{}, stubBuildReader{}, stubWorkflowReader{}, stubReleaseReader{}, repo, nil, nil, deliveryActionPermissions(appaccess.PermDeliveryApplicationsUpdate))
	input := domaindelivery.DeliveryDraftInput{ApplicationDraft: domaindelivery.BlueprintApplicationDraft{Name: "API", Key: "api", Enabled: true}}
	draft, err := service.CreateDeliveryDraft(ctx, deliveryActionPrincipal(), input)
	if err != nil {
		t.Fatal(err)
	}
	if draft.ApplicationDraft.ID != "app-1" || draft.ApplicationDraft.ExpectedVersion == nil || *draft.ApplicationDraft.ExpectedVersion != 4 {
		t.Fatalf("draft version: %#v", draft.ApplicationDraft)
	}
	apps.app.Version = 5
	if _, err := service.ConfirmDeliveryDraft(ctx, deliveryActionPrincipal(), draft.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale draft = %v", err)
	}
	if apps.writes != 0 || repo.draft.Status != domaindelivery.DeliveryDraftStatusDraft {
		t.Fatal("stale draft wrote application or lost retry state")
	}
	if *repo.draft.ApplicationDraft.ExpectedVersion != 4 {
		t.Fatal("confirmation silently refreshed version")
	}

	draft, err = service.CreateDeliveryDraft(ctx, deliveryActionPrincipal(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmDeliveryDraft(ctx, deliveryActionPrincipal(), draft.ID); err != nil {
		t.Fatal(err)
	}
	if apps.writes != 1 || *repo.draft.ApplicationDraft.ExpectedVersion != 6 {
		t.Fatal("successful write version was not retained")
	}
	if !reflect.DeepEqual(apps.input.RepositoryIDs, []string{"repo-1"}) {
		t.Fatal("draft cleared repository associations")
	}

	if _, err := service.upsertApplication(ctx, deliveryActionPrincipal(), domainapp.UpsertInput{ID: "app-1", Name: "API", Key: "api"}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unversioned existing draft accepted: %v", err)
	}
}

func TestDraftCreationRetryUsesOriginalIntentBeforeVersionCapture(t *testing.T) {
	apps := &versionedDraftApplication{stubApplicationReader: stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "API", Key: "api", Version: 4}}}
	repo := &draftRepository{}
	service := New(apps, stubCatalogReader{}, stubBuildReader{}, stubWorkflowReader{}, stubReleaseReader{}, repo, nil, nil, deliveryActionPermissions(appaccess.PermDeliveryApplicationsUpdate))
	input := domaindelivery.DeliveryDraftInput{IdempotencyKey: "create-draft-1", ApplicationDraft: domaindelivery.BlueprintApplicationDraft{Name: "API", Key: "api"}}
	first, err := service.CreateDeliveryDraft(context.Background(), deliveryActionPrincipal(), input)
	if err != nil {
		t.Fatal(err)
	}
	apps.app.Version = 5
	replay, err := service.CreateDeliveryDraft(context.Background(), deliveryActionPrincipal(), input)
	if err != nil || replay.ID != first.ID || *replay.ApplicationDraft.ExpectedVersion != 4 || repo.createCount != 1 {
		t.Fatalf("replay recaptured a changed version: %+v %v", replay, err)
	}
	input.ApplicationDraft.Name = "Changed"
	if _, err := service.CreateDeliveryDraft(context.Background(), deliveryActionPrincipal(), input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("changed creation intent accepted: %v", err)
	}
}

func TestDraftScopeGuardRunsBeforeDomainWritesAndReceiptReplay(t *testing.T) {
	apps := &versionedDraftApplication{stubApplicationReader: stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "API", Key: "api", Version: 4}}}
	repo := &draftRepository{}
	service := New(apps, stubCatalogReader{}, stubBuildReader{}, stubWorkflowReader{}, stubReleaseReader{}, repo, nil, nil, deliveryActionPermissions(appaccess.PermDeliveryApplicationsUpdate))
	input := domaindelivery.DeliveryDraftInput{IdempotencyKey: "guard-draft-1", ApplicationDraft: domaindelivery.BlueprintApplicationDraft{Name: "API", Key: "api"}}
	blocked := domaindelivery.WithDraftScopeCheck(context.Background(), func(_ context.Context, draft domaindelivery.DeliveryDraft) error {
		if draft.ApplicationDraft.ID != "app-1" { t.Fatal("guard did not receive the resolved application") }
		return apperrors.ErrAccessDenied
	})
	if _, err := service.CreateDeliveryDraft(blocked, deliveryActionPrincipal(), input); !errors.Is(err, apperrors.ErrAccessDenied) || repo.createCount != 0 { t.Fatalf("create guard: %v", err) }
	draft, err := service.CreateDeliveryDraft(context.Background(), deliveryActionPrincipal(), input); if err != nil { t.Fatal(err) }
	if _, err := service.CreateDeliveryDraft(blocked, deliveryActionPrincipal(), input); !errors.Is(err, apperrors.ErrAccessDenied) || repo.createCount != 1 { t.Fatalf("replay guard: %v", err) }
	if _, err := service.ConfirmDeliveryDraft(blocked, deliveryActionPrincipal(), draft.ID); !errors.Is(err, apperrors.ErrAccessDenied) || apps.writes != 0 { t.Fatalf("confirm guard: %v", err) }
}
