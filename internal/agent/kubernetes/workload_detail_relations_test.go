package kubernetes

import (
	"context"
	"testing"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestWorkloadDetailsIncludeTargetedRelations(t *testing.T) {
	t.Parallel()

	const namespace = "platform"
	controller := true
	template := func(name string) corev1.PodTemplateSpec {
		return corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "app", EnvFrom: []corev1.EnvFromSource{{
					ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: name + "-env"}},
				}}}},
				Volumes: []corev1.Volume{
					{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: name + "-config"}}}},
					{Name: "secret", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: name + "-secret"}}},
					{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: name + "-data"}}},
				},
			},
		}
	}
	selector := func(name string) *metav1.LabelSelector {
		return &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}
	}
	owner := func(kind, name string, uid types.UID) []metav1.OwnerReference {
		return []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: kind, Name: name, UID: uid, Controller: &controller}}
	}
	pod := func(name, app string, owners []metav1.OwnerReference) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{"app": app}, OwnerReferences: owners}}
	}

	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: namespace, OwnerReferences: owner("Application", "shop", "app-uid")}, Spec: appsv1.DeploymentSpec{Selector: selector("web"), Template: template("web")}}
	statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: namespace}, Spec: appsv1.StatefulSetSpec{Selector: selector("db"), Template: template("db")}}
	daemonSet := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: namespace, UID: "ds-uid"}, Spec: appsv1.DaemonSetSpec{Selector: selector("node-agent"), Template: template("node-agent")}}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "migrate", Namespace: namespace, UID: "job-uid", OwnerReferences: owner("CronJob", "nightly", "cron-uid")}, Spec: batchv1.JobSpec{Selector: selector("migrate"), Template: template("migrate")}}
	cronJob := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: namespace, UID: "cron-uid"}, Spec: batchv1.CronJobSpec{JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: template("nightly")}}}}
	observed := metav1.Now()
	ownedJob := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "nightly-1", Namespace: namespace, OwnerReferences: owner("CronJob", "nightly", "cron-uid")}, Status: batchv1.JobStatus{StartTime: &observed, Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: observed}}}}
	unownedJob := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "other-1", Namespace: namespace, OwnerReferences: owner("CronJob", "other", "other-uid")}}
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: namespace}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "web"}}}
	ingress := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: namespace}, Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "web"}}}}}}}}}}

	typed := kubernetesfake.NewSimpleClientset(
		deployment, statefulSet, daemonSet, job, cronJob, ownedJob, unownedJob, service, ingress,
		pod("web-1", "web", nil), pod("db-0", "db", nil), pod("node-agent-1", "node-agent", owner("DaemonSet", "node-agent", "ds-uid")), pod("old-ds-pod", "node-agent", owner("DaemonSet", "node-agent", "old-uid")), pod("unowned-ds-pod", "node-agent", nil),
		pod("migrate-1", "migrate", owner("Job", "migrate", "job-uid")), pod("migrate-wrong-owner", "migrate", owner("Job", "other", "other-job-uid")),
	)
	client := &Client{typed: typed}
	ctx := context.Background()

	deploymentDetail, err := client.GetDeploymentDetail(ctx, namespace, deployment.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(deploymentDetail.Pods) != 1 || !hasWorkloadRelation(deploymentDetail.RelatedResources, "Application", "shop", "owner") ||
		!hasWorkloadRelation(deploymentDetail.RelatedResources, "Service", "web", "selected-by-service") ||
		!hasWorkloadRelation(deploymentDetail.RelatedResources, "Ingress", "web", "routes-service") ||
		!hasWorkloadRelation(deploymentDetail.RelatedResources, "PersistentVolumeClaim", "web-data", "volume") ||
		!hasWorkloadRelation(deploymentDetail.RelatedResources, "ConfigMap", "web-config", "config") ||
		!hasWorkloadRelation(deploymentDetail.RelatedResources, "Secret", "web-secret", "secret") {
		t.Fatalf("deployment detail missing relations: %#v", deploymentDetail)
	}

	statefulSetDetail, err := client.GetStatefulSetDetail(ctx, namespace, statefulSet.Name)
	if err != nil || len(statefulSetDetail.Pods) != 1 {
		t.Fatalf("statefulset detail = %#v, err = %v", statefulSetDetail, err)
	}
	daemonSetDetail, err := client.GetDaemonSetDetail(ctx, namespace, daemonSet.Name)
	if err != nil || len(daemonSetDetail.Pods) != 1 {
		t.Fatalf("daemonset detail = %#v, err = %v", daemonSetDetail, err)
	}
	jobDetail, err := client.GetJobDetail(ctx, namespace, job.Name)
	if err != nil || len(jobDetail.Pods) != 1 || jobDetail.Pods[0].Name != "migrate-1" || !hasWorkloadRelation(jobDetail.RelatedResources, "CronJob", "nightly", "owner") {
		t.Fatalf("job detail = %#v, err = %v", jobDetail, err)
	}
	cronJobDetail, err := client.GetCronJobDetail(ctx, namespace, cronJob.Name)
	if err != nil || len(cronJobDetail.Jobs) != 2 || !hasJob(cronJobDetail.Jobs, ownedJob.Name) || hasJob(cronJobDetail.Jobs, unownedJob.Name) || !hasWorkloadRelation(cronJobDetail.RelatedResources, "Secret", "nightly-secret", "secret") {
		t.Fatalf("cronjob detail = %#v, err = %v", cronJobDetail, err)
	}
	for _, execution := range cronJobDetail.Jobs {
		if execution.Name == ownedJob.Name && (execution.StartTime == "" || len(execution.Conditions) != 1 || execution.Conditions[0].Type != "Complete") {
			t.Fatalf("native execution data missing: %#v", execution)
		}
	}
}

func hasJob(items []domainresource.JobView, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func hasWorkloadRelation(items []domainresource.WorkloadRelationView, kind, name, relation string) bool {
	for _, item := range items {
		if item.Kind == kind && item.Name == name && item.Relation == relation {
			return true
		}
	}
	return false
}
