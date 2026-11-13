package delivery

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/helmrelease"
	aa "github.com/opensoha/soha/internal/application/access"
	dc "github.com/opensoha/soha/internal/domain/catalog"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	dr "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type publishedSourceReader struct {
	*referenceTargetsStub
	details   map[string]dr.HelmReleaseDetailView
	workloads map[string][]dr.ResourceSourceSnapshot
}

func (r *publishedSourceReader) GetHelmReleaseDetail(_ context.Context, _ di.Principal, _, namespace, name string) (dr.HelmReleaseDetailView, error) {
	return r.details[namespace+"/"+name], nil
}
func (r *publishedSourceReader) GetHelmReleaseWorkloadSources(_ context.Context, _ di.Principal, _, namespace, name string) ([]dr.ResourceSourceSnapshot, error) {
	return r.workloads[namespace+"/"+name], nil
}

type sourcePublishingHelmRuntime struct {
	helmRuntimeCheck
	reader *publishedSourceReader
}

func (r *sourcePublishingHelmRuntime) PrepareHelmDelivery(ctx context.Context, p di.Principal, payload sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error) {
	prepared, err := r.helmRuntimeCheck.PrepareHelmDelivery(ctx, p, payload)
	if err != nil {
		return prepared, err
	}
	detail := r.reader.details[payload.Snapshot.Namespace+"/"+payload.Snapshot.ReleaseName]
	prepared.Snapshot.ExpectedRevision, _ = strconv.Atoi(detail.Revision)
	prepared.Snapshot.Operation = sohaapi.HelmDeliverySnapshotOperationUpgrade
	return prepared, nil
}
func (r *sourcePublishingHelmRuntime) ExecuteHelmDelivery(_ context.Context, _ di.Principal, payload sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error) {
	// Provider fixture changes its live native state exactly as the approved apply does.
	// Core's real hydration/private-impact paths remain in use before every call.
	if payload.Action == sohaapi.Apply {
		r.calls++
		key := payload.Snapshot.Namespace + "/" + payload.Snapshot.ReleaseName
		detail := r.reader.details[key]
		detail.Revision = strconv.Itoa(payload.Snapshot.ExpectedRevision + 1)
		r.reader.details[key] = detail
		resource := r.reader.workloads[key][0]
		resource.SpecDigest = strings.Repeat("d", 64)
		resource.Generation++
		r.reader.workloads[key] = []dr.ResourceSourceSnapshot{resource}
	}
	return sohaapi.HelmExecutionTaskResult{Stopped: true, Ready: true, Status: "deployed", Revision: payload.Snapshot.ExpectedRevision + 1, RenderedDigest: payload.Snapshot.RenderedDigest}, nil
}

