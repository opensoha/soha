package aigateway

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func newGatewayGovernanceListRepository(now time.Time) *memoryGatewayRepository {
	lastUsed := now.Add(-time.Hour)
	repo := gatewayGovernanceIdentityRepository(now, &lastUsed)
	repo.accessPolicies = []domainaigateway.AccessPolicy{{
		ID: "policy-1", Name: "allow audit", Enabled: true, SubjectType: "role", SubjectID: "auditor",
		AIClientID: "codex", Effect: "allow", ToolPatterns: []string{"k8s.*"}, CreatedAt: now, UpdatedAt: now,
	}}
	repo.skillBindings = []domainaigateway.SkillBinding{{
		ID: "binding-1", SubjectType: "role", SubjectID: "auditor", AIClientID: "codex", SkillID: "k8s-sre",
		CapabilityRefs: []string{"k8s.*"}, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}}
	repo.approvalRequests = gatewayGovernanceApprovalFixtures(now)
	repo.auditLogs = gatewayGovernanceAuditFixtures(now)
	return repo
}

func gatewayGovernanceIdentityRepository(now time.Time, lastUsed *time.Time) *memoryGatewayRepository {
	return &memoryGatewayRepository{
		personalTokens: []domainaigateway.PersonalAccessToken{{
			ID: "pat-1", UserID: "user-2", Name: "other user", TokenHash: "hash-should-not-leak",
			TokenPrefix: "soha_pat_abcd", Metadata: map[string]any{"token": "secret", "purpose": "automation"},
			CreatedAt: now, UpdatedAt: now,
		}},
		serviceAccounts: map[string]domainaigateway.ServiceAccount{
			"svc-1": {
				ID: "svc-1", Name: "Deploy Bot", Status: "active", OwnerUserID: "owner-1",
				RoleIDs: []string{"deployer"}, CreatedAt: now, UpdatedAt: now,
			},
		},
		serviceAccountTokens: []domainaigateway.ServiceAccountToken{{
			ID: "sat-1", ServiceAccountID: "svc-1", Name: "deploy", TokenHash: "service-hash-should-not-leak",
			TokenPrefix: "soha_sat_abcd", Metadata: map[string]any{"apiKey": "secret", "purpose": "deploy"},
			LastUsedAt: lastUsed, CreatedAt: now, UpdatedAt: now,
		}},
		aiClients: map[string]domainaigateway.AIClient{
			"codex":    {ID: "codex", Name: "Codex", Kind: "mcp", Status: "active", CreatedAt: now, UpdatedAt: now},
			"disabled": {ID: "disabled", Name: "Disabled", Kind: "mcp", Status: "disabled", CreatedAt: now, UpdatedAt: now},
		},
		toolGrants: []domainaigateway.ToolGrant{{
			ID: "grant-1", SubjectType: "role", SubjectID: "auditor", AIClientID: "codex", ToolName: "k8s.pods.logs",
			Effect: "allow", RiskLevel: domainaigateway.RiskLevelRead, CreatedAt: now, UpdatedAt: now,
		}},
	}
}

func gatewayGovernanceApprovalFixtures(now time.Time) []domainaigateway.ApprovalRequest {
	return []domainaigateway.ApprovalRequest{{
		ID: "approval-1", Status: "pending", Strategy: "require_approval", ActorType: "user", ActorID: "user-2",
		AIClientID: "codex", SkillID: "delivery-tester", ToolName: "delivery.actions.trigger",
		RiskLevel: domainaigateway.RiskLevelExecute,
		ToolInput: map[string]any{"token": "secret-token", "applicationId": "app-1"},
		RelatedIDs: map[string]any{
			"approvalRouting": map[string]any{
				"approvalMode": "all", "requiredApprovals": 1,
				"decisions": []any{map[string]any{"userId": "approver-1", "comment": "token=secret"}},
			},
		},
		Summary: "pending token=secret", CreatedAt: now, UpdatedAt: now,
	}}
}

func gatewayGovernanceAuditFixtures(now time.Time) []domainaigateway.AuditLog {
	return []domainaigateway.AuditLog{{
		ID: "audit-1", ActorType: "user", ActorID: "user-2", AIClientID: "codex", SkillID: "delivery-tester",
		ToolName: "delivery.actions.trigger", RiskLevel: domainaigateway.RiskLevelExecute,
		Action: "ai_gateway.tool.invoke", Result: "pending", Summary: "held token=secret",
		Metadata: map[string]any{"approvalRequestId": "approval-1", "token": "secret-token"}, CreatedAt: now,
	}}
}

