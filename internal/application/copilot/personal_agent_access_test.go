package copilot

import (
	"context"
	"errors"
	"strings"
	"testing"

	access "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Existing personal-feature fixtures represent already-approved users.
func (r *inspectionAuthzTestRepository) GetPersonalAgentAccess(_ context.Context, owner string) (domain.PersonalAgentAccess, error) {
	if r.accessStates != nil {
		if item, ok := r.accessStates[owner]; ok {
			return item, nil
		}
		return domain.PersonalAgentAccess{UserID: owner, Status: "not_requested"}, nil
	}
	return domain.PersonalAgentAccess{UserID: owner, Status: "approved", Version: 1}, nil
}

func TestNativePersonalToolsRequireAnApprovedPersonalSession(t *testing.T) {
	for _, tc := range []struct {
		kind, status string
		denied       bool
	}{
		{"main", "not_requested", false}, {"personal_main", "approved", false}, {"personal_main", "revoked", true},
	} {
		t.Run(tc.kind+"_"+tc.status, func(t *testing.T) {
			service, loop, gateway, _ := nativeLoopFixture(t)
			repo, ok := service.sessions.(*inspectionAuthzTestRepository)
			if !ok {
				t.Fatal("native fixture requires inspectionAuthzTestRepository")
			}
			repo.accessStates = map[string]domain.PersonalAgentAccess{"reader": {UserID: "reader", Status: tc.status, Version: 1}}
			repo.session = domain.Session{ID: "session", CreatedBy: "reader", Metadata: map[string]any{"conversationKind": tc.kind}}
			gateway.catalog = appaigateway.NewPersonalEnvironmentCapabilityProvider(service).Tools()
			capture := &nativeQueueCapture{inspectionAuthzTestRepository: repo, nativeLoopStore: loop}
			service.agentRuns = capture
			_, err := service.queueNativeMessage(t.Context(), identity.Principal{UserID: "reader", Roles: []string{"reader"}}, repo.session, domain.SessionMetadata{}, domain.WorkbenchSendMessageInput{Content: "hello"}, "zh-CN")
			if tc.denied {
				if !errors.Is(err, apperrors.ErrAccessDenied) {
					t.Fatalf("revoked queue: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, binding := range capture.run.ToolBindings {
				if strings.HasPrefix(binding.ToolName, "personal.environment.") {
					count++
				}
			}
			if (count > 0) != (tc.kind == "personal_main") {
				t.Fatalf("tool snapshot crossed workspace: %+v", capture.run.ToolBindings)
			}
			if tc.kind == "main" {
				_, err := service.executeChatAgentTool(t.Context(), capture.run, nativePersonalEnvironmentBindings()[0], map[string]any{})
				if !errors.Is(err, apperrors.ErrAccessDenied) || gateway.lastCall.CapabilityVersion != "" {
					t.Fatalf("forged personal binding reached Gateway: %v", err)
				}
			} else {
				repo.accessStates["reader"] = domain.PersonalAgentAccess{UserID: "reader", Status: "revoked", Version: 2}
				if _, err := service.executeChatAgentTool(t.Context(), capture.run, nativePersonalEnvironmentBindings()[0], map[string]any{}); !errors.Is(err, apperrors.ErrAccessDenied) {
					t.Fatalf("frozen tool bypassed revocation: %v", err)
				}
				if _, err := service.FindNativeRequest(t.Context(), identity.Principal{UserID: "reader", Roles: []string{"reader"}}, "session", "original"); !errors.Is(err, apperrors.ErrAccessDenied) {
					t.Fatalf("recovery bypassed revocation: %v", err)
				}
			}
		})
	}
}
func (r *inspectionAuthzTestRepository) RequestPersonalAgentAccess(ctx context.Context, owner string) (domain.PersonalAgentAccess, error) {
	item, _ := r.GetPersonalAgentAccess(ctx, owner)
	if item.Status == "approved" || item.Status == "pending" {
		return item, nil
	}
	item.Status = "pending"
	item.Version++
	if r.accessStates == nil {
		r.accessStates = map[string]domain.PersonalAgentAccess{}
	}
	r.accessStates[owner] = item
	return item, nil
}
func (r *inspectionAuthzTestRepository) ListPersonalAgentAccess(context.Context) ([]domain.PersonalAgentAccess, error) {
	return []domain.PersonalAgentAccess{}, nil
}
func (r *inspectionAuthzTestRepository) DecidePersonalAgentAccess(ctx context.Context, owner, actor, status string, version int64) (domain.PersonalAgentAccess, error) {
	item, _ := r.GetPersonalAgentAccess(ctx, owner)
	allowed := item.Status == "pending" && (status == "approved" || status == "rejected") || item.Status == "approved" && status == "revoked"
	if item.Version != version || !allowed {
		return item, apperrors.ErrConflict
	}
	item.Status = status
	item.Version++
	item.DecidedBy = actor
	r.accessStates[owner] = item
	return item, nil
}
func (r *inspectionAuthzTestRepository) ListPersonalSessions(context.Context, string, int) ([]domain.Session, error) {
	return []domain.Session{r.session}, nil
}

func TestPersonalAgentActivationIsIndependentOfChatAndRuntime(t *testing.T) {
	defer access.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{"chat": {access.PermObserveAIChatUse}, "admin": {access.PermAIEnvironmentsUpdate}})
	repo.accessStates = map[string]domain.PersonalAgentAccess{}
	ctx := context.Background()
	user := identity.Principal{UserID: "owner", Roles: []string{"chat"}}
	admin := identity.Principal{UserID: "admin", Roles: []string{"admin"}}
	if _, err := service.EnsureMainSession(ctx, user, "internal", "zh-CN"); err != nil {
		t.Fatal(err)
	}
	ordinary := repo.session
	if _, err := service.EnsurePersonalAssistant(ctx, user, "zh-CN"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("chat opened personal agent: %v", err)
	}
	if repo.session.ID != ordinary.ID {
		t.Fatal("denied access changed ordinary chat")
	}
	pending, err := service.RequestPersonalAgentAccess(ctx, user)
	if err != nil || pending.Status != "pending" {
		t.Fatalf("request: %+v %v", pending, err)
	}
	if _, err := service.DecidePersonalAgentAccess(ctx, user, user.UserID, "approved", pending.Version); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("self-service approval: %v", err)
	}
	approved, err := service.DecidePersonalAgentAccess(ctx, admin, user.UserID, "approved", pending.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecidePersonalAgentAccess(ctx, admin, user.UserID, "rejected", pending.Version); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale approval: %v", err)
	}
	assistant, err := service.EnsurePersonalAssistant(ctx, user, "zh-CN")
	if err != nil || assistant.Name != "heya" || assistant.MainSessionID == ordinary.ID || !isPersonalSession(repo.session) {
		t.Fatalf("personal identity: %+v %v", assistant, err)
	}
	if _, err := service.DecidePersonalAgentAccess(ctx, admin, user.UserID, "revoked", approved.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StreamMessage(ctx, user, assistant.MainSessionID, domain.WorkbenchSendMessageInput{Content: "hi"}, "zh-CN"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked stream accepted: %v", err)
	}
}
