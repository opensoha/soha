package auth

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestDockerRuntimeFilePermissionIsIndependent(t *testing.T) {
	var catalog struct {
		Permissions []struct {
			Key            string   `json:"key"`
			Resource       string   `json:"resource"`
			Action         string   `json:"action"`
			RiskLevel      string   `json:"riskLevel"`
			ScopeKinds     []string `json:"scopeKinds"`
			ApprovalPolicy string   `json:"approvalPolicy"`
			Status         string   `json:"status"`
			Assignable     bool     `json:"assignable"`
			LegacyAliases  []string `json:"legacyAliases"`
			ReplacedBy     []string `json:"replacedBy"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(PermissionCatalogJSON(), &catalog); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, p := range catalog.Permissions {
		if p.Key == "docker.services.files" {
			found++
			if p.Resource != "services" || p.Action != "files" || p.RiskLevel != "high" || p.ApprovalPolicy != "eligible" || p.Status != "active" || !p.Assignable || len(p.LegacyAliases) != 0 || !slices.Equal(p.ScopeKinds, []string{"workspace", "project", "resource"}) {
				t.Fatalf("file permission must be explicit and independent: %+v", p)
			}
		}
		if p.Key == "docker.services.view" || p.Key == "docker.services.manage" || p.Key == "docker.services.actions" {
			if slices.Contains(p.ReplacedBy, "docker.services.files") {
				t.Fatalf("coarse key must not implicitly grant new file permission: %s", p.Key)
			}
		}
	}
	if found != 1 {
		t.Fatalf("file permission count = %d, want exactly one", found)
	}
}
