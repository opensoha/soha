package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func rolloutFixture(t *testing.T, strategy string) []*unstructured.Unstructured {
	t.Helper()
	var documents []*unstructured.Unstructured
	err := json.Unmarshal([]byte(`[
{"apiVersion":"argoproj.io/v1alpha1","kind":"Rollout","metadata":{"name":"web","namespace":"demo"},"spec":{"replicas":2,"selector":{"matchLabels":{"app":"web"}},"template":{"metadata":{"labels":{"app":"web"}},"spec":{"containers":[{"name":"app","image":"registry.example/app@sha256:`+strings.Repeat("a", 64)+`"}]}},"strategy":{"canary":{"stableService":"web-stable","canaryService":"web-preview","trafficRouting":{"traefik":{"weightedTraefikServiceName":"web-traffic"}},"steps":[{"setWeight":20},{"pause":{}},{"analysis":{"templates":[{"templateName":"web-metrics"}]}},{"setWeight":100}]}}}},
{"apiVersion":"v1","kind":"Service","metadata":{"name":"web-stable","namespace":"demo"},"spec":{"selector":{"app":"web"},"ports":[{"port":8080,"targetPort":8080}]}},
{"apiVersion":"v1","kind":"Service","metadata":{"name":"web-preview","namespace":"demo"},"spec":{"selector":{"app":"web"},"ports":[{"port":8080,"targetPort":8080}]}},
{"apiVersion":"argoproj.io/v1alpha1","kind":"AnalysisTemplate","metadata":{"name":"web-metrics","namespace":"demo"},"spec":{"metrics":[{"name":"success","interval":"2s","count":3,"successCondition":"result >= 0.99","failureLimit":0,"provider":{"web":{"url":"http://web-preview.demo.svc:8080/metrics","jsonPath":"{$.successRate}"}}}]}},
{"apiVersion":"traefik.io/v1alpha1","kind":"TraefikService","metadata":{"name":"web-traffic","namespace":"demo"},"spec":{"weighted":{"services":[{"name":"web-stable","port":8080,"weight":100},{"name":"web-preview","port":8080,"weight":0}]}}},
{"apiVersion":"traefik.io/v1alpha1","kind":"IngressRoute","metadata":{"name":"web-route","namespace":"demo"},"spec":{"entryPoints":["web"],"routes":[{"kind":"Rule","match":"PathPrefix(\"/\")","services":[{"name":"web-traffic","kind":"TraefikService"}]}]}}
]`), &documents)
	if err != nil {
		t.Fatal(err)
	}
	if strategy == "blueGreen" {
		documents = documents[:4]
		_ = unstructured.SetNestedMap(documents[0].Object, map[string]any{"blueGreen": map[string]any{"activeService": "web-stable", "previewService": "web-preview", "autoPromotionEnabled": false, "prePromotionAnalysis": map[string]any{"templates": []any{map[string]any{"templateName": "web-metrics"}}}}}, "spec", "strategy")
	}
	return documents
}

func rolloutLiveFixture(t *testing.T) ([]*unstructured.Unstructured, *fake.FakeDynamicClient) {
	documents := rolloutFixture(t, "canary")
	objects := make([]runtime.Object, 0, len(documents))
	for index, document := range documents {
		live := document.DeepCopy()
		live.SetUID(types.UID(fmt.Sprintf("uid-%d", index)))
		live.SetResourceVersion("42")
		live.SetAnnotations(map[string]string{RolloutOwnerAnnotation: "opensoha-manifest/test:web"})
		if IsArgoRollout(live) {
			live.SetGeneration(3)
			annotations := live.GetAnnotations()
			annotations[RolloutOperationAnnotation] = "operation-1"
			live.SetAnnotations(annotations)
			live.Object["status"] = map[string]any{"observedGeneration": "3", "phase": "Paused", "currentPodHash": "new", "stableRS": "old", "currentStepIndex": int64(1), "pauseConditions": []any{map[string]any{"reason": "CanaryPauseStep"}}}
		}
		if live.GetKind() == "Service" {
			hash := "old"
			if live.GetName() == "web-preview" {
				hash = "new"
			}
			_ = unstructured.SetNestedField(live.Object, hash, "spec", "selector", rolloutPodHash)
		}
		if live.GetKind() == "TraefikService" {
			_ = unstructured.SetNestedSlice(live.Object, []any{map[string]any{"name": "web-stable", "port": int64(8080), "weight": int64(80)}, map[string]any{"name": "web-preview", "port": int64(8080), "weight": int64(20)}}, "spec", "weighted", "services")
		}
		objects = append(objects, live)
	}
	return documents, fake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
}

