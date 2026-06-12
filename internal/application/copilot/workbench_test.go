package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestToolsetAllowsToolHonorsAdapterSelectionAndDisabledTools(t *testing.T) {
	toolset := domaincopilot.SessionToolset{
		EnabledAdapterIDs: []string{"metrics.v1"},
		DisabledToolNames: []string{"metrics.v1.metrics.anomaly_summary"},
	}

	if toolsetAllowsTool(toolset, "traces.v1", "traces.find_slow_spans") {
		t.Fatalf("expected trace tool to be blocked when traces adapter is not enabled")
	}
	if toolsetAllowsTool(toolset, "metrics.v1", "metrics.anomaly_summary") {
		t.Fatalf("expected disabled metrics tool to be blocked")
	}
	if !toolsetAllowsTool(toolset, "metrics.v1", "metrics.list") {
		t.Fatalf("expected other metrics tools to remain available")
	}
	if !toolsetAllowsTool(domaincopilot.SessionToolset{EnabledAdapterIDs: []string{"logs"}}, "logs.v1", "logs.query") {
		t.Fatalf("expected source-kind adapter selection to allow versioned adapter id")
	}
}

func TestBuildInspectionReviewArtifactCreatesEvidenceAndGraph(t *testing.T) {
	startedAt := time.Date(2026, 5, 24, 10, 30, 0, 0, time.UTC)
	run := domaincopilot.InspectionRun{
		ID:       "run-1",
		TaskID:   "task-1",
		Status:   "completed",
		Severity: "critical",
		Summary:  "Inspection completed with 1 finding.",
		Findings: []domaincopilot.InspectionFinding{{
			ID:             "finding-1",
			Title:          "Alert pressure is elevated",
			Severity:       "critical",
			Summary:        "There are critical firing alerts.",
			Recommendation: "Open Alerts and validate whether incidents should block releases.",
			Source:         "alerts",
			Data:           map[string]any{"criticalAlerts": []string{"api-latency"}},
		}},
		Report:    map[string]any{"alertCount": 1},
		StartedAt: startedAt,
	}
	scope := domaincopilot.SessionScope{ClusterID: "cluster-a", Namespace: "default"}

	artifact := buildInspectionReviewArtifact(scope, run, "en-US")

	if artifact.Kind != "inspection_review" || artifact.RunID != run.ID {
		t.Fatalf("unexpected artifact identity: kind=%s runID=%s", artifact.Kind, artifact.RunID)
	}
	if len(artifact.Evidence) != 1 {
		t.Fatalf("expected 1 evidence item, got %d", len(artifact.Evidence))
	}
	if artifact.Evidence[0].Kind != "inspection.finding" || artifact.Evidence[0].Attributes["source"] != "alerts" {
		t.Fatalf("unexpected evidence payload: %#v", artifact.Evidence[0])
	}
	if len(artifact.Recommendations) != 1 {
		t.Fatalf("expected finding recommendation to be promoted, got %d", len(artifact.Recommendations))
	}
	if artifact.DataSourceSnapshot["alertCount"] != 1 {
		t.Fatalf("expected report fields in data source snapshot: %#v", artifact.DataSourceSnapshot)
	}
	if artifact.Graph == nil {
		t.Fatalf("expected review graph")
	}
	if !graphHasNode(artifact.Graph, "inspection-finding:finding-1") {
		t.Fatalf("expected finding node in graph: %#v", artifact.Graph.Nodes)
	}
	if !graphHasEdge(artifact.Graph, "inspection-run:run-1->inspection-finding:finding-1") {
		t.Fatalf("expected run-to-finding edge in graph: %#v", artifact.Graph.Edges)
	}
}

func TestWithinCooldownWindowHonorsConfiguredSeconds(t *testing.T) {
	recentRuns := []domaincopilot.RootCauseRun{{CreatedAt: time.Now().UTC().Add(-2 * time.Minute)}}

	if !withinCooldownWindow(recentRuns, 300) {
		t.Fatalf("expected recent automation run to be inside the cooldown window")
	}
	if withinCooldownWindow(recentRuns, 60) {
		t.Fatalf("expected old automation run to be outside the shorter cooldown window")
	}
	if withinCooldownWindow(recentRuns, 0) {
		t.Fatalf("expected zero cooldown to disable policy-level cooldown")
	}
}

func TestWithinAgentRunWindowsMirrorAutomationRunWindows(t *testing.T) {
	recentRuns := []domaincopilot.AgentRun{{CreatedAt: time.Now().UTC().Add(-2 * time.Minute)}}

	if !withinAgentRunDedupWindow(recentRuns, 300) {
		t.Fatalf("expected recent external agent run to be inside the dedup window")
	}
	if withinAgentRunDedupWindow(recentRuns, 60) {
		t.Fatalf("expected old external agent run to be outside the shorter dedup window")
	}
	if !withinAgentRunDedupWindow(recentRuns, 0) {
		t.Fatalf("expected zero dedup window to fall back to the default dedup window")
	}
	if !withinAgentRunCooldownWindow(recentRuns, 300) {
		t.Fatalf("expected recent external agent run to be inside the cooldown window")
	}
	if withinAgentRunCooldownWindow(recentRuns, 0) {
		t.Fatalf("expected zero cooldown to disable external agent policy cooldown")
	}
}

func TestExternalAIReplyParsesDataPrefixedOpenAICompatibleChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected chat completion path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我先按当前会话梳理问题。\"}}]}\n"))
		_, _ = w.Write([]byte("data: [DONE]\n"))
	}))
	defer server.Close()

	service := &Service{http: server.Client()}
	reply, err := service.externalAIReply(context.Background(), domainsettings.AIProviderSettings{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "provider-model",
	}, []chatProviderMessage{
		{Role: "system", Content: systemPrompt("zh-CN")},
		{Role: "user", Content: "帮我梳理当前问题"},
	}, "zh-CN")
	if err != nil {
		t.Fatalf("external AI reply: %v", err)
	}
	if reply != "我先按当前会话梳理问题。" {
		t.Fatalf("expected data-prefixed reply, got %q", reply)
	}
}

func TestOpenAICompatibleChatReplyFromBodyAccumulatesSSEChunks(t *testing.T) {
	reply, err := openAICompatibleChatReplyFromBody([]byte(
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\".\"}}]}\n" +
			"data: [DONE]\n",
	))
	if err != nil {
		t.Fatalf("parse streamed reply: %v", err)
	}
	if reply != "Hello world." {
		t.Fatalf("expected accumulated streamed reply, got %q", reply)
	}
}

func TestLegacyPlatformContextPredicatePreservesAnalysisArtifacts(t *testing.T) {
	message := domaincopilot.Message{
		Role:    "assistant",
		Content: "发现数据库连接异常",
		Metadata: map[string]any{
			"source": "platform-context",
			"analysisArtifacts": []domaincopilot.AnalysisArtifact{{
				Kind:    "root_cause",
				RunID:   "run-1",
				Summary: "发现数据库连接异常",
			}},
		},
	}
	if isLegacyPlatformContextMessage(message) {
		t.Fatalf("analysis artifacts persisted with old platform-context source must stay visible")
	}
}

func TestAutomationAgentSkillIDsPreservePlaybooksAndAddProviderSkill(t *testing.T) {
	skills := automationAgentSkillIDs("root_cause", []string{"release-correlation", "cluster-health"})
	if !containsString(skills, "release-correlation") || !containsString(skills, "cluster-health") {
		t.Fatalf("expected platform playbooks to be preserved, got %#v", skills)
	}
	if !containsString(skills, "root-cause-investigation") {
		t.Fatalf("expected provider skill to be added, got %#v", skills)
	}
}

func TestBuildAlertAutomationDedupPrefix(t *testing.T) {
	if got := buildAlertAutomationDedupPrefix(" policy:one "); got != "policy:one:" {
		t.Fatalf("unexpected dedup prefix: %q", got)
	}
	if got := buildAlertAutomationDedupPrefix(" "); got != "" {
		t.Fatalf("expected empty prefix for blank policy id, got %q", got)
	}
}

func TestSessionAnalysisRunPersistenceUsesAutomationContext(t *testing.T) {
	createdBy, triggerType, dedupKey := sessionAnalysisRunPersistence("automation:policy-1", domaincopilot.SessionMetadata{
		PinnedContext: map[string]any{
			"triggerType": "alert_webhook",
			"dedupKey":    "policy-1:fingerprint:cluster-a:default",
		},
	})

	if createdBy != automationRootCauseCreatedBy {
		t.Fatalf("expected automation runs to use automation owner, got %q", createdBy)
	}
	if triggerType != "alert_webhook" {
		t.Fatalf("expected pinned trigger type, got %q", triggerType)
	}
	if dedupKey != "policy-1:fingerprint:cluster-a:default" {
		t.Fatalf("expected pinned dedup key, got %q", dedupKey)
	}
}

func TestApplySessionAnalysisPatchPersistsExplicitAnalysisMode(t *testing.T) {
	metadata := applySessionAnalysisPatch(domaincopilot.SessionMetadata{
		Mode:    "root_cause",
		Summary: "old summary",
		AnalysisRunRefs: []domaincopilot.AnalysisRunRef{{
			ID:     "run-old",
			Kind:   "root_cause",
			Status: "completed",
		}},
	}, map[string]any{
		"mode":    " trace ",
		"summary": " slow spans detected ",
		"analysisRunRefs": []domaincopilot.AnalysisRunRef{{
			ID:     "trace:run-1",
			Kind:   "trace",
			Status: "completed",
		}},
	})

	if metadata.Mode != "trace" {
		t.Fatalf("expected explicit analysis mode to become session mode, got %q", metadata.Mode)
	}
	if metadata.Summary != "slow spans detected" {
		t.Fatalf("expected trimmed summary, got %q", metadata.Summary)
	}
	if len(metadata.AnalysisRunRefs) != 1 || metadata.AnalysisRunRefs[0].Kind != "trace" {
		t.Fatalf("expected trace run refs, got %#v", metadata.AnalysisRunRefs)
	}
}

func TestRunnableSessionAnalysisModeRejectsNonArtifactModes(t *testing.T) {
	for _, mode := range []string{"root_cause", "performance", "trace", "inspection_review"} {
		if !isRunnableSessionAnalysisMode(mode) {
			t.Fatalf("expected %q to be runnable", mode)
		}
	}
	for _, mode := range []string{"", "general", "unknown"} {
		if isRunnableSessionAnalysisMode(mode) {
			t.Fatalf("expected %q to be rejected for explicit analysis", mode)
		}
	}
}

func TestInspectionSessionHandoffRequiresChatAndViewPermissions(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"partial": {appaccess.PermObserveAIChatUse},
	})

	_, err := service.CreateSessionFromInspectionRun(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"partial"},
	}, "run-1", "en-US")

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("expected access denied for missing observe.ai.view, got %v", err)
	}
	if repo.listInspectionRunsCalled {
		t.Fatalf("expected handoff to fail before reading inspection runs")
	}
}

func TestSessionToInspectionHandoffRequiresChatAndManagePermissions(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"partial": {appaccess.PermObserveAIInspectionManage},
	})

	_, err := service.CreateInspectionTaskFromSession(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"partial"},
	}, "session-1", domaincopilot.InspectionTaskInput{Title: "Generated inspection"}, "en-US")

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("expected access denied for missing observe.ai.chat, got %v", err)
	}
	if repo.getSessionCalled {
		t.Fatalf("expected handoff to fail before reading the session")
	}
}

func TestWorkbenchCatalogAllowsChatUsersWithoutExposingProviderSettings(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, _ := newInspectionAuthzTestService(map[string][]string{
		"chat": {appaccess.PermObserveAIChatUse},
	})
	service.settings = inspectionAuthzSettingsResolver{settings: domainsettings.AISettings{
		Provider: domainsettings.AIProviderSettings{
			APIKey: "secret-key",
			Model:  "gpt-test",
		},
		SkillsRegistry: []domainsettings.SkillDefinition{{
			ID:             "skill-1",
			Name:           "Skill One",
			Description:    "Safe skill summary",
			Enabled:        true,
			InputSchema:    map[string]any{"secret": "not exported"},
			OutputSchema:   map[string]any{"secret": "not exported"},
			CapabilityRefs: []string{"logs.query"},
		}},
	}}

	catalog, err := service.GetWorkbenchCatalog(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"chat"},
	})

	if err != nil {
		t.Fatalf("expected chat user to read workbench catalog, got %v", err)
	}
	if len(catalog.DataSources) != 1 {
		t.Fatalf("expected one data source summary, got %#v", catalog.DataSources)
	}
	dataSourcePayload, err := json.Marshal(catalog.DataSources[0])
	if err != nil {
		t.Fatalf("marshal data source summary: %v", err)
	}
	if strings.Contains(string(dataSourcePayload), "credential") {
		t.Fatalf("expected safe data source summary, got %#v", catalog.DataSources)
	}
	if len(catalog.AnalysisProfiles) != 1 || catalog.AnalysisProfiles[0].ID != "profile:inspection" {
		t.Fatalf("expected profile summaries, got %#v", catalog.AnalysisProfiles)
	}
	if len(catalog.SkillsRegistry) != 1 || catalog.SkillsRegistry[0].ID != "skill-1" {
		t.Fatalf("expected safe skill summary, got %#v", catalog.SkillsRegistry)
	}
}

func TestWorkbenchCatalogRejectsUsersWithoutAIWorkbenchPermissions(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"none": {},
	})

	_, err := service.GetWorkbenchCatalog(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"none"},
	})

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("expected access denied without AI permissions, got %v", err)
	}
	if repo.listDataSourcesCalled {
		t.Fatalf("expected catalog to fail before reading data sources")
	}
}

