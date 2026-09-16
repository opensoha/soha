package manifestruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

func TestKustomizeDirectWithKubernetes(t *testing.T) {
	kubeconfig, image := os.Getenv("SOHA_MANIFEST_TEST_KUBECONFIG"), os.Getenv("SOHA_MANIFEST_TEST_IMAGE")
	if kubeconfig == "" {
		t.Skip("set SOHA_MANIFEST_TEST_KUBECONFIG and SOHA_MANIFEST_TEST_IMAGE for an isolated local cluster")
	}
	imageName, imageDigest, valid := strings.Cut(image, "@")
	if !valid || !strings.HasPrefix(imageDigest, "sha256:") {
		t.Fatal("test image must be pinned by digest")
	}
	ctx, client, mapper, namespace := newDirectIntegrationCluster(t, kubeconfig)
	files := []domainmanifest.File{
		{Path: "base/kustomization.yaml", Content: "resources: [deployment.yaml, config.yaml]\n"},
		{Path: "base/config.yaml", Content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: api-settings\ndata:\n  version: one\n"},
		{Path: "base/deployment.yaml", Content: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  replicas: 1
  selector:
    matchLabels: {app: api}
  template:
    metadata:
      labels: {app: api}
    spec:
      terminationGracePeriodSeconds: 0
      containers:
      - {name: api, image: api:latest}
      - {name: sidecar, image: sidecar:latest}
`},
		{Path: "overlays/test/kustomization.yaml", Content: "resources: [../../base]\n"},
	}
	binding := domainmanifest.EnvironmentBinding{ID: "binding-test", Namespace: namespace, Kustomize: &domainmanifest.KustomizeOptions{EntryPath: "overlays/test", Images: []domainmanifest.KustomizeImage{{Name: "api", NewName: imageName, Digest: imageDigest}, {Name: "sidecar", NewName: imageName, Digest: imageDigest}}}}
	item := domainmanifest.Package{ID: "package-test", Renderer: domainmanifest.RendererKustomize}
	rendered, err := NewRenderer().Render(ctx, item, binding, files, 1)
	requireDirectIntegrationNoError(t, err)
	payload := domainmanifest.TaskPayload{Action: domainmanifest.TaskActionPreflight, PackageID: item.ID, BindingID: binding.ID, DeploymentID: "deployment-test", Generation: 1, Revision: 1, IdempotencyKey: "direct-test", Namespace: namespace, FieldManager: "opensoha-manifest/test", RenderedDigest: rendered.RenderedDigest, Documents: rendered.Documents}
	preflight, err := preflightDocuments(ctx, client, mapper, payload)
	if err != nil || preflight.Preflight == nil || !preflight.Preflight.Ready {
		t.Fatalf("preflight: %#v, %v", preflight, err)
	}
	configmaps := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace(namespace)
	if _, err := configmaps.Get(ctx, "api-settings", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("dry run persisted a resource: %v", err)
	}
	payload.Action = domainmanifest.TaskActionApply
	if _, err := applyDocuments(ctx, client, mapper, payload); err != nil {
		t.Fatal(err)
	}
	requireDirectIntegrationHealthy(t, ctx, client, mapper, payload)
	for _, version := range []string{"two", "one"} {
		files[1].Content = strings.ReplaceAll(files[1].Content, "version: one", "version: two")
		next, err := NewRenderer().Render(ctx, item, binding, files, 2)
		requireDirectIntegrationNoError(t, err)
		if version == "one" {
			next = rendered
		}
		update := payload
		update.Documents = next.Documents
		update.RenderedDigest = next.RenderedDigest
		if _, err := applyDocuments(ctx, client, mapper, update); err != nil {
			t.Fatal(err)
		}
		live, err := configmaps.Get(ctx, "api-settings", metav1.GetOptions{})
		requireDirectIntegrationNoError(t, err)
		value, _, _ := unstructured.NestedString(live.Object, "data", "version")
		if value != version {
			t.Fatalf("configuration = %q, want %q", value, version)
		}
	}
	if _, err := configmaps.Patch(ctx, "api-settings", types.MergePatchType, []byte(`{"data":{"version":"external"}}`), metav1.PatchOptions{FieldManager: "external-test"}); err != nil {
		t.Fatal(err)
	}
	observed, err := observeDocuments(ctx, client, mapper, payload)
	if err != nil || observed.Drift == nil || !observed.Drift.Drifted {
		t.Fatalf("external drift missing: %#v, %v", observed, err)
	}
	preflight, err = preflightDocuments(ctx, client, mapper, payload)
	if err != nil || preflight.Preflight.Ready {
		t.Fatalf("field ownership conflict was not rejected: %#v, %v", preflight, err)
	}
	if output := os.Getenv("SOHA_MANIFEST_TEST_PAYLOAD_OUT"); output != "" {
		encoded, err := json.Marshal(payload)
		requireDirectIntegrationNoError(t, err)
		requireDirectIntegrationNoError(t, os.WriteFile(output, encoded, 0o600))
	}
	t.Logf("Direct: %d resources, fixed image digest, dry-run, ready observation, update, rollback, drift and SSA ownership conflict passed", len(rendered.Documents))
}

func requireDirectIntegrationNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func requireDirectIntegrationHealthy(t *testing.T, ctx context.Context, client dynamic.Interface, mapper meta.RESTMapper, payload domainmanifest.TaskPayload) {
	t.Helper()
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		observed, err := observeDocuments(ctx, client, mapper, payload)
		if err != nil {
			return false, err
		}
		for _, resource := range observed.Inventory {
			if resource.Health != "healthy" {
				return false, nil
			}
		}
		if len(observed.Inventory) != 2 || observed.Drift.Drifted {
			return false, fmt.Errorf("unexpected inventory/drift: %#v", observed)
		}
		return true, nil
	})
	requireDirectIntegrationNoError(t, err)
}

func newDirectIntegrationCluster(t *testing.T, kubeconfig string) (context.Context, dynamic.Interface, meta.RESTMapper, string) {
	t.Helper()
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	requireDirectIntegrationNoError(t, err)
	endpoint, err := url.Parse(config.Host)
	if err != nil || (endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "localhost") {
		t.Fatal("integration test requires an isolated loopback cluster")
	}
	config.Timeout = 10 * time.Second
	client, err := dynamic.NewForConfig(config)
	requireDirectIntegrationNoError(t, err)
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	requireDirectIntegrationNoError(t, err)
	resources, err := restmapper.GetAPIGroupResources(discoveryClient)
	requireDirectIntegrationNoError(t, err)
	mapper := restmapper.NewDiscoveryRESTMapper(resources)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	namespace := "soha-manifest-test-" + uuid.NewString()[:8]
	namespaces := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"})
	if _, err := namespaces.Create(ctx, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": namespace}}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := namespaces.Delete(cleanup, namespace, metav1.DeleteOptions{}); err != nil {
			t.Error(err)
			return
		}
		if err := wait.PollUntilContextCancel(cleanup, time.Second, true, func(ctx context.Context) (bool, error) {
			_, err := namespaces.Get(ctx, namespace, metav1.GetOptions{})
			return apierrors.IsNotFound(err), nil
		}); err != nil {
			t.Errorf("namespace cleanup: %v", err)
		}
	})
	return ctx, client, mapper, namespace
}