func TestRolloutRestoreWaitsForCurrentGenerationBeforeAcceptingFailure(t *testing.T) {
	for _, phase := range []string{"Degraded", "Aborted"} {
		t.Run(phase, func(t *testing.T) {
			documents, client := rolloutLiveFixture(t)
			root, err := client.Resource(rollouts).Namespace("demo").Get(t.Context(), "web", metav1.GetOptions{})
			operatorRequire(t, err)
			root.SetGeneration(4)
			if phase == "Aborted" {
				_ = unstructured.SetNestedField(root.Object, true, "status", "abort")
			} else {
				_ = unstructured.SetNestedField(root.Object, phase, "status", "phase")
			}
			_, err = client.Resource(rollouts).Namespace("demo").Update(t.Context(), root, metav1.UpdateOptions{})
			operatorRequire(t, err)
			observed, err := ObserveRollout(t.Context(), client, documents, "opensoha-manifest/test", "operation-1")
			if err != nil || observed.Health != "progressing" {
				t.Fatalf("previous generation failure must not abort a new restore: %+v, %v", observed, err)
			}
			_ = unstructured.SetNestedField(root.Object, "4", "status", "observedGeneration")
			_, err = client.Resource(rollouts).Namespace("demo").Update(t.Context(), root, metav1.UpdateOptions{})
			operatorRequire(t, err)
			observed, err = ObserveRollout(t.Context(), client, documents, "opensoha-manifest/test", "operation-1")
			if err != nil || observed.Health != "degraded" {
				t.Fatalf("current generation failure must remain terminal: %+v, %v", observed, err)
			}
		})
	}
}

func TestRolloutObservationAcceptsOmittedEmptyContainerLists(t *testing.T) {
	documents, client := rolloutLiveFixture(t)
	containers, _, _ := unstructured.NestedSlice(documents[0].Object, "spec", "template", "spec", "containers")
	container, ok := containers[0].(map[string]any)
	if !ok {
		t.Fatal("unexpected container")
	}
	container["env"] = []any{}
	_ = unstructured.SetNestedSlice(documents[0].Object, containers, "spec", "template", "spec", "containers")
	observed, err := ObserveRollout(t.Context(), client, documents, "opensoha-manifest/test", "operation-1")
	if err != nil || observed.Drifted || observed.Health != "progressing" {
		t.Fatalf("omitted empty env must not abort a healthy progression: %+v, %v", observed, err)
	}
	container, ok = containers[0].(map[string]any)
	if !ok {
		t.Fatal("unexpected container")
	}
	container["env"] = []any{map[string]any{"name": "REQUIRED", "value": "yes"}}
	_ = unstructured.SetNestedSlice(documents[0].Object, containers, "spec", "template", "spec", "containers")
	observed, err = ObserveRollout(t.Context(), client, documents, "opensoha-manifest/test", "operation-1")
	if err != nil || !observed.Drifted || observed.Health != "degraded" {
		t.Fatalf("missing required env must still fail: %+v, %v", observed, err)
	}
}

