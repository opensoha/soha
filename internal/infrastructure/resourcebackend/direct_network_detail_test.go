package resourcebackend

import (
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildNetworkDetails(t *testing.T) {
	backend := networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "api", Port: networkingv1.ServiceBackendPort{Number: 80}}}
	ingress := buildIngressDetail(networkingv1.Ingress{Spec: networkingv1.IngressSpec{
		TLS:   []networkingv1.IngressTLS{{Hosts: []string{"*.example.com"}}},
		Rules: []networkingv1.IngressRule{{Host: "api.example.com", IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Path: "/", Backend: backend}}}}}},
	}}, []domainresource.IngressBackendView{{ServiceName: "api"}})
	if len(ingress.Routes) != 1 || !ingress.Routes[0].TLS || ingress.Routes[0].ServicePort != "80" {
		t.Fatalf("buildIngressDetail() = %#v", ingress)
	}
	endpoint := buildEndpointSliceDetail(discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{discoveryv1.LabelServiceName: "api"}}, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.1.0.1"}}}})
	if endpoint.ServiceName != "api" || len(endpoint.Endpoints) != 1 {
		t.Fatalf("buildEndpointSliceDetail() = %#v", endpoint)
	}
	policy := buildNetworkPolicyDetail(networkingv1.NetworkPolicy{Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}}, Egress: []networkingv1.NetworkPolicyEgressRule{{}}}}, []domainresource.PodView{{Name: "api-1"}})
	if policy.PodSelector != "app=api" || len(policy.Rules) != 1 || len(policy.MatchingPods) != 1 {
		t.Fatalf("buildNetworkPolicyDetail() = %#v", policy)
	}
}
