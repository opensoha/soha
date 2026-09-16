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
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDeliveryJobReplayIdentityAndArtifact(t *testing.T) {
	ctx := context.Background()
	client := fake.NewClientset()
	backend := NewClusters(fakeClusterManager{bundle: &k8sinfra.Bundle{Typed: client}})
	request := appexecution.ExecutionJobRequest{TaskID: "task:delivery/1", TaskKind: "build", Namespace: "jobs", Commands: []string{"build"}, DefaultImage: "alpine", Retain: true}
	request.Name = appexecution.DeliveryJobName(request.TaskID)
	for range 2 {
		if _, err := backend.CreateExecutionJob(ctx, "cluster", request); err != nil {
			t.Fatal(err)
		}
	}
	jobs, _ := client.BatchV1().Jobs("jobs").List(ctx, metav1.ListOptions{})
	if len(jobs.Items) != 1 {
		t.Fatalf("replay created %d Jobs", len(jobs.Items))
	}
	job := &jobs.Items[0]
	if job.Spec.TTLSecondsAfterFinished != nil || len(validation.IsValidLabelValue(job.Labels["soha.io/execution-task"])) != 0 || job.Annotations["soha.io/execution-task"] != request.TaskID {
		t.Fatalf("invalid retained Job identity: %+v", job.ObjectMeta)
	}
	changed := request
	changed.Commands = []string{"different build"}
	if _, err := backend.CreateExecutionJob(ctx, "cluster", changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("different input adopted historical Job: %v", err)
	}
	job.UID = "owned-job"
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if _, err := client.BatchV1().Jobs("jobs").Update(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "runner", Namespace: "jobs", Labels: map[string]string{"job-name": job.Name}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job"))}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "runner", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Message: "registry/app@" + digest, ExitCode: 0}}}}}}
	if _, err := client.CoreV1().Pods("jobs").Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	ref := appexecution.ExecutionJobRef{ClusterID: "cluster", Namespace: "jobs", Name: job.Name, TaskID: request.TaskID}
	inspection, err := backend.InspectExecutionJob(ctx, ref)
	if err != nil || inspection.State != appexecution.ExecutionJobSucceeded || inspection.ImageDigest != digest {
		t.Fatalf("real termination result: %+v %v", inspection, err)
	}
	ref.TaskID = "different-task"
	if _, err := backend.InspectExecutionJob(ctx, ref); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("foreign Job inspected: %v", err)
	}
}

func TestDeliveryJobCancellationBlocksDelayedCreateAndWaitsForPods(t *testing.T) {
	ctx := context.Background()
	client := fake.NewClientset()
	backend := NewClusters(fakeClusterManager{bundle: &k8sinfra.Bundle{Typed: client}})
	request := appexecution.ExecutionJobRequest{TaskID: "task:cancel-race", TaskKind: "build", Name: appexecution.DeliveryJobName("task:cancel-race"), Namespace: "jobs", Retain: true, Commands: []string{"build"}, DefaultImage: "alpine"}
	inspection, err := backend.StopDeliveryJob(ctx, "cluster", request)
	if err != nil || inspection.State != appexecution.ExecutionJobRunning {
		t.Fatalf("stop before Create: %+v %v", inspection, err)
	}
	if _, err := backend.CreateExecutionJob(ctx, "cluster", request); err != nil {
		t.Fatal(err)
	}
	job, _ := client.BatchV1().Jobs("jobs").Get(ctx, request.Name, metav1.GetOptions{})
	if job.Spec.Suspend == nil || !*job.Spec.Suspend {
		t.Fatal("delayed create unsuspended canceled Job")
	}
	job.UID = "cancel-job"
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobSuspended, Status: corev1.ConditionTrue}}
	if _, err := client.BatchV1().Jobs("jobs").Update(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "active", Namespace: "jobs", Labels: map[string]string{"job-name": job.Name}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job"))}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	if _, err := client.CoreV1().Pods("jobs").Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	inspection, err = backend.StopDeliveryJob(ctx, "cluster", request)
	if err != nil || inspection.State != appexecution.ExecutionJobRunning {
		t.Fatalf("active Pod acknowledged stopped: %+v %v", inspection, err)
	}
	if err := client.CoreV1().Pods("jobs").Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	inspection, err = backend.StopDeliveryJob(ctx, "cluster", request)
	if err != nil || inspection.State != appexecution.ExecutionJobCanceled {
		t.Fatalf("controller and Pods confirmed stop: %+v %v", inspection, err)
	}
}
