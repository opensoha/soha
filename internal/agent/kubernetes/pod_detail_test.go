package kubernetes

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
)

func TestBuildPodDetailIncludesVolumesAndRelatedResources(t *testing.T) {
	deploymentUID := types.UID("deployment-uid")
	replicaSetUID := types.UID("replicaset-uid")

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-pod",
			Namespace: "monitoring",
			Labels: map[string]string{
				"app": "demo",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: "demo-rs",
					UID:  replicaSetUID,
				},
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "demo-sa",
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "demo:1.0",
					VolumeMounts: []corev1.VolumeMount{
						{Name: "config-volume", MountPath: "/etc/config"},
						{Name: "data", MountPath: "/data", ReadOnly: true},
					},
					EnvFrom: []corev1.EnvFromSource{
						{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "env-config"}}},
					},
					Env: []corev1.EnvVar{
						{
							Name: "TOKEN",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "env-secret"},
									Key:                  "token",
								},
							},
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "config-volume",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "demo-config"},
						},
					},
				},
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "demo-pvc",
							ReadOnly:  true,
						},
					},
				},
				{
					Name: "projected",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							Sources: []corev1.VolumeProjection{
								{Secret: &corev1.SecretProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "projected-secret"}}},
							},
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					Ready:        true,
					RestartCount: 1,
					State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			},
		},
	}

	service := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-svc",
			Namespace: "monitoring",
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app": "demo"},
		},
	}

	ingress := networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-ing",
			Namespace: "monitoring",
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "demo.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{Name: "demo-svc"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	replicaSet := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-rs",
			Namespace: "monitoring",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "Deployment",
					Name: "demo-deploy",
					UID:  deploymentUID,
				},
			},
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "demo"},
			},
		},
	}

	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-deploy",
			Namespace: "monitoring",
			UID:       deploymentUID,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "demo"},
			},
		},
	}

	client := &Client{
		typed: k8sfake.NewSimpleClientset(&pod, &service, &ingress, &replicaSet, &deployment),
	}

	detail := client.buildPodDetail(context.Background(), pod)
	if len(detail.Volumes) != 3 {
		t.Fatalf("volume count = %d, want 3", len(detail.Volumes))
	}

	configVolume := findVolume(detail.Volumes, "config-volume")
	if configVolume == nil {
		t.Fatalf("config volume missing: %#v", detail.Volumes)
	}
	if configVolume.Type != "ConfigMap" || configVolume.SourceName != "demo-config" {
		t.Fatalf("config volume = %#v, want ConfigMap/demo-config", configVolume)
	}
	if len(configVolume.VolumeMounts) != 1 || configVolume.VolumeMounts[0].MountPath != "/etc/config" {
		t.Fatalf("config volume mounts = %#v, want /etc/config", configVolume.VolumeMounts)
	}

	dataVolume := findVolume(detail.Volumes, "data")
	if dataVolume == nil || !dataVolume.ReadOnly {
		t.Fatalf("data volume = %#v, want readonly pvc volume", dataVolume)
	}

	if findRelatedResource(detail.RelatedResources, "ServiceAccount", "demo-sa") == nil {
		t.Fatalf("service account relation missing: %#v", detail.RelatedResources)
	}
	if findRelatedResource(detail.RelatedResources, "ConfigMap", "demo-config") == nil {
		t.Fatalf("configmap relation missing: %#v", detail.RelatedResources)
	}
	if findRelatedResource(detail.RelatedResources, "ConfigMap", "env-config") == nil {
		t.Fatalf("env configmap relation missing: %#v", detail.RelatedResources)
	}
	if findRelatedResource(detail.RelatedResources, "Secret", "env-secret") == nil {
		t.Fatalf("env secret relation missing: %#v", detail.RelatedResources)
	}
	if findRelatedResource(detail.RelatedResources, "Secret", "projected-secret") == nil {
		t.Fatalf("projected secret relation missing: %#v", detail.RelatedResources)
	}
	if findRelatedResource(detail.RelatedResources, "PersistentVolumeClaim", "demo-pvc") == nil {
		t.Fatalf("pvc relation missing: %#v", detail.RelatedResources)
	}
	if findRelatedResource(detail.RelatedResources, "Service", "demo-svc") == nil {
		t.Fatalf("service relation missing: %#v", detail.RelatedResources)
	}
	if findRelatedResource(detail.RelatedResources, "Ingress", "demo-ing") == nil {
		t.Fatalf("ingress relation missing: %#v", detail.RelatedResources)
	}
	if findRelatedResource(detail.RelatedResources, "ReplicaSet", "demo-rs") == nil {
		t.Fatalf("replicaset relation missing: %#v", detail.RelatedResources)
	}
	if findRelatedResource(detail.RelatedResources, "Deployment", "demo-deploy") == nil {
		t.Fatalf("deployment relation missing: %#v", detail.RelatedResources)
	}
}

func findVolume(items []domainresource.PodVolumeView, name string) *domainresource.PodVolumeView {
	for index := range items {
		if items[index].Name == name {
			return &items[index]
		}
	}
	return nil
}

func findRelatedResource(items []domainresource.PodRelatedResourceView, kind, name string) *domainresource.PodRelatedResourceView {
	for index := range items {
		if items[index].Kind == kind && items[index].Name == name {
			return &items[index]
		}
	}
	return nil
}