func TestRolloutFrozenResources(t *testing.T) {
	for _, strategy := range []string{"canary", "blueGreen"} {
		if _, err := ValidateRolloutResources(rolloutFixture(t, strategy)); err != nil {
			t.Fatal(strategy, err)
		}
	}
	for _, scenario := range []string{"missing service", "namespace", "mutable image", "job analysis", "external metric", "unbounded metric", "invalid port", "skip analysis", "second root", "controller owner", "backend port", "other traffic", "unreferenced", "preset pause"} {
		t.Run(scenario, func(t *testing.T) {
			documents := rolloutFixture(t, "canary")
			switch scenario {
			case "missing service":
				documents = append(documents[:1], documents[2:]...)
			case "namespace":
				documents[1].SetNamespace("outside")
			case "mutable image":
				_ = unstructured.SetNestedSlice(documents[0].Object, []any{map[string]any{"name": "app", "image": "app:latest"}}, "spec", "template", "spec", "containers")
			case "job analysis", "external metric", "unbounded metric":
				metrics, _, _ := unstructured.NestedSlice(documents[3].Object, "spec", "metrics")
				metric, ok := metrics[0].(map[string]any)
				if !ok {
					t.Fatal("unexpected metric")
				}
				switch scenario {
				case "job analysis":
					metric["provider"] = map[string]any{"job": map[string]any{}}
				case "external metric":
					metric["provider"] = map[string]any{"web": map[string]any{"url": "http://other.demo.svc/metrics"}}
				case "unbounded metric":
					metric["interval"] = "24h"
				}
				_ = unstructured.SetNestedSlice(documents[3].Object, metrics, "spec", "metrics")
			case "skip analysis":
				_ = unstructured.SetNestedSlice(documents[0].Object, []any{map[string]any{"setWeight": int64(100)}}, "spec", "strategy", "canary", "steps")
			case "invalid port":
				documents = rolloutFixture(t, "blueGreen")
				_ = unstructured.SetNestedSlice(documents[1].Object, []any{"invalid"}, "spec", "ports")
			case "second root":
				other := documents[0].DeepCopy()
				other.SetName("other")
				documents = append(documents, other)
			case "controller owner":
				documents[1].SetAnnotations(map[string]string{RolloutOwnerAnnotation: "another"})
			case "backend port":
				_ = unstructured.SetNestedSlice(documents[1].Object, []any{map[string]any{"port": int64(9090)}}, "spec", "ports")
			case "other traffic":
				_ = unstructured.SetNestedMap(documents[0].Object, map[string]any{"nginx": map[string]any{}}, "spec", "strategy", "canary", "trafficRouting")
			case "unreferenced":
				other := documents[1].DeepCopy()
				other.SetName("other")
				documents = append(documents, other)
			case "preset pause":
				_ = unstructured.SetNestedField(documents[0].Object, true, "spec", "paused")
			}
			if _, err := ValidateRolloutResources(documents); err == nil {
				t.Fatal("unsafe bundle accepted")
			}
		})
	}
}

