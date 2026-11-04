package delivery

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	du "github.com/opensoha/soha/internal/domain/templateupdate"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type adoptionRecoveryRepository struct {
	*impactTestRepository
	beforeRestore func()
	persist       func(context.Context, dd.DeliveryPlan, dd.DeliveryPlan) error
	updates       int
	read          func(context.Context, dd.DeliveryPlan) (dd.DeliveryPlan, error)
}

func (r *adoptionRecoveryRepository) UpdateDeliveryPlan(ctx context.Context, next dd.DeliveryPlan) (dd.DeliveryPlan, error) {
	if next.Status == dd.DeliveryPlanStatusDraft && r.beforeRestore != nil {
		change := r.beforeRestore
		r.beforeRestore = nil
		change()
	}
	previous := r.plan
	if next.ExpectedUpdatedAt == nil || !next.ExpectedUpdatedAt.Equal(previous.UpdatedAt) {
		return dd.DeliveryPlan{}, apperrors.ErrConflict
	}
	next.UpdatedAt = previous.UpdatedAt.Add(time.Microsecond)
	stamp := next.UpdatedAt
	next.ExpectedUpdatedAt = &stamp
	if r.persist != nil {
		if err := r.persist(ctx, previous, next); err != nil {
			return dd.DeliveryPlan{}, err
		}
	}
	r.plan = next
	r.updates++
	return next, nil
}

type adoptionRecoveryOwner struct {
	adoptionOwner
	apply       func(context.Context, string, func(context.Context) error) error
	readErr     error
	readContext func(context.Context)
}

func (o *adoptionRecoveryOwner) Apply(ctx context.Context, _ di.Principal, _ string, planID string, receipt func(context.Context) error) error {
	o.applied++
	return o.apply(ctx, planID, receipt)
}
func (o *adoptionRecoveryOwner) ReadCandidate(ctx context.Context, _ di.Principal, _ string) (du.FrozenCandidate, error) {
	if o.readContext != nil {
		o.readContext(ctx)
	}
	return o.frozen, o.readErr
}

func adoptionRecoveryFixture(t *testing.T) (referenceFixture, *adoptionRecoveryRepository, *adoptionRecoveryOwner, dd.DeliveryPlan) {
	t.Helper()
	f, base, _ := impactFixture(t, false)
	repo := &adoptionRecoveryRepository{impactTestRepository: base}
	f.service.repository = repo
	owner := &adoptionRecoveryOwner{adoptionOwner: adoptionOwner{frozen: du.FrozenCandidate{Candidate: sohaapi.TemplateUpdateCandidate{ID: "recovery-candidate", Digest: "recovery-digest", Eligible: true, RequiresApproval: true, Target: sohaapi.TemplateUpdateTarget{Scope: "environment", Kind: "workflow", ApplicationID: "app", ApplicationEnvironmentID: "test-b"}}}}}
	f.service.templateUpdates = owner
	plan, err := f.service.createTemplateAdoptionPlan(t.Context(), deliveryActionPrincipal(), dd.DeliveryPlanInput{ID: "recovery-plan", TemplateCandidateID: owner.frozen.Candidate.ID, Source: "delivery_batch", Action: "deploy", ApplicationID: "app", ApplicationEnvironmentID: "test-b"})
	if err != nil {
		t.Fatal(err)
	}
	plan.Impact = deliveryPlanApprovalImpact(plan, deliveryActionPrincipal(), "approved", "reviewed configuration")
	plan.UpdatedAt = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	stamp := plan.UpdatedAt
	plan.ExpectedUpdatedAt = &stamp
	repo.plan = plan
	return f, repo, owner, plan
}

func TestTemplateAdoptionKnownRollbackRestoresOnlyItsClaim(t *testing.T) {
	for _, scenario := range []string{"candidate-conflict", "permission-denied", "read-revoked", "candidate-adopted-elsewhere", "candidate-missing", "candidate-read-failed", "callback-rolled-back"} {
		t.Run(scenario, func(t *testing.T) {
			f, repo, owner, plan := adoptionRecoveryFixture(t)
			before := f.catalog.bindings["test-b"]
			cause := apperrors.ErrConflict
			if scenario == "permission-denied" || scenario == "read-revoked" {
				cause = apperrors.ErrAccessDenied
			}
			owner.apply = func(ctx context.Context, _ string, receipt func(context.Context) error) error {
				claimed := repo.plan
				if claimed.Status != dd.DeliveryPlanStatusConfirming {
					t.Fatal("adoption lacked its source busy claim")
				}
				switch scenario {
				case "read-revoked":
					owner.readErr = apperrors.ErrAccessDenied
				case "candidate-adopted-elsewhere":
					owner.frozen.AppliedPlanID = "other-plan"
				case "candidate-missing":
					owner.readErr = apperrors.ErrNotFound
				case "candidate-read-failed":
					owner.readErr = errors.New("candidate database unavailable")
				case "callback-rolled-back":
					changed := before
					changed.WorkflowTemplateVersion++
					f.catalog.bindings[changed.ID] = changed
					if err := receipt(ctx); err != nil {
						t.Fatal(err)
					}
					// The owning atomic transaction rolls back both its configuration and receipt.
					f.catalog.bindings[before.ID] = before
					repo.plan = claimed
				}
				return cause
			}
			result, err := f.service.confirmTemplateAdoptionPlan(t.Context(), deliveryActionPrincipal(), plan)
			if !errors.Is(err, cause) || result.Plan.Status != dd.DeliveryPlanStatusDraft || repo.plan.Status != dd.DeliveryPlanStatusDraft || !deliveryPlanApprovalGranted(repo.plan) {
				t.Fatalf("known failed claim not restored with approval: %v %+v", err, repo.plan)
			}
			if !reflect.DeepEqual(f.catalog.bindings["test-b"], before) || owner.applied != 1 || repo.plan.ConfirmedAt != nil || len(repo.tasks) != 0 {
				t.Fatal("failed adoption changed configuration or queued runtime work")
			}
		})
	}
}

