package docs

import (
	"os"
	"strings"
	"testing"
)

func TestAIGatewayExamplesMentionGovernanceAliases(t *testing.T) {
	files := []string{
		"operations/ai-gateway-examples.md",
		"en/operations/ai-gateway-examples.md",
	}
	required := []string{
		"cached content",
		"tool-use prompt",
		"thoughts token",
		"cached/audio/reasoning/prediction",
		"costCents",
		"openrouter",
		"fireworks",
		"voyage",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			text := strings.Join(strings.Fields(string(raw)), " ")
			for _, needle := range required {
				if !strings.Contains(text, strings.Join(strings.Fields(needle), " ")) {
					t.Fatalf("%s missing %q", file, needle)
				}
			}
		})
	}
}

func TestAIGatewayExamplesHaveOneConditionNotesSection(t *testing.T) {
	files := []string{
		"operations/ai-gateway-examples.md",
		"en/operations/ai-gateway-examples.md",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			if count := strings.Count(string(raw), "Condition notes:"); count != 1 {
				t.Fatalf("%s should contain exactly one Condition notes section, got %d", file, count)
			}
		})
	}
}

func TestAIGatewayExamplesMentionResourcePromptDebugging(t *testing.T) {
	files := []string{
		"operations/ai-gateway-examples.md",
		"en/operations/ai-gateway-examples.md",
	}
	required := []string{
		"soha-cli resource read soha://delivery/applications",
		"soha-cli prompt get soha.delivery.plan_release",
		"soha-cli diagnose --profile gateway-admin --resource soha://k8s/runtime",
		"soha-cli diagnose --profile gateway-admin --prompt soha.k8s.diagnose_workload",
		"resources/read",
		"prompts/get",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			text := strings.Join(strings.Fields(string(raw)), " ")
			for _, needle := range required {
				if !strings.Contains(text, strings.Join(strings.Fields(needle), " ")) {
					t.Fatalf("%s missing %q", file, needle)
				}
			}
		})
	}
}

func TestAIGatewayExamplesMentionApprovalSLAGovernance(t *testing.T) {
	files := []string{
		"operations/ai-gateway-examples.md",
		"en/operations/ai-gateway-examples.md",
	}
	required := []string{
		"approval_sla_due_soon",
		"stale_gateway_approvals",
		"pending Gateway approvals",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			text := string(raw)
			for _, needle := range required {
				if !strings.Contains(text, needle) {
					t.Fatalf("%s missing %q", file, needle)
				}
			}
		})
	}
}

func TestAIGatewayExamplesMentionDeliveryAndK8sToolCommands(t *testing.T) {
	files := []string{
		"operations/ai-gateway-examples.md",
		"en/operations/ai-gateway-examples.md",
	}
	required := []string{
		"soha-cli tool call delivery.build_sources.list",
		"soha-cli tool call delivery.release_targets.list",
		"soha-cli tool call k8s.deployments.events",
		"soha-cli tool call k8s.services.backends",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			text := string(raw)
			for _, needle := range required {
				if !strings.Contains(text, needle) {
					t.Fatalf("%s missing %q", file, needle)
				}
			}
		})
	}
}

func TestSohaCLIDocsMentionGatewayCommandSurface(t *testing.T) {
	files := []string{
		"operations/soha-cli.md",
		"en/operations/soha-cli.md",
	}
	required := []string{
		"service-account list|create|token-list|token-create|token-revoke",
		"approval list|timeline|approve|reject|cancel",
		"governance status",
		"mcp install",
		"completion bash|zsh",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			text := string(raw)
			for _, needle := range required {
				if !strings.Contains(text, needle) {
					t.Fatalf("%s missing %q", file, needle)
				}
			}
		})
	}
}

func TestAIGatewayArchitectureDocsMentionCurrentCLISurface(t *testing.T) {
	files := []string{
		"architecture/ai-gateway.md",
		"en/architecture/ai-gateway.md",
		"architecture/ai-gateway-roadmap.md",
	}
	required := []string{
		"tool call",
		"resource read",
		"prompt get",
		"token list|create|revoke",
		"service-account list|create|token-list|token-create|token-revoke",
		"audit list",
		"governance status",
		"approval list|timeline|approve|reject|cancel",
		"completion bash|zsh",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			text := string(raw)
			for _, needle := range required {
				if !strings.Contains(text, needle) {
					t.Fatalf("%s missing %q", file, needle)
				}
			}
		})
	}
}

