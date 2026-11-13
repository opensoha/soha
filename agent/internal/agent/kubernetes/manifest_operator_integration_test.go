package kubernetes

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

func TestManifestOperatorWithKubernetes(t *testing.T) {
	path, namespace, image := os.Getenv("SOHA_OPERATOR_TEST_KUBECONFIG"), os.Getenv("SOHA_OPERATOR_TEST_NAMESPACE"), os.Getenv("SOHA_OPERATOR_TEST_IMAGE")
	if path == "" {
		t.Skip("set the isolated Operator fixture kubeconfig, namespace and image")
	}
	if !strings.HasPrefix(namespace, "soha-workflow-r6-") || !strings.Contains(image, "@sha256:") {
		t.Fatal("isolated namespace and pinned image required")
	}
	config, err := clientcmd.BuildConfigFromFlags("", path)
	requireOperatorRuntime(t, err)
	endpoint, err := url.Parse(config.Host)
	if err != nil || endpoint.Hostname() != "127.0.0.1" {
		t.Fatal("only the loopback fixture is supported")
	}
	config.Timeout = 10 * time.Second
	client := &Client{}
	client.dynamic, err = dynamic.NewForConfig(config)
	requireOperatorRuntime(t, err)
	client.discovery, err = discovery.NewDiscoveryClientForConfig(config)
	requireOperatorRuntime(t, err)
	name := "agent-periodic-" + uuid.NewString()[:8]
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, resource := range []schema.GroupVersionResource{
		{Group: "apps", Version: "v1", Resource: "deployments"},
		{Group: "workloads.soha.io", Version: "v1alpha1", Resource: "workloadcronjobs"},
	} {
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			_ = client.dynamic.Resource(resource).Namespace(namespace).Delete(cleanup, name, metav1.DeleteOptions{})
		})
	}
	source := fmt.Sprintf(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":%q,"namespace":%q},"spec":{"replicas":1,"selector":{"matchLabels":{"app":%q}},"template":{"metadata":{"labels":{"app":%q}},"spec":{"containers":[{"name":"main","image":%q,"command":["sleep","3600"]}]}}}}`, name, namespace, name, name, image)
	root := `{"apiVersion":"workloads.soha.io/v1alpha1","kind":"WorkloadCronJob","metadata":{"name":%q,"namespace":%q},"spec":{"sourceRef":{"kind":"Deployment","name":%q,"container":"main"},"targetContainer":"task","cronJobSpec":{"schedule":%q,"suspend":true,"jobTemplate":{"spec":{"template":{"spec":{"restartPolicy":"Never","containers":[{"name":"task","image":%q,"command":["true"]}]}}}}}}}`
	payload := sohaapi.ManifestExecutionTaskPayload{Action: "apply", Generation: 10, Namespace: namespace, FieldManager: "soha-agent-operator-test"}
	for index, schedule := range []string{"0 2 * * *", "0 3 * * *", "0 2 * * *"} {
		payload.Documents = []sohaapi.ManifestRenderedDocument{
			{APIVersion: "apps/v1", Kind: "Deployment", Namespace: namespace, Name: name, Content: source},
			{APIVersion: "workloads.soha.io/v1alpha1", Kind: "WorkloadCronJob", Namespace: namespace, Name: name, Content: fmt.Sprintf(root, name, namespace, name, schedule, image)},
		}
		for i := range payload.Documents {
			payload.Documents[i].ContentDigest = manifestTestContentDigest(payload.Documents[i].Content)
		}
		payload.Action = "preflight"
		result, err := client.ExecuteManifestTask(ctx, payload)
		requireOperatorRuntime(t, err)
		if result.Preflight == nil || !result.Preflight.Ready {
			t.Fatalf("preflight: %+v", result)
		}
		payload.Action = "apply"
		if index == 2 {
			payload.Action = "rollback"
		}
		_, err = client.ExecuteManifestTask(ctx, payload)
		requireOperatorRuntime(t, err)
		payload.Action = "observe"
		requireOperatorRuntime(t, wait.PollUntilContextCancel(ctx, 500*time.Millisecond, true, func(ctx context.Context) (bool, error) {
			result, err = client.ExecuteManifestTask(ctx, payload)
			if err != nil {
				return false, err
			}
			if len(result.Inventory) != 2 || result.Inventory[1].Health != "healthy" {
				return false, nil
			}
			item := result.Inventory[1]
			if item.UID == "" || item.Generation != 10 || item.ResourceGeneration != int64(index+1) || item.ObservedResourceGeneration == nil || *item.ObservedResourceGeneration != item.ResourceGeneration {
				t.Fatalf("Agent lost Operator observation: %+v", item)
			}
			return true, nil
		}))
	}
	stopped, stop := context.WithCancel(ctx)
	stop()
	payload.Action = "apply"
	if _, err := client.ExecuteManifestTask(stopped, payload); err == nil {
		t.Fatal("canceled Agent operation unexpectedly succeeded")
	}
	t.Log("real Agent runtime: preflight, apply, observed generations, upgrade, rollback and canceled execution passed")
}

func requireOperatorRuntime(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
