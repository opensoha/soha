package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	du "github.com/opensoha/soha/internal/domain/templateupdate"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type receiptAdoptionOwner struct {
	adoptionOwner
	changed func()
}

func (o *receiptAdoptionOwner) Apply(ctx context.Context, _ di.Principal, _ string, planID string, receipt func(context.Context) error) error {
	o.applied++
	if o.changed != nil {
		o.changed()
	}
	if err := receipt(ctx); err != nil {
		return err
	}
	o.frozen.AppliedPlanID = planID
	return nil
}

func TestApplicationScopedTemplatePlanRetainsCandidateCASWithoutRuntimeAssociation(t *testing.T) {
	for _, kind := range []sohaapi.TemplateUpdateKind{"build", "workflow"} {
		t.Run(string(kind), func(t *testing.T) {
			f, repo, _ := impactFixture(t, false)
			owner := &receiptAdoptionOwner{adoptionOwner: adoptionOwner{frozen: du.FrozenCandidate{Candidate: sohaapi.TemplateUpdateCandidate{ID: "candidate", Digest: "candidate-digest", Eligible: true, Target: sohaapi.TemplateUpdateTarget{Scope: "application", Kind: kind, ApplicationID: "app"}}}}}
			f.service.templateUpdates = owner
			plan, err := f.service.createTemplateAdoptionPlan(t.Context(), deliveryActionPrincipal(), dd.DeliveryPlanInput{ID: "configuration-plan", TemplateCandidateID: "candidate", Source: "delivery_batch", Action: "deploy", ApplicationID: "app"})
			if err != nil {
				t.Fatal(err)
			}
			if repo.snapshot != nil || plan.ApplicationEnvironmentID != "" {
				t.Fatal("application configuration became a runtime binding")
			}
			result, err := f.service.confirmTemplateAdoptionPlan(t.Context(), deliveryActionPrincipal(), plan)
			if err != nil || result.Plan.Status != "confirmed" || owner.applied != 1 {
				t.Fatalf("configuration Plan failed: %v", err)
			}
			if _, err := f.service.confirmTemplateAdoptionPlan(t.Context(), deliveryActionPrincipal(), result.Plan); err != nil || owner.applied != 1 {
				t.Fatalf("repeated acknowledgement replayed configuration: %v", err)
			}
			owner.frozen.Candidate.Target.Scope = "environment"
			if _, err := f.service.createPlanWithServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), dd.DeliveryPlanInput{ID: "forged", TemplateCandidateID: "candidate", Source: "delivery_batch", Action: "deploy", ApplicationID: "app", Impact: map[string]any{"configurationOnly": true}}); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("client Impact bypassed runtime association: %v", err)
			}
		})
	}
}

func TestConfirmedTemplatePlanRecoversItsOwnConfigurationReceipt(t *testing.T) {
	f, _, _ := impactFixture(t, false)
	owner := &receiptAdoptionOwner{adoptionOwner: adoptionOwner{frozen: du.FrozenCandidate{Candidate: sohaapi.TemplateUpdateCandidate{ID: "candidate", Digest: "candidate-digest", Eligible: true, Target: sohaapi.TemplateUpdateTarget{Scope: "environment", Kind: "workflow", ApplicationID: "app", ApplicationEnvironmentID: "test-b"}}}}}
	owner.changed = func() {
		binding := f.catalog.bindings["test-b"]
		binding.WorkflowTemplateVersion++
		f.catalog.bindings[binding.ID] = binding
	}
	f.service.templateUpdates = owner
	plan, err := f.service.createTemplateAdoptionPlan(t.Context(), deliveryActionPrincipal(), dd.DeliveryPlanInput{ID: "config-with-source", TemplateCandidateID: "candidate", Source: "delivery_batch", Action: "deploy", ApplicationID: "app", ApplicationEnvironmentID: "test-b"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.service.confirmTemplateAdoptionPlan(t.Context(), deliveryActionPrincipal(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.validateServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), result.Plan); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("test did not change its own configuration: %v", err)
	}
	if _, err := f.service.confirmTemplateAdoptionPlan(t.Context(), deliveryActionPrincipal(), result.Plan); err != nil || owner.applied != 1 {
		t.Fatalf("own receipt recovery failed: %v", err)
	}
	owner.frozen.AppliedPlanID = "other-plan"
	if _, err := f.service.confirmTemplateAdoptionPlan(t.Context(), deliveryActionPrincipal(), result.Plan); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("another Plan receipt accepted: %v", err)
	}
}