func TestAIGatewayArchitectureDocsMentionCurrentGatewayAPIs(t *testing.T) {
	files := []string{
		"architecture/ai-gateway.md",
		"en/architecture/ai-gateway.md",
		"architecture/ai-gateway-roadmap.md",
	}
	required := []string{
		"GET /api/v1/ai-gateway/capabilities",
		"POST /api/v1/ai-gateway/tools/:toolName/invoke",
		"POST /api/v1/ai-gateway/resources/read",
		"POST /api/v1/ai-gateway/prompts/get",
		"GET /api/v1/ai-gateway/governance/status",
		"GET /api/v1/ai-gateway/audit-logs",
		"GET /api/v1/ai-gateway/approval-requests",
		"GET /api/v1/ai-gateway/approval-requests/:requestID/timeline",
		"POST /api/v1/ai-gateway/approval-requests/:requestID/approve",
		"POST /api/v1/ai-gateway/approval-requests/:requestID/reject",
		"POST /api/v1/ai-gateway/approval-requests/:requestID/cancel",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			text := strings.Join(strings.Fields(string(raw)), " ")
			for _, needle := range required {
				if !strings.Contains(text, strings.Join(strings.Fields(needle), " ")) {
					t.Fatalf("%s missing %q", file, needle)
				}
			}
		})
	}
}

func TestAIGatewayDocsMentionCurrentDefaultTools(t *testing.T) {
	files := []string{
		"operations/soha-cli.md",
		"en/operations/soha-cli.md",
		"architecture/ai-gateway.md",
		"en/architecture/ai-gateway.md",
		"architecture/ai-gateway-roadmap.md",
	}
	required := []string{
		"delivery.applications.list",
		"delivery.applications.detail",
		"delivery.applications.create",
		"delivery.application_environments.list",
		"delivery.application_services.list",
		"delivery.build_sources.list",
		"delivery.release_targets.list",
		"delivery.actions.trigger",
		"delivery.release_bundles.list",
		"delivery.execution_tasks.list",
		"delivery.execution_logs.list",
		"delivery.approval_policies.list",
		"delivery.workflow_templates.list",
		"delivery.release_context.diff",
		"delivery.rollback.context",
		"k8s.pods.list",
		"k8s.pods.logs",
		"k8s.pods.describe",
		"k8s.deployments.list",
		"k8s.deployments.rollout_status",
		"k8s.deployments.events",
		"k8s.services.list",
		"k8s.services.backends",
		"k8s.routes.context",
		"k8s.storage.context",
		"k8s.nodes.detail",
		"k8s.events.list",
		"diagnosis.release_failure.analyze",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			text := string(raw)
			for _, needle := range required {
				if !strings.Contains(text, needle) {
					t.Fatalf("%s missing %q", file, needle)
				}
			}
		})
	}
}

func TestAIGatewayDocsAvoidObsoleteFirstVersionPhrases(t *testing.T) {
	files := []string{
		"operations/soha-cli.md",
		"en/operations/soha-cli.md",
		"architecture/ai-gateway.md",
		"en/architecture/ai-gateway.md",
		"architecture/ai-gateway-roadmap.md",
	}
	forbidden := []string{
		"首版可调用工具",
		"首版 manifest",
		"当前第一版",
		"第一版可运行后端入口",
		"first-version command surface",
		"first-version CLI",
		"  - 浏览器验证 `/ai-workbench/gateway`",
		"待可登录演示环境补充",
		"有可登录演示环境后",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			text := string(raw)
			for _, needle := range forbidden {
				if strings.Contains(text, needle) {
					t.Fatalf("%s still contains obsolete phrase %q", file, needle)
				}
			}
		})
	}
}

func TestAIGatewayRoadmapMentionsLoggedInConsoleVerification(t *testing.T) {
	raw, err := os.ReadFile("architecture/ai-gateway-roadmap.md")
	if err != nil {
		t.Fatalf("read roadmap: %v", err)
	}
	text := string(raw)
	required := []string{
		"2026-05-30 Chrome 登录态浏览器核对已完成",
		"`admin / soha` 登录后返回 `/ai-workbench/gateway`",
		"`AI工作台` 下 `AI Gateway` 菜单项可见并处于选中状态",
		"Manifest / AI Clients / Tokens / Service Accounts / Tool Grants / Access Policies / Skill Bindings / Governance / Approvals / Audit",
		"未提交正式截图制品",
	}
	for _, needle := range required {
		if !strings.Contains(text, needle) {
			t.Fatalf("roadmap missing logged-in console evidence %q", needle)
		}
	}
}
