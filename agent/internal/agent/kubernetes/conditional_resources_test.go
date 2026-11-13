package kubernetes

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	contractresource "github.com/opensoha/soha/contracts/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clienttesting "k8s.io/client-go/testing"
)

func TestConditionalPreflightDoesNotExposeProviderConflictValues(t *testing.T) {
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	current := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "sample", "namespace": "test", "uid": "reviewed", "resourceVersion": "1"}, "data": map[string]any{"key": "old"}}}
	dynamic := newSSAFakeDynamicClient(t, gvr, current)
	dynamic.PrependReactor("patch", "configmaps", func(clienttesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, "sample", fmt.Errorf("SENSITIVE_PROVIDER_VALUE"))
	})
	client := &Client{dynamic: dynamic}
	analysis, err := client.DryRunResourceYAMLConditional(t.Context(), "test", "ConfigMap", "sample", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: sample\ndata:\n  key: new\n", contractresource.ResourceMutationPreconditions{ExpectedUID: "reviewed", ExpectedResourceVersion: "1"})
	raw, _ := json.Marshal(analysis)
	if err != nil || len(analysis.Conflicts) == 0 || strings.Contains(string(raw), "SENSITIVE_PROVIDER_VALUE") {
		t.Fatalf("conditional conflict projection: %s %v", raw, err)
	}
}

func TestConditionalResourceEditRetainsReviewedUIDAndVersion(t *testing.T) {
	for _, scenario := range []string{"matching", "preview", "stale-uid", "stale-version", "stale-yaml", "other-kind", "missing-namespace"} {
		t.Run(scenario, func(t *testing.T) {
			gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
			current := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "sample", "namespace": "test", "uid": "reviewed", "resourceVersion": "1"}, "data": map[string]any{"key": "old"}}}
			client := &Client{dynamic: newSSAFakeDynamicClient(t, gvr, current)}
			guard := contractresource.ResourceMutationPreconditions{ExpectedUID: "reviewed", ExpectedResourceVersion: "1"}
			content := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: sample\n  namespace: test\ndata:\n  key: new\n"
			namespace := "test"
			switch scenario {
			case "stale-uid":
				guard.ExpectedUID = "previous"
			case "stale-version":
				guard.ExpectedResourceVersion = "previous"
			case "stale-yaml":
				content = strings.Replace(content, "  name: sample", "  name: sample\n  uid: previous", 1)
			case "other-kind":
				content = strings.Replace(content, "kind: ConfigMap", "kind: Secret", 1)
			case "missing-namespace":
				namespace = ""
			}
			var err error
			if scenario == "preview" {
				_, err = client.DryRunResourceYAMLConditional(t.Context(), namespace, "ConfigMap", "sample", content, guard)
			} else {
				_, err = client.ApplyResourceYAMLConditional(t.Context(), namespace, "ConfigMap", "sample", content, guard)
			}
			allowed := scenario == "matching" || scenario == "preview"
			if (err == nil) != allowed {
				t.Fatalf("conditional edit: %v", err)
			}
			if (scenario == "stale-uid" || scenario == "stale-version") && !apierrors.IsConflict(err) {
				t.Fatal("stale caller identity was not a conflict", err)
			}
			live, readErr := client.dynamic.Resource(gvr).Namespace("test").Get(t.Context(), "sample", metav1.GetOptions{})
			if readErr != nil {
				t.Fatal(readErr)
			}
			value, _, _ := unstructured.NestedString(live.Object, "data", "key")
			want := "old"
			if scenario == "matching" {
				want = "new"
			}
			if value != want || live.GetUID() != current.GetUID() {
				t.Fatalf("rejected/dry-run edit changed live resource: %q", value)
			}
		})
	}
}

func TestResourceIdentityReadsOnlyMetadata(t *testing.T) {
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	current := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "sample", "namespace": "test", "uid": "reviewed", "resourceVersion": "1", "annotations": map[string]any{"private": "sensitive-marker"}}, "data": map[string]any{"token": "sensitive-marker"}}}
	client := &Client{dynamic: newSSAFakeDynamicClient(t, gvr, current)}
	identity, err := client.GetResourceIdentity(t.Context(), "test", "Secret", "sample")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(identity)
	if err != nil || identity.UID != "reviewed" || strings.Contains(string(raw), "sensitive-marker") || strings.Contains(string(raw), "data") {
		t.Fatal("metadata-only read exposed Secret data", err)
	}
}
