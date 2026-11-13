package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestManifestInventoryRetainsResourceObservations(t *testing.T) {
	live := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"observedGeneration": int64(0)}}}
	live.SetGeneration(3)
	live.SetFinalizers([]string{"test.soha.io/cleanup"})
	now := metav1.Now()
	live.SetDeletionTimestamp(&now)
	item := manifestInventory(sohaapi.ManifestExecutionTaskPayload{Generation: 12}, sohaapi.ManifestRenderedDocument{}, live, live)
	if item.Generation != 12 || item.ResourceGeneration != 3 || item.ObservedResourceGeneration == nil || *item.ObservedResourceGeneration != 0 || item.DeletingAt == nil || !item.DeletingAt.Equal(live.GetDeletionTimestamp().Time) || len(item.Finalizers) != 1 {
		t.Fatalf("lost resource observations: %#v", item)
	}
	unstructured.RemoveNestedField(live.Object, "status", "observedGeneration")
	if manifestInventory(sohaapi.ManifestExecutionTaskPayload{}, sohaapi.ManifestRenderedDocument{}, live, live).ObservedResourceGeneration != nil {
		t.Fatal("missing controller observation was fabricated as zero")
	}
}

func TestExecuteManifestTaskRejectsMissingRenderedDocuments(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	discoveryClient := &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{}}
	client := &Client{dynamic: dynamicClient, discovery: discoveryClient}
	for _, action := range []sohaapi.ManifestTaskAction{"preflight", "apply", "repair", "rollback", "observe", "adopt"} {
		t.Run(string(action), func(t *testing.T) {
			result, err := client.ExecuteManifestTask(context.Background(), sohaapi.ManifestExecutionTaskPayload{
				Action: action, PackageID: "legacy-package", Generation: 1, IdempotencyKey: "legacy-task",
			})
			if err == nil || !strings.Contains(err.Error(), "requires rendered documents") || result.Preflight != nil || len(result.Inventory) != 0 {
				t.Fatalf("missing documents must fail: result=%#v err=%v", result, err)
			}
		})
	}
	if len(dynamicClient.Actions()) != 0 || len(discoveryClient.Actions()) != 0 {
		t.Fatal("incomplete manifest payload accessed Kubernetes")
	}
}

func TestManifestRolloutCannotFallBackToGenericApply(t *testing.T) {
	for _, action := range []sohaapi.ManifestTaskAction{"preflight", "apply", "observe", "adopt", "rollout_control"} {
		t.Run(string(action), func(t *testing.T) {
			dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			client := &Client{dynamic: dynamicClient, discovery: &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{}}}
			_, err := client.ExecuteManifestTask(t.Context(), sohaapi.ManifestExecutionTaskPayload{Action: action, Documents: []sohaapi.ManifestRenderedDocument{{APIVersion: "argoproj.io/v1alpha1", Kind: "Rollout"}}})
			if err == nil || !strings.Contains(err.Error(), "rollout requires a frozen namespace") || len(dynamicClient.Actions()) != 0 {
				t.Fatalf("unfrozen rollout reached generic execution: %v", err)
			}
		})
	}
}

func TestExecuteManifestTaskPreflightUsesApplyPatch(t *testing.T) {
	existing := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "settings", "namespace": "payments", "uid": "settings-uid", "resourceVersion": "12"}}}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), existing)
	patches := 0
	dynamicClient.PrependReactor("patch", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		patch := action.(ktesting.PatchAction)
		var object map[string]any
		if err := json.Unmarshal(patch.GetPatch(), &object); err != nil {
			t.Fatal(err)
		}
		patches++
		return true, &unstructured.Unstructured{Object: object}, nil
	})
	discoveryClient := &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{}}
	discoveryClient.Resources = []*metav1.APIResourceList{{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{{Name: "configmaps", SingularName: "configmap", Namespaced: true, Kind: "ConfigMap"}},
	}}
	client := &Client{dynamic: dynamicClient, discovery: discoveryClient}
	payload := sohaapi.ManifestExecutionTaskPayload{
		Action: "preflight", PackageID: "package-1", BindingID: "binding-1",
		Generation: 1, IdempotencyKey: "manifest:test:1", ClusterID: "cluster-1",
		Namespace: "payments", FieldManager: "opensoha-delivery/v1",
		Documents: []sohaapi.ManifestRenderedDocument{{
			Index: 0, Path: "configmap.yaml", APIVersion: "v1", Kind: "ConfigMap",
			Namespace: "payments", Name: "settings", ContentDigest: "digest",
			Content: `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"payments"}}`,
		}},
	}
	payload.Documents[0].ContentDigest = manifestTestContentDigest(payload.Documents[0].Content)

	result, err := client.ExecuteManifestTask(context.Background(), payload)
	if err != nil {
		t.Fatalf("ExecuteManifestTask() error = %v", err)
	}
	if patches != 1 || result.Preflight == nil || !result.Preflight.Ready {
		t.Fatalf("patches=%d result=%#v", patches, result)
	}
}