func assertRelayRuntimeToolManifest(t *testing.T, service *Service, principal domainidentity.Principal) {
	t.Helper()
	manifest, err := service.Capabilities(context.Background(), principal, domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	for _, toolName := range []string{"gateway.manifest.read", "gateway.relay.upstreams.list", "gateway.relay.model_routes.list"} {
		tool, ok := toolByNameFrom(toolName, manifest.Tools)
		if !ok {
			t.Fatalf("expected %s in manifest", toolName)
		}
		if tool.RiskLevel != domainaigateway.RiskLevelRead || !slices.Contains(tool.PermissionKeys, appaccess.PermAIGatewayInvoke) {
			t.Fatalf("unexpected manifest posture for %s: %#v", toolName, tool)
		}
	}
}

func assertGatewayGovernanceListToolManifest(t *testing.T, service *Service, principal domainidentity.Principal) {
	t.Helper()
	manifest, err := service.Capabilities(context.Background(), principal, domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	toolNames := []string{
		"gateway.clients.list", "gateway.tokens.list", "gateway.service_accounts.list", "gateway.tool_grants.list",
		"gateway.access_policies.list", "gateway.skill_bindings.list", "gateway.approvals.list", "gateway.audit_logs.list",
	}
	for _, toolName := range toolNames {
		tool, ok := toolByNameFrom(toolName, manifest.Tools)
		if !ok {
			t.Fatalf("expected %s in manifest", toolName)
		}
		if tool.RiskLevel != domainaigateway.RiskLevelRead || !slices.Contains(tool.PermissionKeys, appaccess.PermAIGatewayInvoke) || !slices.Contains(tool.PermissionKeys, appaccess.PermAIGatewayManage) {
			t.Fatalf("unexpected manifest posture for %s: %#v", toolName, tool)
		}
	}
}

func assertGatewayClientsListResult(t *testing.T, result domainaigateway.ToolInvocationResult) {
	t.Helper()
	items := mustValueAs[[]domainaigateway.AIClient](t, result.Output)
	if len(items) != 1 || items[0].ID != "codex" || result.RelatedIDs["count"] != 1 {
		t.Fatalf("expected active codex client, got %#v", result)
	}
}

func assertGatewayTokensListResult(t *testing.T, result domainaigateway.ToolInvocationResult) {
	t.Helper()
	output := mustValueAs[map[string]any](t, result.Output)
	personalTokens := mustMapFieldAs[[]domainaigateway.PersonalAccessToken](t, output, "personalAccessTokens")
	serviceTokens := mustMapFieldAs[[]domainaigateway.ServiceAccountToken](t, output, "serviceAccountTokens")
	if len(personalTokens) != 1 || personalTokens[0].TokenHash != "" || personalTokens[0].Metadata["token"] != "[REDACTED]" {
		t.Fatalf("expected redacted personal token metadata, got %#v", personalTokens)
	}
	if len(serviceTokens) != 1 || serviceTokens[0].TokenHash != "" || serviceTokens[0].Metadata["apiKey"] != "[REDACTED]" {
		t.Fatalf("expected redacted service token metadata, got %#v", serviceTokens)
	}
	if result.RelatedIDs["personalAccessTokenCount"] != 1 || result.RelatedIDs["serviceAccountTokenCount"] != 1 {
		t.Fatalf("unexpected token related ids: %#v", result.RelatedIDs)
	}
}

func assertGatewayServiceAccountsListResult(t *testing.T, result domainaigateway.ToolInvocationResult) {
	t.Helper()
	items := mustValueAs[[]domainaigateway.ServiceAccount](t, result.Output)
	if len(items) != 1 || items[0].ID != "svc-1" || result.RelatedIDs["count"] != 1 {
		t.Fatalf("expected active service account, got %#v", result)
	}
}

func assertGatewayToolGrantsListResult(t *testing.T, result domainaigateway.ToolInvocationResult) {
	t.Helper()
	items := mustValueAs[[]domainaigateway.ToolGrant](t, result.Output)
	if len(items) != 1 || items[0].ID != "grant-1" || result.RelatedIDs["subjectId"] != "auditor" {
		t.Fatalf("expected grant-1, got %#v", result)
	}
}

func assertGatewayAccessPoliciesListResult(t *testing.T, result domainaigateway.ToolInvocationResult) {
	t.Helper()
	items := mustValueAs[[]domainaigateway.AccessPolicy](t, result.Output)
	if len(items) != 1 || items[0].ID != "policy-1" {
		t.Fatalf("expected policy-1, got %#v", result)
	}
}

func assertGatewaySkillBindingsListResult(t *testing.T, result domainaigateway.ToolInvocationResult) {
	t.Helper()
	items := mustValueAs[[]domainaigateway.SkillBinding](t, result.Output)
	if len(items) != 1 || items[0].ID != "binding-1" || result.RelatedIDs["skillId"] != "k8s-sre" {
		t.Fatalf("expected binding-1, got %#v", result)
	}
}

func assertGatewayApprovalsListResult(t *testing.T, result domainaigateway.ToolInvocationResult) {
	t.Helper()
	items := mustValueAs[[]domainaigateway.ApprovalRequest](t, result.Output)
	if len(items) != 1 || items[0].ID != "approval-1" {
		t.Fatalf("expected approval-1, got %#v", result)
	}
	approval := items[0]
	if approval.ToolInput["token"] != "[REDACTED]" || strings.Contains(approval.Summary, "secret") {
		t.Fatalf("expected redacted approval payload, got %#v", approval)
	}
	if approval.ApprovalTrace == nil || len(approval.ApprovalTrace.Decisions) != 1 || strings.Contains(approval.ApprovalTrace.Decisions[0].Comment, "secret") {
		t.Fatalf("expected redacted approval trace, got %#v", approval.ApprovalTrace)
	}
}

func assertGatewayAuditLogsListResult(t *testing.T, result domainaigateway.ToolInvocationResult) {
	t.Helper()
	items := mustValueAs[[]domainaigateway.AuditLog](t, result.Output)
	if len(items) != 1 || items[0].ID != "audit-1" {
		t.Fatalf("expected audit-1, got %#v", result)
	}
	if items[0].Metadata["token"] != "[REDACTED]" || strings.Contains(items[0].Summary, "secret") {
		t.Fatalf("expected redacted audit log, got %#v", items[0])
	}
	if result.RelatedIDs["approvalRequestId"] != "approval-1" || result.RelatedIDs["limit"] != 10 {
		t.Fatalf("unexpected audit related ids: %#v", result.RelatedIDs)
	}
}

func assertManifestRuntimeTool(t *testing.T, service *Service, principal domainidentity.Principal) {
	t.Helper()
	result, err := service.InvokeTool(context.Background(), principal, domainaigateway.ToolInvocationRequest{
		ToolName: "gateway.manifest.read",
		Input:    map[string]any{"aiClientId": "codex", "skillId": "k8s-sre"},
	})
	if err != nil {
		t.Fatalf("gateway.manifest.read returned error: %v", err)
	}
	manifest := mustValueAs[domainaigateway.Manifest](t, result.Output)
	if manifest.Caller.AIClientID != "codex" || manifest.Caller.SkillID != "k8s-sre" {
		t.Fatalf("expected caller context to be preserved, got %#v", manifest.Caller)
	}
	if result.RelatedIDs["toolCount"] != manifest.Summary.ToolCount || result.RelatedIDs["skillId"] != "k8s-sre" {
		t.Fatalf("unexpected manifest related ids: %#v", result.RelatedIDs)
	}
}

func assertRelayUpstreamsRuntimeTool(t *testing.T, service *Service, principal domainidentity.Principal) {
	t.Helper()
	result, err := service.InvokeTool(context.Background(), principal, domainaigateway.ToolInvocationRequest{
		ToolName: "gateway.relay.upstreams.list",
		Input:    map[string]any{"providerKind": "openai"},
	})
	if err != nil {
		t.Fatalf("gateway.relay.upstreams.list returned error: %v", err)
	}
	upstreams := mustValueAs[[]domainaigateway.LLMUpstream](t, result.Output)
	if len(upstreams) != 1 || upstreams[0].ID != "upstream-openai" {
		t.Fatalf("expected active openai upstream, got %#v", result)
	}
	upstream := upstreams[0]
	if strings.Contains(upstream.BaseURL, "password") || strings.Contains(upstream.BaseURL, "secret") || upstream.APIKeyCiphertext != "" {
		t.Fatalf("expected relay upstream credentials to be redacted, got %#v", upstream)
	}
	if upstream.DefaultHeaders["Authorization"] != "[REDACTED]" || upstream.Metadata["credential"] != "[REDACTED]" {
		t.Fatalf("expected relay upstream metadata to be redacted, got %#v", upstream)
	}
}

func assertRelayRoutesRuntimeTool(t *testing.T, service *Service, principal domainidentity.Principal) {
	t.Helper()
	result, err := service.InvokeTool(context.Background(), principal, domainaigateway.ToolInvocationRequest{
		ToolName: "gateway.relay.model_routes.list",
		Input:    map[string]any{"publicModel": "gpt-4.1"},
	})
	if err != nil {
		t.Fatalf("gateway.relay.model_routes.list returned error: %v", err)
	}
	routes := mustValueAs[[]domainaigateway.LLMModelRoute](t, result.Output)
	if len(routes) != 1 || routes[0].ID != "route-gpt" || result.RelatedIDs["publicModel"] != "gpt-4.1" {
		t.Fatalf("expected route-gpt, got %#v", result)
	}
	if routes[0].TransformPolicy["apiKey"] != "[REDACTED]" || routes[0].Metadata["token"] != "[REDACTED]" {
		t.Fatalf("expected route policy metadata to be redacted, got %#v", routes[0])
	}
}

func assertApprovalRelayRuntimeToolManifest(t *testing.T, service *Service) {
	t.Helper()
	manifest, err := service.Capabilities(context.Background(), testPrincipal("admin"), domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	expectedTools := []struct {
		name       string
		risk       domainaigateway.RiskLevel
		permission string
		approval   bool
	}{
		{name: "gateway.approvals.decide", risk: domainaigateway.RiskLevelExecute, permission: appaccess.PermAIGatewayManage},
		{name: "gateway.relay.model_calls.list", risk: domainaigateway.RiskLevelRead, permission: appaccess.PermAIGatewayRelayView},
		{name: "gateway.relay.cache.purge", risk: domainaigateway.RiskLevelExecute, permission: appaccess.PermAIGatewayRelayManage, approval: true},
	}
	for _, expected := range expectedTools {
		tool, ok := toolByNameFrom(expected.name, manifest.Tools)
		if !ok {
			t.Fatalf("expected %s in manifest", expected.name)
		}
		if tool.RiskLevel != expected.risk || tool.RequiresApproval != expected.approval || !slices.Contains(tool.PermissionKeys, appaccess.PermAIGatewayInvoke) || !slices.Contains(tool.PermissionKeys, expected.permission) {
			t.Fatalf("unexpected manifest posture for %s: %#v", expected.name, tool)
		}
	}
}

func assertRelayModelCallsRuntimeTool(t *testing.T, service *Service) {
	t.Helper()
	viewer := testPrincipal("relay-viewer")
	result, err := service.InvokeTool(context.Background(), viewer, domainaigateway.ToolInvocationRequest{
		ToolName: "gateway.relay.model_calls.list",
		Input:    map[string]any{"publicModel": "gpt-4.1", "limit": 10},
	})
	if err != nil {
		t.Fatalf("gateway.relay.model_calls.list returned error: %v", err)
	}
	calls := mustValueAs[[]domainaigateway.LLMCallLog](t, result.Output)
	if len(calls) != 1 || calls[0].ID != "call-1" || result.RelatedIDs["publicModel"] != "gpt-4.1" {
		t.Fatalf("expected filtered call-1, got %#v", result)
	}
	call := calls[0]
	if call.TokenID != "" || call.TokenPrefix != "" || call.SourceIP != "" || call.UserAgent != "" {
		t.Fatalf("expected relay call identity fields to be redacted, got %#v", call)
	}
	if call.RouteTrace["Authorization"] != "[REDACTED]" || call.Metadata["apiKey"] != "[REDACTED]" || strings.Contains(call.ErrorMessage, "secret") {
		t.Fatalf("expected relay call metadata to be redacted, got %#v", call)
	}
	if _, err := service.ListLLMCallLogs(context.Background(), viewer, domainaigateway.LLMCallLogFilter{PublicModel: "gpt-4.1"}); err == nil {
		t.Fatalf("expected raw relay call log API to require relay manage permission")
	}
}

func assertRelayCachePurgeRequiresApproval(t *testing.T, service *Service, repo *relayTestRepository) {
	t.Helper()
	result, err := service.InvokeTool(context.Background(), testPrincipal("admin"), domainaigateway.ToolInvocationRequest{
		ToolName: "gateway.relay.cache.purge",
		Input:    map[string]any{"publicModel": "gpt-4.1", "dryRun": true},
	})
	if err != nil {
		t.Fatalf("gateway.relay.cache.purge dry run returned error: %v", err)
	}
	if result.Result != "pending_approval" || result.RelatedIDs["approvalRequestId"] == "" || len(repo.cacheEntries()) != 2 {
		t.Fatalf("expected direct cache purge to require approval without deleting, result=%#v caches=%#v", result, repo.cacheEntries())
	}
}

func assertApprovalDecisionRuntimeTool(t *testing.T, service *Service) {
	t.Helper()
	result, err := service.InvokeTool(context.Background(), testPrincipal("admin"), domainaigateway.ToolInvocationRequest{
		ToolName: "gateway.approvals.decide",
		Input:    map[string]any{"approvalRequestId": "approval-reject", "decision": "reject", "comment": "no token=secret"},
	})
	if err != nil {
		t.Fatalf("gateway.approvals.decide returned error: %v", err)
	}
	decision := mustValueAs[domainaigateway.ApprovalDecisionResult](t, result.Output)
	if decision.Request.Status != "rejected" || decision.Invocation != nil || result.RelatedIDs["decision"] != "reject" {
		t.Fatalf("expected rejected approval decision, got %#v", result)
	}
	if strings.Contains(decision.Request.DecisionComment, "secret") || decision.Request.ToolInput["token"] != "[REDACTED]" {
		t.Fatalf("expected redacted approval decision output, got %#v", decision.Request)
	}
}

func assertApprovedRelayCachePurge(t *testing.T, service *Service, repo *relayTestRepository) {
	t.Helper()
	result, err := service.InvokeTool(context.Background(), testPrincipal("admin"), domainaigateway.ToolInvocationRequest{
		ToolName: "gateway.approvals.decide",
		Input:    map[string]any{"approvalRequestId": "approval-cache-purge", "decision": "approve", "comment": "purge ok"},
	})
	if err != nil {
		t.Fatalf("gateway.approvals.decide approve cache purge returned error: %v", err)
	}
	decision := mustValueAs[domainaigateway.ApprovalDecisionResult](t, result.Output)
	if decision.Request.Status != "executed" || decision.Invocation == nil || len(repo.cacheEntries()) != 1 {
		t.Fatalf("expected cache purge approval to execute, result=%#v caches=%#v", result, repo.cacheEntries())
	}
	purge := mustValueAs[domainaigateway.LLMRelayCachePurgeResult](t, decision.Invocation.Output)
	if purge.Status != "purged" || purge.PurgedCount != 1 {
		t.Fatalf("expected approved purge to delete one entry, got %#v", purge)
	}
}

func assertFirstAndRepeatedApproval(t *testing.T, service *Service, apps *fakeApplicationService, approver domainidentity.Principal, requestID string) {
	t.Helper()
	first, err := service.ApproveApprovalRequest(context.Background(), approver, requestID, domainaigateway.ApprovalDecisionInput{Comment: "first"})
	if err != nil {
		t.Fatalf("first approval returned error: %v", err)
	}
	assertApprovalQuorumState(t, first, apps, "first")
	repeated, err := service.ApproveApprovalRequest(context.Background(), approver, requestID, domainaigateway.ApprovalDecisionInput{Comment: "still first"})
	if err != nil {
		t.Fatalf("repeated approval returned error: %v", err)
	}
	assertApprovalQuorumState(t, repeated, apps, "still first")
}

func assertApprovalQuorumState(t *testing.T, decision domainaigateway.ApprovalDecisionResult, apps *fakeApplicationService, comment string) {
	t.Helper()
	routing := mapValue(decision.Request.RelatedIDs["approvalRouting"])
	decisions := gatewayApprovalDecisions(routing)
	if decision.Request.Status != "pending" || routing["approvedCount"] != 1 || routing["requiredApprovals"] != 2 || apps.created {
		t.Fatalf("expected one approval without replay, request=%#v apps=%#v", decision.Request, apps)
	}
	if len(decisions) != 1 || decisions[0]["userId"] != "approver-1" || decisions[0]["comment"] != comment {
		t.Fatalf("unexpected approval decision metadata: %#v", decisions)
	}
}

func assertSecondApprovalExecutes(t *testing.T, service *Service, apps *fakeApplicationService, approver domainidentity.Principal, requestID string) {
	t.Helper()
	decision, err := service.ApproveApprovalRequest(context.Background(), approver, requestID, domainaigateway.ApprovalDecisionInput{Comment: "second"})
	if err != nil {
		t.Fatalf("second approval returned error: %v", err)
	}
	if decision.Request.Status != "executed" || !apps.created {
		t.Fatalf("expected second approval to execute owning service, request=%#v apps=%#v", decision.Request, apps)
	}
	routing := mapValue(decision.Request.RelatedIDs["approvalRouting"])
	if routing["approvedCount"] != 2 || len(gatewayApprovalDecisions(routing)) != 2 {
		t.Fatalf("expected final 2/2 approval routing, got %#v", routing)
	}
}

func invokeKubernetesDiagnostic(t *testing.T, service *Service, principal domainidentity.Principal, toolName string, input map[string]any) domainaigateway.ToolInvocationResult {
	t.Helper()
	result, err := service.InvokeTool(context.Background(), principal, domainaigateway.ToolInvocationRequest{ToolName: toolName, Input: input})
	if err != nil {
		t.Fatalf("%s returned error: %v", toolName, err)
	}
	return result
}

func assertKubernetesWorkloadDiagnostics(t *testing.T, service *Service, principal domainidentity.Principal) {
	t.Helper()
	rollout := invokeKubernetesDiagnostic(t, service, principal, "k8s.deployments.rollout_status", map[string]any{"clusterId": "cluster-a", "namespace": "prod", "deploymentName": "api"})
	if mustValueAs[domainresource.DeploymentRolloutStatusView](t, rollout.Output).Status != "progressing" {
		t.Fatalf("expected rollout status output, got %#v", rollout.Output)
	}
	events := invokeKubernetesDiagnostic(t, service, principal, "k8s.deployments.events", map[string]any{"clusterId": "cluster-a", "namespace": "prod", "deploymentName": "api"})
	if len(mustValueAs[[]domainresource.ClusterEventView](t, events.Output)) != 1 || events.RelatedIDs["count"] != 1 {
		t.Fatalf("expected deployment-specific events, got %#v", events)
	}
	describe := invokeKubernetesDiagnostic(t, service, principal, "k8s.pods.describe", map[string]any{"clusterId": "cluster-a", "namespace": "prod", "podName": "api-7d9f"})
	output := mustValueAs[map[string]any](t, describe.Output)
	summary := mustMapFieldAs[map[string]any](t, output, "summary")
	if output["name"] != "api-7d9f" || summary["restarts"] != int32(1) {
		t.Fatalf("expected pod describe summary, got %#v", output)
	}
}

func assertKubernetesNetworkDiagnostics(t *testing.T, service *Service, principal domainidentity.Principal) {
	t.Helper()
	backends := invokeKubernetesDiagnostic(t, service, principal, "k8s.services.backends", map[string]any{"clusterId": "cluster-a", "namespace": "prod", "serviceName": "api"})
	backendOutput := mustValueAs[map[string]any](t, backends.Output)
	if backendOutput["backendPodCount"] != 1 || backends.RelatedIDs["backendPodCount"] != 1 {
		t.Fatalf("expected one matching backend pod, got %#v", backendOutput)
	}
	routes := invokeKubernetesDiagnostic(t, service, principal, "k8s.routes.context", map[string]any{"clusterId": "cluster-a", "namespace": "prod", "serviceName": "api"})
	routeOutput := mustValueAs[map[string]any](t, routes.Output)
	if routeOutput["ingressCount"] != 1 || routeOutput["httpRouteCount"] != 1 || routeOutput["grpcRouteCount"] != 1 {
		t.Fatalf("expected ingress and Gateway API route context, got %#v", routeOutput)
	}
}

func assertKubernetesInfrastructureDiagnostics(t *testing.T, service *Service, principal domainidentity.Principal) {
	t.Helper()
	storage := invokeKubernetesDiagnostic(t, service, principal, "k8s.storage.context", map[string]any{"clusterId": "cluster-a", "namespace": "prod"})
	storageOutput := mustValueAs[map[string]any](t, storage.Output)
	unboundClaims := mustMapFieldAs[[]string](t, storageOutput, "unboundPersistentVolumeClaims")
	if storageOutput["persistentVolumeClaimCount"] != 1 || len(unboundClaims) != 1 {
		t.Fatalf("expected PVC/PV/storage class context, got %#v", storageOutput)
	}
	node := invokeKubernetesDiagnostic(t, service, principal, "k8s.nodes.detail", map[string]any{"clusterId": "cluster-a", "nodeName": "node-a"})
	if mustValueAs[domainresource.NodeDetailView](t, node.Output).PodCount != 1 || node.RelatedIDs["scheduledPodCount"] != 1 {
		t.Fatalf("expected node scheduled pod context, got %#v", node)
	}
}

func assertReleaseFailureEvidence(t *testing.T, resources *fakeResourceService, result domainaigateway.ToolInvocationResult) map[string]any {
	t.Helper()
	if !resources.listedPods || !resources.listedEvents || !resources.readLogs || resources.eventLimit != 25 {
		t.Fatalf("expected runtime evidence collection, got %#v", resources)
	}
	output := mustValueAs[map[string]any](t, result.Output)
	deliveryEvidence := mustMapFieldAs[map[string]any](t, output, "delivery")
	logs := mustMapFieldAs[[]domaindelivery.ExecutionLog](t, deliveryEvidence, "executionLogs")
	if logs[0].Message != "build failed token=[REDACTED]" || logs[0].Metadata["password"] != "[REDACTED]" {
		t.Fatalf("expected redacted delivery logs, got %#v", logs[0])
	}
	runtimeEvidence := mustMapFieldAs[map[string]any](t, output, "runtime")
	podLogs := mustMapFieldAs[domainresource.PodLogsView](t, runtimeEvidence, "podLogs")
	if podLogs.Content != "startup failed password=[REDACTED]" {
		t.Fatalf("expected redacted pod logs, got %#v", podLogs)
	}
	return output
}

func assertReleaseFailureArtifact(t *testing.T, recorder *fakeAnalysisArtifactRecorder, result domainaigateway.ToolInvocationResult, output map[string]any) {
	t.Helper()
	if result.RelatedIDs["executionTaskId"] != "task-1" || result.RelatedIDs["clusterId"] != "cluster-a" || result.RelatedIDs["agentRunId"] != "agent:gateway-1" {
		t.Fatalf("expected diagnosis and artifact related ids, got %#v", result.RelatedIDs)
	}
	artifact := mustMapFieldAs[map[string]any](t, output, "analysisArtifact")
	if artifact["artifactStored"] != true || artifact["agentRunId"] != "agent:gateway-1" {
		t.Fatalf("expected artifact persistence metadata, got %#v", artifact)
	}
	if recorder.input.CapabilityID != "delivery_failure" || len(recorder.input.Evidence) == 0 || len(recorder.input.Hypotheses) == 0 {
		t.Fatalf("expected structured delivery failure artifact input, got %#v", recorder.input)
	}
	if recorder.input.Scope.ClusterID != "cluster-a" || recorder.input.Scope.Namespace != "prod" || recorder.input.Scope.Workload != "api" {
		t.Fatalf("unexpected artifact scope: %#v", recorder.input.Scope)
	}
	if len(recorder.input.ToolExecutions) != 1 || recorder.input.ToolExecutions[0].ToolName != "diagnosis.release_failure.analyze" {
		t.Fatalf("expected Gateway tool execution snapshot, got %#v", recorder.input.ToolExecutions)
	}
	outputSnapshot := mustMapFieldAs[map[string]any](t, recorder.input.Output, "evidenceSummary")
	runtimeSnapshot := mustMapFieldAs[map[string]any](t, outputSnapshot, "runtime")
	if _, ok := runtimeSnapshot["podLogs"]; ok || runtimeSnapshot["podLogBytes"] == 0 {
		t.Fatalf("artifact snapshot must contain only a pod log byte summary: %#v", runtimeSnapshot)
	}
}

func assertGovernanceTokenAndPolicySummary(t *testing.T, status domainaigateway.GovernanceStatus) {
	t.Helper()
	assertGovernanceTokensAndMetrics(t, status)
	assertGovernanceApprovalAndPolicy(t, status)
}

func assertGovernanceTokensAndMetrics(t *testing.T, status domainaigateway.GovernanceStatus) {
	t.Helper()
	if status.Tokens.PersonalAccessTokens.Expired != 1 || len(status.Tokens.ExpiredActive) != 1 || status.Tokens.PersonalAccessTokens.ExpiringSoon != 1 || len(status.Tokens.ExpiringSoon) != 1 {
		t.Fatalf("personal token summary: %#v", status.Tokens)
	}
	if status.Tokens.ServiceAccountTokens.Stale != 1 || len(status.Tokens.Stale) != 1 {
		t.Fatalf("service token summary: %#v", status.Tokens)
	}
	if status.Metrics.TotalCalls != 3 || status.Metrics.DenyCount != 2 || status.Metrics.FailureCount != 1 || status.Metrics.PendingApprovalCount == 0 {
		t.Fatalf("governance metrics: %#v", status.Metrics)
	}
}

func assertGovernanceApprovalAndPolicy(t *testing.T, status domainaigateway.GovernanceStatus) {
	t.Helper()
	if status.Approvals.Pending != 2 || status.Approvals.DueSoon != 1 || status.Approvals.StalePending != 1 || status.Approvals.NextDueRequestID != "approval-due-soon" || status.Approvals.OldestPendingRequestID != "approval-stale" {
		t.Fatalf("approval summary: %#v", status.Approvals)
	}
	if status.PolicyCoverage.BudgetPolicies != 1 || status.PolicyCoverage.RateLimitPolicies != 1 || status.PolicyCoverage.RedactionPolicies != 1 || status.Clients.RegistrationApproval != "configured" || status.Health.Status != "critical" {
		t.Fatalf("governance policy or health summary: %#v", status)
	}
}

func assertGovernanceRedactionSummary(t *testing.T, status domainaigateway.GovernanceStatus) {
	t.Helper()
	redaction := status.Redaction
	if redaction.TotalMatches != 5 || redaction.AuditsWithRedaction != 2 || redaction.InputAudits != 1 || redaction.OutputAudits != 1 {
		t.Fatalf("redaction audit summary: %#v", redaction)
	}
	if redaction.FieldMatches != 1 || redaction.SensitiveKeyMatches != 1 || redaction.SecretClassifierMatches != 1 || redaction.ValuePatternMatches != 1 || redaction.StructuredSecretMatches != 1 {
		t.Fatalf("redaction match summary: %#v", redaction)
	}
	containsCount := func(items []domainaigateway.GovernanceMetricCount, key string, count int) bool {
		return slices.ContainsFunc(items, func(item domainaigateway.GovernanceMetricCount) bool { return item.Key == key && item.Count == count })
	}
	if !containsCount(redaction.TopClassifiers, "github", 1) || !containsCount(redaction.TopTools, "k8s.pods.logs", 1) || !containsCount(redaction.TopPolicies, "policy-1", 2) {
		t.Fatalf("redaction top counts: %#v", redaction)
	}
}

func assertGovernanceRecommendations(t *testing.T, status domainaigateway.GovernanceStatus) {
	t.Helper()
	if len(status.Anomalies) == 0 {
		t.Fatalf("missing anomalies: %#v", status)
	}
	for _, kind := range []string{"approval_sla_due_soon", "stale_gateway_approvals"} {
		if !slices.ContainsFunc(status.Anomalies, func(item domainaigateway.GovernanceFinding) bool {
			return item.Type == kind && item.ApprovalRequestID != ""
		}) {
			t.Fatalf("missing %s anomaly: %#v", kind, status.Anomalies)
		}
	}
	containsAction := func(match func(domainaigateway.GovernanceRecommendationAction) bool) bool {
		return slices.ContainsFunc(status.RecommendationActions, match)
	}
	if !containsAction(func(item domainaigateway.GovernanceRecommendationAction) bool {
		return item.Type == "token_rotation" && item.Severity == "critical" && slices.Contains(item.Refs, "pat-expired") && slices.Contains(item.Refs, "pat-soon")
	}) || !containsAction(func(item domainaigateway.GovernanceRecommendationAction) bool {
		return item.Type == "token_hygiene" && slices.Contains(item.Refs, "sat-stale") && governanceRecommendationHasServiceTokenRef(item)
	}) || !containsAction(func(item domainaigateway.GovernanceRecommendationAction) bool {
		return item.Type == "approval_sla" && item.TargetKind == "approval_requests" && slices.Contains(item.Refs, "approval-due-soon") && slices.Contains(item.Refs, "approval-stale")
	}) {
		t.Fatalf("missing governance recommendation actions: %#v", status.RecommendationActions)
	}
}

func assertHighRiskGovernanceFindings(t *testing.T, status domainaigateway.GovernanceStatus) {
	t.Helper()
	byPolicyID := map[string]domainaigateway.GovernanceFinding{}
	byGrantID := map[string]domainaigateway.GovernanceFinding{}
	for _, finding := range status.Anomalies {
		if finding.PolicyID != "" && finding.Type == "high_risk_allow_without_approval" {
			byPolicyID[finding.PolicyID] = finding
		}
		if finding.GrantID != "" && finding.Type == "high_risk_grant_without_approval" {
			byGrantID[finding.GrantID] = finding
		}
	}
	assertExpectedHighRiskFindings(t, byPolicyID, byGrantID)
	assertHighRiskGovernanceHealth(t, status)
}

func assertExpectedHighRiskFindings(t *testing.T, policies, grants map[string]domainaigateway.GovernanceFinding) {
	t.Helper()
	policy, policyOK := policies["policy-risk-open"]
	grant, grantOK := grants["grant-risk-open"]
	if !policyOK || policy.RiskLevel != domainaigateway.RiskLevelMutate || !grantOK || grant.RiskLevel != domainaigateway.RiskLevelMutate {
		t.Fatalf("missing unguarded findings: policies=%#v grants=%#v", policies, grants)
	}
	for _, id := range []string{"policy-risk-safe", "policy-catalog-guarded", "policy-disabled"} {
		if _, exists := policies[id]; exists {
			t.Fatalf("guarded policy has finding %s: %#v", id, policies[id])
		}
	}
	for _, id := range []string{"grant-risk-safe", "grant-catalog-guarded", "grant-expired"} {
		if _, exists := grants[id]; exists {
			t.Fatalf("guarded grant has finding %s: %#v", id, grants[id])
		}
	}
}

func assertHighRiskGovernanceHealth(t *testing.T, status domainaigateway.GovernanceStatus) {
	t.Helper()
	check := governanceHealthCheckByName(t, status.Health.Checks, "high_risk_guardrails")
	if check.Status != "degraded" || check.Count != 2 || status.Health.Status != "degraded" {
		t.Fatalf("high-risk governance health: %#v", status.Health)
	}
	if !slices.ContainsFunc(status.Recommendations, func(item string) bool { return strings.Contains(item, "high-risk Gateway allow") }) {
		t.Fatalf("missing high-risk recommendation: %#v", status.Recommendations)
	}
	if !slices.ContainsFunc(status.RecommendationActions, func(item domainaigateway.GovernanceRecommendationAction) bool {
		return item.Type == "high_risk_guardrails" && item.Action == "create_high_risk_approval_guardrail" && item.Count == 2 && slices.Contains(item.Refs, "policy-risk-open") && slices.Contains(item.Refs, "grant-risk-open")
	}) {
		t.Fatalf("missing high-risk recommendation action: %#v", status.RecommendationActions)
	}
}

func assertResourceScopeGovernanceStatus(t *testing.T, status domainaigateway.GovernanceStatus) {
	t.Helper()
	coverage := status.PolicyCoverage
	if coverage.ResourceScopedAccessPolicies != 1 || coverage.ResourceScopedToolGrants != 1 || coverage.ResourceScopeState != "configured" {
		t.Fatalf("expected resource scope policy coverage, got %#v", coverage)
	}
	policies, grants := resourceScopeFindings(status.Anomalies)
	assertResourceScopeFindingSet(t, policies, []string{"policy-unscoped", "policy-wildcard-scope"}, []string{"policy-scoped", "policy-read", "policy-dry-run"})
	assertResourceScopeFindingSet(t, grants, []string{"grant-unscoped", "grant-wildcard-scope"}, []string{"grant-scoped", "grant-read", "grant-expired"})
	assertResourceScopeGovernanceHealth(t, status)
}

func resourceScopeFindings(findings []domainaigateway.GovernanceFinding) (map[string]domainaigateway.GovernanceFinding, map[string]domainaigateway.GovernanceFinding) {
	policies := map[string]domainaigateway.GovernanceFinding{}
	grants := map[string]domainaigateway.GovernanceFinding{}
	for _, finding := range findings {
		switch finding.Type {
		case "high_risk_allow_without_resource_scope":
			policies[finding.PolicyID] = finding
		case "high_risk_grant_without_resource_scope":
			grants[finding.GrantID] = finding
		}
	}
	return policies, grants
}

func assertResourceScopeFindingSet(t *testing.T, findings map[string]domainaigateway.GovernanceFinding, expected, excluded []string) {
	t.Helper()
	for _, id := range expected {
		finding, ok := findings[id]
		if !ok || finding.RiskLevel != domainaigateway.RiskLevelExecute {
			t.Fatalf("expected unscoped high-risk finding for %s, got %#v", id, findings)
		}
	}
	for _, id := range excluded {
		if _, ok := findings[id]; ok {
			t.Fatalf("did not expect resource scope finding for %s: %#v", id, findings)
		}
	}
}

func assertResourceScopeGovernanceHealth(t *testing.T, status domainaigateway.GovernanceStatus) {
	t.Helper()
	check := governanceHealthCheckByName(t, status.Health.Checks, "high_risk_resource_scopes")
	if check.Status != "degraded" || check.Count != 4 {
		t.Fatalf("expected degraded resource-scope health check, got %#v", status.Health.Checks)
	}
	if !slices.ContainsFunc(status.Recommendations, func(item string) bool { return strings.Contains(item, "resourceScopes") }) {
		t.Fatalf("expected resource scope recommendation, got %#v", status.Recommendations)
	}
	if !slices.ContainsFunc(status.RecommendationActions, func(item domainaigateway.GovernanceRecommendationAction) bool {
		return item.Type == "high_risk_resource_scopes" && item.Action == "create_resource_scope_guardrail" && item.Count == 4 && item.Metadata["policyTemplate"] == "resource_scope_guardrail"
	}) {
		t.Fatalf("expected high-risk resource-scope recommendation action, got %#v", status.RecommendationActions)
	}
}

func assertSecretClassifierRejected(t *testing.T, apps *fakeApplicationService, repo *memoryGatewayRepository, err error, expected []string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "redaction policy") || apps.listed {
		t.Fatalf("secret classifier was not rejected before service invocation: err=%v listed=%v", err, apps.listed)
	}
	redaction := mapValue(repo.auditLogs[0].Metadata["redaction"])
	classifiers := fmt.Sprint(redaction["classifiers"])
	for _, classifier := range expected {
		if !strings.Contains(classifiers, classifier) {
			t.Fatalf("classifier %s missing from summary: %#v", classifier, redaction)
		}
	}
	if text := fmt.Sprint(repo.auditLogs[0].Metadata); strings.Contains(text, "12345678901234567890") {
		t.Fatalf("classifier audit leaked raw secret: %s", text)
	}
}

func newGroupQuotaTestService(repo *memoryGatewayRepository, apps *fakeApplicationService) *Service {
	permissions := appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"developer":         {appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsCreate},
		"release-manager":   {appaccess.PermAIGatewayManage, appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsCreate},
		"security-reviewer": {appaccess.PermAIGatewayManage, appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsCreate},
	}})
	service := newTestService(permissions, nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})
	return service
}

