package delivery

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestManifestApprovalChecksCurrentControlWithoutRequiringPublisherRole(t *testing.T) {
	for _, scenario := range []string{"approve", "stale", "reject-stale"} {
		t.Run(scenario, func(t *testing.T) {
			repo := &planRepository{plan: domaindelivery.DeliveryPlan{ID: "control-plan", ApplicationID: "app", ApplicationEnvironmentID: "binding", Action: domaindelivery.ApplicationDeliveryActionDeploy, Status: domaindelivery.DeliveryPlanStatusWaitingApproval, RequiresApproval: true, ManifestSnapshots: []domainmanifest.DeliverySnapshot{{TargetID: "target"}}}}
			runtime := &manifestDeliveryStub{}
			if scenario != "approve" {
				runtime.controlError = apperrors.ErrConflict
			}
			s := &Service{applications: stubApplicationReader{app: domainapp.App{ID: "app"}}, catalog: stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{{ID: "binding", ApplicationID: "app"}}}, repository: repo, manifestDelivery: runtime, permissions: deliveryActionPermissions(appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryApplicationEnvApprove)}
			action := "approve"
			if scenario == "reject-stale" {
				action = "reject"
			}
			plan, err := s.DecideDeliveryPlanApproval(t.Context(), deliveryActionPrincipal(), "control-plan", domaindelivery.DeliveryPlanApprovalInput{Action: action})
			if scenario == "stale" {
				if !errors.Is(err, apperrors.ErrConflict) || repo.plan.Status != domaindelivery.DeliveryPlanStatusWaitingApproval {
					t.Fatalf("stale control received approval: %v", err)
				}
				return
			}
			if err != nil || plan.Status != domaindelivery.DeliveryPlanStatusDraft || len(runtime.applied) != 0 || deliveryPlanApprovalGranted(plan) != (scenario == "approve") {
				t.Fatalf("approval required deployment authority or executed resources: %v", err)
			}
		})
	}
}

func manifestContextFixture(t *testing.T) referenceFixture {
	t.Helper()
	f := newReferenceFixture(t)
	binding := f.catalog.bindings["test-b"]
	binding.Targets[0].Enabled = true
	binding.Targets[0].ExecutorKind, binding.Targets[0].ConfigRef = "manifest_ssa", "manifest-binding"
	binding.Targets[0].Metadata = map[string]any{"serviceId": "svc", "secret": "synthetic-sensitive-metadata"}
	member := binding.Targets[0]
	member.ID, member.ConfigRef = "target-c", "member-binding"
	binding.Targets = append(binding.Targets, member)
	f.catalog.bindings[binding.ID] = binding
	return f
}

func TestManifestPublicationContextSeparatesPolicyFromObservation(t *testing.T) {
	for _, scenario := range []string{"stable", "observation-time", "display", "defaults", "unrelated-target", "group-reordered", "service-version", "group-member", "target-position", "target-control", "policy", "classification"} {
		t.Run(scenario, func(t *testing.T) {
			f := manifestContextFixture(t)
			before, err := f.service.CurrentManifestDeliveryContext(t.Context(), deliveryActionPrincipal(), "app", "test-b", "target-b")
			if err != nil {
				t.Fatal(err)
			}
			binding := f.catalog.bindings["test-b"]
			switch scenario {
			case "observation-time":
				binding.UpdatedAt = binding.UpdatedAt.Add(time.Hour)
				binding.Targets[0].UpdatedAt = binding.UpdatedAt
			case "display":
				f.catalog.environments[1].Name = "renamed"
				f.catalog.environments[1].SortOrder++
				f.catalog.environments[1].UpdatedAt = time.Now()
			case "defaults":
				binding.ClusterID, binding.Namespace = "new-default", "new-default"
			case "unrelated-target":
				binding.Targets = append(binding.Targets, domaincatalog.ReleaseTarget{ID: "unrelated", ServiceID: "other"})
			case "group-reordered":
				slices.Reverse(binding.Targets)
			case "service-version":
				apps := referenceApplicationsFixture(t, &f)
				apps.services[0].Version++
				f.service.applications = apps
			case "group-member":
				member := binding.Targets[0]
				member.ID, member.ConfigRef = "second", "second-binding"
				binding.Targets = append(binding.Targets, member)
			case "target-position":
				binding.Targets[0].Namespace = "moved"
			case "target-control":
				binding.Targets[0].Metadata["ownershipMode"] = "managed"
			case "policy":
				binding.ReleasePolicy.RequiresApproval = true
			case "classification":
				f.catalog.environments[1].IsProduction = true
			}
			f.catalog.bindings[binding.ID] = binding
			after, err := f.service.CurrentManifestDeliveryContext(t.Context(), deliveryActionPrincipal(), "app", "test-b", "target-b")
			if err != nil {
				t.Fatal(err)
			}
			stable := slices.Contains([]string{"stable", "observation-time", "display", "defaults", "unrelated-target", "group-reordered"}, scenario)
			if (before == after) != stable || strings.Contains(after.ScopeRevision, "synthetic") {
				t.Fatalf("publication policy stability mismatch: %s", scenario)
			}
		})
	}
}

