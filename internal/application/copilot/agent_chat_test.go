package copilot

import (
	"context"
	"errors"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
	"time"
)

type chatKnowledgeSearcher struct{ calls int }

func (s *chatKnowledgeSearcher) GetBase(_ context.Context, principal domainidentity.Principal, id string) (domainknowledge.KnowledgeBase, error) {
	if principal.UserID != "user-1" || id != "allowed" {
		return domainknowledge.KnowledgeBase{}, apperrors.ErrAccessDenied
	}
	return domainknowledge.KnowledgeBase{ID: id}, nil
}

func (s *chatKnowledgeSearcher) Search(_ context.Context, principal domainidentity.Principal, input domainknowledge.SearchRequest) (domainknowledge.SearchResult, error) {
	s.calls++
	if principal.UserID != "user-1" || len(input.KnowledgeBaseIDs) != 1 || input.KnowledgeBaseIDs[0] != "allowed" {
		return domainknowledge.SearchResult{}, apperrors.ErrAccessDenied
	}
	return domainknowledge.SearchResult{Hits: []domainknowledge.SearchHit{{Content: "maintenance window is 03:00", Citation: domainknowledge.Citation{ID: "citation-1"}}}}, nil
}

func TestExternalChatKnowledgeContextAuthorizedAndRemovable(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
	service.SetExternalAgentProviders([]domaincopilot.AgentProvider{readyChatProvider()})
	repo.session = domaincopilot.Session{ID: "session-1", CreatedBy: "user-1", Metadata: sessionMetadataMap(domaincopilot.SessionMetadata{Mode: "general", AgentProviderID: "hermes-api"})}
	searcher := &chatKnowledgeSearcher{}
	service.SetContextBuilder(NewContextBuilder(searcher, nil))
	principal := domainidentity.Principal{UserID: "user-1", Roles: []string{"chat"}}
	input := domaincopilot.WorkbenchSendMessageInput{Content: "When is maintenance?", KnowledgeContext: &domaincopilot.KnowledgeContextConfig{Enabled: true, KnowledgeBaseIDs: []string{"denied"}}}
	_, err := service.StreamMessage(context.Background(), principal, "session-1", input, "en-US")
	if !errors.Is(err, apperrors.ErrAccessDenied) || repo.createdAgentRun.ID != "" || len(repo.createdMessages) != 0 {
		t.Fatal("denied knowledge must not enqueue or persist a conversation message")
	}
	input.KnowledgeContext.KnowledgeBaseIDs = []string{"allowed"}
	if _, err = service.StreamMessage(context.Background(), principal, "session-1", input, "en-US"); err != nil {
		t.Fatal(err)
	}
	run := domaincopilot.WithOperationState(repo.createdAgentRun, time.Now())
	if run.Chat == nil || run.Chat.Context != "" || searcher.calls != 0 || len(agentPrincipalStringList(run.Input["_sohaKnowledgeBaseIds"])) != 1 {
		t.Fatalf("knowledge must be authorized for on-demand retrieval: %+v", run.Chat)
	}
	snapshot, ok := run.Input["contextSnapshot"].(map[string]any)
	if !ok || snapshot["usageProvenance"] != "estimated_characters" || snapshot["contentHash"] == "" {
		t.Fatal("missing explainable snapshot")
	}
	input.KnowledgeContext = &domaincopilot.KnowledgeContextConfig{Enabled: false}
	if _, err = service.StreamMessage(context.Background(), principal, "session-1", input, "en-US"); err != nil {
		t.Fatal(err)
	}
	run = domaincopilot.WithOperationState(repo.createdAgentRun, time.Now())
	if run.Chat == nil || run.Chat.Context != "" || searcher.calls != 0 || len(agentPrincipalStringList(run.Input["_sohaKnowledgeBaseIds"])) != 0 {
		t.Fatal("removed knowledge was reused")
	}
}

func readyChatProvider() domaincopilot.AgentProvider {
	now := time.Now().UTC()
	return domaincopilot.AgentProvider{ID: "hermes-api", Kind: "hermes-api", Enabled: true, Capabilities: []string{"general"}, SupportsAsync: true, Config: map[string]any{"pluginId": "opensoha.hermes-api", "pluginVersion": "1", "providerVersion": "1", "catalogRevision": 2}, RuntimeStatus: &domaincopilot.AgentProviderRuntimeStatus{State: "ready", LastHeartbeatAt: &now, ObservedAt: now}}
}

