package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// Uses the immutable payload exported by core's TestKustomizeDirectWithKubernetes.
func TestManifestAgentWithKubernetes(t *testing.T) {
	kubeconfig, payloadPath := os.Getenv("SOHA_MANIFEST_TEST_KUBECONFIG"), os.Getenv("SOHA_MANIFEST_TEST_PAYLOAD")
	if kubeconfig == "" || payloadPath == "" {
		t.Skip("set SOHA_MANIFEST_TEST_KUBECONFIG and SOHA_MANIFEST_TEST_PAYLOAD for an isolated local cluster")
	}
	encoded, err := os.ReadFile(payloadPath) //nolint:gosec // Explicit local integration-test input supplied by the operator, never a remote request.
	if err != nil {
		t.Fatal(err)
	}
	var payload sohaapi.ManifestExecutionTaskPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(payload.Namespace, "soha-manifest-test-") || len(payload.Documents) != 2 || payload.RenderedDigest == "" {
		t.Fatal("expected the two-resource core integration test snapshot")
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(config.Host)
	if err != nil || (endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "localhost") {
		t.Fatal("integration test requires an isolated loopback cluster")
	}
	config.Timeout = 10 * time.Second
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{dynamic: dynamicClient, discovery: discoveryClient}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	namespaces := dynamicClient.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"})
	if _, err := namespaces.Create(ctx, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": payload.Namespace}}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := namespaces.Delete(cleanup, payload.Namespace, metav1.DeleteOptions{}); err != nil {
			t.Error(err)
			return
		}
		if err := wait.PollUntilContextCancel(cleanup, time.Second, true, func(ctx context.Context) (bool, error) {
			_, err := namespaces.Get(ctx, payload.Namespace, metav1.GetOptions{})
			return apierrors.IsNotFound(err), nil
		}); err != nil {
			t.Errorf("namespace cleanup: %v", err)
		}
	})
	payload.Action = "preflight"
	preflight, err := client.ExecuteManifestTask(ctx, payload)
	if err != nil || preflight.Preflight == nil || !preflight.Preflight.Ready || preflight.RenderedDigest != payload.RenderedDigest {
		t.Fatalf("preflight: %#v, %v", preflight, err)
	}
	configmaps := dynamicClient.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace(payload.Namespace)
	if _, err := configmaps.Get(ctx, "api-settings", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("dry run persisted a resource: %v", err)
	}
	payload.Action = "apply"
	if result, err := client.ExecuteManifestTask(ctx, payload); err != nil || result.RenderedDigest != preflight.RenderedDigest {
		t.Fatalf("apply: %#v, %v", result, err)
	}
	payload.Action = "observe"
	if err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		observed, err := client.ExecuteManifestTask(ctx, payload)
		if err != nil {
			return false, err
		}
		for _, resource := range observed.Inventory {
			if resource.Health != "healthy" {
				return false, nil
			}
		}
		if len(observed.Inventory) != 2 || observed.Drift == nil || observed.Drift.Drifted {
			return false, fmt.Errorf("unexpected inventory/drift: %#v", observed)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	// Model a configuration update by the same field owner, then replay the old snapshot.
	for _, version := range []string{"two", "one"} {
		if version == "two" {
			content, marshalErr := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "api-settings", "namespace": payload.Namespace}, "data": map[string]any{"version": "two"}})
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			_, err = configmaps.Patch(ctx, "api-settings", types.ApplyPatchType, content, metav1.PatchOptions{FieldManager: payload.FieldManager})
		} else {
			payload.Action = "rollback"
			_, err = client.ExecuteManifestTask(ctx, payload)
		}
		if err != nil {
			t.Fatal(err)
		}
		live, err := configmaps.Get(ctx, "api-settings", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		value, _, _ := unstructured.NestedString(live.Object, "data", "version")
		if value != version {
			t.Fatalf("configuration = %q, want %q", value, version)
		}
	}
	if _, err := configmaps.Patch(ctx, "api-settings", types.MergePatchType, []byte(`{"data":{"version":"external"}}`), metav1.PatchOptions{FieldManager: "external-test"}); err != nil {
		t.Fatal(err)
	}
	payload.Action = "observe"
	observed, err := client.ExecuteManifestTask(ctx, payload)
	if err != nil || observed.Drift == nil || !observed.Drift.Drifted {
		t.Fatalf("external drift missing: %#v, %v", observed, err)
	}
	payload.Action = "preflight"
	preflight, err = client.ExecuteManifestTask(ctx, payload)
	if err != nil || preflight.Preflight == nil || preflight.Preflight.Ready {
		t.Fatalf("ownership conflict was not rejected: %#v, %v", preflight, err)
	}
	payload.Action = "apply"
	partial, err := client.ExecuteManifestTask(ctx, payload)
	if err == nil || len(partial.Inventory) != 1 || len(partial.Diagnostics) != 1 {
		t.Fatalf("partial apply must retain successful inventory: %#v, %v", partial, err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := client.ExecuteManifestTask(canceled, payload); err == nil {
		t.Fatal("canceled apply succeeded")
	}
	t.Logf("Agent: core snapshot %s, dry-run, ready observation, rollback, drift, SSA conflict, partial apply and canceled context passed", payload.RenderedDigest)
}
