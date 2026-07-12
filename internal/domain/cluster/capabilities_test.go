package cluster

import "testing"

func TestDefaultCapabilityMatrixDocumentsAgentGaps(t *testing.T) {
	byKey := indexCapabilityMatrix(t, DefaultCapabilityMatrix())

	yamlApply := byKey["resource.yaml.apply"]
	expectCapability(t, yamlApply.RiskLevel == CapabilityRiskMutate, "yaml apply risk = %q", yamlApply.RiskLevel)
	expectCapability(t, yamlApply.RequiresApproval, "yaml apply should require approval")
	expectCapability(t, yamlApply.Agent.Status == CapabilityStatusPartial, "agent yaml apply status = %q", yamlApply.Agent.Status)
	expectCapability(t, yamlApply.Agent.Reason != "", "agent yaml apply reason is empty")
	expectCapability(t, len(yamlApply.Agent.Notes) > 0, "agent yaml apply notes are empty")

	helmReleases := byKey["helm.releases"]
	expectCapability(t, helmReleases.Agent.Status == CapabilityStatusAvailable, "agent helm releases status = %q", helmReleases.Agent.Status)
	expectCapability(t, helmReleases.RiskLevel == CapabilityRiskMutate, "helm releases risk = %q", helmReleases.RiskLevel)
	expectCapability(t, helmReleases.RequiresApproval, "helm releases should require approval")

	podLogs := byKey["pod.logs"]
	expectCapability(t, podLogs.Agent.Status == CapabilityStatusAvailable, "agent pod logs status = %q", podLogs.Agent.Status)
	expectCapability(t, podLogs.RiskLevel == CapabilityRiskRead, "pod logs risk = %q", podLogs.RiskLevel)
	expectCapability(t, !podLogs.RequiresApproval, "pod logs should not require approval")
	expectCapability(t, byKey["pod.exec"].Agent.Status == CapabilityStatusAvailable, "agent pod exec is unavailable")
	expectCapability(t, byKey["port.forward"].Agent.Status == CapabilityStatusAvailable, "agent port forward is unavailable")

	deliveryActions := byKey["delivery.actions"]
	expectCapability(t, deliveryActions.Agent.Status == CapabilityStatusPartial, "agent delivery actions status = %q", deliveryActions.Agent.Status)
	expectCapability(t, deliveryActions.RiskLevel == CapabilityRiskExecute, "delivery actions risk = %q", deliveryActions.RiskLevel)
	expectCapability(t, deliveryActions.RequiresApproval, "delivery actions should require approval")
	expectCapability(t, len(deliveryActions.Agent.Notes) > 0, "agent delivery actions notes are empty")
}

func indexCapabilityMatrix(t *testing.T, matrix []CapabilityMatrixEntry) map[string]CapabilityMatrixEntry {
	t.Helper()
	if len(matrix) == 0 {
		t.Fatal("DefaultCapabilityMatrix() returned no entries")
	}
	byKey := make(map[string]CapabilityMatrixEntry, len(matrix))
	for _, entry := range matrix {
		if entry.Key == "" {
			t.Fatalf("matrix entry has empty key: %#v", entry)
		}
		if _, exists := byKey[entry.Key]; exists {
			t.Fatalf("duplicate matrix key %q", entry.Key)
		}
		if entry.Direct.Status == "" || entry.Agent.Status == "" {
			t.Fatalf("matrix entry %q missing mode status: %#v", entry.Key, entry)
		}
		if entry.RiskLevel == "" {
			t.Fatalf("matrix entry %q missing risk level: %#v", entry.Key, entry)
		}
		if len(entry.RequiredScopes) == 0 {
			t.Fatalf("matrix entry %q missing required scopes: %#v", entry.Key, entry)
		}
		if entry.DocsURL == "" {
			t.Fatalf("matrix entry %q missing docs URL: %#v", entry.Key, entry)
		}
		byKey[entry.Key] = entry
	}
	return byKey
}

func expectCapability(t *testing.T, condition bool, format string, args ...any) {
	t.Helper()
	if !condition {
		t.Fatalf(format, args...)
	}
}
