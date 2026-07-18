package resourcebackend

import (
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMapPodViewIncludesRequestsAndLimits(t *testing.T) {
	t.Parallel()

	view := mapPodView(corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
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
			}},
		},
	})

	if view.Requests.CPU != "250m" || view.Requests.Memory != "128Mi" {
		t.Fatalf("Requests = %+v, want cpu=250m memory=128Mi", view.Requests)
	}
	if view.Limits.CPU != "500m" || view.Limits.Memory != "256Mi" {
		t.Fatalf("Limits = %+v, want cpu=500m memory=256Mi", view.Limits)
	}
}

func TestPodOwnerNameReturnsExactOwner(t *testing.T) {
	t.Parallel()
	owners := []metav1.OwnerReference{{Kind: "Node", Name: "node-a"}, {Kind: "ReplicaSet", Name: "api-7d9f"}}
	if got := podOwnerName(owners, "ReplicaSet"); got != "api-7d9f" {
		t.Fatalf("podOwnerName() = %q, want api-7d9f", got)
	}
}

func TestBuildPodContainersLabelsRoles(t *testing.T) {
	view := buildPodContainers(corev1.Pod{
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init", Image: "init:v1"}},
			Containers:     []corev1.Container{{Name: "app", Image: "app:v1"}, {Name: "proxy", Image: "proxy:v1"}},
		},
	})
	if len(view) != 3 || view[0].Role != "init" || view[1].Role != "main" || view[2].Role != "sidecar" {
		t.Fatalf("container roles = %#v, want init/main/sidecar", view)
	}
}

func TestTerminalSizeQueueAdapterMapsDomainSize(t *testing.T) {
	t.Parallel()

	adapter := terminalSizeQueueAdapter{source: &terminalSizeQueueStub{size: &domainresource.TerminalSize{Width: 120, Height: 40}}}
	size := adapter.Next()
	if size == nil || size.Width != 120 || size.Height != 40 {
		t.Fatalf("Next() = %#v, want 120x40", size)
	}
}

type terminalSizeQueueStub struct {
	size *domainresource.TerminalSize
}

func (s *terminalSizeQueueStub) Next() *domainresource.TerminalSize {
	size := s.size
	s.size = nil
	return size
}
