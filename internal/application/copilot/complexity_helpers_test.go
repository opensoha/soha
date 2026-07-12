package copilot

import (
	"strings"
	"testing"
	"time"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
)

func assertGeneralGatewayEnvelope(t *testing.T, envelope domaincopilot.SessionMessageEnvelope) {
	t.Helper()
	if len(envelope.ToolCalls) != 0 || len(envelope.AnalysisArtifacts) != 0 || len(envelope.SessionPatch) != 0 {
		t.Fatalf("general chat unexpectedly ran analysis: %#v", envelope)
	}
	if len(envelope.Messages) != 2 || envelope.Messages[1].Content != "hello from gateway" {
		t.Fatalf("unexpected general chat messages: %#v", envelope.Messages)
	}
	metadata := envelope.Messages[1].Metadata
	if metadata["source"] != "gateway-model-route" || metadata["model"] != "gpt-public" {
		t.Fatalf("unexpected gateway route metadata: %#v", metadata)
	}
	if metadata["thinkingSummary"] == "" || metadata["agentStatus"] == nil {
		t.Fatalf("missing replay metadata snapshot: %#v", metadata)
	}
	artifacts := mustCopilotValue[[]domaincopilot.AnalysisArtifact](t, metadata["analysisArtifacts"])
	if len(artifacts) != 0 {
		t.Fatalf("general chat analysis artifacts: %#v", artifacts)
	}
	if _, ok := metadata["providerKind"]; ok {
		t.Fatalf("general chat exposed provider kind: %#v", metadata)
	}
	if _, ok := metadata["providerId"]; ok {
		t.Fatalf("general chat exposed provider id: %#v", metadata)
	}
}

func assertGeneralGatewayPersistence(t *testing.T, repo *inspectionAuthzTestRepository) {
	t.Helper()
	if len(repo.createdMessages) != 2 {
		t.Fatalf("persisted messages: %#v", repo.createdMessages)
	}
}

func assertGeneralGatewayRequest(t *testing.T, request appaigateway.WorkbenchRelayRequest) {
	t.Helper()
	if request.PublicModel != "gpt-public" || request.RouteID != "route-openai" || request.Endpoint != "chat/completions" {
		t.Fatalf("unexpected workbench request: %#v", request)
	}
	if request.SessionID != "session-1" || request.Mode != "general" {
		t.Fatalf("missing session correlation: %#v", request)
	}
	if len(request.Messages) == 0 || request.Messages[len(request.Messages)-1].Content != "hi" {
		t.Fatalf("current user message missing: %#v", request.Messages)
	}
}

func assertNarrowedInspectionArtifact(t *testing.T, artifacts []domaincopilot.AnalysisArtifact) {
	t.Helper()
	if len(artifacts) != 1 {
		t.Fatalf("inspection artifacts: %#v", artifacts)
	}
	artifact := artifacts[0]
	if artifact.Scope.ClusterID != "cluster-a" || artifact.Scope.Namespace != "payments" || artifact.Scope.TimeRangeMinutes != 30 {
		t.Fatalf("narrowed scope: %#v", artifact.Scope)
	}
	adapters := mustCopilotValue[[]string](t, artifact.DataSourceSnapshot["enabledAdapterIds"])
	if len(adapters) != 1 || adapters[0] != "metrics.v1" {
		t.Fatalf("adapter allowlist: %#v", adapters)
	}
	skills := mustCopilotValue[[]string](t, artifact.DataSourceSnapshot["enabledSkillIds"])
	if len(skills) != 1 || skills[0] != "inspection-review" {
		t.Fatalf("skill allowlist: %#v", skills)
	}
	disabled := mustCopilotValue[[]string](t, artifact.DataSourceSnapshot["disabledToolNames"])
	if !containsString(disabled, "logs.v1.logs.raw") || !containsString(disabled, "metrics.v1.metrics.raw") {
		t.Fatalf("disabled tool union: %#v", disabled)
	}
	budget := mustCopilotValue[map[string]any](t, artifact.ToolExecutions[0].Output["budgetOverrides"])
	if budget["timeoutSeconds"] != 60 || budget["maxEvidenceItems"] != 10 {
		t.Fatalf("conservative budget: %#v", budget)
	}
	if _, ok := budget["maxIterations"]; ok {
		t.Fatalf("unknown budget key accepted: %#v", budget)
	}
}

func assertInspectionStreamContract(t *testing.T, result domaincopilot.WorkbenchStreamResult) {
	t.Helper()
	if !containsStreamEvent(result.Events, "thinking.delta") || !containsStreamEvent(result.Events, "tool.completed") || !containsStreamEvent(result.Events, "artifact.updated") {
		t.Fatalf("missing analysis stream events: %#v", result.Events)
	}
	assistant := result.Envelope.Messages[len(result.Envelope.Messages)-1]
	metadata := assistant.Metadata
	if metadata["thinkingSummary"] == "" || metadata["toolExecutions"] == nil || metadata["sources"] == nil || metadata["agentStatus"] == nil {
		t.Fatalf("missing assistant replay metadata: %#v", metadata)
	}
}

