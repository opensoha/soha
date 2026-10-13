package copilot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestPersonalAssistantOwnershipAndTasks(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
	ctx := context.Background()
	user := domainidentity.Principal{UserID: "u1", Roles: []string{"chat"}}
	denied := domainidentity.Principal{UserID: "denied"}
	if _, err := service.ListPersonalAssistantTasks(ctx, domainidentity.Principal{Roles: []string{"chat"}}); !errors.Is(err, apperrors.ErrAccessDenied) || repo.listAgentRunsCalled {
		t.Fatalf("missing owner queried tasks: %v", err)
	}
	if _, err := service.EnsurePersonalAssistant(ctx, denied, "zh-CN"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("identity permission: %v", err)
	}
	first, err := service.EnsurePersonalAssistant(ctx, user, "zh-CN")
	if err != nil || first.MainSessionID != repo.session.ID || first.ID == first.MainSessionID {
		t.Fatalf("identity binding: %+v %v", first, err)
	}
	renamed, err := service.RenamePersonalAssistant(ctx, user, "  小苏  ", "zh-CN")
	if err != nil || renamed.ID != first.ID || renamed.Name != "小苏" {
		t.Fatalf("rename: %+v %v", renamed, err)
	}
	again, err := service.EnsurePersonalAssistant(ctx, user, "en-US")
	if err != nil || again.ID != first.ID || again.Name != "小苏" {
		t.Fatalf("identity reset: %+v %v", again, err)
	}
	for _, name := range []string{" ", strings.Repeat("名", 61), "name\nline"} {
		if _, err := service.RenamePersonalAssistant(ctx, user, name, "zh-CN"); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("invalid name accepted: %q %v", name, err)
		}
	}
	other, err := service.EnsurePersonalAssistant(ctx, domainidentity.Principal{UserID: "u2", Roles: []string{"chat"}}, "zh-CN")
	if err != nil || other.ID == first.ID || other.MainSessionID == first.MainSessionID {
		t.Fatalf("identity isolation: %+v %v", other, err)
	}
	assertPersonalAssistantTasksAndArtifacts(t, service, repo, user, denied, first)
}

func assertPersonalAssistantTasksAndArtifacts(t *testing.T, service *Service, repo *inspectionAuthzTestRepository, user, denied domainidentity.Principal, first domain.PersonalAssistant) {
	t.Helper()
	ctx := context.Background()
	repo.agentRuns = []domain.AgentRun{{ID: "mine-main", CreatedBy: "u1", SessionID: first.MainSessionID, CallbackToken: "private"}, {ID: "mine-side", CreatedBy: "u1", SessionID: "side"}, {ID: "other", CreatedBy: "u2"}}
	if _, err := service.ListPersonalAssistantTasks(ctx, denied); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("tasks permission: %v", err)
	}
	tasks, err := service.ListPersonalAssistantTasks(ctx, user)
	if err != nil || len(tasks) != 2 || tasks[0].CallbackToken != "" || repo.agentRunFilter.CreatedBy != user.UserID || repo.agentRunFilter.Limit != 50 || !repo.agentRunFilter.ExcludeArchivedSessions {
		t.Fatalf("personal tasks leaked: %+v %v", tasks, err)
	}
	if _, err := service.ListPersonalAssistantArtifacts(ctx, denied); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("artifacts permission: %v", err)
	}
	artifact := domain.AnalysisArtifact{Kind: "report", RunID: "report-1", Summary: "Saved report"}
	repo.personalArtifacts = []domain.PersonalArtifact{{ID: "task:mine-main:0", SessionID: first.MainSessionID, Artifact: artifact, CreatedAt: time.Now()}, {ID: "task:mine-main:0", Artifact: artifact}, {ID: "message:side:0", SessionID: "side", Artifact: artifact}}
	repo.agentRuns[0].AnalysisArtifacts = []domain.AnalysisArtifact{artifact}
	repo.agentRuns[1].AnalysisArtifacts = []domain.AnalysisArtifact{artifact}
	artifacts, err := service.ListPersonalAssistantArtifacts(ctx, user)
	if err != nil || len(artifacts) != 3 {
		t.Fatalf("artifacts lost or duplicated: %+v %v", artifacts, err)
	}
}
