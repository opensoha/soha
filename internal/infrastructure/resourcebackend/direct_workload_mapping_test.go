package resourcebackend

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestWorkloadMappings(t *testing.T) {
	t.Parallel()

	created := metav1.NewTime(time.Now().Add(-90 * time.Minute))
	replicas := int32(3)

	deploymentDetail := mapDeploymentDetail(appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a", CreationTimestamp: created},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Image: "example/api:v1"}}}},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 2, UpdatedReplicas: 3, AvailableReplicas: 2, ObservedGeneration: 5,
			Conditions: []appsv1.DeploymentCondition{{
				Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue,
				Reason: "NewReplicaSetAvailable", Message: "rollout complete", LastTransitionTime: created,
			}},
		},
	})
	if deploymentDetail.DesiredReplicas != 3 || deploymentDetail.ReadyReplicas != 2 || deploymentDetail.Strategy != "RollingUpdate" {
		t.Fatalf("deploymentDetail = %#v", deploymentDetail)
	}
	if len(deploymentDetail.Containers) != 1 || deploymentDetail.Containers[0].Image != "example/api:v1" {
		t.Fatalf("deploymentDetail.Containers = %#v", deploymentDetail.Containers)
	}

	rollout := mapDeploymentRolloutStatus(appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a", Annotations: map[string]string{"deployment.kubernetes.io/revision": "7"}},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			UpdatedReplicas: 3, ReadyReplicas: 3, AvailableReplicas: 3, ObservedGeneration: 5,
			Conditions: []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}},
		},
	})
	if rollout.Status != "healthy" || rollout.Revision != "7" {
		t.Fatalf("rollout = %#v", rollout)
	}

	statefulSet := mapStatefulSetDetail(appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "team-a", CreationTimestamp: created},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas, ServiceName: "db-headless",
			Selector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app": "db"}},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType},
		},
		Status: appsv1.StatefulSetStatus{ReadyReplicas: 2, CurrentReplicas: 3, CurrentRevision: "db-1", UpdateRevision: "db-2"},
	})
	if statefulSet.ServiceName != "db-headless" || statefulSet.CurrentRevision != "db-1" || statefulSet.UpdateRevision != "db-2" {
		t.Fatalf("statefulSet = %#v", statefulSet)
	}

	daemonSet := mapDaemonSet(appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{
		DesiredNumberScheduled: 5, CurrentNumberScheduled: 4, NumberReady: 3,
		NumberAvailable: 2, UpdatedNumberScheduled: 4,
	}})
	if daemonSet.DesiredNumber != 5 || daemonSet.CurrentNumber != 4 || daemonSet.UpdatedNumber != 4 {
		t.Fatalf("daemonSet = %#v", daemonSet)
	}

}

func TestBatchWorkloadMappings(t *testing.T) {
	t.Parallel()
	created := metav1.NewTime(time.Now().Add(-90 * time.Minute))
	parallelism, completions := int32(2), int32(4)
	completionMode := batchv1.IndexedCompletion
	suspend := true
	timeZone := "Asia/Shanghai"
	lastSchedule := metav1.NewTime(time.Date(2026, 6, 4, 1, 2, 3, 0, time.UTC))
	jobDetail := mapJobDetail(batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "migrate", Namespace: "team-a", CreationTimestamp: created},
		Spec:       batchv1.JobSpec{Completions: &completions, Parallelism: &parallelism, CompletionMode: &completionMode},
		Status:     batchv1.JobStatus{Succeeded: 2, Failed: 1, Active: 1, StartTime: &created, CompletionTime: &lastSchedule},
	})
	if jobDetail.Completions != 4 || jobDetail.Parallelism != 2 || jobDetail.CompletionTime != "2026-06-04T01:02:03Z" {
		t.Fatalf("jobDetail = %#v", jobDetail)
	}

	cronJobDetail := mapCronJobDetail(batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "team-a", CreationTimestamp: created},
		Spec: batchv1.CronJobSpec{
			Schedule: "0 1 * * *", Suspend: &suspend,
			ConcurrencyPolicy: batchv1.ForbidConcurrent, TimeZone: &timeZone,
		},
		Status: batchv1.CronJobStatus{
			Active: []corev1.ObjectReference{{Name: "nightly-1"}, {Name: "nightly-2"}}, LastScheduleTime: &lastSchedule,
		},
	})
	if cronJobDetail.ActiveJobs != 2 || cronJobDetail.ConcurrencyPolicy != "Forbid" || cronJobDetail.TimeZone != "Asia/Shanghai" {
		t.Fatalf("cronJobDetail = %#v", cronJobDetail)
	}

	replicas := int32(3)
	replicaSet := mapReplicaSet(appsv1.ReplicaSet{Spec: appsv1.ReplicaSetSpec{Replicas: &replicas}, Status: appsv1.ReplicaSetStatus{ReadyReplicas: 2, AvailableReplicas: 1}})
	if replicaSet.DesiredReplicas != 3 || replicaSet.ReadyReplicas != 2 || replicaSet.AvailableReplicas != 1 {
		t.Fatalf("replicaSet = %#v", replicaSet)
	}

}

func TestPolicyWorkloadMappings(t *testing.T) {
	t.Parallel()
	minReplicas := int32(2)
	hpa := mapHorizontalPodAutoscaler(autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "api"},
			MinReplicas:    &minReplicas, MaxReplicas: 10,
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{CurrentReplicas: 3, DesiredReplicas: 5},
	})
	if hpa.TargetRef != "Deployment/api" || hpa.MinReplicas != 2 || hpa.MaxReplicas != 10 {
		t.Fatalf("hpa = %#v", hpa)
	}

	minAvailable, maxUnavailable := intstr.FromInt32(2), intstr.FromString("25%")
	pdb := mapPodDisruptionBudget(policyv1.PodDisruptionBudget{
		Spec:   policyv1.PodDisruptionBudgetSpec{MinAvailable: &minAvailable, MaxUnavailable: &maxUnavailable},
		Status: policyv1.PodDisruptionBudgetStatus{CurrentHealthy: 3, DesiredHealthy: 2, DisruptionsAllowed: 1},
	})
	if pdb.MinAvailable != "2" || pdb.MaxUnavailable != "25%" || pdb.DisruptionsAllowed != 1 {
		t.Fatalf("pdb = %#v", pdb)
	}
}
