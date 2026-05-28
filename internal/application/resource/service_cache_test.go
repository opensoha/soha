package resource

import (
	"testing"

	domainaccess "github.com/soha/soha/internal/domain/access"
	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
)

func TestShouldUseInformerCache(t *testing.T) {
	if shouldUseInformerCache("") {
		t.Fatalf("shouldUseInformerCache(\"\") = true, want false")
	}
	if shouldUseInformerCache("   ") {
		t.Fatalf("shouldUseInformerCache(blank) = true, want false")
	}
	if !shouldUseInformerCache("erp-front") {
		t.Fatalf("shouldUseInformerCache(namespace) = false, want true")
	}
}

func TestShouldPopulatePodUsageSummaries(t *testing.T) {
	if shouldPopulatePodUsageSummaries("") {
		t.Fatalf("shouldPopulatePodUsageSummaries(\"\") = true, want false")
	}
	if shouldPopulatePodUsageSummaries("   ") {
		t.Fatalf("shouldPopulatePodUsageSummaries(blank) = true, want false")
	}
	if !shouldPopulatePodUsageSummaries("erp-front") {
		t.Fatalf("shouldPopulatePodUsageSummaries(namespace) = false, want true")
	}
}

func TestMapPodIncludesRequestsAndLimits(t *testing.T) {
	t.Parallel()

	view := mapPod(corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    apiresource.MustParse("250m"),
							corev1.ResourceMemory: apiresource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    apiresource.MustParse("500m"),
							corev1.ResourceMemory: apiresource.MustParse("256Mi"),
						},
					},
				},
			},
		},
	}, domainaccess.Decision{})

	if view.Requests.CPU != "250m" || view.Requests.Memory != "128Mi" {
		t.Fatalf("Requests = %+v, want cpu=250m memory=128Mi", view.Requests)
	}
	if view.Limits.CPU != "500m" || view.Limits.Memory != "256Mi" {
		t.Fatalf("Limits = %+v, want cpu=500m memory=256Mi", view.Limits)
	}
}
