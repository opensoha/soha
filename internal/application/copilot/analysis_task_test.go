package copilot

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestAnalysisTaskPreservesConversationDefaults(t *testing.T) {
	for _, kind := range []string{"main", "side", ""} {
		for _, task := range []struct{ mode, provider string }{
			{"inspection_review", "internal"},
			{"performance", "internal"},
			{"root_cause", "hermes"},
			{"performance", "hermes"},
		} {
			t.Run(kind+"/"+task.mode+"/"+task.provider, func(t *testing.T) {
				service, repo := newInspectionAuthzTestService(map[string][]string{
					"chat": {appaccess.PermObserveAIChatUse, appaccess.PermObserveAIRootCauseRun, appaccess.PermObserveMonitoringView, appaccess.PermObserveAIView},
				})
				repo.session = domaincopilot.Session{
					ID: "session-1", CreatedBy: "user-1", CreatedAt: time.Now().UTC(),
					Metadata: sessionMetadataMap(domaincopilot.SessionMetadata{
						ConversationKind: kind, Mode: "general", AgentProviderID: "internal",
						Scope: domaincopilot.SessionScope{ClusterID: "cluster-a", TimeRangeMinutes: 30},
					}),
				}
				principal := domainidentity.Principal{UserID: "user-1", Roles: []string{"chat"}}
				result, err := service.StreamMessage(context.Background(), principal, repo.session.ID,
					domaincopilot.WorkbenchSendMessageInput{Content: "Investigate the incident", Mode: task.mode, AgentProviderID: task.provider}, "en-US")
				if err != nil {
					t.Fatalf("analysis task: %v", err)
				}
				wantMode, wantProvider := "general", "internal"
				if kind == "" {
					wantMode, wantProvider = task.mode, task.provider
				}
				metadata := parseSessionMetadata(repo.session.Metadata)
				if metadata.Mode != wantMode || metadata.AgentProviderID != wantProvider || metadata.ConversationKind != kind {
					t.Fatalf("conversation defaults changed: mode=%s provider=%s kind=%s", metadata.Mode, metadata.AgentProviderID, metadata.ConversationKind)
				}
				if result.Envelope.SessionPatch["mode"] != wantMode {
					t.Fatalf("session patch must match stored defaults: %#v", result.Envelope.SessionPatch)
				}
				assistant := result.Envelope.Messages[len(result.Envelope.Messages)-1]
				if assistant.Metadata["mode"] != task.mode {
					t.Fatalf("task lost its analysis type: %#v", assistant.Metadata)
				}
				if kind != "" {
					_, err = service.StreamMessage(context.Background(), principal, repo.session.ID,
						domaincopilot.WorkbenchSendMessageInput{Content: "Explain the result", ContextSelection: &domaincopilot.ContextSelection{}}, "en-US")
					if err != nil {
						t.Fatalf("ordinary follow-up must accept chat context: %v", err)
					}
				}
			})
		}
	}
}

func TestLegacyProfessionalEntrypointsRequireCapabilityPermission(t *testing.T) {
	for _, provider := range []string{"internal", "hermes"} {
		for _, mode := range []string{"root_cause", "performance", "trace", "inspection_review"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				service, repo := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
				repo.session = domaincopilot.Session{ID: "session", CreatedBy: "user", Metadata: sessionMetadataMap(domaincopilot.SessionMetadata{Mode: "general", Scope: domaincopilot.SessionScope{ClusterID: "cluster"}})}
				principal := domainidentity.Principal{UserID: "user", Roles: []string{"chat"}}
				if _, err := service.StreamMessage(t.Context(), principal, "session", domaincopilot.WorkbenchSendMessageInput{Content: "Analyze", Mode: mode, AgentProviderID: provider}, "en-US"); !errors.Is(err, apperrors.ErrAccessDenied) {
					t.Fatalf("stream bypassed capability permission: %v", err)
				}
				if _, err := service.RunSessionAnalysis(t.Context(), principal, "session", domaincopilot.RootCauseRunInput{Question: "Analyze", Kind: mode, AgentProviderID: provider}, "en-US"); !errors.Is(err, apperrors.ErrAccessDenied) {
					t.Fatalf("analysis bypassed capability permission: %v", err)
				}
				if repo.createdAgentRun.ID != "" {
					t.Fatal("unauthorized external task was queued")
				}
			})
		}
	}
}
