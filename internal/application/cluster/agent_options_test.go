package cluster

import (
	"strings"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
)

func TestAgentOptionsPreserveDirectDefaultAndRejectUnsafeGrants(t *testing.T) {
	for _, input := range []domaincluster.RegisterInput{
		{PrometheusTransport: "fallback"},
		{PrometheusBaseURL: "https://user:pass@example.com"},
		{AgentCustomResourceRules: []domaincluster.AgentCustomResourceRule{{APIGroup: "*", Resources: []string{"widgets"}, Verbs: []string{"get"}}}},
		{AgentCustomResourceRules: []domaincluster.AgentCustomResourceRule{{APIGroup: "example.io", Resources: []string{"*"}, Verbs: []string{"get"}}}},
		{AgentCustomResourceRules: []domaincluster.AgentCustomResourceRule{{APIGroup: "example.io", Resources: []string{"widgets"}, Verbs: []string{"*"}}}},
	} {
		if err := validateAgentOptions(&input, domaincluster.ConnectionModeAgent); err == nil {
			t.Fatal("unsafe options accepted")
		}
	}
	input := domaincluster.RegisterInput{}
	if err := validateAgentOptions(&input, domaincluster.ConnectionModeAgent); err != nil || input.PrometheusTransport != "direct" {
		t.Fatalf("legacy default changed: %v", err)
	}
	input.PrometheusTransport = "agent"
	if err := validateAgentOptions(&input, domaincluster.ConnectionModeDirectKubeconfig); err == nil {
		t.Fatal("direct cluster accepted Agent transport")
	}
	input.AgentCustomResourceRules = []domaincluster.AgentCustomResourceRule{{APIGroup: "apps--demo.example.io", Resources: []string{"widget-sets"}, Verbs: []string{"get", "list"}, Namespaces: []string{"team-a"}}}
	if err := validateAgentOptions(&input, domaincluster.ConnectionModeAgent); err != nil {
		t.Fatalf("valid Kubernetes DNS names rejected: %v", err)
	}
}

func TestAgentManifestScopesCustomRulesAndEnablesDeliveryRunner(t *testing.T) {
	connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "private"}, Metadata: map[string]any{
		"token": "test-installation-token", "prometheus_url": "http://prometheus.monitoring:9090", "prometheus_bearer_token": "test-prometheus-token",
		"agent_custom_resource_rules": []domaincluster.AgentCustomResourceRule{{APIGroup: "example.io", Resources: []string{"widgets"}, Verbs: []string{"get", "list"}, Namespaces: []string{"apps"}}},
	}}
	content, err := renderAgentManifest(connection, "https://soha.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"kind: Role\n", "kind: RoleBinding\n", "namespace: apps", "example.io", "widgets", "selfsubjectaccessreviews", "runtime.execution_tasks.cancel", "soha.io/config-checksum", "provider_kinds: []", "SOHA_AGENT_PROMETHEUS_BEARER_TOKEN"} {
		if !strings.Contains(string(content), expected) {
			t.Fatalf("manifest missing %q", expected)
		}
	}
	if strings.Contains(string(content), "apiGroups:\n- '*'") {
		t.Fatal("wildcard grant")
	}
}
