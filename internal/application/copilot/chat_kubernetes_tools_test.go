package copilot

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestChatKubernetesToolsAreExecutableAndPinResourceScope(t *testing.T) {
	bindings := chatKubernetesToolBindings()
	if len(bindings) != 21 || chatKubernetesReadTool("k8s.resources.create.trigger") || chatKubernetesReadTool("k8s.secrets.detail") {
		t.Fatal("read tool set includes a write or secret-data tool, or is incomplete")
	}
	for _, binding := range bindings {
		t.Run(binding.ToolName, func(t *testing.T) {
			gateway := &chatGatewayToolStub{}
			service := &Service{workbenchInvoker: gateway}
			run := domaincopilot.AgentRun{Scope: domaincopilot.SessionScope{ClusterID: "cluster", Namespace: "team", Pod: "pod", Node: "node", Workload: "api", Service: "service", TimeRangeMinutes: 10}, Input: map[string]any{"_sohaPrincipal": map[string]any{"userId": "reader", "roles": []string{"reader"}, "permissionKeys": []string{binding.PermissionKey}}}}
			input := map[string]any{"crdName": "widgets.example.com", "limit": 20, "tailLines": 30, "previous": true, "stepSeconds": 60, "untrustedOption": "must-not-forward"}
			if _, err := service.executeChatAgentTool(context.Background(), run, binding, input); err != nil {
				t.Fatal(err)
			}
			if gateway.calls != 1 || gateway.input.Input["clusterId"] != "cluster" || gateway.input.Input["untrustedOption"] != nil {
				t.Fatalf("gateway arguments = %#v", gateway.input.Input)
			}
			for _, key := range []string{"clusterId", "namespace", "podName", "nodeName", "deploymentName", "serviceName"} {
				if _, pinned := gateway.input.Input[key]; !pinned {
					continue
				}
				if _, err := chatResourceToolArguments(run, binding.ToolName, map[string]any{key: "other"}); !errors.Is(err, apperrors.ErrAccessDenied) {
					t.Fatalf("%s scope bypass: %v", key, err)
				}
			}
			if binding.ToolName == "k8s.pods.metrics" || binding.ToolName == "k8s.deployments.metrics" {
				if gateway.input.Input["rangeMinutes"] != 10 {
					t.Fatal("metric default exceeds pinned time range")
				}
				if _, err := chatResourceToolArguments(run, binding.ToolName, map[string]any{"rangeMinutes": 11}); !errors.Is(err, apperrors.ErrAccessDenied) {
					t.Fatalf("time scope bypass: %v", err)
				}
			}
		})
	}
}

func TestChatReadBindingsRequireEveryGatewayPermission(t *testing.T) {
	permissions := map[string][]string{}
	for _, tool := range (appaigateway.BuiltinCapabilityProvider{}).Tools() {
		permissions[tool.Name] = tool.PermissionKeys
	}
	for _, binding := range chatKubernetesToolBindings() {
		t.Run(binding.ToolName, func(t *testing.T) {
			required := permissions[binding.ToolName]
			if len(required) < 2 {
				t.Fatal("missing Gateway permissions")
			}
			for _, key := range required {
				if !appaccess.IsActiveAssignablePermission(key) {
					t.Fatalf("tool requires an unassignable permission: %s", key)
				}
			}
			roles := map[string][]string{"reader": required}
			service := &Service{permissions: appaccess.NewPermissionResolver(inspectionAuthzRoleReader{matrix: roles})}
			principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
			bindings := []domaincopilot.AgentToolBinding{binding}
			if got, err := service.filterAgentToolBindingsByPrincipal(context.Background(), principal, bindings); err != nil || len(got) != 1 {
				t.Fatalf("complete permissions denied: %v, %v", got, err)
			}
			for index := range required {
				roles["reader"] = append(append([]string{}, required[:index]...), required[index+1:]...)
				if got, err := service.filterAgentToolBindingsByPrincipal(context.Background(), principal, bindings); err != nil || len(got) != 0 {
					t.Fatalf("missing %s exposed tool: %v, %v", required[index], got, err)
				}
			}
		})
	}
}
