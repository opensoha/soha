package contentdlp

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/contentdlp"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Execution is deferred with the rest of task53's tests; vet compiles these cases.
func TestWarningAuthorizationBindsExactTransferAndIsConsumedOnce(t *testing.T) {
	ctx := context.Background()
	project := domaindocker.Project{ID: "p", HostID: "h", UpdatedAt: time.Now().UTC()}
	policy := domain.DefaultPolicy(project.ID)
	policy.Published = &domain.Content{Enabled: true, Mode: "warn", Detectors: []string{"private_key"}}
	policy.PublishedVersion = 2
	store := &memoryStore{policy: policy}
	service, err := New(store, projectReader{project}, scopeAuthorizer{true}, appaccess.NewPermissionResolver(dlpRoleReader{}))
	if err != nil {
		t.Fatal(err)
	}
	p := identity.Principal{Roles: []string{"dlp-test"}, UserID: "actor", PermissionKeys: []string{appaccess.PermDockerServicesView}}
	file := domaindocker.ProjectVolumeFileContent{ProjectID: "p", ServiceName: "app", Target: "/data", Path: "/key.txt", Content: "-----BEGIN PRIVATE KEY-----"}
	file.SizeBytes = int64(len(file.Content))
	snapshot := Snapshot{Policy: policy, Project: project}
	_, err = service.Check(ctx, p, snapshot, file)
	var denied *Denial
	if !errors.As(err, &denied) || denied.Decision.RequiredAction != "acknowledge" {
		t.Fatalf("expected warning, got %v", err)
	}
	grant, err := service.Authorize(ctx, p, "p", denied.Decision.EventID, domain.AuthorizationInput{Action: "acknowledge"})
	if err != nil {
		t.Fatal(err)
	}
	changed := file
	changed.Path = "/other.txt"
	if _, err = service.Check(ctx, p, snapshot, changed, domain.TransferAccess{Token: grant.Token}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("changed path accepted")
	}
	changed = file
	changed.Content += " changed"
	changed.SizeBytes = int64(len(changed.Content))
	if _, err = service.Check(ctx, p, snapshot, changed, domain.TransferAccess{Token: grant.Token}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("changed content accepted")
	}
	other := p
	other.UserID = "other"
	if _, err = service.Check(ctx, other, snapshot, file, domain.TransferAccess{Token: grant.Token}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("another actor accepted")
	}
	decision, err := service.Check(ctx, p, snapshot, file, domain.TransferAccess{Token: grant.Token})
	if err != nil || decision == nil || !decision.Allowed || decision.AuthorizationKind != "acknowledge" {
		t.Fatalf("exact retry failed: %+v %v", decision, err)
	}
	if _, err = service.Check(ctx, p, snapshot, file, domain.TransferAccess{Token: grant.Token}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("consumed grant reused")
	}
}

func TestAppealReviewCannotApproveOwnEvent(t *testing.T) {
	project := domaindocker.Project{ID: "p", UpdatedAt: time.Now().UTC()}
	store := &memoryStore{records: map[string]domain.EventRecord{"e": {Event: domain.Event{ID: "e", ProjectID: "p", ActorID: "actor", Action: "transfer"}}}}
	service, err := New(store, projectReader{project}, scopeAuthorizer{true}, appaccess.NewPermissionResolver(dlpRoleReader{}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Review(context.Background(), identity.Principal{Roles: []string{"dlp-test"}, UserID: "actor", PermissionKeys: []string{PermissionReview}}, "p", "e", domain.ReviewInput{ExpectedRevision: 1, Resolution: "approved", ReasonCode: "confirmed_false_positive"})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("self review accepted", err)
	}
}
