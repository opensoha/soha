package resourcebackend

import (
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestBuildResourceGraphConnectsWorkloadNetworkConfigAndEvidence(t *testing.T) {
	controller := true
	replicas := int32(2)
	snapshot := resourceGraphSnapshot{
		deployments: []appsv1.Deployment{{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a", UID: types.UID("deployment-uid")},
			Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}}, Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "api", EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "api-config"}}}, {SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "api-secret"}}}}}},
					Volumes:    []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "api-data"}}}},
				},
			}},
		}},
		replicaSets: []appsv1.ReplicaSet{{ObjectMeta: metav1.ObjectMeta{Name: "api-rs", Namespace: "team-a", UID: types.UID("rs-uid"), OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", UID: types.UID("deployment-uid"), Controller: &controller}}}}},
		pods:        []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "team-a", UID: types.UID("pod-uid"), Labels: map[string]string{"app": "api"}, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-rs", UID: types.UID("rs-uid"), Controller: &controller}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}},
		services:    []corev1.Service{{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "api"}}}},
		ingresses:   []networkingv1.Ingress{{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a"}, Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "api"}}}}}}}}}}},
		hpas:        []autoscalingv2.HorizontalPodAutoscaler{{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a"}, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "api"}}}},
		pdbs:        []policyv1.PodDisruptionBudget{{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a"}, Spec: policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}}}}},
		events:      []corev1.Event{{ObjectMeta: metav1.ObjectMeta{Name: "scheduled", Namespace: "team-a"}, Type: "Normal", Reason: "Scheduled", Message: "Pod scheduled", InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-1", Namespace: "team-a"}}},
	}

	graph, err := buildResourceGraph("cluster-a", "team-a", "Deployment", "api", snapshot)
	if err != nil {
		t.Fatalf("buildResourceGraph() error = %v", err)
	}
	for _, kind := range []string{"Deployment", "ReplicaSet", "Pod", "Service", "Ingress", "ConfigMap", "Secret", "PersistentVolumeClaim", "HorizontalPodAutoscaler", "PodDisruptionBudget"} {
		if !graphHasKind(graph, kind) {
			t.Fatalf("graph nodes do not contain %s: %#v", kind, graph.Nodes)
		}
	}
	if len(graph.Edges) < 9 || len(graph.Evidence) != 1 || graph.Evidence[0].Type != "kubernetes-event" {
		t.Fatalf("graph = %#v", graph)
	}
}

func TestBuildResourceGraphConnectsStandalonePodDependencies(t *testing.T) {
	snapshot := resourceGraphSnapshot{pods: []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "debug", Namespace: "team-a"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "debug", EnvFrom: []corev1.EnvFromSource{
				{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "debug-config"}}},
				{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"}}},
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "debug-secret"}}},
			}}},
			Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "debug-data"}}}},
		},
	}}}

	graph, err := buildResourceGraph("cluster-a", "team-a", "Pod", "debug", snapshot)
	if err != nil {
		t.Fatalf("buildResourceGraph() error = %v", err)
	}
	for _, kind := range []string{"ConfigMap", "Secret", "PersistentVolumeClaim"} {
		if !graphHasKind(graph, kind) {
			t.Fatalf("standalone Pod graph does not contain %s: %#v", kind, graph.Nodes)
		}
	}
	for _, node := range graph.Nodes {
		if node.Resource.Kind == "ConfigMap" && node.Resource.Name == "kube-root-ca.crt" {
			t.Fatalf("standalone Pod graph contains Kubernetes-injected root CA ConfigMap")
		}
	}
}

func graphHasKind(graph domainresource.ResourceGraph, kind string) bool {
	for _, node := range graph.Nodes {
		if node.Resource.Kind == kind {
			return true
		}
	}
	return false
}
