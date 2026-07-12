package resourcebackend

import (
	"fmt"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
)

func mapDeployment(item appsv1.Deployment) domainresource.DeploymentView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.DeploymentView{
		Name: item.Name, Namespace: item.Namespace, Labels: item.Labels,
		DesiredReplicas: desired, ReadyReplicas: item.Status.ReadyReplicas,
		UpdatedReplicas: item.Status.UpdatedReplicas, Available: item.Status.AvailableReplicas,
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapDeploymentDetail(item appsv1.Deployment) domainresource.DeploymentDetailView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	containers := make([]domainresource.WorkloadContainerView, 0, len(item.Spec.Template.Spec.Containers))
	for _, container := range item.Spec.Template.Spec.Containers {
		containers = append(containers, domainresource.WorkloadContainerView{Name: container.Name, Image: container.Image})
	}
	conditions := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason,
			Message: condition.Message, LastTransitionTime: condition.LastTransitionTime.Format(time.RFC3339),
		})
	}
	selector := map[string]string(nil)
	if item.Spec.Selector != nil {
		selector = item.Spec.Selector.MatchLabels
	}
	return domainresource.DeploymentDetailView{
		Name: item.Name, Namespace: item.Namespace, DesiredReplicas: desired,
		ReadyReplicas: item.Status.ReadyReplicas, UpdatedReplicas: item.Status.UpdatedReplicas,
		AvailableReplicas: item.Status.AvailableReplicas, ObservedGeneration: item.Status.ObservedGeneration,
		Strategy: string(item.Spec.Strategy.Type), CreatedAt: item.CreationTimestamp.Format(time.RFC3339),
		Labels: item.Labels, Annotations: item.Annotations, Selector: selector,
		Containers: containers, Conditions: conditions,
	}
}

func mapDeploymentRolloutStatus(item appsv1.Deployment) domainresource.DeploymentRolloutStatusView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	status, message := "progressing", "rollout is progressing"
	for _, condition := range item.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue && item.Status.UpdatedReplicas == desired && item.Status.AvailableReplicas == desired {
			status, message = "healthy", "deployment is fully available"
		}
		if condition.Type == appsv1.DeploymentReplicaFailure && condition.Status == corev1.ConditionTrue {
			status, message = "degraded", condition.Message
		}
	}
	conditions := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason,
			Message: condition.Message, LastTransitionTime: condition.LastTransitionTime.Format(time.RFC3339),
		})
	}
	return domainresource.DeploymentRolloutStatusView{
		Name: item.Name, Namespace: item.Namespace,
		Revision: item.Annotations["deployment.kubernetes.io/revision"], Status: status, Message: message,
		DesiredReplicas: desired, UpdatedReplicas: item.Status.UpdatedReplicas,
		ReadyReplicas: item.Status.ReadyReplicas, AvailableReplicas: item.Status.AvailableReplicas,
		ObservedGeneration: item.Status.ObservedGeneration, Conditions: conditions,
	}
}

func mapStatefulSet(item appsv1.StatefulSet) domainresource.StatefulSetView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.StatefulSetView{
		Name: item.Name, Namespace: item.Namespace, ServiceName: item.Spec.ServiceName,
		DesiredReplicas: desired, ReadyReplicas: item.Status.ReadyReplicas,
		CurrentReplicas: item.Status.CurrentReplicas, AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapStatefulSetDetail(item appsv1.StatefulSet) domainresource.StatefulSetDetailView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	selector := map[string]string(nil)
	if item.Spec.Selector != nil {
		selector = item.Spec.Selector.MatchLabels
	}
	return domainresource.StatefulSetDetailView{
		Name: item.Name, Namespace: item.Namespace, ServiceName: item.Spec.ServiceName,
		DesiredReplicas: desired, ReadyReplicas: item.Status.ReadyReplicas,
		CurrentReplicas: item.Status.CurrentReplicas, UpdateStrategy: string(item.Spec.UpdateStrategy.Type),
		CurrentRevision: item.Status.CurrentRevision, UpdateRevision: item.Status.UpdateRevision,
		CreatedAt: item.CreationTimestamp.Format(time.RFC3339), Labels: item.Labels,
		Annotations: item.Annotations, Selector: selector,
	}
}

