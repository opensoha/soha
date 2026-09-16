package executionbackend

import (
	"context"
	"fmt"

	appexecution "github.com/opensoha/soha/internal/application/execution"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Clusters) StopDeliveryJob(ctx context.Context, clusterID string, request appexecution.ExecutionJobRequest) (appexecution.ExecutionJobInspection, error) {
	pending := appexecution.ExecutionJobInspection{State: appexecution.ExecutionJobRunning}
	if request.Name == "" || !request.Retain {
		return pending, fmt.Errorf("%w: cancellation requires a retained delivery Job", apperrors.ErrInvalidArgument)
	}
	bundle, err := c.bundle(ctx, clusterID)
	if err != nil {
		return pending, err
	}
	job, err := buildExecutionJob(request)
	if err != nil {
		return pending, err
	}
	if err := ensureNamespaceExists(ctx, bundle, job.Namespace); err != nil {
		return pending, err
	}
	suspend := true
	job.Spec.Suspend = &suspend
	// A suspended Job also covers cancellation before the first Create returns.
	// Its unique name prevents delayed requests from starting fresh Pods.
	created, err := bundle.Typed.BatchV1().Jobs(job.Namespace).Create(ctx, &job, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(err) {
		created, err = bundle.Typed.BatchV1().Jobs(job.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
	}
	if err != nil {
		return pending, err
	}
	if created.Annotations["soha.io/execution-task"] != request.TaskID || created.Annotations["soha.io/execution-request"] != job.Annotations["soha.io/execution-request"] {
		return pending, fmt.Errorf("%w: cannot stop a Job with different execution identity", apperrors.ErrConflict)
	}
	if deliveryJobTerminalState(created) != appexecution.ExecutionJobRunning {
		return c.InspectExecutionJob(ctx, appexecution.ExecutionJobRef{ClusterID: clusterID, Namespace: job.Namespace, Name: job.Name, TaskID: request.TaskID})
	}
	if created.Spec.Suspend == nil || !*created.Spec.Suspend {
		created.Spec.Suspend = &suspend
		if _, err := bundle.Typed.BatchV1().Jobs(job.Namespace).Update(ctx, created, metav1.UpdateOptions{}); err != nil {
			return pending, err
		}
		return pending, nil
	}
	stopped, err := deliveryJobStopped(ctx, bundle, created)
	if stopped {
		return appexecution.ExecutionJobInspection{State: appexecution.ExecutionJobCanceled}, nil
	}
	return pending, err
}

func deliveryJobStopped(ctx context.Context, bundle *k8sinfra.Bundle, job *batchv1.Job) (bool, error) {
	suspended := false
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobSuspended && condition.Status == corev1.ConditionTrue {
			suspended = true
		}
	}
	if !suspended {
		return false, nil
	}
	pods, err := bundle.Typed.CoreV1().Pods(job.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + job.Name})
	if err != nil {
		return false, err
	}
	for _, pod := range pods.Items {
		if metav1.IsControlledBy(&pod, job) && pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
			return false, nil
		}
	}
	return true, nil
}

func deliveryJobTerminalState(job *batchv1.Job) appexecution.ExecutionJobState {
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			return appexecution.ExecutionJobSucceeded
		case batchv1.JobFailed:
			return appexecution.ExecutionJobFailed
		}
	}
	return appexecution.ExecutionJobRunning
}
