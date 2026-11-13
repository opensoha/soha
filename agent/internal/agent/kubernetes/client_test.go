package kubernetes

import (
	"strings"
	"testing"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestBuildServiceDetailIncludesEndpointsPodsAndPorts(t *testing.T) {
	ready := true
	detail := buildServiceDetail(corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{
		Port: 80, Protocol: corev1.ProtocolTCP,
	}}}}, []discoveryv1.EndpointSlice{{Endpoints: []discoveryv1.Endpoint{{
		Addresses: []string{"10.1.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready},
	}}}}, []domainresource.PodView{{Name: "api-1"}})
	if len(detail.Endpoints) != 1 || detail.Endpoints[0].Ready == nil || !*detail.Endpoints[0].Ready || len(detail.BackendPods) != 1 || len(detail.PortMappings) != 1 || detail.PortMappings[0].TargetPort != "80" {
		t.Fatalf("buildServiceDetail() = %#v", detail)
	}
}

func TestMapServiceIncludesAssignedNodePort(t *testing.T) {
	t.Parallel()

	view := mapService(corev1.Service{Spec: corev1.ServiceSpec{
		Type: corev1.ServiceTypeNodePort,
		Ports: []corev1.ServicePort{
			{Name: "http", Port: 80, NodePort: 30080, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(8080)},
			{Name: "metrics", Port: 9090, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromString("metrics-backend")},
		},
	}})

	if len(view.Ports) != 2 || view.Ports[0] != "http:80/tcp (nodePort:30080)" || view.Ports[1] != "metrics:9090/tcp" {
		t.Fatalf("mapService() ports = %#v", view.Ports)
	}
	if len(view.PortMappings) != 2 || view.PortMappings[0].TargetPort != "8080" || view.PortMappings[0].NodePort != 30080 || view.PortMappings[1].TargetPort != "metrics-backend" {
		t.Fatalf("mapService() port mappings = %#v", view.PortMappings)
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
	})

	if view.Requests.CPU != "250m" || view.Requests.Memory != "128Mi" {
		t.Fatalf("Requests = %+v, want cpu=250m memory=128Mi", view.Requests)
	}
	if view.Limits.CPU != "500m" || view.Limits.Memory != "256Mi" {
		t.Fatalf("Limits = %+v, want cpu=500m memory=256Mi", view.Limits)
	}
}

func TestBuildRESTConfigUsesInClusterWhenKubeconfigEmpty(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	_, err := buildRESTConfig(cfgpkg.KubernetesConfig{})
	if err == nil {
		t.Fatal("buildRESTConfig() succeeded, want in-cluster config error outside Kubernetes")
	}
	if !strings.Contains(err.Error(), "KUBERNETES_SERVICE_HOST") {
		t.Fatalf("buildRESTConfig() error = %v, want in-cluster config error", err)
	}
}