func TestSendMessageGeneralModeUsesModelProviderWithoutAutoAnalysis(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected chat completion path: %s", r.URL.Path)
		}
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider payload: %v", err)
		}
		if len(payload.Messages) != 2 || payload.Messages[1].Content != "hi" {
			t.Fatalf("expected system + user provider messages, got %#v", payload.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Hello from the model."}}]}`))
	}))
	defer server.Close()

	service, repo := newInspectionAuthzTestService(map[string][]string{
		"chat": {appaccess.PermObserveAIChatUse},
	})
	service.http = server.Client()
	service.settings = inspectionAuthzSettingsResolver{settings: domainsettings.AISettings{
		Provider: domainsettings.AIProviderSettings{
			ID:           "provider-1",
			ProviderKind: "openai-compatible",
			Enabled:      true,
			BaseURL:      server.URL,
			APIKey:       "secret-key",
			Model:        "test-model",
		},
	}}
	repo.session = domaincopilot.Session{
		ID:        "session-1",
		Title:     "General chat",
		CreatedBy: "user-1",
		Metadata:  sessionMetadataMap(domaincopilot.SessionMetadata{Mode: "general"}),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	envelope, err := service.SendMessage(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"chat"},
	}, "session-1", "hi", "en-US")
	if err != nil {
		t.Fatalf("send general chat message: %v", err)
	}
	if len(envelope.ToolCalls) != 0 || len(envelope.AnalysisArtifacts) != 0 || len(envelope.SessionPatch) != 0 {
		t.Fatalf("expected general chat to skip automatic analysis, got toolCalls=%#v artifacts=%#v patch=%#v", envelope.ToolCalls, envelope.AnalysisArtifacts, envelope.SessionPatch)
	}
	if len(envelope.Messages) != 2 || envelope.Messages[1].Content != "Hello from the model." {
		t.Fatalf("expected model reply envelope, got %#v", envelope.Messages)
	}
	metadata := envelope.Messages[1].Metadata
	if metadata["source"] != "model-provider" || metadata["model"] != "test-model" || metadata["providerKind"] != "openai-compatible" {
		t.Fatalf("expected model-provider metadata, got %#v", metadata)
	}
	if len(repo.createdMessages) != 2 {
		t.Fatalf("expected user and assistant messages to be persisted, got %#v", repo.createdMessages)
	}
}