func TestTemplateAdoptionRecoveryKeepsConcurrentOrAmbiguousState(t *testing.T) {
	for _, scenario := range []string{"new-claim", "confirmed-other-receipt", "cas-race", "unknown-database", "timeout", "business-plus-unknown", "unknown-outcome-read-failed", "plan-outcome-read-failed", "contradictory-own-receipt"} {
		t.Run(scenario, func(t *testing.T) {
			f, repo, owner, plan := adoptionRecoveryFixture(t)
			owner.apply = func(context.Context, string, func(context.Context) error) error {
				switch scenario {
				case "new-claim":
					next := repo.plan
					next.UpdatedAt = next.UpdatedAt.Add(time.Second)
					repo.plan = next
				case "confirmed-other-receipt":
					next := repo.plan
					next.Status = dd.DeliveryPlanStatusConfirmed
					next.UpdatedAt = next.UpdatedAt.Add(time.Second)
					repo.plan = next
					owner.frozen.AppliedPlanID = "other-plan"
				case "cas-race":
					repo.beforeRestore = func() { next := repo.plan; next.UpdatedAt = next.UpdatedAt.Add(time.Second); repo.plan = next }
				case "unknown-database":
					return errors.New("commit acknowledgement lost")
				case "timeout":
					return context.DeadlineExceeded
				case "business-plus-unknown":
					return errors.Join(apperrors.ErrConflict, errors.New("connection outcome unknown"))
				case "unknown-outcome-read-failed":
					owner.readErr = errors.New("candidate database unavailable")
					return errors.New("transaction outcome unknown")
				case "plan-outcome-read-failed":
					repo.read = func(context.Context, dd.DeliveryPlan) (dd.DeliveryPlan, error) {
						return dd.DeliveryPlan{}, errors.New("Plan database unavailable")
					}
				case "contradictory-own-receipt":
					owner.frozen.AppliedPlanID = repo.plan.ID
				}
				return apperrors.ErrConflict
			}
			_, err := f.service.confirmTemplateAdoptionPlan(t.Context(), deliveryActionPrincipal(), plan)
			expected := dd.DeliveryPlanStatusConfirming
			if scenario == "confirmed-other-receipt" {
				expected = dd.DeliveryPlanStatusConfirmed
			}
			if err == nil || repo.plan.Status != expected || repo.updates != 1 {
				t.Fatalf("recovery overwrote concurrent/uncertain outcome: %v updates=%d status=%s", err, repo.updates, repo.plan.Status)
			}
		})
	}
}

func TestTemplateAdoptionLostAcknowledgementReturnsOriginalReceipt(t *testing.T) {
	f, repo, owner, plan := adoptionRecoveryFixture(t)
	owner.apply = func(ctx context.Context, planID string, receipt func(context.Context) error) error {
		changed := f.catalog.bindings["test-b"]
		changed.WorkflowTemplateVersion++
		f.catalog.bindings[changed.ID] = changed
		if err := receipt(ctx); err != nil {
			return err
		}
		owner.frozen.AppliedPlanID = planID
		return errors.New("commit acknowledgement lost")
	}
	result, err := f.service.confirmTemplateAdoptionPlan(t.Context(), deliveryActionPrincipal(), plan)
	if err != nil || result.Plan.Status != dd.DeliveryPlanStatusConfirmed || result.Plan.ConfirmedAt == nil || owner.applied != 1 || repo.updates != 2 {
		t.Fatalf("persisted receipt was not recovered: %v %+v", err, result.Plan)
	}
	confirmed := repo.plan
	again, err := f.service.confirmTemplateAdoptionPlan(t.Context(), deliveryActionPrincipal(), confirmed)
	if err != nil || !reflect.DeepEqual(again.Plan, confirmed) || owner.applied != 1 {
		t.Fatal("receipt recovery replayed or rewrote adoption", err)
	}
}

func TestTemplateAdoptionRecoveryIgnoresRequestCancellationWithBoundedDeadline(t *testing.T) {
	f, repo, owner, plan := adoptionRecoveryFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	owner.apply = func(context.Context, string, func(context.Context) error) error {
		cancel()
		return apperrors.ErrConflict
	}
	owner.readContext = func(ctx context.Context) {
		deadline, ok := ctx.Deadline()
		if ctx.Err() != nil || !ok || time.Until(deadline) > 5*time.Second {
			t.Error("recovery did not use its bounded independent context")
		}
	}
	if _, err := f.service.confirmTemplateAdoptionPlan(ctx, deliveryActionPrincipal(), plan); !errors.Is(err, apperrors.ErrConflict) || repo.plan.Status != dd.DeliveryPlanStatusDraft {
		t.Fatalf("canceled request stranded known rollback: %v", err)
	}
}

func (r *adoptionRecoveryRepository) GetDeliveryPlan(ctx context.Context, _ string) (dd.DeliveryPlan, error) {
	if r.read != nil {
		plan, err := r.read(ctx, r.plan)
		if err != nil {
			return plan, err
		}
		r.plan = plan
	}
	return r.plan, nil
}
