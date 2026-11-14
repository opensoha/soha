package auth

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestGatewayRouteMutationPermissionsAreExplicit(t *testing.T) {
	var catalog struct {
		Permissions []struct {
			Key            string   `json:"key"`
			Resource       string   `json:"resource"`
			Action         string   `json:"action"`
			RiskLevel      string   `json:"riskLevel"`
			ScopeKinds     []string `json:"scopeKinds"`
			ApprovalPolicy string   `json:"approvalPolicy"`
			LegacyAliases  []string `json:"legacyAliases"`
		}
	}
	if err := json.Unmarshal(PermissionCatalogJSON(), &catalog); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{"http-routes", "grpc-routes", "backend-tls-policies", "reference-grants"} {
		for _, action := range []string{"create", "update", "delete"} {
			key := "platform.network." + resource + "." + action
			var index = -1
			for i, permission := range catalog.Permissions {
				if permission.Key == key {
					index = i
					break
				}
			}
			if index < 0 {
				t.Fatalf("missing %s", key)
			}
			p := catalog.Permissions[index]

			risk, approval := "mutate", "never"
			if action == "delete" {
				risk, approval = "high", "eligible"
			}
			if p.Resource != "network."+resource || p.Action != action || p.RiskLevel != risk || p.ApprovalPolicy != approval || !slices.Contains(p.ScopeKinds, "namespace") || len(p.LegacyAliases) != 0 {
				t.Fatalf("invalid explicit mutation permission: %+v", p)
			}
		}
	}
}
