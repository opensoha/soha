package kubernetes

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

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/clientcmd"
)

func TestGitOpsAgentCannotFallBackToDirectApply(t *testing.T) {
	var mapper manifestRESTMapper
	for _, action := range []sohaapi.ManifestTaskAction{"preflight", "apply", "observe", "adopt"} {
		client := fake.NewSimpleDynamicClient(runtime.NewScheme())
		payload := sohaapi.ManifestExecutionTaskPayload{Action: action, Documents: []sohaapi.ManifestRenderedDocument{{APIVersion: "argoproj.io/v1alpha1", Kind: "Application"}}}
		if _, err := (&Client{dynamic: client}).executeGitOpsManifest(t.Context(), mapper, payload); err == nil {
			t.Fatal("unfrozen GitOps task accepted")
		}
		if len(client.Actions()) != 0 {
			t.Fatal("invalid task accessed Kubernetes")
		}
	}
}

func TestGitOpsAgentWithKubernetes(t *testing.T) {
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
	dynamicClient, err := dynamic.NewForConfig(config)
	require(err)
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	require(err)
	client := &Client{dynamic: dynamicClient, discovery: discoveryClient}
	data, err := os.ReadFile(os.Getenv("SOHA_ARGOCD_TEST_STATE"))
	require(err)
	var state struct {
		RepositoryURL string                                  `json:"repositoryURL"`
		Commit        string                                  `json:"v1"`
		Image         string                                  `json:"image"`
		Documents     map[string][]*unstructured.Unstructured `json:"documents"`
	}
	require(json.Unmarshal(data, &state))
	namespace, name := "soha-workflow-r6-gitops", "r6-gitops-agent"
	app := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": map[string]any{"name": name, "namespace": namespace}, "spec": map[string]any{"project": "r6-gitops", "destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": namespace}, "source": map[string]any{"repoURL": state.RepositoryURL, "targetRevision": state.Commit, "path": ".", "kustomize": map[string]any{"namespace": namespace, "images": []any{"app=" + state.Image}}}, "syncPolicy": map[string]any{"syncOptions": []any{"FailOnSharedResource=true"}}}}}
	document := func(object *unstructured.Unstructured) sohaapi.ManifestRenderedDocument {
		content, err := json.Marshal(object.Object)
		require(err)
		digest := sha256.Sum256(content)
		return sohaapi.ManifestRenderedDocument{APIVersion: object.GetAPIVersion(), Kind: object.GetKind(), Namespace: object.GetNamespace(), Name: object.GetName(), Path: strings.ToLower(object.GetKind()) + ".json", Content: string(content), ContentDigest: hex.EncodeToString(digest[:])}
	}
	payload := sohaapi.ManifestExecutionTaskPayload{Action: "preflight", PackageID: "r6-agent", BindingID: "r6-agent", DeploymentID: "r6-agent", Generation: 1, IdempotencyKey: "r6-agent-" + time.Now().UTC().Format("20060102T150405.000000000"), FieldManager: "opensoha-manifest/r6-agent", Namespace: namespace, Documents: []sohaapi.ManifestRenderedDocument{document(app)}}
	for _, child := range state.Documents["v1"] {
		payload.GitOpsDocuments = append(payload.GitOpsDocuments, document(child))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	applications := dynamicClient.Resource(schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}).Namespace(namespace)
	if _, err := applications.Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("fixture Application already exists")
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Log("failed isolated GitOps fixture retained")
			return
		}
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		_ = applications.Delete(cleanup, name, metav1.DeleteOptions{})
		for _, gvr := range []schema.GroupVersionResource{{Group: "apps", Version: "v1", Resource: "deployments"}, {Version: "v1", Resource: "services"}} {
			_ = dynamicClient.Resource(gvr).Namespace(namespace).Delete(cleanup, "r6-gitops-http", metav1.DeleteOptions{})
		}
	})
	preflight, err := client.ExecuteManifestTask(ctx, payload)
	require(err)
	if preflight.Preflight == nil || !preflight.Preflight.Ready || preflight.Preflight.ResourceCount != 3 {
		t.Fatalf("preflight=%+v", preflight)
	}
	if _, err := applications.Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("preflight wrote Application")
	}
	payload.Action = "apply"
	_, err = client.ExecuteManifestTask(ctx, payload)
	require(err)
	payload.Action = "observe"
	require(wait.PollUntilContextTimeout(ctx, time.Second, 45*time.Second, true, func(ctx context.Context) (bool, error) {
		result, err := client.ExecuteManifestTask(ctx, payload)
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
	}))
	deployments := dynamicClient.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace(namespace)
	live, err := deployments.Get(ctx, "r6-gitops-http", metav1.GetOptions{})
	require(err)
	require(unstructured.SetNestedField(live.Object, int64(2), "spec", "replicas"))
	_, err = deployments.Update(ctx, live, metav1.UpdateOptions{FieldManager: "soha-r6-drift-fixture"})
	require(err)
	result, err := client.ExecuteManifestTask(ctx, payload)
	require(err)
	if result.Drift == nil || !result.Drift.Drifted || len(result.Drift.Resources) == 0 || result.Inventory[0].Health == "healthy" {
		t.Fatal("live child drift was hidden by Application health")
	}
	t.Log("Agent preflight wrote nothing; apply/observe returned root and frozen children; live child drift prevented healthy acceptance")
}
