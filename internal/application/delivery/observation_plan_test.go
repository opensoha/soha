package delivery

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	da "github.com/opensoha/soha/internal/domain/application"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type missingObservationPlanRepository struct{ *impactTestRepository }

func (r missingObservationPlanRepository) GetDeliveryPlan(context.Context, string) (dd.DeliveryPlan, error) {
	return dd.DeliveryPlan{}, apperrors.ErrNotFound
}

func TestObservationPlanReaderPreservesApprovalAndCurrentReadScope(t *testing.T) {
	for _, scenario := range []string{"approved", "no-approval-required", "missing-approval", "rejected", "draft", "confirming", "environment-denied", "environment-mismatch", "application-denied", "permission-denied", "missing-plan"} {
		t.Run(scenario, func(t *testing.T) {
			f, repo, input := impactFixture(t, false)
			f.service.permissions = deliveryActionPermissions(appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryApplicationEnvView)
			_, err := f.service.createPlanWithServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), input)
			if err != nil {
				t.Fatal(err)
			}
			repo.plan.Status = dd.DeliveryPlanStatusConfirmed
			repo.plan.RequiresApproval = true
			repo.plan.Impact = map[string]any{"approval": []any{map[string]any{"status": "approved"}}}
			want := error(nil)
			switch scenario {
			case "no-approval-required":
				repo.plan.RequiresApproval = false
				repo.plan.Impact = nil
			case "missing-approval":
				repo.plan.Impact = nil
				want = apperrors.ErrConflict
			case "rejected":
				repo.plan.Impact["approval"] = []any{map[string]any{"status": "approved"}, map[string]any{"status": "rejected"}}
				want = apperrors.ErrConflict
			case "draft":
				repo.plan.Status = dd.DeliveryPlanStatusDraft
				want = apperrors.ErrConflict
			case "confirming":
				repo.plan.Status = dd.DeliveryPlanStatusConfirming
				want = apperrors.ErrConflict
			case "environment-denied":
				f.catalog.denied["test-b"] = apperrors.ErrAccessDenied
				want = apperrors.ErrAccessDenied
			case "environment-mismatch":
				binding := f.catalog.bindings["test-b"]
				binding.ApplicationID = "another-app"
				f.catalog.bindings["test-b"] = binding
				want = apperrors.ErrAccessDenied
			case "application-denied":
				repo.plan.ApplicationEnvironmentID = ""
				apps := referenceApplicationsFixture(t, &f)
				apps.getErrByID = map[string]error{"app": apperrors.ErrAccessDenied}
				f.service.applications = apps
				want = apperrors.ErrAccessDenied
			case "permission-denied":
				f.service.permissions = deliveryActionPermissions()
				want = apperrors.ErrAccessDenied
			case "missing-plan":
				f.service.repository = missingObservationPlanRepository{repo}
				want = apperrors.ErrNotFound
			}
			plan, err := f.service.GetConfirmedDeliveryObservationPlan(t.Context(), deliveryActionPrincipal(), input.ID)
			if !errors.Is(err, want) {
				t.Fatalf("got %v, want %v", err, want)
			}
			if want == nil && plan.ID != input.ID {
				t.Fatal("reader lost confirmed plan")
			}
		})
	}
}

func TestObservationPlanReaderAllowsStaleGlobalServiceImpactButNativeControlsDoNot(t *testing.T) {
	f, repo, input := impactFixture(t, false)
	f.service.permissions = deliveryActionPermissions(appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryApplicationEnvView)
	_, err := f.service.createPlanWithServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), input)
	if err != nil {
		t.Fatal(err)
	}
	repo.plan.Status = dd.DeliveryPlanStatusConfirmed
	repo.plan.RequiresApproval = true
	repo.plan.Impact = map[string]any{"approval": []any{map[string]any{"status": "approved"}}}
	if _, err := f.service.GetConfirmedDeliveryPlan(t.Context(), deliveryActionPrincipal(), input.ID); err != nil {
		t.Fatal(err)
	}
	apps := referenceApplicationsFixture(t, &f)
	apps.services = append(apps.services, da.Service{ID: "unrelated-new-service", ApplicationID: "app", Version: 1, Enabled: true})
	f.service.applications = apps
	if _, err := f.service.GetConfirmedDeliveryPlan(t.Context(), deliveryActionPrincipal(), input.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("native write control accepted stale impact: %v", err)
	}
	plan, err := f.service.GetConfirmedDeliveryObservationPlan(t.Context(), deliveryActionPrincipal(), input.ID)
	if err != nil || plan.ID != input.ID {
		t.Fatalf("stale publication impact blocked observation: %v", err)
	}
}
