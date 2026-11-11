package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestArgoCDWithKubernetes(t *testing.T) {
	kubeconfig := os.Getenv("SOHA_ARGOCD_TEST_KUBECONFIG")
	if kubeconfig == "" {
		t.Skip("set explicit loopback kubeconfig and SOHA_ARGOCD_TEST_STATE for the private GitOps fixture")
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	operatorRequire(t, err)
	endpoint, err := url.Parse(config.Host)
	if err != nil || endpoint.Hostname() != "127.0.0.1" {
		t.Fatal("only the loopback fixture cluster is supported")
	}
	config.Timeout = 15 * time.Second
	client, err := dynamic.NewForConfig(config)
	operatorRequire(t, err)
	kube, err := kubernetes.NewForConfig(config)
	operatorRequire(t, err)
	data, err := os.ReadFile(os.Getenv("SOHA_ARGOCD_TEST_STATE")) // #nosec G703 -- Opt-in integration test reads the operator-selected local fixture path; no server request controls it.
	operatorRequire(t, err)
	var state map[string]json.RawMessage
	operatorRequire(t, json.Unmarshal(data, &state))
	value := func(key string) string {
		var value string
		operatorRequire(t, json.Unmarshal(state[key], &value))
		return value
	}
	namespace, name := "soha-workflow-r6-gitops", "r6-gitops-runtime"
	app := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": map[string]any{"name": name, "namespace": namespace}, "spec": map[string]any{"project": "r6-gitops", "source": map[string]any{"repoURL": value("repositoryURL"), "path": ".", "targetRevision": value("v1"), "kustomize": map[string]any{"namespace": namespace, "images": []any{"app=" + value("image")}}}, "destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": namespace}, "syncPolicy": map[string]any{"syncOptions": []any{"FailOnSharedResource=true"}}}}}
	children := func(version string) []*unstructured.Unstructured {
		var documents map[string][]*unstructured.Unstructured
		operatorRequire(t, json.Unmarshal(state["documents"], &documents))
		if len(documents[version]) != 2 {
			t.Fatal("frozen fixture requires two source-derived child documents")
		}
		return documents[version]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	resources := client.Resource(argoApplications).Namespace(namespace)
	options := metav1.PatchOptions{FieldManager: "soha-r6-gitops-integration"}
	id := uuid.NewString()
	preflight := options
	preflight.DryRun = []string{metav1.DryRunAll}
	_, err = ApplyArgoApplication(ctx, client, app, children("v1"), preflight, id+":preflight")
	operatorRequire(t, err)
	if _, err = resources.Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("preflight created an Application or fixture already exists")
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Log("failed fixture retained for diagnosis in its isolated namespace")
			return
		}
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		_ = resources.Delete(cleanup, name, metav1.DeleteOptions{})
		for _, gvr := range []schema.GroupVersionResource{{Group: "apps", Version: "v1", Resource: "deployments"}, {Version: "v1", Resource: "services"}} {
			_ = client.Resource(gvr).Namespace(namespace).Delete(cleanup, "r6-gitops-http", metav1.DeleteOptions{})
		}
	})
	apply := func(version, operation string) {
		operatorRequire(t, unstructured.SetNestedField(app.Object, value(version), "spec", "source", "targetRevision"))
		live, err := ApplyArgoApplication(ctx, client, app, children(version), options, id+operation)
		if err != nil {
			var status any
			if live != nil {
				status = live.Object["status"]
			}
			t.Fatalf("sync %s: %v; status=%v", version, err, status)
		}
		err = wait.PollUntilContextTimeout(ctx, time.Second, 45*time.Second, true, func(ctx context.Context) (bool, error) {
			live, err := resources.Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			observed, err := ObserveArgoApplication(ctx, client, app, live, children(version), options.FieldManager)
			if err != nil {
				return false, err
			}
			return observed.Health == "healthy" && observed.OperationID == id+operation && observed.Revision == value(version), nil
		})
		operatorRequire(t, err)
		body, err := kube.CoreV1().RESTClient().Get().Namespace(namespace).Resource("services").Name("r6-gitops-http:8080").SubResource("proxy").DoRaw(ctx)
		if err != nil || strings.TrimSpace(string(body)) != "gitops-"+version {
			t.Fatalf("actual HTTP %s: %q %v", version, body, err)
		}
		t.Logf("Argo operation %s applied commit %s with owned, healthy resources and actual HTTP %s", operation, value(version), version)
	}
	apply("v1", ":deploy")
	apply("v2", ":upgrade")
	apply("v1", ":rollback")
	operatorRequire(t, unstructured.SetNestedField(app.Object, value("hold"), "spec", "source", "targetRevision"))
	stopping, stop := context.WithCancel(ctx)
	observedHold := make(chan error, 1)
	go func() {
		err := wait.PollUntilContextTimeout(ctx, time.Second, 45*time.Second, true, func(ctx context.Context) (bool, error) {
			live, err := resources.Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			return argoOperationID(live, "status", "operationState", "operation") == id+":cancel" && argoString(live, "status", "operationState", "phase") == "Running", nil
		})
		observedHold <- err
		stop()
	}()
	live, err := ApplyArgoApplication(stopping, client, app, children("hold"), options, id+":cancel")
	operatorRequire(t, <-observedHold)
	if !errors.Is(err, context.Canceled) || live == nil || argoOperationActive(live) {
		t.Fatalf("cancel was not confirmed: %v", err)
	}
	t.Log("controller confirmed cancellation; restoring through a new operation")
	apply("v1", ":restore")
	// The direct writer cannot adopt a resource managed by Argo CD, even with force.
	deployment, err := client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace(namespace).Get(ctx, "r6-gitops-http", metav1.GetOptions{})
	operatorRequire(t, err)
	if ValidateDirectManifestOwner(deployment) == nil {
		t.Fatal("direct writer accepted Argo CD-owned resource")
	}
	t.Logf("real Argo CD GitOps lifecycle passed for %s/%s", namespace, name)
}
