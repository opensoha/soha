package executionbackend

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	appexecution "github.com/opensoha/soha/internal/application/execution"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestDeliveryJobWithKubernetes(t *testing.T) {
	configPath := os.Getenv("SOHA_EXECUTION_TEST_KUBECONFIG")
	if configPath == "" {
		t.Skip("set SOHA_EXECUTION_TEST_KUBECONFIG to an isolated local test cluster")
	}
	config, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		t.Fatal(err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	backend := NewClusters(fakeClusterManager{bundle: &k8sinfra.Bundle{Typed: client}})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	namespace := "soha-delivery-job-test-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := client.CoreV1().Namespaces().Delete(cleanup, namespace, metav1.DeleteOptions{}); err != nil {
			t.Errorf("cleanup namespace: %v", err)
		}
	})
	digest := "sha256:" + strings.Repeat("b", 64)
	request := appexecution.ExecutionJobRequest{TaskID: "task:" + uuid.NewString(), TaskKind: "build", Namespace: namespace, Retain: true, DefaultImage: "alpine:3.20", Commands: []string{"printf '%s' '" + digest + "' > .soha-image-digest"}}
	request.Name = appexecution.DeliveryJobName(request.TaskID)
	ref, err := backend.CreateExecutionJob(ctx, "test", request)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := backend.CreateExecutionJob(ctx, "test", request); err != nil || again != ref {
		t.Fatalf("Job replay: %+v %v", again, err)
	}
	awaitDeliveryJob(t, ctx, func() bool {
		inspection, err := backend.InspectExecutionJob(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		if inspection.State == appexecution.ExecutionJobFailed {
			t.Fatalf("fixture Job failed: %+v", inspection)
		}
		if inspection.State != appexecution.ExecutionJobSucceeded {
			return false
		}
		if inspection.ImageDigest != digest {
			t.Fatalf("termination artifact = %q", inspection.ImageDigest)
		}
		return true
	})
	request.TaskID, request.Commands = "task:"+uuid.NewString(), []string{"sleep 300"}
	request.Name = appexecution.DeliveryJobName(request.TaskID)
	if _, err := backend.CreateExecutionJob(ctx, "test", request); err != nil {
		t.Fatal(err)
	}
	awaitDeliveryJob(t, ctx, func() bool {
		job, err := client.BatchV1().Jobs(namespace).Get(ctx, request.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return job.Status.Active > 0
	})
	awaitDeliveryJob(t, ctx, func() bool {
		inspection, err := backend.StopDeliveryJob(ctx, "test", request)
		if err != nil {
			t.Fatal(err)
		}
		return inspection.State == appexecution.ExecutionJobCanceled
	})
	if _, err := backend.CreateExecutionJob(ctx, "test", request); err != nil {
		t.Fatal(err)
	}
	job, err := client.BatchV1().Jobs(namespace).Get(ctx, request.Name, metav1.GetOptions{})
	if err != nil || job.Spec.Suspend == nil || !*job.Spec.Suspend {
		t.Fatalf("late Create resurrected Job: %v", err)
	}
	t.Log("real Kubernetes accepted labels, reused stable identity, returned termination digest and confirmed suspended Job without active Pods")
}

func awaitDeliveryJob(t *testing.T, ctx context.Context, ready func() bool) {
	t.Helper()
	for !ready() {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Second):
		}
	}
}