func TestRolloutControlsUseIdentityAndCannotSkipGates(t *testing.T) {
	for _, scenario := range []string{"promote", "pause", "abort", "stale UID", "stale RV", "inconclusive", "timed pause", "other owner", "other operation"} {
		t.Run(scenario, func(t *testing.T) {
			documents, client := rolloutLiveFixture(t)
			root, _ := client.Resource(rollouts).Namespace("demo").Get(context.Background(), "web", metav1.GetOptions{})
			input := sohaapi.ProgressiveRolloutControlInput{Action: "promote", UID: "uid-0", ResourceVersion: "42"}
			switch scenario {
			case "pause", "abort":
				input.Action = sohaapi.ProgressiveRolloutControlInputAction(scenario)
			case "stale UID":
				input.UID = "replaced"
			case "stale RV":
				input.ResourceVersion = "41"
			case "inconclusive":
				_ = unstructured.SetNestedSlice(root.Object, []any{map[string]any{"reason": "InconclusiveAnalysis"}}, "status", "pauseConditions")
			case "timed pause":
				steps, _, _ := unstructured.NestedSlice(root.Object, "spec", "strategy", "canary", "steps")
				steps[1] = map[string]any{"pause": map[string]any{"duration": "1m"}}
				_ = unstructured.SetNestedSlice(root.Object, steps, "spec", "strategy", "canary", "steps")
				_ = unstructured.SetNestedSlice(documents[0].Object, steps, "spec", "strategy", "canary", "steps")
			case "other owner":
				annotations := root.GetAnnotations()
				annotations[RolloutOwnerAnnotation] = "opensoha-manifest/test:other-root"
				root.SetAnnotations(annotations)
			case "other operation":
				annotations := root.GetAnnotations()
				annotations[RolloutOperationAnnotation] = "new-operation"
				root.SetAnnotations(annotations)
			}
			_ = client.Tracker().Update(rollouts, root, "demo")
			writes := 0
			client.PrependReactor("patch", "rollouts", func(action ktesting.Action) (bool, runtime.Object, error) {
				writes++
				patch, ok := action.(ktesting.PatchAction)
				if !ok {
					t.Fatal("unexpected patch action")
				}
				var body map[string]any
				_ = json.Unmarshal(patch.GetPatch(), &body)
				metadata, ok := body["metadata"].(map[string]any)
				if !ok {
					t.Fatal("unexpected patch metadata")
				}
				if metadata["uid"] != "uid-0" || metadata["resourceVersion"] != "42" {
					t.Fatal("control lost its CAS")
				}
				if status, ok := body["status"].(map[string]any); ok {
					if status["currentStepIndex"] != nil || status["promoteFull"] != nil {
						t.Fatal("control skipped native analysis")
					}
				}
				return true, root, nil
			})
			_, err := ControlRollout(context.Background(), client, documents, "opensoha-manifest/test", "operation-1", input)
			accepted := scenario == "promote" || scenario == "pause" || scenario == "abort"
			if accepted && (err != nil || writes != 1) || !accepted && (err == nil || writes != 0) {
				t.Fatalf("writes=%d err=%v", writes, err)
			}
		})
	}
}

func TestRolloutStopRequiresActualStableTraffic(t *testing.T) {
	documents, client := rolloutLiveFixture(t)
	root, _ := client.Resource(rollouts).Namespace("demo").Get(context.Background(), "web", metav1.GetOptions{})
	_ = unstructured.SetNestedField(root.Object, true, "status", "abort")
	_ = unstructured.SetNestedField(root.Object, "Degraded", "status", "phase")
	_ = client.Tracker().Update(rollouts, root, "demo")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := StopRollout(ctx, client, documents, "opensoha-manifest/test", "operation-1", "uid-0"); !errors.Is(err, ErrRolloutStopUnconfirmed) {
		t.Fatalf("unconfirmed traffic stop: %v", err)
	}
	gvr := rolloutResourceTypes["traefik.io/TraefikService"]
	weighted, _ := client.Resource(gvr).Namespace("demo").Get(context.Background(), "web-traffic", metav1.GetOptions{})
	_ = unstructured.SetNestedSlice(weighted.Object, []any{map[string]any{"name": "web-stable", "port": int64(8080), "weight": int64(100)}, map[string]any{"name": "web-preview", "port": int64(8080), "weight": int64(0)}}, "spec", "weighted", "services")
	_ = client.Tracker().Update(gvr, weighted, "demo")
	if err := StopRollout(context.Background(), client, documents, "opensoha-manifest/test", "operation-1", "uid-0"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDirectManifestOwner(root); !errors.Is(err, ErrResourceOwnership) {
		t.Fatal("native mutation accepted a Rollout")
	}
	if _, err := ApplyManifest(context.Background(), client.Resource(rollouts).Namespace("demo"), documents[0], metav1.PatchOptions{}); !errors.Is(err, ErrResourceOwnership) {
		t.Fatal("generic apply accepted a Rollout")
	}
}

func TestRolloutPreflightPreservesControllerFields(t *testing.T) {
	documents, client := rolloutLiveFixture(t)
	root, _ := client.Resource(rollouts).Namespace("demo").Get(context.Background(), "web", metav1.GetOptions{})
	_ = unstructured.SetNestedField(root.Object, "Healthy", "status", "phase")
	_ = client.Tracker().Update(rollouts, root, "demo")
	writes := 0
	client.PrependReactor("update", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		update, ok := action.(ktesting.UpdateAction)
		if !ok {
			t.Fatal("unexpected update action")
		}
		write, ok := update.GetObject().(*unstructured.Unstructured)
		if !ok {
			t.Fatal("unexpected update object")
		}
		updateOptions, ok := action.(interface{ GetUpdateOptions() metav1.UpdateOptions })
		if !ok {
			t.Fatal("unexpected update options")
		}
		if write.GetUID() == "" || write.GetResourceVersion() != "42" || len(updateOptions.GetUpdateOptions().DryRun) != 1 {
			t.Fatal("preflight lost identity or dry-run")
		}
		if write.GetKind() == "Service" && argoString(write, "spec", "selector", rolloutPodHash) == "" {
			t.Fatal("preflight removed the native selector hash")
		}
		if write.GetKind() == "TraefikService" {
			t.Fatal("preflight rewrote native traffic weights")
		}
		writes++
		return true, write, nil
	})
	_, err := ApplyRollout(context.Background(), client, documents, metav1.PatchOptions{FieldManager: "opensoha-manifest/test", DryRun: []string{metav1.DryRunAll}}, "operation-2")
	if err != nil || writes != 5 {
		t.Fatalf("preflight writes=%d err=%v", writes, err)
	}
}

