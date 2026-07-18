package resourcebackend

import (
	"context"
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestBuildWorkloadAssociationsUsesSelectorAndOwner(t *testing.T) {
	t.Parallel()
	ownerUID := types.UID("rs-uid")
	controller := true
	client := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-owned", Namespace: "team-a", Labels: map[string]string{"app": "api"}, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-rs", UID: ownerUID, Controller: &controller}}}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "runtime-data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "runtime-data"}}}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-other", Namespace: "team-a", Labels: map[string]string{"app": "api"}, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "other", UID: "other-uid", Controller: &controller}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-owned", Namespace: "team-a", Labels: map[string]string{"app": "web"}, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-rs", UID: ownerUID, Controller: &controller}}}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "api"}}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a"}, Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "api"}}}}}}}}}},
	)
	template := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api"}},
		Spec: corev1.PodSpec{
			Volumes:    []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "api-data"}}}},
			Containers: []corev1.Container{{Name: "api", EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "api-config"}}}, {SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "api-secret"}}}}}},
		},
	}
	pods, related, err := buildWorkloadAssociations(context.Background(), &k8sinfra.Bundle{Typed: client}, "team-a", labels.Set{"app": "api"}.AsSelector(), ownerUID, "ReplicaSet", template, []metav1.OwnerReference{{Kind: "Deployment", Name: "api"}})
	if err != nil {
		t.Fatalf("buildWorkloadAssociations() error = %v", err)
	}
	if len(pods) != 1 || pods[0].Name != "api-owned" {
		t.Fatalf("pods = %#v, want only api-owned", pods)
	}
	wantRelations := map[string]bool{
		"Deployment/api/owner": true, "Service/api/selected-by-service": true,
		"Ingress/api/routes-service": true, "PersistentVolumeClaim/api-data/volume": true, "PersistentVolumeClaim/runtime-data/volume": true,
		"ConfigMap/api-config/config": true, "Secret/api-secret/secret": true,
	}
	for _, item := range related {
		delete(wantRelations, relationKey(item))
	}
	if len(wantRelations) != 0 {
		t.Fatalf("missing relations = %#v; got %#v", wantRelations, related)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "list" && action.GetResource().Resource == "pods" {
			listAction, ok := action.(clienttesting.ListAction)
			if !ok {
				t.Fatalf("pod action type = %T, want ListAction", action)
			}
			if got := listAction.GetListRestrictions().Labels.String(); got != "app=api" {
				t.Fatalf("pod label selector = %q, want app=api", got)
			}
			return
		}
	}
	t.Fatal("pod list action was not recorded")
}

func TestBuildCronJobAssociationsUsesOwnerUID(t *testing.T) {
	t.Parallel()
	cronUID := types.UID("cron-uid")
	client := fake.NewSimpleClientset(
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "backup-100", Namespace: "team-a", OwnerReferences: []metav1.OwnerReference{{Kind: "CronJob", UID: cronUID}}}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "backup-200", Namespace: "team-a", OwnerReferences: []metav1.OwnerReference{{Kind: "CronJob", UID: "other"}}}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "team-a", OwnerReferences: []metav1.OwnerReference{{Kind: "CronJob", UID: cronUID}}}},
	)
	jobs, _, err := buildCronJobAssociations(context.Background(), &k8sinfra.Bundle{Typed: client}, batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "team-a", UID: cronUID}})
	if err != nil {
		t.Fatalf("buildCronJobAssociations() error = %v", err)
	}
	if len(jobs) != 2 || jobs[0].Name != "unrelated" || jobs[1].Name != "backup-100" {
		t.Fatalf("jobs = %#v, want exact owner UID matches", jobs)
	}
}

func relationKey(item domainresource.WorkloadRelationView) string {
	return item.Kind + "/" + item.Name + "/" + item.Relation
}

func TestReplicaSetAndReplicationControllerDetailMappings(t *testing.T) {
	t.Parallel()
	replicas := int32(3)
	rs := mapReplicaSetDetail(appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "api-rs", Namespace: "team-a"}, Spec: appsv1.ReplicaSetSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}}}})
	rc := mapReplicationControllerDetail(corev1.ReplicationController{ObjectMeta: metav1.ObjectMeta{Name: "api-rc", Namespace: "team-a"}, Spec: corev1.ReplicationControllerSpec{Replicas: &replicas, Selector: map[string]string{"app": "api"}}})
	if rs.DesiredReplicas != replicas || rs.Selector["app"] != "api" || rc.DesiredReplicas != replicas || rc.Selector["app"] != "api" {
		t.Fatalf("detail mappings = rs %#v rc %#v", rs, rc)
	}
}
