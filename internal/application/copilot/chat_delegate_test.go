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

type specialistTestRepository struct {
	*inspectionAuthzTestRepository
	parent, child domaincopilot.AgentRun
	creates       int
}

func (r *specialistTestRepository) CreateDelegatedAgentRun(_ context.Context, parentID string, child domaincopilot.AgentRun) (domaincopilot.AgentRun, error) {
	if r.child.ID != "" {
		return r.child, nil
	}
	r.creates++
	r.child = child
	r.child.Status = "completed"
	r.child.Output = map[string]any{"answer": "Evidence found"}
	return r.child, nil
}
func (r *specialistTestRepository) GetAgentRun(_ context.Context, owner, id string) (domaincopilot.AgentRun, error) {
	if id == r.parent.ID {
		return r.parent, nil
	}
	if id == r.child.ID {
		return r.child, nil
	}
	return domaincopilot.AgentRun{}, apperrors.ErrNotFound
}
func (r *specialistTestRepository) CancelAgentRun(_ context.Context, input domaincopilot.AgentRunCancelInput) (domaincopilot.AgentRun, error) {
	r.child.Status = "canceled"
	return r.child, nil
}
func TestChatSpecialistUsesIndependentReadOnlyContextAndOneRun(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, base := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse, appaccess.PermAIKnowledgeView}})
	service.SetExternalAgentProviders([]domaincopilot.AgentProvider{readyChatProvider()})
	parent := domaincopilot.AgentRun{ID: "parent", ProviderID: "hermes-api", CapabilityID: "general", SessionID: "session", CreatedBy: "user-1", Status: "running", QueuedAt: time.Now(), TimeoutSeconds: 300, ToolBindings: chatAgentToolBindings(domaincopilot.SessionMetadata{KnowledgeContext: domaincopilot.KnowledgeContextConfig{Enabled: true, KnowledgeBaseIDs: []string{"selected"}}}), Input: map[string]any{"locale": "en-US", "chat": "private parent history", "_sohaKnowledgeBaseIds": []string{"selected"}, "_sohaPrincipal": map[string]any{"userId": "user-1", "roles": []string{"chat"}, "permissionKeys": []string{appaccess.PermObserveAIChatUse, appaccess.PermAIKnowledgeView}}}}
	repo := &specialistTestRepository{inspectionAuthzTestRepository: base, parent: parent}
	service.agentRuns = repo
	for range 2 {
		result, err := service.delegateChatSpecialist(context.Background(), parent, map[string]any{"query": "Check maintenance evidence"})
		if err != nil || result["status"] != "completed" {
			t.Fatalf("result=%v err=%v", result, err)
		}
	}
	child := repo.child
	if repo.creates != 1 || child.ParentRunID != parent.ID || child.ID != "parent:specialist" || child.Chat == nil || len(child.Chat.History) != 0 || child.Chat.Question != "Check maintenance evidence" || len(child.SkillBindings) != 0 {
		t.Fatalf("incorrect specialist: %+v", child)
	}
	if len(child.ToolBindings) != 1 || child.ToolBindings[0].ToolName != "knowledge.search" {
		t.Fatalf("not permission intersection: %+v", child.ToolBindings)
	}
	if child.TimeoutSeconds > parent.TimeoutSeconds || remainingChatToolEvidenceTokens(parent)+remainingChatToolEvidenceTokens(child) != 6000 {
		t.Fatal("shared bounds exceeded")
	}
	if _, err := service.delegateChatSpecialist(context.Background(), parent, map[string]any{"query": "Different"}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("changed delegate accepted: %v", err)
	}
	if _, err := service.delegateChatSpecialist(context.Background(), child, map[string]any{"query": "Recursive"}); err == nil {
		t.Fatal("recursive delegation accepted")
	}
	repo.child.Status = "running"
	if err := service.cancelChatSpecialist(context.Background(), parent); err != nil || repo.child.Status != "canceled" {
		t.Fatalf("cancel propagation: %v", err)
	}
	service.agentPrincipals = &chatToolPrincipalResolver{principal: domainidentity.Principal{UserID: "user-1", Roles: []string{"chat"}}}
	repo.parent.Status = "canceled"
	child.Status = "running"
	child.ClaimedByAgentID = "runner"
	if _, err := service.authorizeChatAgentTool(context.Background(), child, child.ToolBindings[0]); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("tool after parent cancellation: %v", err)
	}
}