func TestRolloutDependencyUpdateRetriesAndSkipsUnchanged(t *testing.T) {
	documents, client := rolloutLiveFixture(t)
	resources := client.Resource(rolloutResourceTypes["/Service"]).Namespace("demo")
	live, err := resources.Get(t.Context(), "web-preview", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wanted := documents[2].DeepCopy()
	_ = unstructured.SetNestedSlice(live.Object, []any{map[string]any{"port": int64(8080), "targetPort": int64(8080), "protocol": "TCP"}}, "spec", "ports")
	_ = client.Tracker().Update(rolloutResourceTypes["/Service"], live, "demo")
	wanted.SetAnnotations(live.GetAnnotations())
	options := metav1.PatchOptions{FieldManager: "opensoha-manifest/test"}
	if _, err := updateRolloutResource(t.Context(), resources, wanted, live, options, "web"); err != nil {
		t.Fatal(err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" {
			t.Fatal("unchanged Service was rewritten")
		}
	}
	_ = unstructured.SetNestedSlice(wanted.Object, []any{map[string]any{"port": int64(8080), "targetPort": int64(9090)}}, "spec", "ports")
	writes := 0
	client.PrependReactor("update", "services", func(action ktesting.Action) (bool, runtime.Object, error) {
		writes++
		if writes < 4 {
			return true, nil, apierrors.NewConflict(rolloutResourceTypes["/Service"].GroupResource(), "web-preview", errors.New("native controller changed selector"))
		}
		update, ok := action.(ktesting.UpdateAction)
		if !ok {
			t.Fatal("unexpected update action")
		}
		write, ok := update.GetObject().(*unstructured.Unstructured)
		if !ok {
			t.Fatal("unexpected update object")
		}
		if argoString(write, "spec", "selector", rolloutPodHash) != "new" {
			t.Fatal("native selector lost during retry")
		}
		return true, write, nil
	})
	if _, err := updateRolloutResource(t.Context(), resources, wanted, live, options, "web"); err != nil || writes != 4 {
		t.Fatalf("writes=%d err=%v", writes, err)
	}
}

func TestRolloutTaskValidatesFrozenPayloadAndReturnsNativeEvidence(t *testing.T) {
	for _, scenario := range []string{"observe", "digest", "namespace", "owner", "mixed GitOps", "adopt"} {
		t.Run(scenario, func(t *testing.T) {
			documents, client := rolloutLiveFixture(t)
			payload := sohaapi.ManifestExecutionTaskPayload{Action: sohaapi.ManifestTaskActionObserve, PackageID: "package", BindingID: "test", DeploymentID: "deployment", Generation: 7, Namespace: "demo", FieldManager: "opensoha-manifest/test", IdempotencyKey: "operation-1", RenderedDigest: "frozen"}
			for _, item := range documents {
				content, _ := item.MarshalJSON()
				sum := sha256.Sum256(content)
				payload.Documents = append(payload.Documents, sohaapi.ManifestRenderedDocument{APIVersion: item.GetAPIVersion(), Kind: item.GetKind(), Namespace: item.GetNamespace(), Name: item.GetName(), Content: string(content), ContentDigest: hex.EncodeToString(sum[:])})
			}
			switch scenario {
			case "digest":
				payload.Documents[1].Content += " "
			case "namespace":
				payload.Namespace = "outside"
			case "owner":
				payload.FieldManager = "opensoha-manifest/another"
			case "mixed GitOps":
				payload.GitOpsDocuments = payload.Documents[:1]
			case "adopt":
				payload.Action = sohaapi.ManifestTaskActionAdopt
			}
			result, err := ExecuteRolloutTask(t.Context(), client, payload)
			if scenario != "observe" {
				if err == nil || len(client.Actions()) != 0 {
					t.Fatalf("invalid task reached Kubernetes: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Rollout == nil || *result.Rollout.CanaryWeight != 20 || len(result.Inventory) != 6 || result.Drift == nil || result.Drift.Drifted {
				t.Fatalf("missing native evidence: %+v", result)
			}
			for _, item := range result.Inventory {
				if item.UID == "" || item.Generation != 7 || item.DesiredObjectDigest != item.ObservedObjectDigest {
					t.Fatalf("bad inventory: %+v", item)
				}
				if item.Kind != "Service" && item.Health != "progressing" {
					t.Fatalf("native policy readiness must follow the paused rollout: %+v", item)
				}
			}
		})
	}
}

func TestRolloutAnalysisEvidenceSurvivesStepCompletion(t *testing.T) {
	root := rolloutFixture(t, "canary")[0]
	root.SetUID("current-owner")
	root.SetAnnotations(map[string]string{"rollout.argoproj.io/revision": "2"})
	base := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "AnalysisRun",
		"metadata": map[string]any{"name": "current", "namespace": "demo", "uid": "run-uid", "labels": map[string]any{rolloutPodHash: "current-hash"}, "annotations": map[string]any{"rollout.argoproj.io/revision": "2"}},
		"spec":     map[string]any{"metrics": []any{map[string]any{"name": "success", "count": int64(2), "interval": "3s", "successCondition": "result >= 0.99"}}},
		"status": map[string]any{"phase": "Successful", "metricResults": []any{map[string]any{"name": "success", "phase": "Successful", "count": int64(2), "successful": int64(2), "measurements": []any{
			map[string]any{"startedAt": "2026-09-14T00:00:00Z", "finishedAt": "2026-09-14T00:00:01Z", "value": "0.99"},
			map[string]any{"startedAt": "2026-09-14T00:00:03Z", "finishedAt": "2026-09-14T00:00:04Z", "value": "1"},
		}}}},
	}}
	base.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(root, root.GroupVersionKind())})
	objects := []runtime.Object{base}
	for _, mode := range []string{"old-owner", "old-revision", "old-hash"} {
		run := base.DeepCopy()
		run.SetName(mode)
		run.SetUID(types.UID(mode))
		switch mode {
		case "old-owner":
			refs := run.GetOwnerReferences()
			refs[0].UID = "old-owner"
			run.SetOwnerReferences(refs)
		case "old-revision":
			run.SetAnnotations(map[string]string{"rollout.argoproj.io/revision": "1"})
		case "old-hash":
			run.SetLabels(map[string]string{rolloutPodHash: "old-hash"})
		}
		objects = append(objects, run)
	}
	client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{analysisRuns: "AnalysisRunList"}, objects...)
	state := sohaapi.ProgressiveRolloutStatus{CurrentRevision: "current-hash"}
	if err := observeRolloutAnalyses(t.Context(), client, root, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Metrics) != 1 {
		t.Fatalf("included unrelated analysis: %+v", state.Metrics)
	}
	metric := state.Metrics[0]
	if metric.AnalysisRun != "current" || metric.Count != 2 || metric.Value != "1" || metric.StartedAt != "2026-09-14T00:00:00Z" || metric.FinishedAt != "2026-09-14T00:00:04Z" {
		t.Fatalf("completed observation evidence lost: %+v", metric)
	}
}
