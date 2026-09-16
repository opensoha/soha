package manifestruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

func TestGitOpsCannotFallBackToDirectApply(t *testing.T) {
	mapper := staticRESTMapper{}
	for _, action := range []string{domainmanifest.TaskActionPreflight, domainmanifest.TaskActionApply, domainmanifest.TaskActionObserve, domainmanifest.TaskActionAdopt} {
		client := fake.NewSimpleDynamicClient(runtime.NewScheme())
		payload := domainmanifest.TaskPayload{Action: action, Documents: []domainmanifest.RenderedDocument{{APIVersion: "argoproj.io/v1alpha1", Kind: "Application"}}}
		if _, err := executeGitOpsDocuments(t.Context(), client, mapper, payload); err == nil {
			t.Fatal("unfrozen GitOps task accepted")
		}
		if len(client.Actions()) != 0 {
			t.Fatal("invalid task accessed Kubernetes")
		}
	}
}

func TestGitOpsDirectWithKubernetes(t *testing.T) {
	kubeconfig := os.Getenv("SOHA_ARGOCD_TEST_KUBECONFIG")
	if kubeconfig == "" {
		t.Skip("set explicit loopback kubeconfig and SOHA_ARGOCD_TEST_STATE for the isolated GitOps fixture")
	}
	require := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require(err)
	endpoint, err := url.Parse(config.Host)
	if err != nil || endpoint.Hostname() != "127.0.0.1" {
		t.Fatal("only the loopback fixture cluster is supported")
	}
	config.Timeout = 15 * time.Second
	client, err := dynamic.NewForConfig(config)
	require(err)
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	require(err)
	resources, err := restmapper.GetAPIGroupResources(discoveryClient)
	require(err)
	mapper := restmapper.NewDiscoveryRESTMapper(resources)
	data, err := os.ReadFile(os.Getenv("SOHA_ARGOCD_TEST_STATE"))
	require(err)
	var state struct {
		RepositoryURL string                                  `json:"repositoryURL"`
		Commit        string                                  `json:"v1"`
		Image         string                                  `json:"image"`
		Documents     map[string][]*unstructured.Unstructured `json:"documents"`
	}
	require(json.Unmarshal(data, &state))
	namespace, name := "soha-workflow-r6-gitops", "r6-gitops-core"
	app := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": map[string]any{"name": name, "namespace": namespace}, "spec": map[string]any{"project": "r6-gitops", "destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": namespace}, "source": map[string]any{"repoURL": state.RepositoryURL, "targetRevision": state.Commit, "path": ".", "kustomize": map[string]any{"namespace": namespace, "images": []any{"app=" + state.Image}}}, "syncPolicy": map[string]any{"syncOptions": []any{"FailOnSharedResource=true"}}}}}

	payload := domainmanifest.TaskPayload{Action: domainmanifest.TaskActionPreflight, PackageID: "r6-core", BindingID: "r6-core", DeploymentID: "r6-core", Generation: 1, IdempotencyKey: "r6-core-" + time.Now().UTC().Format("20060102T150405.000000000"), FieldManager: "opensoha-manifest/r6-core", Namespace: namespace, Documents: []domainmanifest.RenderedDocument{gitOpsTestDocument(t, app)}}
	for _, child := range state.Documents["v1"] {
		payload.GitOpsDocuments = append(payload.GitOpsDocuments, gitOpsTestDocument(t, child))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	applications := client.Resource(schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}).Namespace(namespace)
	if _, err := applications.Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("fixture Application already exists")
	}
	t.Cleanup(func() {
		cleanupGitOpsFixture(t, client, applications, namespace, name)
	})
	preflight, err := preflightDocuments(ctx, client, mapper, payload)
	require(err)
	if preflight.Preflight == nil || !preflight.Preflight.Ready || preflight.Preflight.ResourceCount != 3 {
		t.Fatalf("preflight=%+v", preflight)
	}
	if _, err := applications.Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("preflight wrote Application")
	}
	payload.Action = domainmanifest.TaskActionApply
	_, err = applyDocuments(ctx, client, mapper, payload)
	require(err)
	payload.Action = domainmanifest.TaskActionObserve
	require(wait.PollUntilContextTimeout(ctx, time.Second, 45*time.Second, true, gitOpsHealthy(client, mapper, payload)))
	deployments := client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace(namespace)
	live, err := deployments.Get(ctx, "r6-gitops-http", metav1.GetOptions{})
	require(err)
	require(unstructured.SetNestedField(live.Object, int64(2), "spec", "replicas"))
	_, err = deployments.Update(ctx, live, metav1.UpdateOptions{FieldManager: "soha-r6-drift-fixture"})
	require(err)
	result, err := observeDocuments(ctx, client, mapper, payload)
	require(err)
	if result.Drift == nil || !result.Drift.Drifted || len(result.Drift.Resources) == 0 || result.Inventory[0].Health == "healthy" {
		t.Fatal("live child drift was hidden by Application health")
	}
	t.Log("Core preflight wrote nothing; apply/observe returned root and frozen children; live child drift prevented healthy acceptance")
}

func cleanupGitOpsFixture(t *testing.T, client dynamic.Interface, applications dynamic.ResourceInterface, namespace, name string) {
	t.Helper()
	if t.Failed() {
		t.Log("failed isolated GitOps fixture retained")
		return
	}
	cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	_ = applications.Delete(cleanup, name, metav1.DeleteOptions{})
	for _, gvr := range []schema.GroupVersionResource{{Group: "apps", Version: "v1", Resource: "deployments"}, {Version: "v1", Resource: "services"}} {
		_ = client.Resource(gvr).Namespace(namespace).Delete(cleanup, "r6-gitops-http", metav1.DeleteOptions{})
	}
}

func gitOpsHealthy(client dynamic.Interface, mapper restMapper, payload domainmanifest.TaskPayload) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		result, err := observeDocuments(ctx, client, mapper, payload)
		if err != nil {
			return false, err
		}
		if len(result.Inventory) != 3 || len(result.EvidenceRefs) != 1 || result.Drift == nil || result.Drift.Drifted {
			return false, nil
		}
		for _, item := range result.Inventory {
			if item.UID == "" || item.DesiredObjectDigest == "" || item.Health != "healthy" {
				return false, nil
			}
		}
		return true, nil
	}
}

func gitOpsTestDocument(t *testing.T, object *unstructured.Unstructured) domainmanifest.RenderedDocument {
	t.Helper()
	content, err := json.Marshal(object.Object)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	return domainmanifest.RenderedDocument{APIVersion: object.GetAPIVersion(), Kind: object.GetKind(), Namespace: object.GetNamespace(), Name: object.GetName(), Path: strings.ToLower(object.GetKind()) + ".json", Content: string(content), ContentDigest: hex.EncodeToString(digest[:])}
}
