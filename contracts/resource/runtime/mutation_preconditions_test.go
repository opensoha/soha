package runtime

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	contractresource "github.com/opensoha/soha/contracts/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestConditionalResourceDeletePinsCallerIdentityThroughKubernetesRequest(t *testing.T) {
	for _, scenario := range []string{"matching", "already-deleting", "replaced-uid", "changed-version", "missing-guard", "concurrent-change", "owned-pod", "external-owner"} {
		t.Run(scenario, func(t *testing.T) {
			current := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "sample", "namespace": "test", "uid": "reviewed-uid", "resourceVersion": "opaque/version+1"}}}
			expected := contractresource.ResourceMutationPreconditions{ExpectedUID: "reviewed-uid", ExpectedResourceVersion: "opaque/version+1"}
			current.SetFinalizers([]string{"test.invalid/hold"})
			if scenario == "already-deleting" {
				stamp := metav1.Now()
				current.SetDeletionTimestamp(&stamp)
			}
			if scenario == "replaced-uid" {
				current.SetUID("replacement")
			}
			if scenario == "changed-version" {
				current.SetResourceVersion("opaque/version+2")
			}
			if scenario == "missing-guard" {
				expected.ExpectedUID = ""
			}
			if scenario == "owned-pod" {
				current.SetKind("Pod")
				owner := true
				current.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "controller", UID: "parent", Controller: &owner}})
			}
			if scenario == "external-owner" {
				current.SetAnnotations(map[string]string{"meta.helm.sh/release-name": "owner", "meta.helm.sh/release-namespace": "test"})
			}
			resourceName := "configmaps"
			if scenario == "owned-pod" {
				resourceName = "pods"
			}
			client := dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme(), current)
			deletes := 0
			client.PrependReactor("delete", resourceName, func(action clienttesting.Action) (bool, k8sruntime.Object, error) {
				deletes++
				deletion, ok := action.(clienttesting.DeleteAction)
				if !ok {
					t.Fatal("expected a Kubernetes delete action")
				}
				options := deletion.GetDeleteOptions()
				if options.Preconditions == nil || options.Preconditions.UID == nil || string(*options.Preconditions.UID) != expected.ExpectedUID || options.Preconditions.ResourceVersion == nil || *options.Preconditions.ResourceVersion != expected.ExpectedResourceVersion {
					t.Fatal("Kubernetes delete lost caller UID or resourceVersion")
				}
				if scenario == "concurrent-change" {
					return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: resourceName}, "sample", errors.New("concurrent update"))
				}
				return true, nil, nil
			})
			err := DeleteResourceConditional(t.Context(), client.Resource(schema.GroupVersionResource{Version: "v1", Resource: resourceName}).Namespace("test"), "sample", expected)
			allowed := scenario == "matching" || scenario == "owned-pod" || scenario == "already-deleting"
			if (err == nil) != allowed {
				t.Fatalf("conditional deletion: %v", err)
			}
			wantDeletes := 0
			if scenario == "matching" || scenario == "owned-pod" || scenario == "concurrent-change" {
				wantDeletes = 1
			}
			if deletes != wantDeletes {
				t.Fatalf("delete calls=%d; want %d", deletes, wantDeletes)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "patch" || action.GetVerb() == "update" {
					t.Fatal("conditional deletion changed finalizers or resource content")
				}
			}
			if (scenario == "replaced-uid" || scenario == "changed-version" || scenario == "concurrent-change") && !apierrors.IsConflict(err) {
				t.Fatal("identity race did not return conflict", err)
			}
			if scenario == "external-owner" && !errors.Is(err, ErrResourceOwnership) {
				t.Fatal("external owner protection changed", err)
			}
		})
	}
}

func TestConditionalEditCannotRemoveOrOmitExistingFinalizers(t *testing.T) {
	current := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "sample", "namespace": "test", "uid": "uid", "resourceVersion": "rv"}}}
	current.SetFinalizers([]string{"test.invalid/hold", "test.invalid/retain"})
	guard := contractresource.ResourceMutationPreconditions{ExpectedUID: "uid", ExpectedResourceVersion: "rv"}
	for _, test := range []struct {
		name       string
		finalizers []string
		allowed    bool
	}{
		{"retained", []string{"test.invalid/retain", "test.invalid/hold"}, true},
		{"added", []string{"test.invalid/retain", "test.invalid/hold", "test.invalid/added"}, true},
		{"omitted", nil, false},
		{"cleared", []string{}, false},
		{"partly removed", []string{"test.invalid/hold"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			desired := current.DeepCopy()
			desired.SetFinalizers(test.finalizers)
			err := ValidateResourceEditPreconditions(current, desired, guard)
			if (err == nil) != test.allowed {
				t.Fatalf("finalizer guard: %v", err)
			}
		})
	}
}

func TestResourceMutationPreconditionsRejectUnboundedAndAmbiguousValues(t *testing.T) {
	for _, value := range []string{"", " ", "uid\nother", "uid\x00other", "uid\u2003other", strings.Repeat("a", 257)} {
		guard := contractresource.ResourceMutationPreconditions{ExpectedUID: value, ExpectedResourceVersion: "opaque/non-numeric+version"}
		if guard.Validate() == nil {
			t.Fatal("unsafe guard value accepted")
		}
	}
	if err := (contractresource.ResourceMutationPreconditions{ExpectedUID: "uid", ExpectedResourceVersion: "opaque/non-numeric+version"}).Validate(); err != nil {
		t.Fatal("opaque version was parsed or rejected", err)
	}
}

func TestConditionalEditCannotOverwriteStaleYAMLIdentity(t *testing.T) {
	current := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "sample", "namespace": "test", "uid": "reviewed", "resourceVersion": "opaque-v1"}}}
	guard := contractresource.ResourceMutationPreconditions{ExpectedUID: "reviewed", ExpectedResourceVersion: "opaque-v1"}
	for _, field := range []string{"uid", "version", "kind", "apiVersion", "name", "namespace"} {
		desired := current.DeepCopy()
		switch field {
		case "uid":
			desired.SetUID("stale")
		case "version":
			desired.SetResourceVersion("stale")
		case "kind":
			desired.SetKind("Secret")
		case "apiVersion":
			desired.SetAPIVersion("other/v1")
		case "name":
			desired.SetName("other")
		case "namespace":
			desired.SetNamespace("other")
		}
		if err := ValidateResourceEditPreconditions(current, desired, guard); !apierrors.IsBadRequest(err) {
			t.Fatalf("stale %s was accepted: %v", field, err)
		}
	}
	desired := current.DeepCopy()
	desired.SetUID("")
	desired.SetResourceVersion("")
	if err := ValidateResourceEditPreconditions(current, desired, guard); err != nil {
		t.Fatal("explicit body guard was not accepted", err)
	}
}

func TestResourceIdentityProjectionDoesNotExposeSecretContent(t *testing.T) {
	current := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "sample", "namespace": "test", "uid": "reviewed", "resourceVersion": "1", "annotations": map[string]any{"private": "sensitive-marker"}}, "data": map[string]any{"token": "sensitive-marker"}}}
	value, err := json.Marshal(ResourceIdentityFrom(current))
	if err != nil || strings.Contains(string(value), "sensitive-marker") || strings.Contains(string(value), "annotations") || strings.Contains(string(value), "data") {
		t.Fatal("metadata identity exposed resource content", err)
	}
	if !strings.Contains(string(value), `"uid":"reviewed"`) {
		t.Fatal("metadata identity lost the reviewed UID")
	}
}
