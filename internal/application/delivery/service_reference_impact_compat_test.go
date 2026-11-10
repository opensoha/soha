package delivery

import (
	"context"
	"testing"

	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (stubRepository) ListSourceServiceReferences(context.Context, string, string) ([]dd.ServiceReferenceRecord, error) {
	return []dd.ServiceReferenceRecord{}, nil
}

func (stubRepository) WithServiceReferenceScope(ctx context.Context, _ string, apply func(context.Context) error) error {
	return apply(ctx)
}

func (stubRepository) ServiceReferenceManifestConfigurationRevision(context.Context, string, []string) (string, error) {
	return "fixture-source-configuration", nil
}

func (r *planRepository) saveReferenceSnapshot(plan dd.DeliveryPlan, snapshot dd.ServiceReferencePlanImpact) {
	if r.referenceSnapshots == nil {
		r.referenceSnapshots = map[string]dd.ServiceReferencePlanImpact{}
	}
	snapshot.PlanID = plan.ID
	r.referenceSnapshots[plan.ID] = snapshot
}

func (r *planRepository) CreateDeliveryPlanWithServiceReferenceImpact(ctx context.Context, input dd.DeliveryPlanInput, actor string, snapshot dd.ServiceReferencePlanImpact) (dd.DeliveryPlan, error) {
	plan, err := r.CreateDeliveryPlan(ctx, input, actor)
	if err == nil {
		r.saveReferenceSnapshot(plan, snapshot)
	}
	return plan, err
}

func (r *batchRuntimeRepository) CreateDeliveryPlanWithServiceReferenceImpact(ctx context.Context, input dd.DeliveryPlanInput, actor string, snapshot dd.ServiceReferencePlanImpact) (dd.DeliveryPlan, error) {
	plan, err := r.CreateDeliveryPlan(ctx, input, actor)
	if err == nil {
		r.saveReferenceSnapshot(plan, snapshot)
	}
	return plan, err
}

func (r *planRepository) GetDeliveryPlanServiceReferenceSnapshot(_ context.Context, id string) (dd.ServiceReferencePlanImpact, error) {
	snapshot, ok := r.referenceSnapshots[id]
	if !ok {
		return dd.ServiceReferencePlanImpact{}, apperrors.ErrNotFound
	}
	return snapshot, nil
}

// Manually seeded valid Plans must carry the same evidence as newly created Plans.
func seedPlanReferenceSnapshot(t *testing.T, s *Service, repo *planRepository) {
	t.Helper()
	snapshot, err := s.captureServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), repo.plan)
	if err != nil {
		t.Fatalf("capture fixture service-reference impact: %v", err)
	}
	repo.saveReferenceSnapshot(repo.plan, snapshot)
}

type fixtureExecutionPrincipalReader struct{}

func (fixtureExecutionPrincipalReader) CurrentExecutionPrincipal(_ context.Context, userID, tokenID string) (di.Principal, error) {
	p := deliveryActionPrincipal()
	if p.UserID != userID || p.AccessTokenID != tokenID {
		return di.Principal{}, apperrors.ErrUnauthorized
	}
	return p, nil
}