func mapDaemonSet(item appsv1.DaemonSet) domainresource.DaemonSetView {
	return domainresource.DaemonSetView{
		Name: item.Name, Namespace: item.Namespace,
		DesiredNumber: item.Status.DesiredNumberScheduled, CurrentNumber: item.Status.CurrentNumberScheduled,
		ReadyNumber: item.Status.NumberReady, AvailableNumber: item.Status.NumberAvailable,
		UpdatedNumber: item.Status.UpdatedNumberScheduled, AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapDaemonSetDetail(item appsv1.DaemonSet) domainresource.DaemonSetDetailView {
	selector := map[string]string{}
	if item.Spec.Selector != nil {
		selector = item.Spec.Selector.MatchLabels
	}
	return domainresource.DaemonSetDetailView{
		Name: item.Name, Namespace: item.Namespace,
		DesiredNumber: item.Status.DesiredNumberScheduled, CurrentNumber: item.Status.CurrentNumberScheduled,
		ReadyNumber: item.Status.NumberReady, AvailableNumber: item.Status.NumberAvailable,
		UpdatedNumber: item.Status.UpdatedNumberScheduled, UpdateStrategy: string(item.Spec.UpdateStrategy.Type),
		CreatedAt: item.CreationTimestamp.Format(time.RFC3339), Labels: item.Labels,
		Annotations: item.Annotations, Selector: selector,
	}
}

func mapJob(item batchv1.Job) domainresource.JobView {
	completions := int32(0)
	if item.Spec.Completions != nil {
		completions = *item.Spec.Completions
	}
	completionMode := ""
	if item.Spec.CompletionMode != nil {
		completionMode = string(*item.Spec.CompletionMode)
	}
	return domainresource.JobView{
		Name: item.Name, Namespace: item.Namespace, Completions: completions,
		Succeeded: item.Status.Succeeded, Failed: item.Status.Failed, Active: item.Status.Active,
		CompletionMode: completionMode, AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapJobDetail(item batchv1.Job) domainresource.JobDetailView {
	completions, parallelism := int32(0), int32(1)
	if item.Spec.Completions != nil {
		completions = *item.Spec.Completions
	}
	if item.Spec.Parallelism != nil {
		parallelism = *item.Spec.Parallelism
	}
	completionMode := ""
	if item.Spec.CompletionMode != nil {
		completionMode = string(*item.Spec.CompletionMode)
	}
	startTime, completionTime := "", ""
	if item.Status.StartTime != nil {
		startTime = item.Status.StartTime.Format(time.RFC3339)
	}
	if item.Status.CompletionTime != nil {
		completionTime = item.Status.CompletionTime.Format(time.RFC3339)
	}
	return domainresource.JobDetailView{
		Name: item.Name, Namespace: item.Namespace, Completions: completions, Parallelism: parallelism,
		Succeeded: item.Status.Succeeded, Failed: item.Status.Failed, Active: item.Status.Active,
		CompletionMode: completionMode, CreatedAt: item.CreationTimestamp.Format(time.RFC3339),
		StartTime: startTime, CompletionTime: completionTime, Labels: item.Labels, Annotations: item.Annotations,
	}
}

func mapCronJob(item batchv1.CronJob) domainresource.CronJobView {
	lastScheduleTime := ""
	if item.Status.LastScheduleTime != nil {
		lastScheduleTime = item.Status.LastScheduleTime.Format(time.RFC3339)
	}
	return domainresource.CronJobView{
		Name: item.Name, Namespace: item.Namespace, Schedule: item.Spec.Schedule,
		Suspend: item.Spec.Suspend != nil && *item.Spec.Suspend, ActiveJobs: boundedInt32(len(item.Status.Active)),
		LastScheduleTime: lastScheduleTime, AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapCronJobDetail(item batchv1.CronJob) domainresource.CronJobDetailView {
	lastScheduleTime, timeZone := "", ""
	if item.Status.LastScheduleTime != nil {
		lastScheduleTime = item.Status.LastScheduleTime.Format(time.RFC3339)
	}
	if item.Spec.TimeZone != nil {
		timeZone = *item.Spec.TimeZone
	}
	return domainresource.CronJobDetailView{
		Name: item.Name, Namespace: item.Namespace, Schedule: item.Spec.Schedule,
		Suspend: item.Spec.Suspend != nil && *item.Spec.Suspend, ActiveJobs: boundedInt32(len(item.Status.Active)),
		LastScheduleTime: lastScheduleTime, ConcurrencyPolicy: string(item.Spec.ConcurrencyPolicy),
		TimeZone: timeZone, CreatedAt: item.CreationTimestamp.Format(time.RFC3339),
		Labels: item.Labels, Annotations: item.Annotations,
	}
}

func mapReplicaSet(item appsv1.ReplicaSet) domainresource.ReplicaSetView {
	desired := int32(0)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.ReplicaSetView{
		Name: item.Name, Namespace: item.Namespace, DesiredReplicas: desired,
		ReadyReplicas: item.Status.ReadyReplicas, AvailableReplicas: item.Status.AvailableReplicas,
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapHorizontalPodAutoscaler(item autoscalingv2.HorizontalPodAutoscaler) domainresource.HorizontalPodAutoscalerView {
	minReplicas := int32(1)
	if item.Spec.MinReplicas != nil {
		minReplicas = *item.Spec.MinReplicas
	}
	return domainresource.HorizontalPodAutoscalerView{
		Name: item.Name, Namespace: item.Namespace,
		TargetRef:   fmt.Sprintf("%s/%s", item.Spec.ScaleTargetRef.Kind, item.Spec.ScaleTargetRef.Name),
		MinReplicas: minReplicas, MaxReplicas: item.Spec.MaxReplicas,
		CurrentReplicas: item.Status.CurrentReplicas, DesiredReplicas: item.Status.DesiredReplicas,
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapPodDisruptionBudget(item policyv1.PodDisruptionBudget) domainresource.PodDisruptionBudgetView {
	minAvailable, maxUnavailable := "", ""
	if item.Spec.MinAvailable != nil {
		minAvailable = item.Spec.MinAvailable.String()
	}
	if item.Spec.MaxUnavailable != nil {
		maxUnavailable = item.Spec.MaxUnavailable.String()
	}
	return domainresource.PodDisruptionBudgetView{
		Name: item.Name, Namespace: item.Namespace, MinAvailable: minAvailable,
		MaxUnavailable: maxUnavailable, CurrentHealthy: item.Status.CurrentHealthy,
		DesiredHealthy: item.Status.DesiredHealthy, DisruptionsAllowed: item.Status.DisruptionsAllowed,
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}