func mustValueAs[T any](t *testing.T, value any) T {
	t.Helper()
	result, ok := value.(T)
	if !ok {
		t.Fatalf("value has type %T, want %T", value, *new(T))
	}
	return result
}

func assertHeldDeliveryAction(t *testing.T, result domainaigateway.ToolInvocationResult, delivery *fakeDeliveryService, repo *memoryGatewayRepository) {
	t.Helper()
	if delivery.triggered || result.Result != "pending_approval" || !result.RequiresApproval {
		t.Fatalf("unexpected held delivery result: %#v", result)
	}
	if result.RelatedIDs["approvalRequestId"] == "" || len(repo.approvalRequests) != 1 {
		t.Fatalf("missing durable approval request: result=%#v requests=%#v", result, repo.approvalRequests)
	}
	request := repo.approvalRequests[0]
	if request.Status != "pending" || request.ToolName != "delivery.actions.trigger" || request.ToolInput["applicationId"] != "app-1" || request.ExpiresAt == nil {
		t.Fatalf("unexpected approval request: %#v", request)
	}
}

func assertHeldDeliveryAudit(t *testing.T, repo *memoryGatewayRepository, operations *captureOperationRecorder) {
	t.Helper()
	if len(repo.auditLogs) != 1 || repo.auditLogs[0].Result != "pending_approval" || len(operations.entries) != 1 || operations.entries[0].Result != "pending_approval" {
		t.Fatalf("unexpected pending audit state: gateway=%#v operations=%#v", repo.auditLogs, operations.entries)
	}
	requestID := repo.approvalRequests[0].ID
	if repo.auditLogs[0].Metadata["approvalRequestId"] != requestID || repo.auditLogs[0].Metadata["approvalId"] != requestID {
		t.Fatalf("gateway audit linkage: %#v", repo.auditLogs[0].Metadata)
	}
	metadata := operations.entries[0].Metadata
	if metadata["approvalRequestId"] != requestID || metadata["approvalId"] != requestID || metadata["strategy"] != "require_approval" || metadata["toolName"] != "delivery.actions.trigger" {
		t.Fatalf("operation audit linkage: %#v", metadata)
	}
}