func TestManifestPublicationContextRejectsInvalidAuthority(t *testing.T) {
	for _, scenario := range []string{"disabled", "observe-only", "unknown-mode", "native-import", "helm-authority", "executor", "unbound", "missing-environment", "disabled-environment", "disabled-service", "wrong-application", "permission", "ambiguous-binding"} {
		t.Run(scenario, func(t *testing.T) {
			f := manifestContextFixture(t)
			binding := f.catalog.bindings["test-b"]
			switch scenario {
			case "disabled":
				binding.Targets[0].Enabled = false
			case "observe-only":
				binding.Targets[0].Metadata["ownershipMode"] = "observe_only"
			case "unknown-mode":
				binding.Targets[0].Metadata["ownershipMode"] = "unconfirmed"
			case "native-import":
				binding.Targets[0].Metadata["importSource"] = "kubernetes"
				binding.Targets[0].Metadata["ownershipMode"] = "managed"
			case "helm-authority":
				binding.Targets[0].Metadata["importSource"] = "helm"
			case "executor":
				binding.Targets[0].ExecutorKind = "direct_image_update"
			case "unbound":
				binding.Targets = nil
			case "missing-environment":
				f.catalog.environments = nil
			case "disabled-environment":
				f.catalog.environments[1].Enabled = false
			case "disabled-service":
				apps := referenceApplicationsFixture(t, &f)
				apps.services[0].Enabled = false
				f.service.applications = apps
			case "wrong-application":
				binding.ApplicationID = "another"
			case "permission":
				f.catalog.denied[binding.ID] = apperrors.ErrAccessDenied
			case "ambiguous-binding":
				member := binding.Targets[0]
				member.ID = "duplicate"
				binding.Targets = append(binding.Targets, member)
			}
			f.catalog.bindings[binding.ID] = binding
			_, err := f.service.CurrentManifestDeliveryContext(t.Context(), deliveryActionPrincipal(), "app", "test-b", "target-b")
			want := apperrors.ErrConflict
			if scenario == "permission" {
				want = apperrors.ErrAccessDenied
			}
			if !errors.Is(err, want) {
				t.Fatalf("unsafe publication context accepted: %v", err)
			}
		})
	}
}

func TestManifestPublicationContextRejectsMalformedControlMetadata(t *testing.T) {
	for _, scenario := range []struct {
		name, key string
		value     any
	}{
		{name: "null-mode", key: "ownershipMode", value: nil},
		{name: "boolean-mode", key: "ownershipMode", value: false},
		{name: "null-source", key: "importSource", value: nil},
		{name: "boolean-source", key: "importSource", value: false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := manifestContextFixture(t)
			binding := f.catalog.bindings["test-b"]
			binding.Targets[0].Metadata[scenario.key] = scenario.value
			f.catalog.bindings[binding.ID] = binding
			if _, err := f.service.CurrentManifestDeliveryContext(t.Context(), deliveryActionPrincipal(), "app", "test-b", "target-b"); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("malformed control metadata authorized: %v", err)
			}
		})
	}
}
