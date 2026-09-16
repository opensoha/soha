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
	corev1 "k8s.io/api/core/v1"
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
		DefaultGitImage: "alpine/git:2.47.2",
		TTLSeconds:      120,
		TimeoutSeconds:  45,
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
	if len(job.Spec.Template.Spec.InitContainers) != 1 || job.Spec.Template.Spec.InitContainers[0].Image != "alpine/git:2.47.2" {
		t.Fatalf("checkout containers = %#v", job.Spec.Template.Spec.InitContainers)
	}
	if job.Spec.TTLSecondsAfterFinished == nil || *job.Spec.TTLSecondsAfterFinished != 120 {
		t.Fatalf("TTLSecondsAfterFinished = %#v", job.Spec.TTLSecondsAfterFinished)
	}
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 45 {
		t.Fatalf("execution deadline = %#v", job.Spec.ActiveDeadlineSeconds)
	}
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "DeadlineExceeded"}}
	if _, err := client.BatchV1().Jobs(job.Namespace).UpdateStatus(context.Background(), job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	inspection, err := backend.InspectExecutionJob(context.Background(), ref)
	if err != nil || inspection.State != appexecution.ExecutionJobFailed || inspection.FailureReason != "DeadlineExceeded" {
		t.Fatalf("deadline result = %#v, %v", inspection, err)
	}
}

func TestBuildExecutionJobChecksOutMultipleRepositories(t *testing.T) {
	job, err := buildExecutionJob(appexecution.ExecutionJobRequest{
		TaskID:    "task:multi-repo",
		TaskKind:  "build",
		Namespace: "jobs",
		Commands:  []string{"make build"},
		Workspace: map[string]any{"checkouts": []any{
			map[string]any{"repositoryURL": "https://example.invalid/api.git", "refType": "commit", "refName": "abc123"},
			map[string]any{"repositoryURL": "https://example.invalid/lib.git", "checkoutPath": "shared/lib", "refType": "tag", "refName": "v1.0.0", "submodules": true},
		}},
		DefaultImage:    "alpine:3.20",
		DefaultGitImage: "alpine/git:2.47.2",
	})
	if err != nil {
		t.Fatalf("buildExecutionJob() error = %v", err)
	}
	if len(job.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("init containers = %#v", job.Spec.Template.Spec.InitContainers)
	}
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 300 {
		t.Fatalf("default execution deadline = %#v", job.Spec.ActiveDeadlineSeconds)
	}
	script := job.Spec.Template.Spec.InitContainers[0].Command[2]
	for _, expected := range []string{
		"git clone 'https://example.invalid/api.git' '/workspace'",
		"git checkout 'abc123'",
		"git clone 'https://example.invalid/lib.git' '/workspace/shared/lib'",
		"git checkout 'tags/v1.0.0'",
		"git submodule update --init --recursive",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("checkout script = %q, missing %q", script, expected)
		}
	}
}

func TestBuildExecutionJobRejectsCheckoutPathTraversal(t *testing.T) {
	for _, checkoutPath := range []string{"../escape", "services/../escape"} {
		_, err := buildExecutionJob(appexecution.ExecutionJobRequest{
			TaskID:    "task:invalid-path",
			TaskKind:  "build",
			Namespace: "jobs",
			Commands:  []string{"true"},
			Workspace: map[string]any{"checkout": map[string]any{
				"repositoryURL": "https://example.invalid/api.git",
				"checkoutPath":  checkoutPath,
			}},
		})
		if !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("buildExecutionJob(%q) error = %v, want ErrInvalidArgument", checkoutPath, err)
		}
	}
}

func TestBuildExecutionJobRejectsCommandDirPathTraversal(t *testing.T) {
	for _, commandDir := range []string{"../escape", "services/../../escape", `services\..\..\escape`} {
		_, err := buildExecutionJob(appexecution.ExecutionJobRequest{
			TaskID:    "task:invalid-command-dir",
			TaskKind:  "build",
			Namespace: "jobs",
			Commands:  []string{"true"},
			Runtime:   map[string]any{"commandDir": commandDir},
		})
		if !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("buildExecutionJob(%q) error = %v, want ErrInvalidArgument", commandDir, err)
		}
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
