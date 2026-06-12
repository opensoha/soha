package cluster

import "testing"

func TestDefaultCapabilityMatrixDocumentsAgentGaps(t *testing.T) {
	matrix := DefaultCapabilityMatrix()
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

	yamlApply := byKey["resource.yaml.apply"]
	if yamlApply.RiskLevel != CapabilityRiskMutate || !yamlApply.RequiresApproval {
		t.Fatalf("yaml apply contract = risk %q approval %v, want mutate approval", yamlApply.RiskLevel, yamlApply.RequiresApproval)
	}
	if yamlApply.Agent.Status != CapabilityStatusPartial {
		t.Fatalf("agent yaml apply status = %q, want partial", yamlApply.Agent.Status)
	}
	if yamlApply.Agent.Reason == "" || len(yamlApply.Agent.Notes) == 0 {
		t.Fatal("agent yaml apply should include explanatory reason and notes")
	}

	helmReleases := byKey["helm.releases"]
	if helmReleases.Agent.Status != CapabilityStatusAvailable {
		t.Fatalf("agent helm releases status = %q, want available", helmReleases.Agent.Status)
	}
	if helmReleases.RiskLevel != CapabilityRiskMutate || !helmReleases.RequiresApproval {
		t.Fatalf("helm releases contract = risk %q approval %v, want mutate approval", helmReleases.RiskLevel, helmReleases.RequiresApproval)
	}

	podLogs := byKey["pod.logs"]
	if podLogs.Agent.Status != CapabilityStatusAvailable {
		t.Fatalf("agent pod logs status = %q, want available", podLogs.Agent.Status)
	}

	podExec := byKey["pod.exec"]
	if podExec.Agent.Status != CapabilityStatusAvailable {
		t.Fatalf("agent pod exec status = %q, want available", podExec.Agent.Status)
	}

	portForward := byKey["port.forward"]
	if portForward.Agent.Status != CapabilityStatusAvailable {
		t.Fatalf("agent port forward status = %q, want available", portForward.Agent.Status)
	}

	deliveryActions := byKey["delivery.actions"]
	if deliveryActions.Agent.Status != CapabilityStatusPartial {
		t.Fatalf("agent delivery actions status = %q, want partial", deliveryActions.Agent.Status)
	}
	if deliveryActions.RiskLevel != CapabilityRiskExecute || !deliveryActions.RequiresApproval {
		t.Fatalf("delivery actions contract = risk %q approval %v, want execute approval", deliveryActions.RiskLevel, deliveryActions.RequiresApproval)
	}
	if len(deliveryActions.Agent.Notes) == 0 {
		t.Fatal("agent delivery actions should include explanatory notes")
	}

	if podLogs.RiskLevel != CapabilityRiskRead || podLogs.RequiresApproval {
		t.Fatalf("pod logs contract = risk %q approval %v, want read without approval", podLogs.RiskLevel, podLogs.RequiresApproval)
	}
}
