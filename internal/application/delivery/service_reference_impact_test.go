package delivery

import (
	"context"
	"encoding/json"
	"errors"
	dm "github.com/opensoha/soha/internal/domain/manifest"
	"strings"
	"testing"
	"time"

	dd "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type impactTestRepository struct {
	planRepository
	records     []dd.ServiceReferenceRecord
	snapshot    *dd.ServiceReferencePlanImpact
	beforeLock  func()
	locked      bool
	createError error
	tasks       map[string]dd.ExecutionTask
}

func (r *impactTestRepository) ListSourceServiceReferences(_ context.Context, source, service string) ([]dd.ServiceReferenceRecord, error) {
	result := []dd.ServiceReferenceRecord{}
	for _, item := range r.records {
		if item.SourceBindingID == source && (service == "" || item.ServiceID == service) {
			result = append(result, item)
		}
	}
	return result, nil
}
func (r *impactTestRepository) WithServiceReferenceScope(ctx context.Context, _ string, fn func(context.Context) error) error {
	if r.beforeLock != nil {
		f := r.beforeLock
		r.beforeLock = nil
		f()
	}
	r.locked = true
	defer func() { r.locked = false }()
	return fn(ctx)
}
func (r *impactTestRepository) CreateDeliveryPlanWithServiceReferenceImpact(ctx context.Context, input dd.DeliveryPlanInput, actor string, snapshot dd.ServiceReferencePlanImpact) (dd.DeliveryPlan, error) {
	if r.createError != nil {
		return dd.DeliveryPlan{}, r.createError
	}
	plan, err := r.CreateDeliveryPlan(ctx, input, actor)
	if err == nil {
		snapshot.PlanID = plan.ID
		r.snapshot = &snapshot
	}
	return plan, err
}
func (r *impactTestRepository) GetDeliveryPlanServiceReferenceSnapshot(context.Context, string) (dd.ServiceReferencePlanImpact, error) {
	if r.snapshot == nil {
		return dd.ServiceReferencePlanImpact{}, apperrors.ErrNotFound
	}
	return *r.snapshot, nil
}

func impactFixture(t *testing.T, withConsumer bool) (referenceFixture, *impactTestRepository, dd.DeliveryPlanInput) {
	t.Helper()
	f := newReferenceFixture(t)
	repo := &impactTestRepository{}
	if withConsumer {
		repo.records = []dd.ServiceReferenceRecord{{Reference: dd.ServiceReference{ID: "ref-visible", Revision: 1}, ApplicationID: "app", ConsumerBindingID: "test-a", ServiceID: "svc", SourceBindingID: "test-b", SourceTargetIDs: []string{"target-b"}, ControlRevision: "control", ConsumerConfigurationRevision: "config"}}
	}
	f.service.repository = repo
	if withConsumer {
		repo.records[0].ConsumerEnvironment = f.catalog.environments[0]
		preview, err := f.service.observeSourceReferenceGroup(t.Context(), deliveryActionPrincipal(), "test-b", "svc")
		if err != nil {
			t.Fatal(err)
		}
		repo.records[0].Reference.SourceIdentityRevision, err = dd.ServiceReferenceIdentityRevision(preview)
		if err != nil {
			t.Fatal(err)
		}
		repo.records[0].ControlRevision, err = dd.ServiceReferenceControlRevision(f.catalog.bindings["test-b"], "svc")
		if err != nil {
			t.Fatal(err)
		}
	}
	f.targets.afterRead = func() {
		if repo.locked {
			t.Error("runtime observation while holding DB app lock")
		}
	}
	return f, repo, dd.DeliveryPlanInput{ID: "source-plan", ApplicationID: "app", ApplicationEnvironmentID: "test-b", TargetID: "target-b", Action: dd.ApplicationDeliveryActionDeploy}
}

func TestServiceReferencePlanImpactEmptyVersusMissing(t *testing.T) {
	f, repo, input := impactFixture(t, false)
	plan, err := f.service.createPlanWithServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), input)
	if err != nil {
		t.Fatal(err)
	}
	if repo.snapshot == nil || repo.snapshot.Consumers == nil || len(repo.snapshot.Consumers) != 0 {
		t.Fatal("explicit empty evidence lost")
	}
	if err := f.service.validateServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), plan); err != nil {
		t.Fatal(err)
	}
	repo.snapshot = nil
	if err := f.service.validateServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), plan); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("missing evidence accepted: %v", err)
	}
	standalone := dd.DeliveryPlan{ID: "legacy-standalone"}
	if err := f.service.validateServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), standalone); err != nil {
		t.Fatalf("standalone compatibility broken: %v", err)
	}
}

