package runtime

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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

func TestWorkloadCronJobWithKubernetes(t *testing.T) {
	path, namespace, image := os.Getenv("SOHA_OPERATOR_TEST_KUBECONFIG"), os.Getenv("SOHA_OPERATOR_TEST_NAMESPACE"), os.Getenv("SOHA_OPERATOR_TEST_IMAGE")
	if path == "" {
		t.Skip("set SOHA_OPERATOR_TEST_KUBECONFIG, namespace and digest-pinned image for the isolated controller fixture")
	}
	if !strings.HasPrefix(namespace, "soha-workflow-r6-") || !strings.Contains(image, "@sha256:") {
		t.Fatal("isolated namespace and pinned image are required")
	}
	config, err := clientcmd.BuildConfigFromFlags("", path)
	operatorRequire(t, err)
	endpoint, err := url.Parse(config.Host)
	if err != nil || endpoint.Hostname() != "127.0.0.1" {
		t.Fatal("only the loopback integration cluster is supported")
	}
	config.Timeout = 10 * time.Second
	client, err := dynamic.NewForConfig(config)
	operatorRequire(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := "periodic-" + uuid.NewString()[:8]
	deployments := client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace(namespace)
	roots := client.Resource(schema.GroupVersionResource{Group: "workloads.soha.io", Version: "v1alpha1", Resource: "workloadcronjobs"}).Namespace(namespace)
	targets := client.Resource(schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}).Namespace(namespace)
	source, root := operatorIntegrationObjects(t, namespace, name, image)
	options := metav1.PatchOptions{FieldManager: "soha-operator-integration"}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = deployments.Delete(cleanup, name, metav1.DeleteOptions{})
	})
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = roots.Delete(cleanup, name, metav1.DeleteOptions{})
	})
	_, err = ApplyManifest(ctx, deployments, source, options)
	operatorRequire(t, err)
	_, err = ApplyManifest(ctx, roots, root, options)
	operatorRequire(t, err)
	live := operatorWaitHealth(t, ctx, client, roots, name, "healthy")
	target, err := targets.Get(ctx, name, metav1.GetOptions{})
	operatorRequire(t, err)
	if metav1.GetControllerOf(target) == nil || metav1.GetControllerOf(target).UID != live.GetUID() {
		t.Fatal("target ownership missing")
	}
	// This task is deliberately suspended; synchronization does not imply a Job ran.
	jobs, err := client.Resource(schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}).Namespace(namespace).List(ctx, metav1.ListOptions{})
	operatorRequire(t, err)
	for _, job := range jobs.Items {
		if owner := metav1.GetControllerOf(&job); owner != nil && owner.UID == target.GetUID() {
			t.Fatal("suspended fixture unexpectedly ran a Job")
		}
	}
	for _, schedule := range []string{"0 3 * * *", "0 2 * * *"} {
		operatorRequire(t, unstructured.SetNestedField(root.Object, schedule, "spec", "cronJobSpec", "schedule"))
		_, err = ApplyManifest(ctx, roots, root, options)
		operatorRequire(t, err)
		operatorWaitHealth(t, ctx, client, roots, name, "healthy")
		target, err = targets.Get(ctx, name, metav1.GetOptions{})
		operatorRequire(t, err)
		actual, _, _ := unstructured.NestedString(target.Object, "spec", "schedule")
		if actual != schedule {
			t.Fatalf("schedule = %s, want %s", actual, schedule)
		}
	}
	// Even force must not write the Operator-owned child directly.
	yes := true
	if _, err := ApplyManifest(ctx, targets, target, metav1.PatchOptions{FieldManager: "other-delivery", Force: &yes, DryRun: []string{metav1.DryRunAll}}); err == nil {
		t.Fatal("second writer was accepted")
	}
	// A missing source must degrade, and recreating it must recover with a new UID.
	operatorRequire(t, deployments.Delete(ctx, name, metav1.DeleteOptions{}))
	operatorWaitHealth(t, ctx, client, roots, name, "degraded")
	_, err = ApplyManifest(ctx, deployments, source, options)
	operatorRequire(t, err)
	operatorWaitHealth(t, ctx, client, roots, name, "healthy")
	operatorVerifyDeletion(t, ctx, client, roots, targets, name)
	t.Log("real Operator: owned CronJob, generation/version observation, update/rollback, second-writer rejection, source-loss recovery and finalizer/foreground deletion passed")
}

