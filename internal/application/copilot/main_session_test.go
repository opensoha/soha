package copilot

import (
	"context"
	"errors"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

func TestMainSessionIsStableAndProtected(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
	ctx := context.Background()
	principal := domainidentity.Principal{UserID: "user-1", Roles: []string{"chat"}}
	denied := domainidentity.Principal{UserID: "user-2", Roles: []string{"viewer"}}
	if _, err := service.EnsureMainSession(ctx, denied, "internal", "zh-CN"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("permission bypass: %v", err)
	}
	if _, err := service.EnsureMainSession(ctx, principal, string(make([]byte, 201)), "zh-CN"); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("unbounded provider: %v", err)
	}
	first, err := service.EnsureMainSession(ctx, principal, "internal", "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if first.CreatedBy != principal.UserID || parseSessionMetadata(first.Metadata).ConversationKind != "main" {
		t.Fatalf("wrong owner or role: %+v", first)
	}
	updated, err := service.UpdateSession(ctx, principal, first.ID, "", "general", "hermes", "", "", nil, nil, "", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	again, err := service.EnsureMainSession(ctx, principal, "internal", "en-US")
	if err != nil || again.ID != first.ID || parseSessionMetadata(again.Metadata).AgentProviderID != "hermes" {
		t.Fatalf("main chat reset: %+v %v", again, err)
	}
	if parseSessionMetadata(updated.Metadata).ConversationKind != "main" {
		t.Fatal("update dropped classification")
	}
	for _, archived := range []bool{false, true} {
		if _, err := service.UpdateSession(ctx, principal, first.ID, "", "", "", "archived", "", nil, nil, "", nil, nil, archived); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("main archived: %v", err)
		}
	}
	if err := service.DeleteSession(ctx, principal, first.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("main deleted: %v", err)
	}
	side, err := service.CreateSession(ctx, principal, "Separate topic", "general", "internal", nil, nil, "workbench", nil, "en-US")
	if err != nil || parseSessionMetadata(side.Metadata).ConversationKind != "side" {
		t.Fatalf("side classification: %+v %v", side, err)
	}
	if _, err := service.UpdateSession(ctx, principal, side.ID, "", "", "", "", "", nil, nil, "", nil, nil, true); err != nil {
		t.Fatalf("ordinary archive changed: %v", err)
	}
	repo.getSessionErr = apperrors.ErrNotFound
	if err := service.DeleteSession(ctx, denied, first.ID); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("delete permission bypass: %v", err)
	}
}