func TestServiceReferencePlanImpactInvalidatesBeforeEffects(t *testing.T) {
	for _, scenario := range []string{"unchanged", "hidden-add", "hidden-remove", "rebind", "source-target", "consumer-config", "source-config", "policy", "service-version", "uid", "runtime-spec-version", "status-only"} {
		t.Run(scenario, func(t *testing.T) {
			f, repo, input := impactFixture(t, true)
			plan, err := f.service.createPlanWithServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), input)
			if err != nil {
				t.Fatal(err)
			}
			f.catalog.denied["test-a"] = apperrors.ErrAccessDenied
			switch scenario {
			case "hidden-add":
				r := repo.records[0]
				r.Reference.ID = "hidden-new"
				repo.records = append(repo.records, r)
			case "hidden-remove":
				repo.records = nil
			case "rebind":
				repo.records[0].SourceTargetIDs = []string{"replacement"}
			case "source-target":
				b := f.catalog.bindings["test-b"]
				b.Targets[0].ConfigRef = "changed"
				f.catalog.bindings[b.ID] = b
			case "consumer-config":
				repo.records[0].ConsumerConfigurationRevision = "changed"
			case "source-config":
				b := f.catalog.bindings["test-b"]
				b.BuildPolicy.SourceID = "changed"
				f.catalog.bindings[b.ID] = b
			case "policy":
				f.catalog.environments[1].RequiresApproval = true
			case "service-version":
				apps := referenceApplicationsFixture(t, &f)
				apps.services[0].Version++
				f.service.applications = apps
			case "uid", "runtime-spec-version", "status-only":
				key := "cluster-b/backend/Deployment/api/"
				observed := f.targets.sources[key]
				if scenario == "uid" {
					observed.Resource.UID = "new-uid"
				}
				if scenario == "runtime-spec-version" {
					observed.SpecDigest = strings.Repeat("b", 64)
				}
				if scenario == "status-only" {
					observed.Resource.ResourceVersion = "100"
					observed.ObservedAt = time.Now().String()
				}
				f.targets.sources[key] = observed
			}
			effects := 0
			err = f.service.withValidatedServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), plan, func(context.Context) error { effects++; return nil })
			stable := scenario == "unchanged" || scenario == "status-only" || scenario == "runtime-spec-version"
			if stable && (err != nil || effects != 1) {
				t.Fatalf("stable source rejected: %v effects=%d", err, effects)
			}
			if !stable && (err == nil || effects != 0) {
				t.Fatalf("stale source reached effects: %v effects=%d", err, effects)
			}
		})
	}
}

func TestServiceReferencePlanImpactRechecksInsideScope(t *testing.T) {
	for _, phase := range []string{"create", "confirm"} {
		t.Run(phase, func(t *testing.T) {
			f, repo, input := impactFixture(t, false)
			var plan dd.DeliveryPlan
			if phase == "confirm" {
				var err error
				plan, err = f.service.createPlanWithServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), input)
				if err != nil {
					t.Fatal(err)
				}
			}
			repo.beforeLock = func() {
				repo.records = []dd.ServiceReferenceRecord{{Reference: dd.ServiceReference{ID: "concurrent", Revision: 1}, ApplicationID: "app", SourceBindingID: "test-b", ConsumerBindingID: "test-a", ServiceID: "svc"}}
			}
			effects := 0
			var err error
			if phase == "create" {
				_, err = f.service.createPlanWithServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), input)
			} else {
				err = f.service.withValidatedServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), plan, func(context.Context) error { effects++; return nil })
			}
			if !errors.Is(err, apperrors.ErrConflict) || effects != 0 {
				t.Fatalf("scope race accepted: %v", err)
			}
			if phase == "create" && repo.createCount != 0 {
				t.Fatal("partial Plan created")
			}
		})
	}
}

func TestServiceReferenceImpactProjectionFiltersBeforePagination(t *testing.T) {
	f, repo, _ := impactFixture(t, true)
	hidden := repo.records[0]
	hidden.Reference.ID = "aaa-hidden"
	hidden.ConsumerBindingID = "hidden-secret-binding"
	second := repo.records[0]
	second.Reference.ID = "zzz-visible"
	repo.records = append(repo.records, hidden, second)
	f.catalog.denied[hidden.ConsumerBindingID] = apperrors.ErrAccessDenied
	page, err := f.service.ListEnvironmentServiceReferenceConsumers(t.Context(), deliveryActionPrincipal(), "test-b", "svc", "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].ReferenceID != "ref-visible" || page.NextCursor != "ref-visible" || !page.HasRestrictedConsumers {
		t.Fatalf("bad visible page: %+v %v", page, err)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "hidden-secret") || strings.Contains(string(raw), "aaa-hidden") {
		t.Fatal("hidden identity leaked")
	}
	next, err := f.service.ListEnvironmentServiceReferenceConsumers(t.Context(), deliveryActionPrincipal(), "test-b", "svc", page.NextCursor, 100)
	if err != nil || len(next.Items) != 1 || next.Items[0].ReferenceID != "zzz-visible" {
		t.Fatalf("bad second page: %+v %v", next, err)
	}
	if _, err := f.service.ListEnvironmentServiceReferenceConsumers(t.Context(), deliveryActionPrincipal(), "test-b", "svc", "aaa-hidden", 1); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("hidden cursor accepted")
	}
}