func TestDiffManifestFieldsIgnoresVolatileMetadata(t *testing.T) {
	desired := map[string]any{"metadata": map[string]any{"name": "api"}, "spec": map[string]any{"replicas": float64(2)}}
	live := map[string]any{"metadata": map[string]any{"name": "api", "resourceVersion": "12"}, "spec": map[string]any{"replicas": float64(3)}}
	fields := diffManifestFields(desired, live, "")
	if len(fields) != 1 || fields[0].Path != "/spec/replicas" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestManifestDriftIgnoresDefaultedContainerFields(t *testing.T) {
	desired := map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"name": "api", "image": "api:v1"}}}}
	container := map[string]any{"name": "api", "image": "api:v1", "imagePullPolicy": "IfNotPresent"}
	live := map[string]any{"spec": map[string]any{"containers": []any{container}}}
	if fields := diffManifestFields(desired, live, ""); len(fields) != 0 {
		t.Fatalf("Kubernetes defaults caused drift: %#v", fields)
	}
	if digestManifestObject(desired) != digestManifestObject(projectManifestObject(desired, live)) {
		t.Fatal("projected object digest differs")
	}
	container["image"] = "api:v2"
	if fields := diffManifestFields(desired, live, ""); len(fields) != 1 {
		t.Fatalf("image drift was lost: %#v", fields)
	}
}

func TestExecuteManifestTaskObserveFailsWhenResourceIsUnavailable(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	discoveryClient := &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{}}
	discoveryClient.Resources = []*metav1.APIResourceList{{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{{Name: "configmaps", SingularName: "configmap", Namespaced: true, Kind: "ConfigMap"}},
	}}
	client := &Client{dynamic: dynamicClient, discovery: discoveryClient}
	payload := sohaapi.ManifestExecutionTaskPayload{
		Action: "observe", PackageID: "package-1", BindingID: "binding-1", DeploymentID: "deployment-1",
		Generation: 2, IdempotencyKey: "manifest:test:observe", ClusterID: "cluster-1", Namespace: "payments",
		Documents: []sohaapi.ManifestRenderedDocument{{
			Index: 0, Path: "configmap.yaml", APIVersion: "v1", Kind: "ConfigMap",
			Namespace: "payments", Name: "missing", ContentDigest: "digest",
			Content: `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"missing","namespace":"payments"}}`,
		}},
	}
	payload.Documents[0].ContentDigest = manifestTestContentDigest(payload.Documents[0].Content)

	result, err := client.ExecuteManifestTask(context.Background(), payload)
	if err == nil {
		t.Fatal("ExecuteManifestTask() error = nil, want unavailable resource failure")
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Stage != "observe" {
		t.Fatalf("diagnostics = %#v, want one observe diagnostic", result.Diagnostics)
	}
}

type manifestTestMapper struct{}

func (manifestTestMapper) RESTMapping(schema.GroupKind, ...string) (*meta.RESTMapping, error) {
	return &meta.RESTMapping{
		Resource: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
		Scope:    meta.RESTScopeNamespace,
	}, nil
}

func TestPrepareManifestDocumentRejectsEnvelopeAndDigestMismatch(t *testing.T) {
	content := `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"payments"}}`
	base := sohaapi.ManifestRenderedDocument{
		Path: "configmap.yaml", APIVersion: "v1", Kind: "ConfigMap", Namespace: "payments",
		Name: "settings", Content: content, ContentDigest: manifestTestContentDigest(content),
	}
	tests := map[string]func(*sohaapi.ManifestRenderedDocument){
		"api version": func(item *sohaapi.ManifestRenderedDocument) { item.APIVersion = "apps/v1" },
		"kind":        func(item *sohaapi.ManifestRenderedDocument) { item.Kind = "Secret" },
		"namespace":   func(item *sohaapi.ManifestRenderedDocument) { item.Namespace = "other" },
		"name":        func(item *sohaapi.ManifestRenderedDocument) { item.Name = "other" },
		"digest":      func(item *sohaapi.ManifestRenderedDocument) { item.ContentDigest = "invalid" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			document := base
			mutate(&document)
			if _, _, err := prepareManifestDocument(manifestTestMapper{}, sohaapi.ManifestExecutionTaskPayload{Namespace: "payments"}, document); err == nil {
				t.Fatal("prepareManifestDocument() error = nil, want mismatch rejection")
			}
		})
	}
}

func TestPublicManifestKubernetesErrorDoesNotExposeProviderDetails(t *testing.T) {
	secret := "token=super-secret kubeconfig=/private/config"
	if got := publicManifestKubernetesError(errors.New(secret)); got != "Kubernetes manifest operation failed" {
		t.Fatalf("publicManifestKubernetesError() = %q, want generic public error", got)
	}
}

func manifestTestContentDigest(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}