func TestReferencedSourceHelmPublishThenObserveRetainsPlanGuards(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d_targets", count), func(t *testing.T) {
			f, repo, input := impactFixture(t, false)
			f.service.permissions = deliveryActionPermissions(aa.PermDeliveryApplicationsView, aa.PermDeliveryApplicationEnvView, aa.PermDeliveryReleasesTrigger, aa.PermDeliveryReleaseBundlesView)
			apps := referenceApplicationsFixture(t, &f)
			apps.services[0].Version = 1
			f.service.applications = apps
			reader := &publishedSourceReader{referenceTargetsStub: f.targets, details: map[string]dr.HelmReleaseDetailView{}, workloads: map[string][]dr.ResourceSourceSnapshot{}}
			binding := f.catalog.bindings["test-b"]
			binding.Targets = nil
			selected := []string{}
			for i := 0; i < count; i++ {
				suffix := strconv.Itoa(i)
				namespace := "backend-" + suffix
				name := "release-" + suffix
				id := "target-" + suffix
				target := dc.ReleaseTarget{ID: id, ServiceID: "svc", ApplicationEnvironmentID: binding.ID, Enabled: true, ExecutorKind: "helm_sdk", TargetKind: "helm_release", WorkloadKind: "HelmRelease", WorkloadName: name, ClusterID: "cluster-b", Namespace: namespace, Metadata: map[string]any{"serviceId": "svc", "ownershipMode": "managed", "importSource": "helm"}, Helm: &sohaapi.HelmDeliveryConfiguration{ReleaseName: name, Source: sohaapi.DeploymentTemplateHelmSource{RepositoryURL: "https://charts.example", Chart: "app", Version: "1.0.0"}}}
				binding.Targets = append(binding.Targets, target)
				selected = append(selected, id)
				root := f.targets.sources["cluster-b/backend/Deployment/api/"]
				root.Resource.Namespace = namespace
				root.Resource.UID = "uid-" + suffix
				root.Generation = 1
				reader.workloads[namespace+"/"+name] = []dr.ResourceSourceSnapshot{root}
				reader.details[namespace+"/"+name] = dr.HelmReleaseDetailView{Name: name, Namespace: namespace, Revision: "7", Status: "deployed"}
			}
			f.catalog.bindings[binding.ID] = binding
			f.service.targets = reader
			preview, err := f.service.observeSourceReferenceGroup(t.Context(), deliveryActionPrincipal(), binding.ID, "svc")
			if err != nil {
				t.Fatal(err)
			}
			identity, err := dd.ServiceReferenceIdentityRevision(preview)
			if err != nil {
				t.Fatal(err)
			}
			control, err := dd.ServiceReferenceControlRevision(binding, "svc")
			if err != nil {
				t.Fatal(err)
			}
			repo.records = []dd.ServiceReferenceRecord{{Reference: dd.ServiceReference{ID: "shared-consumer", Revision: 1, SourceIdentityRevision: identity}, ApplicationID: "app", ConsumerBindingID: "test-a", ServiceID: "svc", SourceBindingID: binding.ID, SourceTargetIDs: selected, ControlRevision: control, ConsumerConfigurationRevision: "consumer-config", ConsumerEnvironment: f.catalog.environments[0]}}
			key, err := keyring.NewKey("source-test", strings.Repeat("k", 32), time.Now(), nil)
			if err != nil {
				t.Fatal(err)
			}
			ring, err := keyring.New(key, nil)
			if err != nil {
				t.Fatal(err)
			}
			runtime := &sourcePublishingHelmRuntime{reader: reader}
			f.service.SetHelmDelivery(HelmDeliveryDependencies{Runtime: runtime, Charts: helmChartCheck{}, Keys: ring})
			f.service.SetExecutionPrincipalReader(fixtureExecutionPrincipalReader{})
			input.TargetID = ""
			input.TargetIDs = selected
			plan, err := f.service.CreateDeliveryPlan(t.Context(), deliveryActionPrincipal(), input)
			if err != nil {
				t.Fatal(err)
			}
			frozenRevision := repo.snapshot.Revision
			plan.TargetIDs = selected
			plan.Status = dd.DeliveryPlanStatusConfirming
			repo.plan = plan
			for _, snapshot := range plan.HelmSnapshots {
				preflight := repo.tasks[snapshot.PreflightTaskID]
				preflight.Status = "completed"
				preflight.Result = map[string]any{"helm": sohaapi.HelmExecutionTaskResult{Stopped: true, Ready: true, Status: "preflighted", Revision: snapshot.ExpectedRevision, RenderedDigest: snapshot.RenderedDigest}}
				repo.tasks[preflight.ID] = preflight
			}
			for _, snapshot := range plan.HelmSnapshots {
				task, err := f.service.queueHelmTask(t.Context(), deliveryActionPrincipal(), snapshot, sohaapi.Apply, 0)
				if err != nil {
					t.Fatal(err)
				}
				hydrated, err := f.service.HydrateExecutionTask(t.Context(), task)
				if err != nil {
					t.Fatalf("next target claim blocked by same Plan's publication: %v", err)
				}
				payload, err := decodeHelmPayload(hydrated.Payload)
				if err != nil {
					t.Fatal(err)
				}
				result, err := runtime.ExecuteHelmDelivery(t.Context(), deliveryActionPrincipal(), payload)
				if err != nil || helmrelease.ValidateCompletedResult(payload, result) != nil {
					t.Fatalf("provider receipt invalid: %v", err)
				}
				task.Status = "completed"
				task.Result = map[string]any{"helm": result}
				repo.tasks[task.ID] = task
			}
			plan.Status = dd.DeliveryPlanStatusConfirmed
			repo.plan = plan
			for _, snapshot := range plan.HelmSnapshots {
				task, err := f.service.queueHelmTask(t.Context(), deliveryActionPrincipal(), snapshot, sohaapi.Observe, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.service.HydrateExecutionTask(t.Context(), task); err != nil {
					t.Fatalf("own successful apply invalidated health observation: %v", err)
				}
			}
			current, err := f.service.captureServiceReferenceImpact(t.Context(), deliveryActionPrincipal(), plan)
			if err != nil || current.Revision != frozenRevision || runtime.calls != count {
				t.Fatalf("publication changed consumer governance or duplicated source: %v calls=%d", err, runtime.calls)
			}
			// Current authoritative configuration still rejects the old intent before provider entry.
			binding = f.catalog.bindings[binding.ID]
			binding.Targets[0].Helm.Source.Version = "2.0.0"
			f.catalog.bindings[binding.ID] = binding
			before := runtime.calls
			payload := sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Observe, Snapshot: plan.HelmSnapshots[0]}
			task := dd.ExecutionTask{ApplicationID: "app", ApplicationEnvironmentID: binding.ID, TaskKind: "helm_observe", ProviderKind: "helm_direct", SecretPrincipal: deliveryActionPrincipal(), Payload: map[string]any{"helm": payload}}
			if _, err := f.service.HydrateExecutionTask(t.Context(), task); err == nil || runtime.calls != before {
				t.Fatalf("changed source config reached provider: %v", err)
			}
		})
	}
}