func (r *impactTestRepository) SourceServiceReferenceConfigurationRevision(context.Context, string) (string, error) {
	return "source-config", nil
}

func TestServiceReferencePlanImpactRejectsPartialAssociation(t *testing.T) {
	for _, scenario := range []string{"application-only", "binding-only"} {
		t.Run(scenario, func(t *testing.T) {
			f, repo, input := impactFixture(t, false)
			if scenario == "application-only" {
				input.ApplicationEnvironmentID = ""
			} else {
				input.ApplicationID = ""
			}
			if _, err := f.service.createPlanWithServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), input); !errors.Is(err, apperrors.ErrConflict) || repo.createCount != 0 {
				t.Fatalf("partial association created Plan: %v", err)
			}
			plan := dd.DeliveryPlan{ID: input.ID, ApplicationID: input.ApplicationID, ApplicationEnvironmentID: input.ApplicationEnvironmentID}
			effects := 0
			err := f.service.withValidatedServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), plan, func(context.Context) error { effects++; return nil })
			if !errors.Is(err, apperrors.ErrConflict) || effects != 0 {
				t.Fatalf("partial association bypassed preflight: %v effects=%d", err, effects)
			}
		})
	}
}

func TestServiceReferencePlanPersistsBeforeOpeningPreparedPreflight(t *testing.T) {
	for _, scenario := range []string{"saved", "save-failed"} {
		t.Run(scenario, func(t *testing.T) {
			f, repo, input := impactFixture(t, false)
			if scenario == "save-failed" {
				repo.createError = errors.New("Plan save failed")
			}
			ctx, _ := dm.WithPreparedOperations(t.Context())
			runnable := 0
			dm.StagePreparedOperation(ctx, func(context.Context) error {
				if repo.snapshot == nil || repo.plan.ID != input.ID {
					t.Fatal("preflight opened without durable Plan/proof")
				}
				runnable++
				return nil
			})
			// This barrier represents the worker polling while bounded live source preparation runs.
			if runnable != 0 {
				t.Fatal("preflight visible during preparation")
			}
			_, err := f.service.createPlanWithServiceReferenceImpact(ctx, deliveryActionPrincipal(), input)
			if scenario == "save-failed" {
				if err == nil || runnable != 0 || repo.snapshot != nil {
					t.Fatalf("failed Plan left runnable orphan: %v", err)
				}
				return
			}
			if err != nil || runnable != 1 {
				t.Fatalf("saved Plan did not release preflight: %v", err)
			}
		})
	}
}

func TestServiceReferenceNewPlanRejectsInvalidHiddenConsumerClassification(t *testing.T) {
	for _, scenario := range []string{"tier", "stage", "production", "disabled", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			f, repo, input := impactFixture(t, true)
			f.catalog.denied["test-a"] = apperrors.ErrAccessDenied
			switch scenario {
			case "tier":
				repo.records[0].ConsumerEnvironment.Tier = "dev"
			case "stage":
				repo.records[0].ConsumerEnvironment.StageLevel++
			case "production":
				repo.records[0].ConsumerEnvironment.IsProduction = true
			case "disabled":
				repo.records[0].ConsumerEnvironment.Enabled = false
			case "missing":
				repo.records[0].ConsumerEnvironment.ID = ""
			}
			if _, err := f.service.createPlanWithServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), input); !errors.Is(err, apperrors.ErrConflict) || repo.createCount != 0 {
				t.Fatalf("invalid hidden classification entered new Plan: %v", err)
			}
		})
	}
}

func (r *impactTestRepository) GetExecutionTask(_ context.Context, id string) (dd.ExecutionTask, error) {
	task, ok := r.tasks[id]
	if !ok {
		return task, apperrors.ErrNotFound
	}
	return task, nil
}
func (r *impactTestRepository) CreateExecutionTask(_ context.Context, task dd.ExecutionTask) (dd.ExecutionTask, error) {
	if r.tasks == nil {
		r.tasks = map[string]dd.ExecutionTask{}
	}
	r.tasks[task.ID] = task
	return task, nil
}
