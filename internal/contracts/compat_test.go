package contracts_test

import (
	"encoding/json"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
)

func TestAuthContractWireCompatibility(t *testing.T) {
	expiresAt := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	authResult := domainidentity.AuthResult{
		User: domainidentity.Principal{
			UserID:   "user-1",
			UserName: "alice",
			Email:    "alice@example.com",
			Roles:    []string{"admin"},
			Teams:    []string{"platform"},
			Projects: []string{"default"},
			Tags:     []string{"ops"},
		},
		Tokens: domainidentity.TokenSet{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			ExpiresAt:    expiresAt,
		},
	}

	contract := roundTrip[sohaapi.AuthResult](t, authResult)
	if contract.User.UserID != authResult.User.UserID {
		t.Fatalf("user id mismatch: got %q want %q", contract.User.UserID, authResult.User.UserID)
	}
	if !contract.Tokens.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expiresAt mismatch: got %s want %s", contract.Tokens.ExpiresAt, expiresAt)
	}
}

func TestAIGatewayContractWireCompatibility(t *testing.T) {
	generatedAt := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	manifest := domainaigateway.Manifest{
		Name:        "soha-ai-gateway",
		Version:     "0.1.0",
		GeneratedAt: generatedAt,
		Principal: domainidentity.Principal{
			UserID:   "user-1",
			UserName: "alice",
			Email:    "alice@example.com",
			Roles:    []string{"admin"},
			Teams:    []string{"platform"},
			Projects: []string{"default"},
			Tags:     []string{"ops"},
		},
		Caller: domainaigateway.CallerContext{
			IdentityMode: "user",
			AIClientID:   "client-1",
			Source:       "web",
		},
		PermissionKeys: []string{"ai-gateway.invoke"},
		Tools: []domainaigateway.ToolCapability{{
			Name:             "platform.cluster.list",
			Title:            "List clusters",
			Description:      "Lists visible clusters.",
			Domain:           "platform",
			Action:           "list",
			RiskLevel:        domainaigateway.RiskLevelRead,
			PermissionKeys:   []string{"platform.clusters.view"},
			RequiresApproval: false,
		}},
		Summary: domainaigateway.ManifestSummary{
			ToolCount: 1,
		},
	}

	contractManifest := roundTrip[sohaapi.AIGatewayManifest](t, manifest)
	if contractManifest.Name != manifest.Name {
		t.Fatalf("manifest name mismatch: got %q want %q", contractManifest.Name, manifest.Name)
	}
	if got := contractManifest.Tools[0].RiskLevel; got != string(domainaigateway.RiskLevelRead) {
		t.Fatalf("riskLevel mismatch: got %q", got)
	}

	invocation := domainaigateway.ToolInvocationResult{
		ToolName:         "platform.cluster.list",
		RiskLevel:        domainaigateway.RiskLevelRead,
		RequiresApproval: false,
		Result:           "success",
		Output:           map[string]any{"count": 1},
	}
	contractInvocation := roundTrip[sohaapi.ToolInvocationResult](t, invocation)
	if contractInvocation.ToolName != invocation.ToolName || contractInvocation.Result != invocation.Result {
		t.Fatalf("tool invocation contract mismatch: %#v", contractInvocation)
	}
}

func TestRunnerContractWireCompatibility(t *testing.T) {
	task := domaindelivery.ExecutionTask{
		ID:                       "task-1",
		ApplicationID:            "app-1",
		ApplicationEnvironmentID: "env-1",
		TaskKind:                 "deploy",
		ProviderKind:             "shell",
		Status:                   "claimed",
		CallbackToken:            "callback-token",
		Payload:                  map[string]any{"command": "deploy"},
	}

	contractTask := roundTrip[sohaapi.ExecutionTask](t, task)
	if contractTask.ID != task.ID {
		t.Fatalf("task id mismatch: got %q want %q", contractTask.ID, task.ID)
	}
	if contractTask.Payload["command"] != "deploy" {
		t.Fatalf("payload mismatch: %#v", contractTask.Payload)
	}
}

func TestPluginContractWireCompatibility(t *testing.T) {
	installedAt := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	item := domainplugin.InstalledPlugin{
		ID:              "opensoha.k8s-sre-pack",
		Name:            "K8s SRE Pack",
		Version:         "0.1.0",
		Publisher:       "opensoha",
		Type:            "skill-pack",
		Status:          "enabled",
		Source:          "marketplace:opensoha/k8s-sre-pack",
		ChecksumStatus:  "verified",
		SignatureStatus: "declared",
		Manifest: domainplugin.PluginManifest{
			ID:        "opensoha.k8s-sre-pack",
			Name:      "K8s SRE Pack",
			Version:   "0.1.0",
			Publisher: "opensoha",
			Type:      "skill-pack",
			Permissions: &domainplugin.PluginPermissionRequest{
				Required: []string{"ai.gateway.view", "ai.gateway.invoke"},
				Domain:   []string{"workspace.resource.view"},
			},
		},
		RequestedPermissions: &domainplugin.PluginPermissionRequest{
			Required: []string{"ai.gateway.view", "ai.gateway.invoke"},
			Domain:   []string{"workspace.resource.view"},
		},
		ConfiguredSecretRefs: map[string]string{"kubeconfig": "secret://k8s/default"},
		InstalledBy:          "admin",
		InstalledAt:          installedAt,
		UpdatedAt:            installedAt,
		Metadata:             map[string]any{"permissionModel": "requested-only"},
	}

	contractPlugin := roundTrip[sohaapi.InstalledPlugin](t, item)
	if contractPlugin.Manifest.ID != item.Manifest.ID {
		t.Fatalf("plugin manifest id mismatch: got %q want %q", contractPlugin.Manifest.ID, item.Manifest.ID)
	}
	if contractPlugin.RequestedPermissions == nil || len(contractPlugin.RequestedPermissions.Required) != 2 {
		t.Fatalf("requested permissions not preserved: %#v", contractPlugin.RequestedPermissions)
	}
	if contractPlugin.Metadata["permissionModel"] != "requested-only" {
		t.Fatalf("metadata mismatch: %#v", contractPlugin.Metadata)
	}
}

func roundTrip[T any](t *testing.T, value any) T {
	t.Helper()

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %T: %v", value, err)
	}

	var out T
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("unmarshal into %T: %v", out, err)
	}
	return out
}