func assertApprovedDeliveryDecision(t *testing.T, decision domainaigateway.ApprovalDecisionResult, delivery *fakeDeliveryService, requestID string) {
	t.Helper()
	if !delivery.triggered || decision.Request.Status != "executed" || decision.Invocation == nil {
		t.Fatalf("unexpected approval decision: %#v", decision)
	}
	if decision.Invocation.Result != "success" || decision.Invocation.RelatedIDs["executionTaskId"] != "task-1" {
		t.Fatalf("unexpected replay invocation: %#v", decision.Invocation)
	}
	if decision.Invocation.RelatedIDs["workflowRunId"] != "workflow-1" || decision.Request.RelatedIDs["workflowRunId"] != "workflow-1" {
		t.Fatalf("missing workflow linkage: %#v", decision)
	}
	variables := delivery.lastActionInput.Variables
	if variables["aiGatewayApprovalRequestId"] != requestID || variables["aiGatewayToolName"] != "delivery.actions.trigger" || decision.Request.DecisionComment != "ship it" || decision.Request.DecidedBy != "user-1" {
		t.Fatalf("unexpected replay decision metadata: request=%#v variables=%#v", decision.Request, variables)
	}
}

func assertApprovalOperationEntries(t *testing.T, entries []domainoperation.Entry, requestID string) {
	t.Helper()
	if len(entries) < 4 {
		t.Fatalf("approval operation entries: %#v", entries)
	}
	byType := map[string]domainoperation.Entry{}
	for _, entry := range entries {
		byType[entry.OperationType+":"+entry.Result] = entry
		if entry.Metadata["approvalRequestId"] != requestID || entry.Metadata["approvalId"] != requestID {
			t.Fatalf("operation approval linkage: %#v", entry)
		}
	}
	if byType["ai_gateway.approval.approve:approved"].TargetScope["approvalRequestId"] != requestID || byType["ai_gateway.tool.invoke:success"].Metadata["workflowRunId"] != "workflow-1" || byType["ai_gateway.approval.execute:executed"].Metadata["workflowRunId"] != "workflow-1" {
		t.Fatalf("operation workflow linkage: %#v", entries)
	}
}

func assertApprovalGatewayAudits(t *testing.T, audits []domainaigateway.AuditLog, requestID string) {
	t.Helper()
	byAction := map[string]domainaigateway.AuditLog{}
	for _, entry := range audits {
		byAction[entry.Action+":"+entry.Result] = entry
		if entry.Metadata["approvalRequestId"] != requestID || entry.Metadata["approvalId"] != requestID {
			t.Fatalf("gateway approval linkage: %#v", entry)
		}
	}
	if byAction["ai_gateway.tool.invoke:success"].Metadata["workflowRunId"] != "workflow-1" || byAction["ai_gateway.approval.execute:executed"].Metadata["workflowRunId"] != "workflow-1" {
		t.Fatalf("gateway workflow linkage: %#v", audits)
	}
}

func mustMapFieldAs[T any](t *testing.T, values map[string]any, key string) T {
	t.Helper()
	value, ok := values[key]
	if !ok {
		t.Fatalf("map is missing key %q", key)
	}
	return mustValueAs[T](t, value)
}

func closeWebSocketOnCleanup(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close WebSocket connection: %v", err)
		}
	})
}
