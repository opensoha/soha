package kubernetes

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
)

func TestGetReplicaSetDetailAggregatesSelectedResources(t *testing.T) {
	replicas := int32(2)
	objects := []runtime.Object{
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: "web-rs", Namespace: "demo", UID: types.UID("web-rs-uid"), Labels: map[string]string{"release": "stable"}, OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "web"}}},
			Spec: appsv1.ReplicaSetSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
				Template: controllerTestTemplate(),
			},
			Status: appsv1.ReplicaSetStatus{ReadyReplicas: 1, AvailableReplicas: 1},
		},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "demo", Labels: map[string]string{"app": "web"}, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "web-rs", UID: types.UID("web-rs-uid")}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "same-label-other-owner", Namespace: "demo", Labels: map[string]string{"app": "web"}, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "other", UID: types.UID("other-rs-uid")}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other-1", Namespace: "demo", Labels: map[string]string{"app": "other"}}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "web-svc", Namespace: "demo"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "web"}}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "web-ing", Namespace: "demo"}, Spec: networkingv1.IngressSpec{DefaultBackend: &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "web-svc"}}}},
	}
	client := &Client{typed: fake.NewSimpleClientset(objects...)}

	detail, err := client.GetReplicaSetDetail(context.Background(), "demo", "web-rs")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Pods) != 1 || detail.Pods[0].Name != "web-1" {
		t.Fatalf("pods = %#v", detail.Pods)
	}
	for _, want := range [][3]string{
		{"Deployment", "web", "owner"},
		{"Service", "web-svc", "selected-by-service"},
		{"Ingress", "web-ing", "routes-service"},
		{"PersistentVolumeClaim", "web-data", "volume"},
		{"ConfigMap", "web-config", "config"},
		{"Secret", "web-secret", "secret"},
	} {
		if !hasControllerRelation(detail.RelatedResources, want[0], want[1], want[2]) {
			t.Errorf("missing relation %v in %#v", want, detail.RelatedResources)
		}
	}
}

func TestGetReplicationControllerDetailUsesSelector(t *testing.T) {
	replicas := int32(1)
	controller := &corev1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{Name: "legacy", Namespace: "demo", UID: types.UID("legacy-uid")},
		Spec: corev1.ReplicationControllerSpec{
			Replicas: &replicas, Selector: map[string]string{"app": "legacy"},
			Template: &corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "legacy"}}},
		},
		Status: corev1.ReplicationControllerStatus{Replicas: 1, ReadyReplicas: 1, AvailableReplicas: 1},
	}
	client := &Client{typed: fake.NewSimpleClientset(
		controller,
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "legacy-1", Namespace: "demo", Labels: map[string]string{"app": "legacy"}, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicationController", Name: "legacy", UID: types.UID("legacy-uid")}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other-1", Namespace: "demo", Labels: map[string]string{"app": "other"}}},
	)}

	detail, err := client.GetReplicationControllerDetail(context.Background(), "demo", "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Pods) != 1 || detail.Pods[0].Name != "legacy-1" || detail.CurrentReplicas != 1 {
		t.Fatalf("detail = %#v", detail)
	}
}

func controllerTestTemplate() corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "web"}},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "web", EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "web-config"}}}, {SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "web-secret"}}}}}},
			Volumes:    []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "web-data"}}}},
		},
	}
}

func hasControllerRelation(items []domainresource.WorkloadRelationView, kind, name, relation string) bool {
	for _, item := range items {
		if item.Kind == kind && item.Name == name && item.Relation == relation {
			return true
		}
	}
	return false
}