func operatorWaitHealth(t *testing.T, ctx context.Context, client dynamic.Interface, roots dynamic.ResourceInterface, name, want string) *unstructured.Unstructured {
	t.Helper()
	var live *unstructured.Unstructured
	var health string
	err := wait.PollUntilContextCancel(ctx, 500*time.Millisecond, true, func(ctx context.Context) (bool, error) {
		var err error
		live, err = roots.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		health, err = ManifestHealth(ctx, client, live)
		return health == want, err
	})
	if err != nil {
		t.Fatalf("Operator health = %s, want %s: %v", health, want, err)
	}
	return live
}

func operatorVerifyDeletion(t *testing.T, ctx context.Context, client dynamic.Interface, roots, targets dynamic.ResourceInterface, name string) {
	t.Helper()
	_, err := roots.Patch(ctx, name, types.MergePatchType, []byte(`{"metadata":{"finalizers":["integration.soha.io/hold"]}}`), metav1.PatchOptions{})
	operatorRequire(t, err)
	if err := DeleteManifest(ctx, roots, name, "old-root-uid"); !apierrors.IsConflict(err) {
		t.Fatalf("replacement fence: %v", err)
	}
	operatorRequire(t, DeleteManifest(ctx, roots, name, ""))
	live, err := roots.Get(ctx, name, metav1.GetOptions{})
	operatorRequire(t, err)
	if live.GetDeletionTimestamp() == nil || len(live.GetFinalizers()) == 0 {
		t.Fatal("finalizer did not preserve deleting state")
	}
	health, err := ManifestHealth(ctx, client, live)
	operatorRequire(t, err)
	if health != "degraded" {
		t.Fatal("deleting object was healthy")
	}
	if _, err := ApplyManifest(ctx, roots, live, metav1.PatchOptions{FieldManager: "soha-operator-integration"}); err == nil {
		t.Fatal("deleting root accepted a new write")
	}
	// Only the fixture removes its own hold finalizer. Production never does this.
	operatorRequire(t, wait.PollUntilContextCancel(ctx, 500*time.Millisecond, true, func(ctx context.Context) (bool, error) {
		_, err := targets.Get(ctx, name, metav1.GetOptions{})
		return apierrors.IsNotFound(err), nil
	}))
	_, err = roots.Patch(ctx, name, types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`), metav1.PatchOptions{})
	operatorRequire(t, err)
	operatorRequire(t, wait.PollUntilContextCancel(ctx, 500*time.Millisecond, true, func(ctx context.Context) (bool, error) {
		_, err := roots.Get(ctx, name, metav1.GetOptions{})
		return apierrors.IsNotFound(err), nil
	}))
}

func operatorIntegrationObjects(t *testing.T, namespace, name, image string) (*unstructured.Unstructured, *unstructured.Unstructured) {
	t.Helper()
	decode := func(raw string) *unstructured.Unstructured {
		var object map[string]any
		operatorRequire(t, json.Unmarshal([]byte(raw), &object))
		return &unstructured.Unstructured{Object: object}
	}
	source := decode(fmt.Sprintf(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":%q,"namespace":%q},"spec":{"replicas":1,"selector":{"matchLabels":{"app":%q}},"template":{"metadata":{"labels":{"app":%q}},"spec":{"containers":[{"name":"app","image":%q,"command":["sleep","3600"]}]}}}}`, name, namespace, name, name, image))
	root := decode(fmt.Sprintf(`{"apiVersion":"workloads.soha.io/v1alpha1","kind":"WorkloadCronJob","metadata":{"name":%q,"namespace":%q},"spec":{"sourceRef":{"kind":"Deployment","name":%q,"container":"app"},"targetContainer":"task","cronJobSpec":{"schedule":"0 2 * * *","suspend":true,"jobTemplate":{"spec":{"template":{"spec":{"restartPolicy":"Never","containers":[{"name":"task","image":%q,"command":["true"]}]}}}}}}}`, name, namespace, name, image))
	return source, root
}

func operatorRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