func assertGatewayTokenStreamResult(t *testing.T, result domaincopilot.WorkbenchStreamResult, request appaigateway.WorkbenchRelayRequest) {
	t.Helper()
	if len(result.Events) != 0 {
		t.Fatalf("live stream returned duplicate events: %#v", result.Events)
	}
	if len(result.Envelope.Messages) != 2 || result.Envelope.Messages[1].Content != "hello stream" {
		t.Fatalf("unexpected stream envelope: %#v", result.Envelope)
	}
	if request.RouteID != "route-openai" || request.Endpoint != "chat/completions" {
		t.Fatalf("unexpected stream request: %#v", request)
	}
	metadata := result.Envelope.Messages[1].Metadata
	if metadata["source"] != "gateway-model-route-stream" || metadata["thinkingSummary"] == "" || metadata["agentStatus"] == nil {
		t.Fatalf("missing stream replay metadata: %#v", metadata)
	}
	status := mustCopilotValue[map[string]any](t, metadata["agentStatus"])
	if status["providerId"] != agentProviderInternal || status["providerKind"] != "openai" {
		t.Fatalf("unexpected gateway provider status: %#v", status)
	}
}

func assertGatewayTokenStreamEvents(t *testing.T, events []domaincopilot.WorkbenchStreamEvent) {
	t.Helper()
	var delta strings.Builder
	for _, event := range events {
		if event.Type == "message.delta" {
			delta.WriteString(event.ContentDelta)
		}
	}
	if delta.String() != "hello stream" {
		t.Fatalf("unexpected deltas %q: %#v", delta.String(), events)
	}
	first := events[0]
	if first.Type != "agent.status" || first.ProviderID != agentProviderInternal || first.ProviderKind != "openai" || first.Status != "running" {
		t.Fatalf("unexpected initial status: %#v", first)
	}
	last := events[len(events)-1]
	if !containsStreamEvent(events, "message.done") || last.Type != "agent.status" {
		t.Fatalf("missing terminal events: %#v", events)
	}
	if last.ProviderID != agentProviderInternal || last.ProviderKind != "openai" || last.Status != "succeeded" {
		t.Fatalf("unexpected final status: %#v", last)
	}
}

func TestRootCausePlaybookEnabled(t *testing.T) {
	tests := []struct {
		name       string
		playbooks  map[string]bool
		conditions []bool
		expected   bool
	}{
		{name: "enabled with all evidence", playbooks: map[string]bool{"release-correlation": true}, conditions: []bool{true, true}, expected: true},
		{name: "disabled playbook", playbooks: map[string]bool{"release-correlation": false}, conditions: []bool{true, true}},
		{name: "missing evidence", playbooks: map[string]bool{"release-correlation": true}, conditions: []bool{true, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rootCausePlaybookEnabled(tt.playbooks, "release-correlation", tt.conditions...); got != tt.expected {
				t.Fatalf("rootCausePlaybookEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLogEvidenceSourceEnabled(t *testing.T) {
	base := domaincopilot.DataSource{ID: "logs-main", Enabled: true, SourceKind: "logs", MCPAdapter: "logs.v1"}
	tests := []struct {
		name     string
		source   domaincopilot.DataSource
		profile  domaincopilot.AnalysisProfile
		toolset  domaincopilot.SessionToolset
		expected bool
	}{
		{name: "default profile", source: base, expected: true},
		{name: "source selected by id", source: base, profile: domaincopilot.AnalysisProfile{EnabledSources: []string{"logs-main"}}, expected: true},
		{name: "source selected by kind", source: base, profile: domaincopilot.AnalysisProfile{EnabledSources: []string{"logs"}}, expected: true},
		{name: "profile excludes source", source: base, profile: domaincopilot.AnalysisProfile{EnabledSources: []string{"metrics"}}},
		{name: "toolset excludes adapter", source: base, toolset: domaincopilot.SessionToolset{EnabledAdapterIDs: []string{"metrics.v1"}}},
		{name: "disabled source", source: domaincopilot.DataSource{ID: "logs-main", SourceKind: "logs", MCPAdapter: "logs.v1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := logEvidenceSourceEnabled(tt.source, tt.profile, tt.toolset, "logs.v1"); got != tt.expected {
				t.Fatalf("logEvidenceSourceEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAlertAutomationBuildersPreserveExecutionContext(t *testing.T) {
	policy := domaincopilot.AutomationPolicy{ID: "policy-1", AnalysisProfileID: "profile-1"}
	instance := domainalert.Instance{
		ID:        "alert-1",
		Title:     "API unavailable",
		ClusterID: "cluster-1",
		Namespace: "production",
		Labels:    map[string]string{"deployment": "payments"},
	}

	input := alertAutomationRootCauseInput(policy, instance)
	if input.AnalysisProfileID != policy.AnalysisProfileID || input.AlertID != instance.ID || input.WorkloadName != "payments" || input.TriggerType != "alert_webhook" {
		t.Fatalf("root cause input = %#v", input)
	}
	session := alertAutomationSession(policy, "trace", instance, "dedup-1")
	if session.ID != "automation:policy-1" || session.CreatedBy != automationRootCauseCreatedBy || session.Metadata["mode"] != "trace" {
		t.Fatalf("automation session = %#v", session)
	}
}

func TestAppendCompletedSessionAnalysisPreservesResultContract(t *testing.T) {
	result := localSessionAnalysisResult{refs: []domaincopilot.AnalysisRunRef{{ID: "existing"}}}
	result = appendCompletedSessionAnalysis(result, []domaincopilot.ToolExecution{{ID: "tool-1"}}, domaincopilot.AnalysisArtifact{RunID: "run-1", Kind: "trace"})
	if len(result.toolCalls) != 1 || len(result.artifacts) != 1 || len(result.refs) != 2 {
		t.Fatalf("result = %#v", result)
	}
	ref := result.refs[1]
	if ref.ID != "run-1" || ref.Kind != "trace" || ref.Status != "completed" {
		t.Fatalf("analysis ref = %#v", ref)
	}
	if _, err := time.Parse(time.RFC3339, ref.CreatedAt); err != nil {
		t.Fatalf("analysis ref createdAt = %q: %v", ref.CreatedAt, err)
	}
}
