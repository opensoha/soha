package aigateway

import (
	"encoding/json"
	"testing"

	contractsopenapi "github.com/opensoha/soha-contracts/openapi"
)

type openAPICapability struct {
	Name             string `json:"name"`
	RiskLevel        string `json:"riskLevel"`
	RequiresApproval bool   `json:"requiresApproval"`
}

type openAPIOperation struct {
	Capability *openAPICapability `json:"x-soha-capability"`
}

type openAPIPathItem struct {
	Get    *openAPIOperation `json:"get"`
	Post   *openAPIOperation `json:"post"`
	Put    *openAPIOperation `json:"put"`
	Patch  *openAPIOperation `json:"patch"`
	Delete *openAPIOperation `json:"delete"`
}

func TestOpenAPICapabilitiesMatchLiveMCPToolCatalog(t *testing.T) {
	var document struct {
		Paths map[string]openAPIPathItem `json:"paths"`
	}
	if err := json.Unmarshal(contractsopenapi.JSON(), &document); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}

	tools := make(map[string]struct {
		riskLevel        string
		requiresApproval bool
		mcpToolName      string
	})
	for _, tool := range defaultTools() {
		tools[tool.Name] = struct {
			riskLevel        string
			requiresApproval bool
			mcpToolName      string
		}{string(tool.RiskLevel), tool.RequiresApproval, tool.MCPToolName}
	}

	checked := 0
	for path, pathItem := range document.Paths {
		for method, operation := range map[string]*openAPIOperation{
			"GET": pathItem.Get, "POST": pathItem.Post, "PUT": pathItem.Put, "PATCH": pathItem.Patch, "DELETE": pathItem.Delete,
		} {
			if operation == nil || operation.Capability == nil {
				continue
			}
			checked++
			capability := operation.Capability
			tool, ok := tools[capability.Name]
			if !ok {
				t.Errorf("%s %s capability %q is missing from the live tool catalog", method, path, capability.Name)
				continue
			}
			if tool.riskLevel != capability.RiskLevel || tool.requiresApproval != capability.RequiresApproval {
				t.Errorf("%s %s capability %q governance = (%s, %t), catalog = (%s, %t)", method, path, capability.Name, capability.RiskLevel, capability.RequiresApproval, tool.riskLevel, tool.requiresApproval)
			}
			if tool.mcpToolName != capability.Name {
				t.Errorf("%s %s capability %q MCP tool = %q", method, path, capability.Name, tool.mcpToolName)
			}
		}
	}
	if checked == 0 {
		t.Fatal("OpenAPI has no x-soha-capability operations")
	}
}
