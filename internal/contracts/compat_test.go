package contracts_test

import (
	"encoding/json"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
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

func TestWorkbenchStreamContractWireCompatibility(t *testing.T) {
	now := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	messageDone := domaincopilot.WorkbenchStreamEvent{
		ID:        "evt-1",
		Type:      "message.done",
		SessionID: "session-1",
		MessageID: "message-1",
		Sequence:  1,
		CreatedAt: now,
		Role:      "assistant",
		Content:   "Root cause summary",
		Metadata:  map[string]any{"source": "model-provider"},
	}
	contractMessage := roundTrip[sohaapi.WorkbenchMessageDoneEvent](t, messageDone)
	if contractMessage.Type != "message.done" || contractMessage.Role != "assistant" || contractMessage.Content != messageDone.Content {
		t.Fatalf("workbench message event mismatch: %#v", contractMessage)
	}

	completedAt := now.Add(1200 * time.Millisecond)
	toolDone := domaincopilot.WorkbenchStreamEvent{
		ID:        "evt-2",
		Type:      "tool.completed",
		SessionID: "session-1",
		RunID:     "run-1",
		Sequence:  2,
		CreatedAt: now,
		ToolCall: &domaincopilot.WorkbenchToolCall{
			ID:            "tool-1",
			AdapterID:     "metrics.v1",
			ToolName:      "metrics.anomaly_summary",
			Status:        "success",
			InputPreview:  map[string]any{"namespace": "payments"},
			OutputPreview: map[string]any{"errorRate": "12%"},
			StartedAt:     &now,
			CompletedAt:   &completedAt,
			DurationMs:    1200,
		},
	}
	contractTool := roundTrip[sohaapi.WorkbenchToolCompletedEvent](t, toolDone)
	if contractTool.Type != "tool.completed" || contractTool.ToolCall.Status != sohaapi.WorkbenchToolCallStatusSuccess || contractTool.ToolCall.DurationMs != 1200 {
		t.Fatalf("workbench tool event mismatch: %#v", contractTool)
	}
}

func TestDeliveryControlPlaneContractWireCompatibility(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	app := domainapp.App{
		ID:             "app-1",
		Name:           "Checkout Platform",
		Key:            "checkout-platform",
		Group:          "commerce",
		Language:       "go",
		RepositoryPath: "commerce/checkout",
		DefaultBranch:  "main",
		DefaultTag:     "latest",
		Enabled:        true,
		BuildSources: []domainapp.BuildSource{{
			ID:        "source-api",
			Name:      "API Dockerfile",
			Type:      domainapp.BuildSourceTypeRepoDockerfile,
			Enabled:   true,
			IsDefault: true,
		}},
		EnvironmentCount: 1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	contractApp := roundTrip[sohaapi.Application](t, app)
	if contractApp.ID != app.ID || contractApp.Key != app.Key || contractApp.BuildSources[0].Type != sohaapi.BuildSourceTypeRepoDockerfile {
		t.Fatalf("application wire mismatch: %#v", contractApp)
	}

	service := domainapp.Service{
		ID:            "svc-api",
		ApplicationID: app.ID,
		Key:           "api",
		Name:          "Checkout API",
		ServiceKind:   domainapp.ServiceKindKubernetesWorkload,
		Enabled:       true,
		Containers: []domainapp.ServiceContainer{{
			ID:              "svc-api:api",
			ServiceID:       "svc-api",
			Name:            "api",
			ImageRepository: "registry.example.com/checkout/api",
			RuntimePorts:    []int{8080},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	contractService := roundTrip[sohaapi.ApplicationService](t, service)
	if contractService.ServiceKind != sohaapi.ApplicationServiceServiceKindKubernetesWorkload || contractService.Containers[0].RuntimePorts[0] != 8080 {
		t.Fatalf("application service wire mismatch: %#v", contractService)
	}

	workflowTemplate := domaincatalog.WorkflowTemplate{
		ID:       "wf-template-1",
		Key:      "release-dag",
		Name:     "Release DAG",
		Category: "release",
		Definition: map[string]any{
			"mode": "delivery_dag",
			"nodes": []any{map[string]any{
				"id":            "verify-ai",
				"type":          "verify",
				"executorKind":  "mcp",
				"targetKind":    "ai_test",
				"capabilityRef": "testing.ui.run",
				"artifactKinds": []any{"test_report", "screenshot", "junit"},
			}},
		},
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	binding := domaincatalog.ApplicationEnvironment{
		ID:                 "binding-test",
		ApplicationID:      app.ID,
		EnvironmentID:      "env-test",
		EnvironmentKey:     "test",
		WorkflowTemplateID: workflowTemplate.ID,
		WorkflowTemplate:   &workflowTemplate,
		BuildPolicy: domaincatalog.BuildPolicy{
			SourceID: "source-api",
			RefType:  "branch",
			RefValue: "main",
		},
		ReleasePolicy: domaincatalog.ReleasePolicy{
			ActionKind:       "deploy",
			RequiresApproval: true,
			VerificationMode: "workflow",
		},
		ResourceSelector: domaincatalog.ResourceSelector{MatchLabels: map[string]string{"app": "checkout-api"}},
		Targets: []domaincatalog.ReleaseTarget{{
			ID:                       "target-1",
			ApplicationEnvironmentID: "binding-test",
			ClusterID:                "cluster-a",
			Namespace:                "checkout-test",
			TargetKind:               "k8s_workload",
			ExecutorKind:             "k8s_job_runner",
			WorkloadKind:             "Deployment",
			WorkloadName:             "checkout-api",
			ContainerName:            "api",
			Enabled:                  true,
			CreatedAt:                now,
			UpdatedAt:                now,
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	contractBinding := roundTrip[sohaapi.ApplicationEnvironment](t, binding)
	if contractBinding.EnvironmentKey != "test" || contractBinding.Targets[0].ExecutorKind != "k8s_job_runner" {
		t.Fatalf("application environment wire mismatch: %#v", contractBinding)
	}
	contractTemplate := roundTrip[sohaapi.WorkflowTemplate](t, workflowTemplate)
	nodes, ok := contractTemplate.Definition["nodes"].([]any)
	if !ok || len(nodes) != 1 {
		t.Fatalf("workflow template definition not preserved: %#v", contractTemplate.Definition)
	}

	bundle := domaindelivery.ReleaseBundle{
		ID:                       "bundle-1",
		ApplicationID:            app.ID,
		ApplicationEnvironmentID: binding.ID,
		Version:                  "1.2.3",
		SourceType:               "build",
		Status:                   "completed",
		ArtifactRef:              "registry.example.com/checkout/api:1.2.3",
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	contractBundle := roundTrip[sohaapi.ReleaseBundle](t, bundle)
	if contractBundle.Version != bundle.Version || contractBundle.ApplicationEnvironmentID != binding.ID {
		t.Fatalf("release bundle wire mismatch: %#v", contractBundle)
	}

	artifact := domaindelivery.ExecutionArtifact{
		ID:              "artifact-1",
		ReleaseBundleID: bundle.ID,
		ApplicationID:   app.ID,
		Kind:            "test_report",
		Name:            "ui-report",
		Ref:             "s3://reports/ui-report.json",
		Status:          "completed",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	contractArtifact := roundTrip[sohaapi.ExecutionArtifact](t, artifact)
	if contractArtifact.Kind != artifact.Kind || contractArtifact.ReleaseBundleID != bundle.ID {
		t.Fatalf("execution artifact wire mismatch: %#v", contractArtifact)
	}

	task := domaindelivery.WithOperationState(domaindelivery.ExecutionTask{
		ID:                       "task-1",
		ReleaseBundleID:          bundle.ID,
		ApplicationID:            app.ID,
		ApplicationEnvironmentID: binding.ID,
		TaskKind:                 "verify",
		ProviderKind:             "mcp",
		TargetKind:               "ai_test",
		Status:                   "queued",
		CallbackToken:            "callback-token",
		Payload:                  map[string]any{"capabilityRef": "testing.ui.run"},
		MaxRetries:               1,
		TimeoutSeconds:           600,
		CreatedAt:                now,
		UpdatedAt:                now,
	}, now)
	contractTask := roundTrip[sohaapi.ExecutionTask](t, task)
	if contractTask.ProviderKind != "mcp" || contractTask.TargetKind != "ai_test" || contractTask.OperationState == nil {
		t.Fatalf("execution task wire mismatch: %#v", contractTask)
	}

	plan := domaindelivery.DeliveryPlan{
		ID:                       "plan-1",
		Source:                   domaindelivery.DeliveryPlanSourceManual,
		Status:                   domaindelivery.DeliveryPlanStatusDraft,
		ApplicationID:            app.ID,
		ApplicationName:          app.Name,
		ApplicationEnvironmentID: binding.ID,
		EnvironmentKey:           "test",
		Action:                   domaindelivery.ApplicationDeliveryActionVerify,
		ReleaseBundleID:          bundle.ID,
		RiskLevel:                "medium",
		RequiresApproval:         true,
		Impact:                   map[string]any{"environmentKey": "test"},
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	contractPlan := roundTrip[sohaapi.DeliveryPlan](t, plan)
	if contractPlan.Action != sohaapi.Verify || contractPlan.EnvironmentKey != "test" {
		t.Fatalf("delivery plan wire mismatch: %#v", contractPlan)
	}

	detail := domaindelivery.ApplicationDetail{
		Application: app,
		Bindings: []domaindelivery.ApplicationBindingSummary{{
			ApplicationEnvironmentID: binding.ID,
			EnvironmentID:            binding.EnvironmentID,
			EnvironmentKey:           binding.EnvironmentKey,
			RequiresApproval:         true,
			WorkflowTemplateID:       workflowTemplate.ID,
			WorkflowTemplate:         &workflowTemplate,
			TargetCount:              1,
			Targets:                  binding.Targets,
			BuildSourceID:            "source-api",
			LatestBundle:             &bundle,
			LatestExecutionTask:      &task,
		}},
		LatestBundle:        &bundle,
		LatestExecutionTask: &task,
	}
	contractDetail := roundTrip[sohaapi.ApplicationDeliveryDetail](t, detail)
	if contractDetail.Application.ID != app.ID || contractDetail.Bindings[0].LatestExecutionTask.ProviderKind != "mcp" {
		t.Fatalf("application delivery detail wire mismatch: %#v", contractDetail)
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