func TestGeneralChatQueuesExternalPluginAndPersistsPlainReply(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
	service.SetExternalAgentProviders([]domaincopilot.AgentProvider{readyChatProvider()})
	repo.session = domaincopilot.Session{ID: "session-1", CreatedBy: "user-1", Metadata: sessionMetadataMap(domaincopilot.SessionMetadata{Mode: "general", AgentProviderID: "hermes-api"})}
	repo.messages = []domaincopilot.Message{{Role: "user", Content: "记住这个话题"}, {Role: "assistant", Content: "好的"}, {Role: "assistant", Content: "模型未配置", Metadata: map[string]any{"source": "model-unconfigured"}}}
	principal := domainidentity.Principal{UserID: "user-1", Roles: []string{"chat"}}
	result, err := service.StreamMessage(context.Background(), principal, "session-1", domaincopilot.WorkbenchSendMessageInput{Content: "继续讨论", EventSink: func(domaincopilot.WorkbenchStreamEvent) bool { return true }}, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	run := domaincopilot.WithOperationState(repo.createdAgentRun, time.Now())
	if run.CapabilityID != "general" || run.Chat == nil || run.Chat.Question != "继续讨论" || len(run.Chat.History) != 2 {
		t.Fatalf("conversation not queued: %+v", run.Chat)
	}
	if len(result.Envelope.AnalysisArtifacts) != 0 || len(run.ToolBindings) != 1 || run.ToolBindings[0].ToolName != "artifact.preview" {
		t.Fatal("general chat inherited an analysis workflow")
	}
	_, err = service.RecordAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{RunID: run.ID, CallbackToken: run.CallbackToken, AgentID: "runner", Status: "completed", Payload: map[string]any{"summary": "这是普通回复"}})
	if err != nil {
		t.Fatal(err)
	}
	if repo.createdMessage.Role != "assistant" || repo.createdMessage.Content != "这是普通回复" {
		t.Fatalf("wrong final reply: %+v", repo.createdMessage)
	}
	if artifacts := repo.createdMessage.Metadata["analysisArtifacts"]; artifacts != nil {
		if items, ok := artifacts.([]domaincopilot.AnalysisArtifact); ok && len(items) > 0 {
			t.Fatal("plain reply became analysis artifact")
		}
	}
}

func TestGeneralChatRejectsUnavailablePluginWithoutModelFallback(t *testing.T) {
	for _, name := range []string{"disabled", "not-installed", "unsupported", "stale"} {
		t.Run(name, func(t *testing.T) {
			defer appaccess.SetRolePermissionMatrix(nil)
			service, repo := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
			provider := readyChatProvider()
			switch name {
			case "disabled":
				provider.Enabled = false
			case "not-installed":
				provider.Config = nil
			case "unsupported":
				provider.Capabilities = []string{"root_cause"}
			case "stale":
				old := time.Now().Add(-3 * time.Minute)
				provider.RuntimeStatus.LastHeartbeatAt = &old
			}
			service.SetExternalAgentProviders([]domaincopilot.AgentProvider{provider})
			repo.session = domaincopilot.Session{ID: "session-1", CreatedBy: "user-1", Metadata: sessionMetadataMap(domaincopilot.SessionMetadata{Mode: "general", AgentProviderID: provider.ID})}
			_, err := service.SendMessage(context.Background(), domainidentity.Principal{UserID: "user-1", Roles: []string{"chat"}}, "session-1", "hello", "en-US")
			if err == nil || len(repo.createdMessages) > 0 || repo.createdAgentRun.ID != "" {
				t.Fatal("unavailable plugin was executed or fell back")
			}
		})
	}
}

func TestAgentChatInputExcludesLegacyFailedAssistantReplies(t *testing.T) {
	messages := []domaincopilot.Message{{Role: "user", Content: "hello"}}
	for _, status := range []string{"failed", "callback_timeout", "canceled"} {
		messages = append(messages, domaincopilot.Message{Role: "assistant", Content: "internal failure", Metadata: map[string]any{"source": "agent-runtime", "agentStatus": map[string]any{"status": status}}})
	}
	input := agentChatInput(messages, "next", "zh-CN")
	if len(input.History) != 0 {
		t.Fatalf("failed reply entered history: %+v", input.History)
	}
}

func TestAgentChatHistoryDropsCanceledRequestWithoutLosingCompletedTurns(t *testing.T) {
	messages := []domaincopilot.Message{
		{ID: "good", Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{ID: "cancelled", Role: "user", Content: "write twenty chapters"},
		{Role: "system", Content: "cancelled", Metadata: map[string]any{"source": "agent-runtime-error", "requestMessageId": "cancelled", "agentStatus": map[string]any{"status": "canceled"}}},
	}
	input := agentChatInput(messages, "search the knowledge base", "en-US")
	if len(input.History) != 2 || input.History[0].Content != "hello" || input.History[1].Content != "hi" {
		t.Fatalf("canceled work survived: %+v", input.History)
	}
}
