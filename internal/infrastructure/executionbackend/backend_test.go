package executionbackend

import (
	"context"
	"errors"
	"strings"
	"testing"

	appexecution "github.com/opensoha/soha/internal/application/execution"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type fakeClusterManager struct {
	ids      []string
	bundle   *k8sinfra.Bundle
	bundleFn func(context.Context, string) (*k8sinfra.Bundle, error)
}

func (m fakeClusterManager) ClusterIDs() []string {
	return append([]string(nil), m.ids...)
}

func (m fakeClusterManager) Bundle(ctx context.Context, clusterID string) (*k8sinfra.Bundle, error) {
	if m.bundleFn != nil {
		return m.bundleFn(ctx, clusterID)
	}
	return m.bundle, nil
}

func TestClustersCreateExecutionJob(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewClusters(fakeClusterManager{
		ids:    []string{"cluster-a"},
		bundle: &k8sinfra.Bundle{Typed: client},
	})

	ref, err := backend.CreateExecutionJob(context.Background(), "cluster-a", appexecution.ExecutionJobRequest{
		TaskID:          "task:build/1",
		TaskKind:        "build",
		Namespace:       "soha-jobs",
		Commands:        []string{"go test ./..."},
		Runtime:         map[string]any{"image": "golang:1.24", "commandDir": "services/api"},
		Workspace:       map[string]any{"checkout": map[string]any{"repositoryURL": "https://example.invalid/repo.git", "refName": "main"}},
		DefaultImage:    "alpine:3.20",
		DefaultGitImage: "alpine/git:2.47.0",
		TTLSeconds:      120,
	})
	if err != nil {
		t.Fatalf("CreateExecutionJob() error = %v", err)
	}
	if ref.ClusterID != "cluster-a" || ref.Namespace != "soha-jobs" || ref.Name == "" {
		t.Fatalf("CreateExecutionJob() ref = %#v", ref)
	}
	if _, err := client.CoreV1().Namespaces().Get(context.Background(), "soha-jobs", metav1.GetOptions{}); err != nil {
		t.Fatalf("created namespace: %v", err)
	}
	job, err := client.BatchV1().Jobs("soha-jobs").Get(context.Background(), ref.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("created job: %v", err)
	}
	if got := job.Spec.Template.Spec.Containers[0]; got.Image != "golang:1.24" || got.WorkingDir != "/workspace/services/api" || !strings.Contains(got.Command[2], "go test ./...") {
		t.Fatalf("runner container = %#v", got)
	}
	if len(job.Spec.Template.Spec.InitContainers) != 1 || job.Spec.Template.Spec.InitContainers[0].Image != "alpine/git:2.47.0" {
		t.Fatalf("checkout containers = %#v", job.Spec.Template.Spec.InitContainers)
	}
	if job.Spec.TTLSecondsAfterFinished == nil || *job.Spec.TTLSecondsAfterFinished != 120 {
		t.Fatalf("TTLSecondsAfterFinished = %#v", job.Spec.TTLSecondsAfterFinished)
	}
}

func TestClustersInspectExecutionJob(t *testing.T) {
	client := fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "job-a", Namespace: "jobs"},
		Status:     batchv1.JobStatus{Succeeded: 1},
	})
	backend := NewClusters(fakeClusterManager{bundle: &k8sinfra.Bundle{Typed: client}})

	inspection, err := backend.InspectExecutionJob(context.Background(), appexecution.ExecutionJobRef{ClusterID: "cluster-a", Namespace: "jobs", Name: "job-a"})
	if err != nil {
		t.Fatalf("InspectExecutionJob() error = %v", err)
	}
	if inspection.State != appexecution.ExecutionJobSucceeded {
		t.Fatalf("InspectExecutionJob() state = %q", inspection.State)
	}
}

func TestClustersInspectExecutionJobMapsNotFound(t *testing.T) {
	backend := NewClusters(fakeClusterManager{bundle: &k8sinfra.Bundle{Typed: fake.NewSimpleClientset()}})

	_, err := backend.InspectExecutionJob(context.Background(), appexecution.ExecutionJobRef{ClusterID: "cluster-a", Namespace: "jobs", Name: "missing"})
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("InspectExecutionJob() error = %v, want ErrNotFound", err)
	}
}

func TestClustersPropagatesCancellation(t *testing.T) {
	backend := NewClusters(fakeClusterManager{bundleFn: func(ctx context.Context, _ string) (*k8sinfra.Bundle, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := backend.CreateExecutionJob(ctx, "cluster-a", appexecution.ExecutionJobRequest{Namespace: "jobs", Commands: []string{"true"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CreateExecutionJob() error = %v, want context.Canceled", err)
	}
}

func TestClustersDeleteExecutionJobIsIdempotent(t *testing.T) {
	backend := NewClusters(fakeClusterManager{bundle: &k8sinfra.Bundle{Typed: fake.NewSimpleClientset()}})
	if err := backend.DeleteExecutionJob(context.Background(), appexecution.ExecutionJobRef{ClusterID: "cluster-a", Namespace: "jobs", Name: "missing"}); err != nil {
		t.Fatalf("DeleteExecutionJob() missing error = %v", err)
	}
}
