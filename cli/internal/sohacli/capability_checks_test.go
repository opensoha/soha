package sohacli

import (
	"encoding/json"
	"testing"
)

func TestMCPMetadataPreservesCapabilityChecksAndRecovery(t *testing.T) {
	var capability ToolCapability
	if err := json.Unmarshal([]byte(`{"name":"extension.create","version":"v3","execution":{"mode":"async","idempotent":true,"recoveryMode":"original_call","checks":[{"purpose":"verification","toolName":"extension.assess","capabilityVersion":"v3"}]}}`), &capability); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(mcpSohaToolMeta(capability))
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Soha struct {
			Execution struct {
				RecoveryMode string                                                  `json:"recoveryMode"`
				Checks       []struct{ Purpose, ToolName, CapabilityVersion string } `json:"checks"`
			} `json:"execution"`
		} `json:"soha"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	execution := metadata.Soha.Execution
	if execution.RecoveryMode != "original_call" || len(execution.Checks) != 1 || execution.Checks[0].Purpose != "verification" || execution.Checks[0].ToolName != "extension.assess" || execution.Checks[0].CapabilityVersion != "v3" {
		t.Fatalf("MCP dropped lifecycle metadata: %s", raw)
	}
}