func TestSendMessageGeneralModeUsesRecentConversationWindow(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	var providerMessages []chatProviderMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []chatProviderMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider payload: %v", err)
		}
		providerMessages = payload.Messages
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"recent reply"}}]}`))
	}))
	defer server.Close()

	service, repo := newInspectionAuthzTestService(map[string][]string{
		"chat": {appaccess.PermObserveAIChatUse},
	})
	service.http = server.Client()
	service.settings = inspectionAuthzSettingsResolver{settings: domainsettings.AISettings{
		Provider: domainsettings.AIProviderSettings{
			ID:           "provider-1",
			ProviderKind: "openai-compatible",
			Enabled:      true,
			BaseURL:      server.URL,
			APIKey:       "secret-key",
			Model:        "test-model",
		},
	}}
	repo.session = domaincopilot.Session{
		ID:        "session-1",
		Title:     "General chat",
		CreatedBy: "user-1",
		Metadata:  sessionMetadataMap(domaincopilot.SessionMetadata{Mode: "general"}),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	for index := 1; index <= 25; index++ {
		repo.messages = append(repo.messages, domaincopilot.Message{
			ID:        fmt.Sprintf("history-%02d", index),
			SessionID: "session-1",
			Role:      "user",
			Content:   fmt.Sprintf("history message %02d", index),
			CreatedAt: time.Now().UTC().Add(time.Duration(index) * time.Second),
		})
	}

	_, err := service.SendMessage(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"chat"},
	}, "session-1", "current question", "en-US")
	if err != nil {
		t.Fatalf("send general chat message: %v", err)
	}
	serialized := fmt.Sprint(providerMessages)
	if strings.Contains(serialized, "history message 01") {
		t.Fatalf("provider payload should not include oldest history: %#v", providerMessages)
	}
	if !strings.Contains(serialized, "history message 25") || !strings.Contains(serialized, "current question") {
		t.Fatalf("provider payload should include recent history and current question: %#v", providerMessages)
	}
}

func TestSendMessageGeneralModeReportsMissingModelProvider(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"chat": {appaccess.PermObserveAIChatUse},
	})
	service.settings = inspectionAuthzSettingsResolver{settings: domainsettings.AISettings{}}
	repo.session = domaincopilot.Session{
		ID:        "session-1",
		Title:     "General chat",
		CreatedBy: "user-1",
		Metadata:  sessionMetadataMap(domaincopilot.SessionMetadata{Mode: "general"}),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	envelope, err := service.SendMessage(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"chat"},
	}, "session-1", "hi", "zh-CN")
	if err != nil {
		t.Fatalf("send general chat message without provider: %v", err)
	}
	if len(envelope.Messages) != 2 {
		t.Fatalf("expected user and assistant messages, got %#v", envelope.Messages)
	}
	assistant := envelope.Messages[1]
	if !strings.Contains(assistant.Content, "没有启用可用的大模型提供方") {
		t.Fatalf("expected explicit missing provider message, got %q", assistant.Content)
	}
	if assistant.Metadata["source"] != "model-unconfigured" {
		t.Fatalf("expected model-unconfigured metadata, got %#v", assistant.Metadata)
	}
	if strings.Contains(assistant.Content, "当前平台上下文") {
		t.Fatalf("general chat should not fall back to platform summary, got %q", assistant.Content)
	}
}

func TestNormalizeAutomationPolicyInputRequiresAlertWebhookTrigger(t *testing.T) {
	_, err := normalizeAutomationPolicyInput(domaincopilot.AutomationPolicyInput{
		Name:              "Manual policy",
		TriggerType:       "manual",
		AnalysisProfileID: "profile:root",
		AnalysisKinds:     []string{"root_cause"},
	})

	if err == nil {
		t.Fatalf("expected unsupported trigger type to be rejected")
	}
}

func TestNormalizeAutomationPolicyInputAllowsInspectionReview(t *testing.T) {
	input, err := normalizeAutomationPolicyInput(domaincopilot.AutomationPolicyInput{
		Name:              "Inspection policy",
		TriggerType:       "alert_webhook",
		AnalysisProfileID: "profile:root",
		AnalysisKinds:     []string{"inspection_review"},
	})

	if err != nil {
		t.Fatalf("expected inspection_review automation kind to be accepted, got %v", err)
	}
	if len(input.AnalysisKinds) != 1 || input.AnalysisKinds[0] != "inspection_review" {
		t.Fatalf("expected inspection_review kind, got %#v", input.AnalysisKinds)
	}
}

func TestNormalizeAutomationPolicyInputDeduplicatesSupportedAnalysisKinds(t *testing.T) {
	input, err := normalizeAutomationPolicyInput(domaincopilot.AutomationPolicyInput{
		Name:              "Alert policy",
		TriggerType:       " alert_webhook ",
		AnalysisProfileID: " profile:root ",
		AnalysisKinds:     []string{"root_cause", " performance ", "root_cause", ""},
	})
	if err != nil {
		t.Fatalf("expected valid automation policy input, got %v", err)
	}

	expected := []string{"root_cause", "performance"}
	if len(input.AnalysisKinds) != len(expected) {
		t.Fatalf("expected analysis kinds %v, got %v", expected, input.AnalysisKinds)
	}
	for i, item := range expected {
		if input.AnalysisKinds[i] != item {
			t.Fatalf("expected analysis kind %q at index %d, got %q", item, i, input.AnalysisKinds[i])
		}
	}
}

func TestRecordAgentRunCallbackSynthesizesArtifactBeforePersist(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:             "agent:run-1",
			ProviderID:     "hermes",
			ProviderKind:   "hermes",
			CapabilityID:   "root_cause",
			CreatedBy:      "user-1",
			Status:         domaincopilot.AgentRunStatusRunning,
			Scope:          domaincopilot.SessionScope{ClusterID: "cluster-a", Namespace: "default"},
			Output:         map[string]any{"summary": "old summary"},
			CallbackToken:  "callback-token",
			TimeoutSeconds: 600,
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
	}
	service := &Service{repo: repo}

	updated, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-1",
		CallbackToken: "callback-token",
		AgentID:       "local-hermes-runner",
		Status:        "completed",
		Payload: map[string]any{
			"summary":         "Hermes found a likely release regression.",
			"recommendations": []any{"Rollback the latest deployment"},
			"usage": map[string]any{
				"prompt_tokens":     40,
				"completion_tokens": 60,
				"estimatedCostUsd":  0.12,
				"model":             "do-not-store",
			},
		},
		ExternalRunID: "hermes:123",
	})
	if err != nil {
		t.Fatalf("record callback: %v", err)
	}
	if len(repo.callback.AnalysisArtifacts) != 1 {
		t.Fatalf("expected synthesized artifact to be persisted through callback, got %#v", repo.callback.AnalysisArtifacts)
	}
	artifact := repo.callback.AnalysisArtifacts[0]
	if artifact.RunID != "agent:run-1" || artifact.Kind != "root_cause" {
		t.Fatalf("unexpected synthesized artifact identity: %#v", artifact)
	}
	if artifact.Summary != "Hermes found a likely release regression." {
		t.Fatalf("expected callback payload summary in artifact, got %q", artifact.Summary)
	}
	if artifact.DataSourceSnapshot["providerId"] != "hermes" || artifact.DataSourceSnapshot["externalRunId"] != "hermes:123" {
		t.Fatalf("expected provider snapshot in artifact, got %#v", artifact.DataSourceSnapshot)
	}
	usage := mapValue(repo.callback.Payload["providerUsage"])
	if usage["totalTokens"] != float64(100) || usage["inputTokens"] != float64(40) || usage["outputTokens"] != float64(60) || usage["totalCost"] != 0.12 {
		t.Fatalf("expected normalized callback usage summary, got %#v", usage)
	}
	artifactUsage := mapValue(artifact.DataSourceSnapshot["providerUsage"])
	if artifactUsage["totalTokens"] != float64(100) || artifactUsage["totalCost"] != 0.12 {
		t.Fatalf("expected provider usage snapshot in artifact, got %#v", artifact.DataSourceSnapshot)
	}
	if text := fmt.Sprint(usage) + fmt.Sprint(artifactUsage); strings.Contains(text, "do-not-store") {
		t.Fatalf("provider usage summary leaked raw provider metadata: %s", text)
	}
	if len(updated.AnalysisArtifacts) != 1 {
		t.Fatalf("expected updated run to include synthesized artifact, got %#v", updated.AnalysisArtifacts)
	}
}

func TestRecordAgentRunCallbackEnrichesProvidedArtifactUsage(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:             "agent:run-usage",
			ProviderID:     "hermes",
			ProviderKind:   "hermes",
			CapabilityID:   "root_cause",
			CreatedBy:      "user-1",
			Status:         domaincopilot.AgentRunStatusRunning,
			CallbackToken:  "callback-token",
			TimeoutSeconds: 600,
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
	}
	service := &Service{repo: repo}

	_, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-usage",
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload: map[string]any{
			"providerUsage": map[string]any{
				"inputTokens":  10,
				"outputTokens": 15,
				"totalCost":    0.04,
				"rawOutput":    "do-not-store",
			},
		},
		AnalysisArtifacts: []domaincopilot.AnalysisArtifact{{
			Kind:    "root_cause",
			RunID:   "agent:run-usage",
			Title:   "Provided artifact",
			Summary: "Provider supplied an artifact.",
		}},
	})
	if err != nil {
		t.Fatalf("record callback: %v", err)
	}
	if len(repo.callback.AnalysisArtifacts) != 1 {
		t.Fatalf("expected provided artifact to be persisted, got %#v", repo.callback.AnalysisArtifacts)
	}
	usage := mapValue(repo.callback.AnalysisArtifacts[0].DataSourceSnapshot["providerUsage"])
	if usage["totalTokens"] != float64(25) || usage["totalCost"] != 0.04 {
		t.Fatalf("expected provided artifact usage snapshot, got %#v", repo.callback.AnalysisArtifacts[0].DataSourceSnapshot)
	}
	if text := fmt.Sprint(repo.callback.AnalysisArtifacts[0].DataSourceSnapshot); strings.Contains(text, "do-not-store") {
		t.Fatalf("artifact usage snapshot leaked raw provider metadata: %s", text)
	}
}

func TestRecordAgentRunCallbackMapsNativeProviderUsageFields(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:             "agent:run-native-usage",
			ProviderID:     "hermes",
			ProviderKind:   "hermes",
			CapabilityID:   "root_cause",
			CreatedBy:      "user-1",
			Status:         domaincopilot.AgentRunStatusRunning,
			CallbackToken:  "callback-token",
			TimeoutSeconds: 600,
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
	}
	service := &Service{repo: repo}

	_, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-native-usage",
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload: map[string]any{
			"usageMetadata": map[string]any{
				"promptTokenCount":     12,
				"candidatesTokenCount": 18,
				"totalTokenCount":      30,
				"estimatedCostUsd":     0.06,
				"model":                "gemini-do-not-store",
			},
			"ollama": map[string]any{
				"prompt_eval_count": 4,
				"eval_count":        9,
				"raw":               "ollama-do-not-store",
			},
			"anthropic": map[string]any{
				"usage": map[string]any{
					"input_tokens":                6,
					"output_tokens":               14,
					"cache_creation_input_tokens": 2,
					"cache_read_input_tokens":     3,
					"response_cost":               0.03,
					"model":                       "claude-do-not-store",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("record callback: %v", err)
	}
	usage := mapValue(repo.callback.Payload["providerUsage"])
	if usage["totalTokens"] != float64(68) || usage["inputTokens"] != float64(27) || usage["outputTokens"] != float64(41) || usage["totalCost"] != 0.09 {
		t.Fatalf("expected native provider usage summary, got %#v", usage)
	}
	artifactUsage := mapValue(repo.callback.AnalysisArtifacts[0].DataSourceSnapshot["providerUsage"])
	if artifactUsage["totalTokens"] != float64(68) || artifactUsage["totalCost"] != 0.09 {
		t.Fatalf("expected native provider usage artifact snapshot, got %#v", repo.callback.AnalysisArtifacts[0].DataSourceSnapshot)
	}
	if text := fmt.Sprint(usage) + fmt.Sprint(artifactUsage); strings.Contains(text, "do-not-store") {
		t.Fatalf("native provider usage summary leaked raw provider metadata: %s", text)
	}
}

func TestRecordAgentRunCallbackMapsAdditionalProviderUsageAliases(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:             "agent:run-additional-usage",
			ProviderID:     "hermes",
			ProviderKind:   "hermes",
			CapabilityID:   "root_cause",
			CreatedBy:      "user-1",
			Status:         domaincopilot.AgentRunStatusRunning,
			CallbackToken:  "callback-token",
			TimeoutSeconds: 600,
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
	}
	service := &Service{repo: repo}

	_, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-additional-usage",
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload: map[string]any{
			"openai": map[string]any{
				"usage": map[string]any{
					"prompt_tokens":     10,
					"completion_tokens": 15,
					"prompt_tokens_details": map[string]any{
						"cached_tokens": 4,
					},
					"completion_tokens_details": map[string]any{
						"reasoning_tokens": 3,
					},
					"billed_amount": 0.04,
					"model":         "openai-do-not-store",
				},
			},
			"bedrock": map[string]any{
				"inputTextTokens":  8,
				"outputTextTokens": 12,
				"inputImageTokens": 5,
				"estimatedCostUsd": 0.03,
				"trace":            "bedrock-do-not-store",
			},
			"cohere": map[string]any{
				"meta": map[string]any{
					"billed_units": map[string]any{
						"read_units":   7,
						"write_units":  11,
						"credits_used": 0.02,
						"raw_response": "cohere-do-not-store",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("record callback: %v", err)
	}
	usage := mapValue(repo.callback.Payload["providerUsage"])
	if usage["totalTokens"] != float64(75) || usage["inputTokens"] != float64(34) || usage["outputTokens"] != float64(41) || !floatNear(usage["totalCost"], 0.09) {
		t.Fatalf("expected additional provider usage aliases, got %#v", usage)
	}
	artifactUsage := mapValue(repo.callback.AnalysisArtifacts[0].DataSourceSnapshot["providerUsage"])
	if artifactUsage["totalTokens"] != float64(75) || !floatNear(artifactUsage["totalCost"], 0.09) {
		t.Fatalf("expected additional provider usage artifact snapshot, got %#v", repo.callback.AnalysisArtifacts[0].DataSourceSnapshot)
	}
	if text := fmt.Sprint(usage) + fmt.Sprint(artifactUsage); strings.Contains(text, "do-not-store") {
		t.Fatalf("additional provider usage summary leaked raw provider metadata: %s", text)
	}
}

func TestRecordAgentRunCallbackMapsExpandedProviderUsageAliases(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:             "agent:run-expanded-usage",
			ProviderID:     "hermes",
			ProviderKind:   "hermes",
			CapabilityID:   "root_cause",
			CreatedBy:      "user-1",
			Status:         domaincopilot.AgentRunStatusRunning,
			CallbackToken:  "callback-token",
			TimeoutSeconds: 600,
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
	}
	service := &Service{repo: repo}

	_, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-expanded-usage",
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload: map[string]any{
			"providerUsage": map[string]any{
				"billable_tokens": 90,
				"totalCostMicros": 120000,
				"inputCostMicros": 50000,
				"raw":             "provider-do-not-store",
			},
			"multimodal": map[string]any{
				"usage": map[string]any{
					"textInputTokens":             6,
					"image_input_tokens":          4,
					"audioInputTokens":            3,
					"textOutputTokens":            8,
					"image_output_tokens":         5,
					"audioOutputTokens":           2,
					"completion_reasoning_tokens": 7,
					"outputCostMicros":            70000,
					"trace":                       "multimodal-do-not-store",
				},
			},
			"anthropic_variant": map[string]any{
				"usage": map[string]any{
					"prompt_tokens":             9,
					"prompt_cache_read_tokens":  2,
					"prompt_cache_write_tokens": 3,
					"response_cost":             0.01,
					"model":                     "claude-do-not-store",
				},
			},
			"generic_cost_adapter": map[string]any{
				"promptCost":     0.011,
				"completionCost": 0.019,
				"raw":            "generic-cost-do-not-store",
			},
		},
	})
	if err != nil {
		t.Fatalf("record callback: %v", err)
	}
	usage := mapValue(repo.callback.Payload["providerUsage"])
	if usage["totalTokens"] != float64(132) || usage["inputTokens"] != float64(27) || usage["outputTokens"] != float64(15) || !floatNear(usage["totalCost"], 0.23) || !floatNear(usage["inputCost"], 0.061) || !floatNear(usage["outputCost"], 0.089) {
		t.Fatalf("expected expanded provider usage aliases, got %#v", usage)
	}
	artifactUsage := mapValue(repo.callback.AnalysisArtifacts[0].DataSourceSnapshot["providerUsage"])
	if artifactUsage["totalTokens"] != float64(132) || !floatNear(artifactUsage["totalCost"], 0.23) || !floatNear(artifactUsage["inputCost"], 0.061) || !floatNear(artifactUsage["outputCost"], 0.089) {
		t.Fatalf("expected expanded provider usage artifact snapshot, got %#v", repo.callback.AnalysisArtifacts[0].DataSourceSnapshot)
	}
	if text := fmt.Sprint(usage) + fmt.Sprint(artifactUsage); strings.Contains(text, "do-not-store") {
		t.Fatalf("expanded provider usage summary leaked raw provider metadata: %s", text)
	}
}

func TestRecordAgentRunCallbackMapsEmergingProviderUsageAliases(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:             "agent:run-emerging-usage",
			ProviderID:     "hermes",
			ProviderKind:   "hermes",
			CapabilityID:   "root_cause",
			CreatedBy:      "user-1",
			Status:         domaincopilot.AgentRunStatusRunning,
			CallbackToken:  "callback-token",
			TimeoutSeconds: 600,
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
	}
	service := &Service{repo: repo}

	_, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-emerging-usage",
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload: map[string]any{
			"gemini": map[string]any{
				"usageMetadata": map[string]any{
					"promptTokenCount":        40,
					"cachedContentTokenCount": 12,
					"toolUsePromptTokenCount": 5,
					"candidatesTokenCount":    24,
					"thoughtsTokenCount":      6,
					"totalCostCents":          9,
					"model":                   "gemini-do-not-store",
				},
			},
			"openai": map[string]any{
				"usage": map[string]any{
					"prompt_tokens": 20,
					"prompt_tokens_details": map[string]any{
						"cached_tokens": 3,
						"audio_tokens":  2,
					},
					"completion_tokens": 10,
					"completion_tokens_details": map[string]any{
						"reasoning_tokens":           4,
						"accepted_prediction_tokens": 3,
						"rejected_prediction_tokens": 1,
						"raw":                        "provider-do-not-store",
					},
					"inputCostCents":  2,
					"outputCostCents": 3,
					"model":           "openai-do-not-store",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("record callback: %v", err)
	}
	usage := mapValue(repo.callback.Payload["providerUsage"])
	if usage["totalTokens"] != float64(130) || usage["inputTokens"] != float64(82) || usage["outputTokens"] != float64(48) || !floatNear(usage["totalCost"], 0.14) || !floatNear(usage["inputCost"], 0.02) || !floatNear(usage["outputCost"], 0.03) {
		t.Fatalf("expected emerging provider usage aliases, got %#v", usage)
	}
	artifactUsage := mapValue(repo.callback.AnalysisArtifacts[0].DataSourceSnapshot["providerUsage"])
	if artifactUsage["totalTokens"] != float64(130) || !floatNear(artifactUsage["totalCost"], 0.14) {
		t.Fatalf("expected emerging provider usage artifact snapshot, got %#v", repo.callback.AnalysisArtifacts[0].DataSourceSnapshot)
	}
	if text := fmt.Sprint(usage) + fmt.Sprint(artifactUsage); strings.Contains(text, "do-not-store") {
		t.Fatalf("emerging provider usage summary leaked raw provider metadata: %s", text)
	}
}

func TestRecordAgentRunCallbackMapsChinaCloudProviderUsageAliases(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:             "agent:run-china-cloud-usage",
			ProviderID:     "hermes",
			ProviderKind:   "hermes",
			CapabilityID:   "root_cause",
			CreatedBy:      "user-1",
			Status:         domaincopilot.AgentRunStatusRunning,
			CallbackToken:  "callback-token",
			TimeoutSeconds: 600,
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
	}
	service := &Service{repo: repo}

	_, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-china-cloud-usage",
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload: map[string]any{
			"dashscope": map[string]any{
				"usage": map[string]any{
					"input_tokens_count":      10,
					"output_tokens_count":     20,
					"prompt_cache_hit_tokens": 3,
					"raw":                     "dashscope-do-not-store",
				},
			},
			"dashscope_multimodal": map[string]any{
				"usage": map[string]any{
					"image_tokens": 4,
					"video_tokens": 5,
					"audio_tokens": 6,
					"raw":          "dashscope-multimodal-do-not-store",
				},
			},
			"moonshot": map[string]any{
				"usage": map[string]any{
					"prompt_token_usage":     11,
					"completion_token_usage": 13,
					"total_cost_usd":         0.04,
					"model":                  "moonshot-do-not-store",
				},
			},
			"zhipu": map[string]any{
				"usage": map[string]any{
					"promptTokensCount":     7,
					"completionTokensCount": 9,
					"estimatedCostCents":    5,
					"trace":                 "zhipu-do-not-store",
				},
			},
			"qianfan": map[string]any{
				"token_usage": map[string]any{
					"input_token_usage":  8,
					"output_token_usage": 12,
					"total_cost_micros":  60000,
					"raw_response":       "qianfan-do-not-store",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("record callback: %v", err)
	}
	usage := mapValue(repo.callback.Payload["providerUsage"])
	if usage["totalTokens"] != float64(108) || usage["inputTokens"] != float64(54) || usage["outputTokens"] != float64(54) || !floatNear(usage["totalCost"], 0.15) {
		t.Fatalf("expected China cloud provider usage aliases, got %#v", usage)
	}
	artifactUsage := mapValue(repo.callback.AnalysisArtifacts[0].DataSourceSnapshot["providerUsage"])
	if artifactUsage["totalTokens"] != float64(108) || !floatNear(artifactUsage["totalCost"], 0.15) {
		t.Fatalf("expected China cloud provider usage artifact snapshot, got %#v", repo.callback.AnalysisArtifacts[0].DataSourceSnapshot)
	}
	if text := fmt.Sprint(usage) + fmt.Sprint(artifactUsage); strings.Contains(text, "do-not-store") {
		t.Fatalf("China cloud provider usage summary leaked raw provider metadata: %s", text)
	}
}

func TestRecordAgentRunCallbackPrefersBilledUsageUnits(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:             "agent:run-billed-usage",
			ProviderID:     "hermes",
			ProviderKind:   "hermes",
			CapabilityID:   "root_cause",
			CreatedBy:      "user-1",
			Status:         domaincopilot.AgentRunStatusRunning,
			CallbackToken:  "callback-token",
			TimeoutSeconds: 600,
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
	}
	service := &Service{repo: repo}

	_, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-billed-usage",
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload: map[string]any{
			"cohere_chat": map[string]any{
				"usage": map[string]any{
					"billed_units": map[string]any{
						"input_tokens":  5,
						"output_tokens": 26,
						"raw":           "billed-do-not-store",
					},
					"tokens": map[string]any{
						"input_tokens":  71,
						"output_tokens": 26,
						"raw":           "tokens-do-not-store",
					},
					"cost": 0.012,
				},
			},
			"cohere_rerank": map[string]any{
				"meta": map[string]any{
					"billed_units": map[string]any{
						"search_units": 2,
						"raw":          "search-do-not-store",
					},
				},
			},
			"voyage_embedding": map[string]any{
				"usage": map[string]any{
					"embedding_tokens": 7,
					"raw":              "embedding-do-not-store",
				},
			},
			"custom_gateway": map[string]any{
				"metering": map[string]any{
					"request_units":  3,
					"response_units": 4,
					"raw":            "unit-do-not-store",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("record callback: %v", err)
	}
	usage := mapValue(repo.callback.Payload["providerUsage"])
	if usage["totalTokens"] != float64(47) || usage["inputTokens"] != float64(8) || usage["outputTokens"] != float64(30) || !floatNear(usage["totalCost"], 0.012) {
		t.Fatalf("expected billed usage units without double counting generic tokens, got %#v", usage)
	}
	artifactUsage := mapValue(repo.callback.AnalysisArtifacts[0].DataSourceSnapshot["providerUsage"])
	if artifactUsage["totalTokens"] != float64(47) || !floatNear(artifactUsage["totalCost"], 0.012) {
		t.Fatalf("expected billed usage artifact snapshot, got %#v", repo.callback.AnalysisArtifacts[0].DataSourceSnapshot)
	}
	if text := fmt.Sprint(usage) + fmt.Sprint(artifactUsage); strings.Contains(text, "do-not-store") {
		t.Fatalf("billed usage summary leaked raw provider metadata: %s", text)
	}
}

func TestRecordAgentRunCallbackMapsAgentToolingUsageAliases(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:             "agent:run-tooling-usage",
			ProviderID:     "hermes",
			ProviderKind:   "hermes",
			CapabilityID:   "root_cause",
			CreatedBy:      "user-1",
			Status:         domaincopilot.AgentRunStatusRunning,
			CallbackToken:  "callback-token",
			TimeoutSeconds: 600,
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
	}
	service := &Service{repo: repo}

	_, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-tooling-usage",
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload: map[string]any{
			"brave_search": map[string]any{
				"usage": map[string]any{
					"queryUnits":       2,
					"braveSearchUnits": 1,
					"raw":              "brave-do-not-store",
				},
			},
			"serpapi": map[string]any{
				"metering": map[string]any{
					"searchCredits":   3,
					"serpapiSearches": 4,
					"trace":           "serpapi-do-not-store",
				},
			},
			"browserbase": map[string]any{
				"usage": map[string]any{
					"browserMinutes":  5,
					"browserSessions": 6,
					"pageLoads":       7,
					"session":         "browserbase-do-not-store",
				},
			},
			"rag_tools": map[string]any{
				"providerUsage": map[string]any{
					"documentPages":   8,
					"parsePages":      9,
					"llamaParsePages": 10,
					"characters":      11,
					"chunks":          12,
					"source":          "rag-do-not-store",
				},
			},
			"helicone": map[string]any{
				"billing": map[string]any{
					"requestCount":     13,
					"providerRequests": 14,
					"totalCostMicros":  90000,
					"raw":              "helicone-do-not-store",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("record callback: %v", err)
	}
	usage := mapValue(repo.callback.Payload["providerUsage"])
	if usage["totalTokens"] != float64(39) || !floatNear(usage["totalCost"], 0.09) {
		t.Fatalf("expected agent tooling usage aliases, got %#v", usage)
	}
	artifactUsage := mapValue(repo.callback.AnalysisArtifacts[0].DataSourceSnapshot["providerUsage"])
	if artifactUsage["totalTokens"] != float64(39) || !floatNear(artifactUsage["totalCost"], 0.09) {
		t.Fatalf("expected agent tooling usage artifact snapshot, got %#v", repo.callback.AnalysisArtifacts[0].DataSourceSnapshot)
	}
	if text := fmt.Sprint(usage) + fmt.Sprint(artifactUsage); strings.Contains(text, "do-not-store") {
		t.Fatalf("agent tooling usage summary leaked raw provider metadata: %s", text)
	}
}

func TestAgentProviderUsageSummaryIgnoresGenericCountsOutsideUsageContainers(t *testing.T) {
	usage := agentProviderUsageSummary(map[string]any{
		"observability": map[string]any{
			"requests":   200,
			"documents":  30,
			"chunks":     40,
			"characters": 5000,
			"raw":        "do-not-store",
		},
	})

	if usage != nil {
		t.Fatalf("expected generic non-usage counters to be ignored, got %#v", usage)
	}
}

func TestAgentUsageWithDerivedTotalsPrefersLargestCanonicalAlias(t *testing.T) {
	values := agentUsageWithDerivedTotals(map[string]any{
		"queryUnits":       2,
		"requestCount":     13,
		"providerRequests": 14,
		"total_tokens":     "do-not-store",
		"totalCostMicros":  90000,
		"costCents":        12,
		"cost":             "do-not-store",
	})

	if values["totalTokens"] != float64(14) || !floatNear(values["totalCost"], 0.12) {
		t.Fatalf("expected largest canonical usage aliases, got %#v", values)
	}
	if text := fmt.Sprint(values); strings.Contains(text, "do-not-store") {
		t.Fatalf("derived usage totals leaked non-numeric alias payload: %s", text)
	}
}

func TestRecordAgentRunCallbackSynthesizesFailureArtifact(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:             "agent:run-2",
			ProviderID:     "hermes",
			ProviderKind:   "hermes",
			CapabilityID:   "trace",
			CreatedBy:      "user-1",
			Status:         domaincopilot.AgentRunStatusRunning,
			Output:         map[string]any{},
			CallbackToken:  "callback-token",
			TimeoutSeconds: 600,
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
	}
	service := &Service{repo: repo}

	updated, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-2",
		CallbackToken: "callback-token",
		AgentID:       "local-hermes-runner",
		Status:        "failed",
		Payload:       map[string]any{"error": "Hermes command exited with status 1"},
		ErrorMessage:  "Hermes command exited with status 1",
	})
	if err != nil {
		t.Fatalf("record failed callback: %v", err)
	}
	if len(repo.callback.AnalysisArtifacts) != 1 {
		t.Fatalf("expected failed callback to persist synthesized artifact, got %#v", repo.callback.AnalysisArtifacts)
	}
	artifact := repo.callback.AnalysisArtifacts[0]
	if artifact.Kind != "trace" || artifact.Summary != "Hermes command exited with status 1" {
		t.Fatalf("unexpected failure artifact: %#v", artifact)
	}
	if len(updated.AnalysisArtifacts) != 1 {
		t.Fatalf("expected updated failed run to include synthesized artifact, got %#v", updated.AnalysisArtifacts)
	}
}

func TestRecordAgentRunCallbackSynthesizesStructuredArtifactFields(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:             "agent:run-structured",
			ProviderID:     "hermes",
			ProviderKind:   "hermes",
			CapabilityID:   "root_cause",
			CreatedBy:      "user-1",
			Status:         domaincopilot.AgentRunStatusRunning,
			Output:         map[string]any{},
			CallbackToken:  "callback-token",
			TimeoutSeconds: 600,
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
	}
	service := &Service{repo: repo}

	_, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-structured",
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload: map[string]any{
			"summary": "Structured Hermes result",
			"evidence": []any{map[string]any{
				"id":      "evidence-1",
				"kind":    "log.pattern",
				"title":   "Error burst",
				"summary": "Error rate increased.",
			}},
			"hypotheses": []any{map[string]any{
				"id":         "hypothesis-1",
				"title":      "Release regression",
				"summary":    "A recent release likely caused the issue.",
				"confidence": 82,
			}},
			"graph": map[string]any{
				"nodes": []any{map[string]any{"id": "service:payment-api", "kind": "service", "title": "payment-api"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("record structured callback: %v", err)
	}
	artifact := repo.callback.AnalysisArtifacts[0]
	if len(artifact.Evidence) != 1 || artifact.Evidence[0].ID != "evidence-1" {
		t.Fatalf("expected evidence to be normalized, got %#v", artifact.Evidence)
	}
	if len(artifact.Hypotheses) != 1 || artifact.Hypotheses[0].Confidence != 82 {
		t.Fatalf("expected hypotheses to be normalized, got %#v", artifact.Hypotheses)
	}
	if artifact.Graph == nil || len(artifact.Graph.Nodes) != 1 {
		t.Fatalf("expected graph to be normalized, got %#v", artifact.Graph)
	}
}

func TestRunRootCauseAnalysisQueuesExternalAgentAndCreatesBusinessRun(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"runner": {appaccess.PermObserveAIRootCauseRun, appaccess.PermObserveAIChatUse},
	})

	run, err := service.RunRootCauseAnalysis(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"runner"},
	}, domaincopilot.RootCauseRunInput{
		Kind:            "root_cause",
		AgentProviderID: "hermes",
		ClusterID:       "cluster-a",
		Namespace:       "payments",
		WorkloadName:    "payment-api",
		Question:        "Investigate alert pressure",
	}, "en-US")
	if err != nil {
		t.Fatalf("queue external root cause run: %v", err)
	}
	if run.Status != domaincopilot.AgentRunStatusQueued {
		t.Fatalf("expected queued business run, got %q", run.Status)
	}
	if repo.createdAgentRun.ProviderID != "hermes" || repo.createdAgentRun.RootCauseRunID != run.ID {
		t.Fatalf("expected linked hermes agent run, got %#v", repo.createdAgentRun)
	}
	if repo.createdAgentRun.Scope.ClusterID != "cluster-a" || repo.createdAgentRun.Scope.Workload != "payment-api" {
		t.Fatalf("unexpected agent run scope: %#v", repo.createdAgentRun.Scope)
	}
	if len(repo.createdAgentRun.ToolBindings) == 0 {
		t.Fatalf("expected agent run to snapshot tool bindings")
	}
	for _, binding := range repo.createdAgentRun.ToolBindings {
		if binding.PermissionKey != "" && binding.PermissionKey != appaccess.PermObserveAIChatUse {
			t.Fatalf("expected tool bindings to be filtered by creator permissions, got %#v", repo.createdAgentRun.ToolBindings)
		}
	}
	if len(repo.createdAgentRun.SkillBindings) == 0 || repo.createdAgentRun.SkillBindings[0].ProviderSkillRef == "" {
		t.Fatalf("expected agent run to snapshot provider skill bindings, got %#v", repo.createdAgentRun.SkillBindings)
	}
}

func TestRecordGatewayAnalysisArtifactCreatesCompletedAgentRun(t *testing.T) {
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"gateway": {appaccess.PermObserveAIChatUse},
	})

	run, err := service.RecordGatewayAnalysisArtifact(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"gateway"},
	}, domaincopilot.GatewayAnalysisArtifactInput{
		CapabilityID: "delivery_failure",
		Title:        "Gateway delivery diagnosis",
		Summary:      "Gateway persisted release failure evidence.",
		SkillIDs:     []string{"k8s-sre", "delivery-tester"},
		Scope:        domaincopilot.SessionScope{ClusterID: "cluster-a", Namespace: "prod", Workload: "api"},
		Input:        map[string]any{"password": "secret", "applicationId": "app-1"},
		Output:       map[string]any{"executionLogCount": 1},
		Evidence: []domaincopilot.RootCauseEvidence{{
			ID:        "evidence-1",
			Kind:      "delivery.execution_logs",
			Title:     "Execution logs",
			Summary:   "One redacted log entry collected.",
			Severity:  "warning",
			ClusterID: "cluster-a",
			Namespace: "prod",
		}},
		Recommendations:    []string{"Check deployment events."},
		DataSourceSnapshot: map[string]any{"rawLogsPersisted": false},
	})
	if err != nil {
		t.Fatalf("record gateway artifact: %v", err)
	}
	if run.Status != domaincopilot.AgentRunStatusCompleted || run.ProviderID != "internal" || run.CapabilityID != "delivery_failure" {
		t.Fatalf("expected completed internal delivery_failure run, got %#v", run)
	}
	if repo.createdAgentRun.ID != run.ID || len(repo.createdAgentRun.AnalysisArtifacts) != 1 {
		t.Fatalf("expected created agent run with artifact, got %#v", repo.createdAgentRun)
	}
	artifact := repo.createdAgentRun.AnalysisArtifacts[0]
	if artifact.Kind != "delivery_failure" || artifact.RunID != run.ID || len(artifact.Evidence) != 1 {
		t.Fatalf("unexpected persisted artifact: %#v", artifact)
	}
	if artifact.DataSourceSnapshot["artifactContract"] != "soha.analysisArtifact.v1" || artifact.DataSourceSnapshot["rawLogsPersisted"] != false {
		t.Fatalf("expected artifact contract and no raw logs marker, got %#v", artifact.DataSourceSnapshot)
	}
	if repo.createdAgentRun.Input["password"] != "[REDACTED]" {
		t.Fatalf("expected Gateway artifact input to be redacted, got %#v", repo.createdAgentRun.Input)
	}
}

func TestQueueGatewayAnalysisAgentRunCreatesQueuedExternalRun(t *testing.T) {
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"gateway": {
			appaccess.PermObserveAIChatUse,
			appaccess.PermDeliveryExecutionTasksView,
			appaccess.PermDeliveryApplicationsView,
			appaccess.PermDeliveryReleasesView,
		},
	})

	run, err := service.QueueGatewayAnalysisAgentRun(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"gateway"},
	}, domaincopilot.GatewayAnalysisAgentRunInput{
		AgentProviderID: "hermes",
		TimeoutSeconds:  900,
		GatewayAnalysisArtifactInput: domaincopilot.GatewayAnalysisArtifactInput{
			CapabilityID: "delivery_failure",
			Title:        "Gateway delivery diagnosis",
			Summary:      "Gateway queued release failure evidence.",
			SkillIDs:     []string{"k8s-sre", "delivery-tester"},
			Scope:        domaincopilot.SessionScope{ClusterID: "cluster-a", Namespace: "prod", Workload: "api"},
			Input:        map[string]any{"apiKey": "secret", "applicationId": "app-1"},
			Output:       map[string]any{"executionLogCount": 1},
			Evidence: []domaincopilot.RootCauseEvidence{{
				ID:        "evidence-1",
				Kind:      "delivery.execution_logs",
				Title:     "Execution logs",
				Summary:   "One redacted log entry collected.",
				Severity:  "warning",
				ClusterID: "cluster-a",
				Namespace: "prod",
			}},
			Recommendations:    []string{"Check deployment events."},
			DataSourceSnapshot: map[string]any{"rawLogsPersisted": false},
		},
	})
	if err != nil {
		t.Fatalf("queue gateway agent run: %v", err)
	}
	if run.Status != domaincopilot.AgentRunStatusQueued || run.ProviderID != "hermes" || run.CapabilityID != "delivery_failure" {
		t.Fatalf("expected queued hermes delivery_failure run, got %#v", run)
	}
	if repo.createdAgentRun.ID != run.ID || repo.createdAgentRun.TimeoutSeconds != 900 {
		t.Fatalf("expected created queued agent run, got %#v", repo.createdAgentRun)
	}
	if repo.createdAgentRun.Input["apiKey"] != "[REDACTED]" {
		t.Fatalf("expected Gateway queue input to be redacted, got %#v", repo.createdAgentRun.Input)
	}
	if repo.createdAgentRun.Input["evidence"] == nil || repo.createdAgentRun.Input["recommendations"] == nil {
		t.Fatalf("expected queued run to carry structured evidence, got %#v", repo.createdAgentRun.Input)
	}
	snapshot := mapValue(repo.createdAgentRun.Input["dataSourceSnapshot"])
	if snapshot["analysisRuntime"] != "agent_runtime_claim_callback" || snapshot["providerId"] != "hermes" || snapshot["rawLogsPersisted"] != false {
		t.Fatalf("expected Agent Runtime snapshot, got %#v", snapshot)
	}
	if len(repo.createdAgentRun.ToolBindings) == 0 || len(repo.createdAgentRun.SkillBindings) == 0 {
		t.Fatalf("expected tool and skill bindings for external delivery analysis, got tools=%#v skills=%#v", repo.createdAgentRun.ToolBindings, repo.createdAgentRun.SkillBindings)
	}
}

func TestQueueGatewayAnalysisAgentRunDefaultsExternalProvider(t *testing.T) {
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"gateway": {appaccess.PermObserveAIChatUse},
	})

	run, err := service.QueueGatewayAnalysisAgentRun(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"gateway"},
	}, domaincopilot.GatewayAnalysisAgentRunInput{
		GatewayAnalysisArtifactInput: domaincopilot.GatewayAnalysisArtifactInput{
			CapabilityID: "delivery_failure",
			Summary:      "Queue with default external provider.",
			Input:        map[string]any{"applicationId": "app-1"},
		},
	})
	if err != nil {
		t.Fatalf("queue gateway agent run with default provider: %v", err)
	}
	if run.ProviderID != "hermes" || repo.createdAgentRun.ProviderID != "hermes" || run.Status != domaincopilot.AgentRunStatusQueued {
		t.Fatalf("expected default async external provider, got run=%#v created=%#v", run, repo.createdAgentRun)
	}
}

func TestAgentRunReturnPathsIncludeOperationState(t *testing.T) {
	queuedAt := time.Now().UTC().Add(-2 * time.Minute)
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"viewer": {appaccess.PermObserveAIView, appaccess.PermObserveAIChatUse},
	})
	repo.agentRuns = []domaincopilot.AgentRun{{
		ID:             "agent:queued",
		ProviderID:     "hermes",
		ProviderKind:   "hermes",
		CapabilityID:   "root_cause",
		CreatedBy:      "user-1",
		Status:         domaincopilot.AgentRunStatusQueued,
		CallbackToken:  "callback-token",
		TimeoutSeconds: 60,
		QueuedAt:       queuedAt,
		CreatedAt:      queuedAt,
		UpdatedAt:      queuedAt,
	}}

	listed, err := service.ListAgentRuns(context.Background(), domainidentity.Principal{UserID: "user-1", Roles: []string{"viewer"}})
	if err != nil {
		t.Fatalf("list agent runs: %v", err)
	}
	if len(listed) != 1 || listed[0].OperationState == nil || listed[0].OperationState.Phase != "pending" || !listed[0].OperationState.TimeoutStale {
		t.Fatalf("expected listed run operation state, got %#v", listed)
	}
	if listed[0].CallbackToken != "" {
		t.Fatalf("expected list response to hide callback token")
	}

	claimed, err := service.ClaimAgentRun(context.Background(), domaincopilot.AgentRunClaimInput{AgentID: "runner-1"})
	if err != nil {
		t.Fatalf("claim agent run: %v", err)
	}
	if claimed.OperationState == nil || claimed.OperationState.Phase != "running" || claimed.OperationState.ClaimedByAgentID != "runner-1" {
		t.Fatalf("expected claimed run operation state, got %#v", claimed)
	}

	callback, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         claimed.ID,
		CallbackToken: "callback-token",
		AgentID:       "runner-1",
		Status:        domaincopilot.AgentRunStatusFailed,
		Payload:       map[string]any{"failureReason": "provider_error", "message": "Hermes failed"},
		ErrorMessage:  "Hermes failed",
	})
	if err != nil {
		t.Fatalf("record agent callback: %v", err)
	}
	if callback.OperationState == nil || callback.OperationState.Phase != "failed" || callback.OperationState.FailureReason != "provider_error" {
		t.Fatalf("expected callback operation state, got %#v", callback.OperationState)
	}
}

func TestCancelAgentRunRecordsOperationState(t *testing.T) {
	queuedAt := time.Now().UTC().Add(-time.Minute)
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"chat": {appaccess.PermObserveAIChatUse},
	})
	repo.agentRuns = []domaincopilot.AgentRun{{
		ID:             "agent:cancel-me",
		ProviderID:     "hermes",
		ProviderKind:   "hermes",
		CapabilityID:   "delivery_failure",
		CreatedBy:      "user-1",
		Status:         domaincopilot.AgentRunStatusQueued,
		CallbackToken:  "callback-token",
		TimeoutSeconds: 600,
		QueuedAt:       queuedAt,
		CreatedAt:      queuedAt,
		UpdatedAt:      queuedAt,
	}}

	canceled, err := service.CancelAgentRun(context.Background(), domainidentity.Principal{UserID: "user-1", Roles: []string{"chat"}}, "agent:cancel-me")
	if err != nil {
		t.Fatalf("cancel agent run: %v", err)
	}
	if canceled.Status != domaincopilot.AgentRunStatusCanceled || canceled.OperationState == nil {
		t.Fatalf("expected canceled run with operation state, got %#v", canceled)
	}
	if canceled.OperationState.Phase != "canceled" || !canceled.OperationState.Terminal || canceled.OperationState.FailureMessage != "canceled by user" {
		t.Fatalf("unexpected canceled operation state: %#v", canceled.OperationState)
	}
}

func TestGatewayAnalysisAgentRunsReturnOperationState(t *testing.T) {
	service, _ := newInspectionAuthzTestService(map[string][]string{
		"gateway": {appaccess.PermObserveAIChatUse},
	})

	completed, err := service.RecordGatewayAnalysisArtifact(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"gateway"},
	}, domaincopilot.GatewayAnalysisArtifactInput{
		CapabilityID: "delivery_failure",
		Summary:      "Gateway completed inline analysis.",
	})
	if err != nil {
		t.Fatalf("record gateway analysis artifact: %v", err)
	}
	if completed.OperationState == nil || completed.OperationState.Phase != "succeeded" || !completed.OperationState.Terminal {
		t.Fatalf("expected completed gateway operation state, got %#v", completed.OperationState)
	}

	queued, err := service.QueueGatewayAnalysisAgentRun(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"gateway"},
	}, domaincopilot.GatewayAnalysisAgentRunInput{
		GatewayAnalysisArtifactInput: domaincopilot.GatewayAnalysisArtifactInput{
			CapabilityID: "delivery_failure",
			Summary:      "Gateway queued external analysis.",
		},
	})
	if err != nil {
		t.Fatalf("queue gateway agent run: %v", err)
	}
	if queued.OperationState == nil || queued.OperationState.Phase != "pending" || !queued.OperationState.RunnerClaimRequired {
		t.Fatalf("expected queued gateway operation state, got %#v", queued.OperationState)
	}
}

func TestRunSessionAnalysisQueuesExternalRootCauseBusinessRun(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"chat": {appaccess.PermObserveAIChatUse},
	})
	session := domaincopilot.Session{
		ID:        "session-1",
		Title:     "Payment incident",
		CreatedBy: "user-1",
		Metadata: sessionMetadataMap(domaincopilot.SessionMetadata{
			Mode:            "root_cause",
			AgentProviderID: "hermes",
			Scope: domaincopilot.SessionScope{
				ClusterID:        "cluster-a",
				Namespace:        "payments",
				Workload:         "payment-api",
				TimeRangeMinutes: 30,
			},
			Toolset: domaincopilot.SessionToolset{
				EnabledAdapterIDs: []string{"logs.v1"},
				EnabledSkillIDs:   []string{"root-cause-investigation"},
				BudgetOverrides:   map[string]any{"timeoutSeconds": 180},
			},
		}),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	repo.session = session

	envelope, err := service.RunSessionAnalysis(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"chat"},
	}, "session-1", domaincopilot.RootCauseRunInput{
		Kind:     "root_cause",
		Question: "Investigate payment-api errors",
	}, "en-US")
	if err != nil {
		t.Fatalf("queue session root cause agent run: %v", err)
	}
	if repo.createdRootCauseRun.ID == "" || repo.createdRootCauseRun.SessionID != "session-1" {
		t.Fatalf("expected session-linked root cause business run, got %#v", repo.createdRootCauseRun)
	}
	if repo.createdRootCauseRun.CreatedBy != "session:session-1" {
		t.Fatalf("expected root cause owner to remain session-scoped, got %q", repo.createdRootCauseRun.CreatedBy)
	}
	if repo.createdAgentRun.ProviderID != "hermes" || repo.createdAgentRun.RootCauseRunID != repo.createdRootCauseRun.ID {
		t.Fatalf("expected linked hermes agent run, got %#v", repo.createdAgentRun)
	}
	if repo.createdAgentRun.CreatedBy != "user-1" {
		t.Fatalf("expected agent run owner to stay session owner for callback message persistence, got %q", repo.createdAgentRun.CreatedBy)
	}
	if repo.createdAgentRun.Scope.ClusterID != "cluster-a" || repo.createdAgentRun.Scope.Workload != "payment-api" {
		t.Fatalf("unexpected agent run scope: %#v", repo.createdAgentRun.Scope)
	}
	if envelope.SessionPatch["rootCauseRunId"] != repo.createdRootCauseRun.ID || envelope.SessionPatch["agentRunId"] != repo.createdAgentRun.ID {
		t.Fatalf("expected session patch to expose both run ids, got %#v", envelope.SessionPatch)
	}
}

func TestRecordAgentRunCallbackBackfillsRootCauseRun(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:             "agent:run-3",
			ProviderID:     "hermes",
			ProviderKind:   "hermes",
			CapabilityID:   "root_cause",
			RootCauseRunID: "rca:run-1",
			CreatedBy:      "user-1",
			Status:         domaincopilot.AgentRunStatusRunning,
			Output:         map[string]any{},
			CallbackToken:  "callback-token",
			TimeoutSeconds: 600,
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
		rootCauseRun: domaincopilot.RootCauseRun{
			ID:        "rca:run-1",
			Kind:      "root_cause",
			CreatedBy: "user-1",
			Status:    domaincopilot.AgentRunStatusQueued,
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		},
	}
	service := &Service{repo: repo}

	_, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-3",
		CallbackToken: "callback-token",
		AgentID:       "local-hermes-runner",
		Status:        "completed",
		Payload:       map[string]any{"summary": "Hermes found release drift."},
		AnalysisArtifacts: []domaincopilot.AnalysisArtifact{{
			Kind:            "root_cause",
			RunID:           "agent:run-3",
			Summary:         "Hermes found release drift.",
			Recommendations: []string{"Rollback release bundle"},
			Evidence: []domaincopilot.RootCauseEvidence{{
				ID:      "evidence-1",
				Kind:    "delivery.release",
				Title:   "Release changed",
				Summary: "Latest release overlaps with alert start time.",
			}},
		}},
		ExternalRunID: "hermes:456",
	})
	if err != nil {
		t.Fatalf("record callback: %v", err)
	}
	if repo.rootCauseRun.Status != "completed" || repo.rootCauseRun.Summary != "Hermes found release drift." {
		t.Fatalf("expected root cause run to be backfilled, got %#v", repo.rootCauseRun)
	}
	if len(repo.rootCauseRun.Evidence) != 1 || len(repo.rootCauseRun.Recommendations) != 1 {
		t.Fatalf("expected evidence and recommendations to be copied, got %#v", repo.rootCauseRun)
	}
	if repo.rootCauseRun.DataSourceSnapshot["agentRunId"] != "agent:run-3" || repo.rootCauseRun.DataSourceSnapshot["externalRunId"] != "hermes:456" {
		t.Fatalf("expected agent runtime snapshot, got %#v", repo.rootCauseRun.DataSourceSnapshot)
	}
}

func TestRecordAgentRunCallbackBackfillsSessionOwnedRootCauseRun(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:             "agent:run-session",
			ProviderID:     "hermes",
			ProviderKind:   "hermes",
			CapabilityID:   "root_cause",
			RootCauseRunID: "rca:session-run",
			CreatedBy:      "user-1",
			Status:         domaincopilot.AgentRunStatusRunning,
			Input:          map[string]any{"rootCauseRunOwner": "session:session-1"},
			CallbackToken:  "callback-token",
			TimeoutSeconds: 600,
			CreatedAt:      createdAt,
			UpdatedAt:      createdAt,
		},
		rootCauseRun: domaincopilot.RootCauseRun{
			ID:        "rca:session-run",
			Kind:      "root_cause",
			SessionID: "session-1",
			CreatedBy: "session:session-1",
			Status:    domaincopilot.AgentRunStatusQueued,
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		},
	}
	service := &Service{repo: repo}

	_, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-session",
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload:       map[string]any{"summary": "Session-owned RCA backfilled."},
	})
	if err != nil {
		t.Fatalf("record callback: %v", err)
	}
	if repo.rootCauseLookupCreatedBy != "session:session-1" {
		t.Fatalf("expected callback to use root cause owner, got %q", repo.rootCauseLookupCreatedBy)
	}
	if repo.rootCauseRun.Status != "completed" || repo.rootCauseRun.Summary != "Session-owned RCA backfilled." {
		t.Fatalf("expected session-owned root cause run to be backfilled, got %#v", repo.rootCauseRun)
	}
}

func TestRecordAgentToolCallRejectsUnboundTool(t *testing.T) {
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:            "agent:tool-denied",
			ProviderID:    "hermes",
			ProviderKind:  "hermes",
			CapabilityID:  "root_cause",
			Status:        domaincopilot.AgentRunStatusRunning,
			CallbackToken: "callback-token",
			ToolBindings: []domaincopilot.AgentToolBinding{{
				ID:           "observability.logs",
				ToolKind:     "mcp",
				AdapterID:    "logs.v1",
				ToolName:     "logs.query",
				CapabilityID: "root_cause",
			}},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	service := &Service{repo: repo}

	_, err := service.RecordAgentToolCall(context.Background(), domaincopilot.AgentToolCallInput{
		RunID:         "agent:tool-denied",
		CallbackToken: "callback-token",
		AgentID:       "local-hermes-runner",
		ToolName:      "events.query",
		Input:         map[string]any{"limit": 5},
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("expected access denied for unbound tool, got %v", err)
	}
	if repo.callback.RunID != "" {
		t.Fatalf("expected denied tool call to skip callback persistence, got %#v", repo.callback)
	}
}

func TestRecordAgentToolCallExecutesBoundEventsTool(t *testing.T) {
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:           "agent:tool-events",
			ProviderID:   "hermes",
			ProviderKind: "hermes",
			CapabilityID: "root_cause",
			Status:       domaincopilot.AgentRunStatusRunning,
			Scope: domaincopilot.SessionScope{
				ClusterID: "cluster-a",
				Namespace: "payments",
				Workload:  "payment-api",
			},
			CallbackToken: "callback-token",
			ToolBindings: []domaincopilot.AgentToolBinding{{
				ID:           "platform.events",
				CapabilityID: "root_cause",
				ToolKind:     "mcp",
				AdapterID:    "platform-native.v1",
				ToolName:     "events.query",
			}},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	service := &Service{
		repo: repo,
		events: agentToolEventReader{items: []domainevent.Envelope{{
			ID:        "event-1",
			Source:    "kubernetes",
			Category:  "workload",
			Severity:  "warning",
			ClusterID: "cluster-a",
			Namespace: "payments",
			Summary:   "payment-api restart backoff",
		}, {
			ID:        "event-2",
			Source:    "kubernetes",
			Category:  "workload",
			Severity:  "warning",
			ClusterID: "cluster-a",
			Namespace: "orders",
			Summary:   "orders-api restart backoff",
		}}},
	}

	result, err := service.RecordAgentToolCall(context.Background(), domaincopilot.AgentToolCallInput{
		RunID:         "agent:tool-events",
		CallbackToken: "callback-token",
		AgentID:       "local-hermes-runner",
		ToolBindingID: "platform.events",
		Input:         map[string]any{"limit": 5},
	})
	if err != nil {
		t.Fatalf("record tool call: %v", err)
	}
	if result.ToolExecution.Status != "success" || result.ToolExecution.ToolName != "events.query" {
		t.Fatalf("unexpected tool execution: %#v", result.ToolExecution)
	}
	if result.Output["count"] != 1 {
		t.Fatalf("expected filtered event count, got %#v", result.Output)
	}
	if repo.callback.Status != domaincopilot.AgentRunStatusRunning || repo.callback.RunID != "agent:tool-events" {
		t.Fatalf("expected running callback persistence, got %#v", repo.callback)
	}
	if len(repo.agentRun.ToolExecutions) != 1 || repo.agentRun.ToolExecutions[0].ToolName != "events.query" {
		t.Fatalf("expected tool execution snapshot on run, got %#v", repo.agentRun.ToolExecutions)
	}
	if repo.agentRun.Status != domaincopilot.AgentRunStatusRunning {
		t.Fatalf("expected tool call to keep run running, got %q", repo.agentRun.Status)
	}
}

func TestRecordAgentToolCallExecutesDeliveryAndAlertTools(t *testing.T) {
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:            "agent:tool-delivery",
			ProviderID:    "hermes",
			ProviderKind:  "hermes",
			CapabilityID:  "delivery_failure",
			Status:        domaincopilot.AgentRunStatusRunning,
			Scope:         domaincopilot.SessionScope{ClusterID: "cluster-a", Namespace: "payments", Workload: "payment-api"},
			Input:         map[string]any{"applicationId": "app-payments"},
			CallbackToken: "callback-token",
			ToolBindings: []domaincopilot.AgentToolBinding{{
				ID:           "delivery.releases",
				CapabilityID: "delivery_failure",
				ToolKind:     "internal_api",
				ToolName:     "delivery.releases.list",
			}, {
				ID:           "delivery.builds",
				CapabilityID: "delivery_failure",
				ToolKind:     "internal_api",
				ToolName:     "delivery.builds.list",
			}, {
				ID:           "observability.alerts",
				CapabilityID: "delivery_failure",
				ToolKind:     "internal_api",
				ToolName:     "alerts.list",
			}},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	service := &Service{
		repo: repo,
		releases: agentToolReleaseReader{items: []domainrelease.Record{{
			ID:             "release-1",
			ApplicationID:  "app-payments",
			ClusterID:      "cluster-a",
			Namespace:      "payments",
			DeploymentName: "payment-api",
			Status:         "failed",
			CreatedAt:      time.Now().UTC(),
		}, {
			ID:             "release-2",
			ApplicationID:  "app-payments",
			ClusterID:      "cluster-a",
			Namespace:      "orders",
			DeploymentName: "orders-api",
			Status:         "completed",
			CreatedAt:      time.Now().UTC(),
		}}},
		builds: agentToolBuildReader{items: []domainbuild.Record{{
			ID:            "build-1",
			ApplicationID: "app-payments",
			SourceSystem:  "ci",
			Status:        "failed",
			CreatedAt:     time.Now().UTC(),
		}}},
		alerts: agentToolAlertReader{items: []domainalert.Instance{{
			ID:        "alert-1",
			Title:     "payment-api high error rate",
			Summary:   "payment-api has high 5xx rate",
			Severity:  "critical",
			Status:    "firing",
			ClusterID: "cluster-a",
			Namespace: "payments",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}}},
	}

	releaseResult, err := service.RecordAgentToolCall(context.Background(), domaincopilot.AgentToolCallInput{
		RunID:         "agent:tool-delivery",
		CallbackToken: "callback-token",
		ToolBindingID: "delivery.releases",
		Input:         map[string]any{"limit": 5},
	})
	if err != nil {
		t.Fatalf("record release tool call: %v", err)
	}
	if releaseResult.Output["count"] != 1 {
		t.Fatalf("expected one scoped release, got %#v", releaseResult.Output)
	}
	buildResult, err := service.RecordAgentToolCall(context.Background(), domaincopilot.AgentToolCallInput{
		RunID:         "agent:tool-delivery",
		CallbackToken: "callback-token",
		ToolBindingID: "delivery.builds",
		Input:         map[string]any{"limit": 5},
	})
	if err != nil {
		t.Fatalf("record build tool call: %v", err)
	}
	if buildResult.Output["count"] != 1 {
		t.Fatalf("expected one build, got %#v", buildResult.Output)
	}
	alertResult, err := service.RecordAgentToolCall(context.Background(), domaincopilot.AgentToolCallInput{
		RunID:         "agent:tool-delivery",
		CallbackToken: "callback-token",
		ToolBindingID: "observability.alerts",
		Input:         map[string]any{"limit": 5},
	})
	if err != nil {
		t.Fatalf("record alert tool call: %v", err)
	}
	if alertResult.Output["count"] != 1 {
		t.Fatalf("expected one alert, got %#v", alertResult.Output)
	}
	alerts, ok := alertResult.Output["alerts"].([]map[string]any)
	if !ok || len(alerts) != 1 {
		t.Fatalf("expected one alert summary, got %#v", alertResult.Output["alerts"])
	}
	if alerts[0]["startsAt"] != "" || alerts[0]["lastSeenAt"] != "" {
		t.Fatalf("expected zero alert times to be omitted, got %#v", alerts[0])
	}
	if len(repo.agentRun.ToolExecutions) != 3 {
		t.Fatalf("expected three tool executions, got %#v", repo.agentRun.ToolExecutions)
	}
}

func TestRecordAgentToolCallExecutesWorkbenchContextTools(t *testing.T) {
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:            "agent:tool-workbench-context",
			ProviderID:    "hermes",
			ProviderKind:  "hermes",
			CapabilityID:  "platform_resource_diagnosis",
			Status:        domaincopilot.AgentRunStatusRunning,
			Scope:         domaincopilot.SessionScope{ClusterID: "cluster-a", Namespace: "payments", Workload: "payment-api", Service: "payment-api"},
			Input:         map[string]any{"applicationId": "app-payments", "dockerHostId": "docker-host-1", "composeProjectId": "compose-1", "virtualizationConnectionId": "pve-1", "vmId": "vm-1"},
			CallbackToken: "callback-token",
			ToolBindings: []domaincopilot.AgentToolBinding{{
				ID:           "delivery.execution_tasks",
				CapabilityID: "delivery_failure",
				ToolKind:     "internal_api",
				ToolName:     "delivery.execution_tasks.list",
			}, {
				ID:           "platform.resources",
				CapabilityID: "platform_resource_diagnosis",
				ToolKind:     "internal_api",
				ToolName:     "platform.resources.snapshot",
			}, {
				ID:           "docker.operations",
				CapabilityID: "docker_diagnosis",
				ToolKind:     "internal_api",
				ToolName:     "docker.operations.list",
			}, {
				ID:           "docker.services",
				CapabilityID: "docker_diagnosis",
				ToolKind:     "internal_api",
				ToolName:     "docker.services.list",
			}, {
				ID:           "virtualization.operations",
				CapabilityID: "virtualization_diagnosis",
				ToolKind:     "internal_api",
				ToolName:     "virtualization.operations.list",
			}, {
				ID:           "oncall.routes",
				CapabilityID: "oncall_brief",
				ToolKind:     "internal_api",
				ToolName:     "oncall.routes.resolve",
			}},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	service := &Service{
		repo: repo,
		execution: agentToolExecutionReader{items: []domaindelivery.ExecutionTask{{
			ID:            "task-1",
			ApplicationID: "app-payments",
			TaskKind:      "release",
			ProviderKind:  "ci_agent_runner",
			Status:        "failed",
			CreatedAt:     time.Now().UTC(),
		}}},
		resources: agentToolResourceReader{
			nodes:       []domainresource.NodeView{{Name: "node-1", Status: "Ready", PodCount: 12}},
			pods:        []domainresource.PodView{{Name: "payment-api-abc", Namespace: "payments", Phase: "Running", ReadyContainers: "1/1"}},
			deployments: []domainresource.DeploymentView{{Name: "payment-api", Namespace: "payments", DesiredReplicas: 2, ReadyReplicas: 1}},
			services:    []domainresource.ServiceView{{Name: "payment-api", Namespace: "payments", Type: "ClusterIP", Selector: map[string]string{"app": "payment-api"}}},
		},
		docker: agentToolDockerReader{
			operations: []domaindocker.Operation{{ID: "docker-op-1", HostID: "docker-host-1", ProjectID: "compose-1", OperationKind: "project_deploy", Status: "failed", CreatedAt: time.Now().UTC()}},
			services:   []domaindocker.Service{{ID: "docker-svc-1", HostID: "docker-host-1", ProjectID: "compose-1", Name: "payment-api", Status: "exited"}},
		},
		virtualization: agentToolVirtualizationReader{items: []domainvirtualization.Task{{ID: "virt-task-1", Provider: "pve", ConnectionID: "pve-1", VMID: "vm-1", TaskKind: "vm_action", Status: "failed", CreatedAt: time.Now().UTC()}}},
		oncall:         agentToolOnCallResolver{result: map[string]any{"routeId": "route-1", "targetRef": "schedule:payments"}},
	}

	for _, item := range []struct {
		bindingID string
		key       string
	}{
		{bindingID: "delivery.execution_tasks", key: "executionTasks"},
		{bindingID: "platform.resources", key: "pods"},
		{bindingID: "docker.operations", key: "operations"},
		{bindingID: "docker.services", key: "services"},
		{bindingID: "virtualization.operations", key: "operations"},
		{bindingID: "oncall.routes", key: "resolution"},
	} {
		result, err := service.RecordAgentToolCall(context.Background(), domaincopilot.AgentToolCallInput{
			RunID:         "agent:tool-workbench-context",
			CallbackToken: "callback-token",
			ToolBindingID: item.bindingID,
			Input:         map[string]any{"limit": 5},
		})
		if err != nil {
			t.Fatalf("record %s tool call: %v", item.bindingID, err)
		}
		if _, ok := result.Output[item.key]; !ok {
			t.Fatalf("expected output key %s for %s, got %#v", item.key, item.bindingID, result.Output)
		}
	}
	if len(repo.agentRun.ToolExecutions) != 6 {
		t.Fatalf("expected six workbench context tool executions, got %#v", repo.agentRun.ToolExecutions)
	}
}

func TestRecordAgentRunCallbackPreservesPriorToolCalls(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	repo := &agentRuntimeCallbackTestRepository{
		agentRun: domaincopilot.AgentRun{
			ID:            "agent:run-tools",
			ProviderID:    "hermes",
			ProviderKind:  "hermes",
			CapabilityID:  "root_cause",
			Status:        domaincopilot.AgentRunStatusRunning,
			Output:        map[string]any{},
			CallbackToken: "callback-token",
			ToolExecutions: []domaincopilot.ToolExecution{{
				ID:       "tool:events",
				ToolName: "events.query",
				Status:   "success",
			}},
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		},
	}
	service := &Service{repo: repo}

	updated, err := service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:run-tools",
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload:       map[string]any{"summary": "Hermes completed with tool context."},
		ToolExecutions: []domaincopilot.ToolExecution{{
			ID:       "tool:agent-analysis",
			ToolName: "hermes.analysis",
			Status:   "completed",
		}},
	})
	if err != nil {
		t.Fatalf("record callback: %v", err)
	}
	if len(updated.ToolExecutions) != 2 {
		t.Fatalf("expected prior tool calls and final provider execution to be preserved, got %#v", updated.ToolExecutions)
	}
	if updated.ToolExecutions[0].ID != "tool:events" || updated.ToolExecutions[1].ID != "tool:agent-analysis" {
		t.Fatalf("unexpected merged tool executions: %#v", updated.ToolExecutions)
	}
}

func TestAgentRunHeartbeatExpiredUsesQueuedStartedAndHeartbeatTimes(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(-11 * time.Minute)
	heartbeatAt := now.Add(-2 * time.Minute)

	if !agentRunHeartbeatExpired(domaincopilot.AgentRun{
		Status:         domaincopilot.AgentRunStatusQueued,
		QueuedAt:       now.Add(-11 * time.Minute),
		TimeoutSeconds: 600,
	}, now) {
		t.Fatalf("expected queued run past timeout to expire")
	}
	if agentRunHeartbeatExpired(domaincopilot.AgentRun{
		Status:          domaincopilot.AgentRunStatusRunning,
		QueuedAt:        now.Add(-30 * time.Minute),
		StartedAt:       &startedAt,
		LastHeartbeatAt: &heartbeatAt,
		TimeoutSeconds:  600,
	}, now) {
		t.Fatalf("expected recent heartbeat to keep running agent run alive")
	}
	if agentRunHeartbeatExpired(domaincopilot.AgentRun{
		Status:         domaincopilot.AgentRunStatusCompleted,
		QueuedAt:       now.Add(-30 * time.Minute),
		TimeoutSeconds: 600,
	}, now) {
		t.Fatalf("expected terminal agent run to never expire")
	}
}

func newInspectionAuthzTestService(matrix map[string][]string) (*Service, *inspectionAuthzTestRepository) {
	repo := &inspectionAuthzTestRepository{}
	return New(repo, nil, nil, nil, nil, nil, nil, nil, nil, appaccess.NewPermissionResolver(inspectionAuthzRoleReader{matrix: matrix})), repo
}

type inspectionAuthzRoleReader struct {
	matrix map[string][]string
}

func (r inspectionAuthzRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r.matrix, nil
}

type inspectionAuthzTestRepository struct {
	listInspectionRunsCalled bool
	getSessionCalled         bool
	listDataSourcesCalled    bool
	session                  domaincopilot.Session
	messages                 []domaincopilot.Message
	createdMessage           domaincopilot.Message
	createdMessages          []domaincopilot.Message
	createdRootCauseRun      domaincopilot.RootCauseRun
	createdAgentRun          domaincopilot.AgentRun
	agentRuns                []domaincopilot.AgentRun
}

type agentRuntimeCallbackTestRepository struct {
	inspectionAuthzTestRepository
	agentRun                 domaincopilot.AgentRun
	rootCauseRun             domaincopilot.RootCauseRun
	rootCauseLookupCreatedBy string
	callback                 domaincopilot.AgentRunCallbackInput
}

func (r *agentRuntimeCallbackTestRepository) GetAgentRun(_ context.Context, _, runID string) (domaincopilot.AgentRun, error) {
	if r.agentRun.ID == runID {
		return r.agentRun, nil
	}
	return domaincopilot.AgentRun{}, apperrors.ErrNotFound
}

func (r *agentRuntimeCallbackTestRepository) GetRootCauseRun(_ context.Context, createdBy, runID string) (domaincopilot.RootCauseRun, error) {
	r.rootCauseLookupCreatedBy = createdBy
	if r.rootCauseRun.ID == runID && (createdBy == "" || r.rootCauseRun.CreatedBy == createdBy) {
		return r.rootCauseRun, nil
	}
	return domaincopilot.RootCauseRun{}, apperrors.ErrNotFound
}

func (r *agentRuntimeCallbackTestRepository) UpdateRootCauseRun(_ context.Context, run domaincopilot.RootCauseRun) (domaincopilot.RootCauseRun, error) {
	r.rootCauseRun = run
	return run, nil
}

func (r *agentRuntimeCallbackTestRepository) UpdateAgentRunCallback(_ context.Context, input domaincopilot.AgentRunCallbackInput) (domaincopilot.AgentRun, error) {
	r.callback = input
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = domaincopilot.AgentRunStatusRunning
	}
	r.agentRun.Status = status
	r.agentRun.Output = input.Payload
	r.agentRun.ToolExecutions = mergeAgentRuntimeTestToolExecutions(r.agentRun.ToolExecutions, input.ToolExecutions)
	r.agentRun.AnalysisArtifacts = input.AnalysisArtifacts
	r.agentRun.ClaimedByAgentID = input.AgentID
	r.agentRun.ExternalRunID = input.ExternalRunID
	return r.agentRun, nil
}

func (r *agentRuntimeCallbackTestRepository) CancelAgentRun(_ context.Context, input domaincopilot.AgentRunCancelInput) (domaincopilot.AgentRun, error) {
	if r.agentRun.ID != input.RunID {
		return domaincopilot.AgentRun{}, apperrors.ErrNotFound
	}
	r.agentRun.Status = domaincopilot.AgentRunStatusCanceled
	r.agentRun.Output = mergeAgentRunCallbackPayload(r.agentRun.Output, map[string]any{"cancelReason": input.Reason, "canceledBy": input.RequestedBy})
	r.agentRun.ErrorMessage = input.Reason
	now := time.Now().UTC()
	r.agentRun.CompletedAt = &now
	return r.agentRun, nil
}

func mergeAgentRuntimeTestToolExecutions(current []domaincopilot.ToolExecution, patch []domaincopilot.ToolExecution) []domaincopilot.ToolExecution {
	merged := append([]domaincopilot.ToolExecution(nil), current...)
	indexByID := map[string]int{}
	for index, item := range merged {
		if trimmed := strings.TrimSpace(item.ID); trimmed != "" {
			indexByID[trimmed] = index
		}
	}
	for _, item := range patch {
		id := strings.TrimSpace(item.ID)
		if id != "" {
			if index, ok := indexByID[id]; ok {
				merged[index] = item
				continue
			}
			indexByID[id] = len(merged)
		}
		merged = append(merged, item)
	}
	return merged
}

type agentToolEventReader struct {
	items []domainevent.Envelope
}

func (r agentToolEventReader) List(_ context.Context, limit int) ([]domainevent.Envelope, error) {
	if limit > 0 && len(r.items) > limit {
		return r.items[:limit], nil
	}
	return r.items, nil
}

type agentToolReleaseReader struct {
	items []domainrelease.Record
}

func (r agentToolReleaseReader) List(_ context.Context, filter domainrelease.Filter) ([]domainrelease.Record, error) {
	items := make([]domainrelease.Record, 0, len(r.items))
	for _, item := range r.items {
		if filter.ApplicationID != "" && item.ApplicationID != filter.ApplicationID {
			continue
		}
		if filter.ClusterID != "" && item.ClusterID != filter.ClusterID {
			continue
		}
		items = append(items, item)
	}
	if filter.Limit > 0 && len(items) > filter.Limit {
		return items[:filter.Limit], nil
	}
	return items, nil
}

type agentToolBuildReader struct {
	items []domainbuild.Record
}

func (r agentToolBuildReader) List(_ context.Context, filter domainbuild.Filter) ([]domainbuild.Record, error) {
	items := make([]domainbuild.Record, 0, len(r.items))
	for _, item := range r.items {
		if filter.ApplicationID != "" && item.ApplicationID != filter.ApplicationID {
			continue
		}
		items = append(items, item)
	}
	if filter.Limit > 0 && len(items) > filter.Limit {
		return items[:filter.Limit], nil
	}
	return items, nil
}

type agentToolExecutionReader struct {
	items []domaindelivery.ExecutionTask
}

func (r agentToolExecutionReader) ListExecutionTasks(_ context.Context, _ domainidentity.Principal, filter domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	items := make([]domaindelivery.ExecutionTask, 0, len(r.items))
	for _, item := range r.items {
		if filter.ApplicationID != "" && item.ApplicationID != filter.ApplicationID {
			continue
		}
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		items = append(items, item)
	}
	if filter.Limit > 0 && len(items) > filter.Limit {
		return items[:filter.Limit], nil
	}
	return items, nil
}

type agentToolResourceReader struct {
	nodes       []domainresource.NodeView
	pods        []domainresource.PodView
	deployments []domainresource.DeploymentView
	services    []domainresource.ServiceView
}

func (r agentToolResourceReader) ListNodes(context.Context, domainidentity.Principal, string) ([]domainresource.NodeView, error) {
	return r.nodes, nil
}

func (r agentToolResourceReader) ListPods(_ context.Context, _ domainidentity.Principal, _, namespace string) ([]domainresource.PodView, error) {
	out := make([]domainresource.PodView, 0, len(r.pods))
	for _, item := range r.pods {
		if namespace != "" && item.Namespace != namespace {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (r agentToolResourceReader) ListDeployments(_ context.Context, _ domainidentity.Principal, _, namespace string) ([]domainresource.DeploymentView, error) {
	out := make([]domainresource.DeploymentView, 0, len(r.deployments))
	for _, item := range r.deployments {
		if namespace != "" && item.Namespace != namespace {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (r agentToolResourceReader) ListServices(_ context.Context, _ domainidentity.Principal, _, namespace string) ([]domainresource.ServiceView, error) {
	out := make([]domainresource.ServiceView, 0, len(r.services))
	for _, item := range r.services {
		if namespace != "" && item.Namespace != namespace {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

type agentToolDockerReader struct {
	operations []domaindocker.Operation
	services   []domaindocker.Service
}

func (r agentToolDockerReader) ListOperations(_ context.Context, _ domainidentity.Principal, filter domaindocker.OperationFilter) (domaindocker.Page[domaindocker.Operation], error) {
	items := make([]domaindocker.Operation, 0, len(r.operations))
	for _, item := range r.operations {
		if filter.HostID != "" && item.HostID != filter.HostID {
			continue
		}
		if filter.ProjectID != "" && item.ProjectID != filter.ProjectID {
			continue
		}
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		items = append(items, item)
	}
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return domaindocker.Page[domaindocker.Operation]{Items: items, Total: len(items)}, nil
}

func (r agentToolDockerReader) ListServices(_ context.Context, _ domainidentity.Principal, filter domaindocker.ServiceFilter) (domaindocker.Page[domaindocker.Service], error) {
	items := make([]domaindocker.Service, 0, len(r.services))
	for _, item := range r.services {
		if filter.HostID != "" && item.HostID != filter.HostID {
			continue
		}
		if filter.ProjectID != "" && item.ProjectID != filter.ProjectID {
			continue
		}
		if filter.Search != "" && !strings.Contains(item.Name, filter.Search) {
			continue
		}
		items = append(items, item)
	}
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return domaindocker.Page[domaindocker.Service]{Items: items, Total: len(items)}, nil
}

type agentToolVirtualizationReader struct {
	items []domainvirtualization.Task
}

func (r agentToolVirtualizationReader) ListOperations(_ context.Context, _ domainidentity.Principal, filter domainvirtualization.TaskFilter) ([]domainvirtualization.Task, error) {
	items := make([]domainvirtualization.Task, 0, len(r.items))
	for _, item := range r.items {
		if filter.ConnectionID != "" && item.ConnectionID != filter.ConnectionID {
			continue
		}
		if filter.VMID != "" && item.VMID != filter.VMID {
			continue
		}
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		items = append(items, item)
	}
	if filter.Limit > 0 && len(items) > filter.Limit {
		return items[:filter.Limit], nil
	}
	return items, nil
}

type agentToolOnCallResolver struct {
	result map[string]any
}

func (r agentToolOnCallResolver) ResolveOnCall(context.Context, domainidentity.Principal, domainalert.OnCallResolveInput) (map[string]any, error) {
	return r.result, nil
}

type agentToolAlertReader struct {
	items []domainalert.Instance
}

func (r agentToolAlertReader) Summary(context.Context, domainidentity.Principal) (domainalert.Summary, error) {
	return domainalert.Summary{TotalCount: len(r.items)}, nil
}

func (r agentToolAlertReader) ListAlerts(_ context.Context, _ domainidentity.Principal, filter domainalert.Filter) ([]domainalert.Instance, error) {
	items := make([]domainalert.Instance, 0, len(r.items))
	for _, item := range r.items {
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if filter.ClusterID != "" && item.ClusterID != filter.ClusterID {
			continue
		}
		items = append(items, item)
	}
	if filter.Limit > 0 && len(items) > filter.Limit {
		return items[:filter.Limit], nil
	}
	return items, nil
}

func (r agentToolAlertReader) ListChannels(context.Context, domainidentity.Principal) ([]domainalert.NotificationChannel, error) {
	return nil, nil
}

func (r *inspectionAuthzTestRepository) ListSessions(context.Context, string, int) ([]domaincopilot.Session, error) {
	return nil, nil
}

func (r *inspectionAuthzTestRepository) GetSession(context.Context, string, string) (domaincopilot.Session, error) {
	r.getSessionCalled = true
	if r.session.ID != "" {
		return r.session, nil
	}
	return domaincopilot.Session{}, errors.New("unexpected session read")
}

func (r *inspectionAuthzTestRepository) CreateSession(context.Context, domaincopilot.Session) (domaincopilot.Session, error) {
	return domaincopilot.Session{}, errors.New("unexpected session create")
}

func (r *inspectionAuthzTestRepository) UpdateSession(_ context.Context, _, _ string, session domaincopilot.Session) (domaincopilot.Session, error) {
	if r.session.ID != "" {
		r.session = session
		return r.session, nil
	}
	return domaincopilot.Session{}, errors.New("unexpected session update")
}

func (r *inspectionAuthzTestRepository) DeleteSession(context.Context, string, string) error {
	return errors.New("unexpected session delete")
}

func (r *inspectionAuthzTestRepository) ListMessages(context.Context, string, int) ([]domaincopilot.Message, error) {
	return append([]domaincopilot.Message(nil), r.messages...), nil
}

func (r *inspectionAuthzTestRepository) CreateMessage(_ context.Context, message domaincopilot.Message) (domaincopilot.Message, error) {
	if r.session.ID != "" {
		r.createdMessage = message
		r.createdMessages = append(r.createdMessages, message)
		return r.createdMessage, nil
	}
	return domaincopilot.Message{}, errors.New("unexpected message create")
}

func (r *inspectionAuthzTestRepository) ListDataSources(context.Context) ([]domaincopilot.DataSource, error) {
	r.listDataSourcesCalled = true
	return []domaincopilot.DataSource{{
		ID:                "ds-1",
		Name:              "Prometheus",
		SourceKind:        "metrics",
		BackendType:       "prometheus",
		Enabled:           true,
		CredentialRef:     "secret/ref",
		MCPAdapter:        "metrics.v1",
		ValidationStatus:  "success",
		ValidationMessage: "ready",
	}}, nil
}

func (r *inspectionAuthzTestRepository) GetDataSource(context.Context, string) (domaincopilot.DataSource, error) {
	return domaincopilot.DataSource{}, nil
}

func (r *inspectionAuthzTestRepository) CreateDataSource(context.Context, domaincopilot.DataSource) (domaincopilot.DataSource, error) {
	return domaincopilot.DataSource{}, nil
}

func (r *inspectionAuthzTestRepository) UpdateDataSource(context.Context, string, domaincopilot.DataSourceInput) (domaincopilot.DataSource, error) {
	return domaincopilot.DataSource{}, nil
}

func (r *inspectionAuthzTestRepository) UpdateDataSourceValidation(context.Context, string, string, string, time.Time) (domaincopilot.DataSource, error) {
	return domaincopilot.DataSource{}, nil
}

func (r *inspectionAuthzTestRepository) ListAnalysisProfiles(context.Context) ([]domaincopilot.AnalysisProfile, error) {
	return []domaincopilot.AnalysisProfile{{
		ID:               "profile:inspection",
		Name:             "Inspection",
		Mode:             "inspection",
		EnabledSources:   []string{"logs.v1", "metrics.v1", "traces.v1"},
		EnabledPlaybooks: []string{"root-cause-investigation"},
		TimeoutSeconds:   120,
		Enabled:          true,
	}}, nil
}

func (r *inspectionAuthzTestRepository) GetAnalysisProfile(_ context.Context, profileID string) (domaincopilot.AnalysisProfile, error) {
	for _, profile := range []domaincopilot.AnalysisProfile{{
		ID:               "profile:inspection",
		Name:             "Inspection",
		Mode:             "inspection",
		EnabledSources:   []string{"logs.v1", "metrics.v1", "traces.v1"},
		EnabledPlaybooks: []string{"root-cause-investigation"},
		TimeoutSeconds:   120,
		Enabled:          true,
	}, {
		ID:               "profile:root",
		Name:             "Root Cause",
		Mode:             "root_cause",
		EnabledSources:   []string{"logs.v1", "metrics.v1", "traces.v1"},
		EnabledPlaybooks: []string{"root-cause-investigation"},
		TimeoutSeconds:   120,
		Enabled:          true,
	}} {
		if profile.ID == profileID {
			return profile, nil
		}
	}
	return domaincopilot.AnalysisProfile{}, apperrors.ErrNotFound
}

func (r *inspectionAuthzTestRepository) CreateAnalysisProfile(context.Context, domaincopilot.AnalysisProfile) (domaincopilot.AnalysisProfile, error) {
	return domaincopilot.AnalysisProfile{}, nil
}

func (r *inspectionAuthzTestRepository) UpdateAnalysisProfile(context.Context, string, domaincopilot.AnalysisProfileInput) (domaincopilot.AnalysisProfile, error) {
	return domaincopilot.AnalysisProfile{}, nil
}

func (r *inspectionAuthzTestRepository) ListAutomationPolicies(context.Context) ([]domaincopilot.AutomationPolicy, error) {
	return nil, nil
}

func (r *inspectionAuthzTestRepository) CreateAutomationPolicy(context.Context, domaincopilot.AutomationPolicy) (domaincopilot.AutomationPolicy, error) {
	return domaincopilot.AutomationPolicy{}, nil
}

func (r *inspectionAuthzTestRepository) UpdateAutomationPolicy(context.Context, string, domaincopilot.AutomationPolicyInput) (domaincopilot.AutomationPolicy, error) {
	return domaincopilot.AutomationPolicy{}, nil
}

func (r *inspectionAuthzTestRepository) DeleteAutomationPolicy(context.Context, string) error {
	return nil
}

func (r *inspectionAuthzTestRepository) ListRootCauseRuns(context.Context, string, domaincopilot.RootCauseRunFilter) ([]domaincopilot.RootCauseRun, error) {
	return nil, nil
}

func (r *inspectionAuthzTestRepository) GetRootCauseRun(context.Context, string, string) (domaincopilot.RootCauseRun, error) {
	return domaincopilot.RootCauseRun{}, nil
}

func (r *inspectionAuthzTestRepository) CreateRootCauseRun(_ context.Context, run domaincopilot.RootCauseRun) (domaincopilot.RootCauseRun, error) {
	r.createdRootCauseRun = run
	return run, nil
}

func (r *inspectionAuthzTestRepository) UpdateRootCauseRun(_ context.Context, run domaincopilot.RootCauseRun) (domaincopilot.RootCauseRun, error) {
	r.createdRootCauseRun = run
	return run, nil
}

func (r *inspectionAuthzTestRepository) ListAgentRuns(context.Context, domaincopilot.AgentRunFilter) ([]domaincopilot.AgentRun, error) {
	return append([]domaincopilot.AgentRun(nil), r.agentRuns...), nil
}

func (r *inspectionAuthzTestRepository) GetAgentRun(_ context.Context, _ string, runID string) (domaincopilot.AgentRun, error) {
	for _, run := range r.agentRuns {
		if run.ID == runID {
			return run, nil
		}
	}
	return domaincopilot.AgentRun{}, nil
}

func (r *inspectionAuthzTestRepository) CreateAgentRun(_ context.Context, run domaincopilot.AgentRun) (domaincopilot.AgentRun, error) {
	r.createdAgentRun = run
	r.agentRuns = append(r.agentRuns, run)
	return run, nil
}

func (r *inspectionAuthzTestRepository) ClaimAgentRun(_ context.Context, input domaincopilot.AgentRunClaimInput) (domaincopilot.AgentRun, error) {
	now := time.Now().UTC()
	for index := range r.agentRuns {
		if r.agentRuns[index].Status != domaincopilot.AgentRunStatusQueued {
			continue
		}
		r.agentRuns[index].Status = domaincopilot.AgentRunStatusRunning
		r.agentRuns[index].ClaimedByAgentID = input.AgentID
		r.agentRuns[index].StartedAt = &now
		r.agentRuns[index].LastHeartbeatAt = &now
		r.agentRuns[index].UpdatedAt = now
		return r.agentRuns[index], nil
	}
	return domaincopilot.AgentRun{}, apperrors.ErrNotFound
}

func (r *inspectionAuthzTestRepository) UpdateAgentRunCallback(_ context.Context, input domaincopilot.AgentRunCallbackInput) (domaincopilot.AgentRun, error) {
	now := time.Now().UTC()
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = domaincopilot.AgentRunStatusRunning
	}
	for index := range r.agentRuns {
		if r.agentRuns[index].ID != input.RunID {
			continue
		}
		r.agentRuns[index].Status = status
		r.agentRuns[index].Output = mergeAgentRunCallbackPayload(r.agentRuns[index].Output, input.Payload)
		r.agentRuns[index].ToolExecutions = input.ToolExecutions
		r.agentRuns[index].AnalysisArtifacts = input.AnalysisArtifacts
		r.agentRuns[index].ClaimedByAgentID = input.AgentID
		r.agentRuns[index].ExternalRunID = input.ExternalRunID
		r.agentRuns[index].ErrorMessage = input.ErrorMessage
		r.agentRuns[index].LastHeartbeatAt = &now
		if agentRunStatusTerminal(status) {
			r.agentRuns[index].CompletedAt = &now
		}
		r.agentRuns[index].UpdatedAt = now
		return r.agentRuns[index], nil
	}
	return domaincopilot.AgentRun{}, apperrors.ErrNotFound
}

func (r *inspectionAuthzTestRepository) CancelAgentRun(_ context.Context, input domaincopilot.AgentRunCancelInput) (domaincopilot.AgentRun, error) {
	now := time.Now().UTC()
	for index := range r.agentRuns {
		if r.agentRuns[index].ID != input.RunID {
			continue
		}
		r.agentRuns[index].Status = domaincopilot.AgentRunStatusCanceled
		r.agentRuns[index].Output = mergeAgentRunCallbackPayload(r.agentRuns[index].Output, map[string]any{
			"cancelReason": input.Reason,
			"canceledBy":   input.RequestedBy,
		})
		r.agentRuns[index].ErrorMessage = input.Reason
		r.agentRuns[index].CompletedAt = &now
		r.agentRuns[index].UpdatedAt = now
		return r.agentRuns[index], nil
	}
	return domaincopilot.AgentRun{}, apperrors.ErrNotFound
}

func (r *inspectionAuthzTestRepository) ListInspectionTasks(context.Context, string, int) ([]domaincopilot.InspectionTask, error) {
	return nil, nil
}

func (r *inspectionAuthzTestRepository) GetInspectionTask(context.Context, string, string) (domaincopilot.InspectionTask, error) {
	return domaincopilot.InspectionTask{}, nil
}

func (r *inspectionAuthzTestRepository) ListDueInspectionTasks(context.Context, time.Time, int) ([]domaincopilot.InspectionTask, error) {
	return nil, nil
}

func (r *inspectionAuthzTestRepository) CreateInspectionTask(context.Context, domaincopilot.InspectionTask) (domaincopilot.InspectionTask, error) {
	return domaincopilot.InspectionTask{}, errors.New("unexpected inspection task create")
}

func (r *inspectionAuthzTestRepository) UpdateInspectionTask(context.Context, string, string, domaincopilot.InspectionTaskInput) (domaincopilot.InspectionTask, error) {
	return domaincopilot.InspectionTask{}, nil
}

func (r *inspectionAuthzTestRepository) DeleteInspectionTask(context.Context, string, string) error {
	return nil
}

func (r *inspectionAuthzTestRepository) TouchInspectionTaskRun(context.Context, string, time.Time) error {
	return nil
}

func (r *inspectionAuthzTestRepository) ListInspectionRuns(context.Context, string, domaincopilot.InspectionRunFilter) ([]domaincopilot.InspectionRun, error) {
	r.listInspectionRunsCalled = true
	return nil, errors.New("unexpected inspection run list")
}

func (r *inspectionAuthzTestRepository) CreateInspectionRun(context.Context, domaincopilot.InspectionRun) (domaincopilot.InspectionRun, error) {
	return domaincopilot.InspectionRun{}, nil
}

type inspectionAuthzSettingsResolver struct {
	settings domainsettings.AISettings
}

func (r inspectionAuthzSettingsResolver) ResolveAISettings(context.Context) (domainsettings.AISettings, error) {
	return r.settings, nil
}

func graphHasNode(graph *domaincopilot.AnalysisGraph, nodeID string) bool {
	for _, node := range graph.Nodes {
		if node.ID == nodeID {
			return true
		}
	}
	return false
}

func floatNear(value any, expected float64) bool {
	number, ok := positiveFloat(value)
	if !ok {
		return false
	}
	diff := number - expected
	return diff < 0.000001 && diff > -0.000001
}

func graphHasEdge(graph *domaincopilot.AnalysisGraph, edgeID string) bool {
	for _, edge := range graph.Edges {
		if edge.ID == edgeID {
			return true
		}
	}
	return false
}
