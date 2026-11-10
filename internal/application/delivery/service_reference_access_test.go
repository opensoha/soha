package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestReferenceReadDoesNotRequireConfigurationAuthority(t *testing.T) {
	f := newReferenceFixture(t)
	f.catalog.updateErr = apperrors.ErrAccessDenied
	apps := referenceApplicationsFixture(t, &f)
	apps.importScopeErr = apperrors.ErrAccessDenied
	f.service.applications = apps
	if _, _, err := f.service.previewReferenceWithAccess(context.Background(), deliveryActionPrincipal(), "test-a", f.input, false); err != nil {
		t.Fatalf("authorized read failed: %v", err)
	}
	if f.catalog.updatePermission != "" {
		t.Fatal("read requested update permission")
	}
	if _, err := f.preview(context.Background()); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("configuration preview bypassed update permission: %v", err)
	}
}

func TestReferenceSourceObservationUsesSourceScopeAndCurrentReadPermissions(t *testing.T) {
	f := newReferenceFixture(t)
	f.catalog.denied["test-a"] = apperrors.ErrAccessDenied
	f.catalog.updateErr = apperrors.ErrAccessDenied
	apps := referenceApplicationsFixture(t, &f)
	apps.importScopeErr = apperrors.ErrAccessDenied
	f.service.applications = apps
	preview, err := f.service.observeSourceReferenceGroup(context.Background(), deliveryActionPrincipal(), "test-b", "svc")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Source.ApplicationEnvironmentID != "test-b" || len(preview.Targets) != 1 || preview.Targets[0].Resources[0].Resource.UID != "uid-api" {
		t.Fatalf("incorrect physical source: %+v", preview)
	}
	if len(preview.SourceRevision) != 64 || f.catalog.updatePermission != "" {
		t.Fatal("missing digest or requested write authority")
	}
	f.catalog.revokeAfterObservation = true
	if _, err := f.service.observeSourceReferenceGroup(context.Background(), deliveryActionPrincipal(), "test-b", "svc"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("source revocation not checked: %v", err)
	}
}

func TestReferenceSourceObservationRejectsConcurrentConfigurationChange(t *testing.T) {
	f := newReferenceFixture(t)
	f.targets.afterRead = func() {
		b := f.catalog.bindings["test-b"]
		b.Targets[0].Namespace = "changed"
		f.catalog.bindings["test-b"] = b
	}
	if _, err := f.service.observeSourceReferenceGroup(context.Background(), deliveryActionPrincipal(), "test-b", "svc"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("source change accepted: %v", err)
	}
}
