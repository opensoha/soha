package copilot

import (
	"errors"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestNativeRequestRecoveryKeepsOwnerSessionAndOriginalRun(t *testing.T) {
	service, repo := newInspectionAuthzTestService(map[string][]string{"reader": {appaccess.PermObserveAIChatUse}})
	principal := domainidentity.Principal{UserID: "owner", Roles: []string{"reader"}}
	repo.session = domaincopilot.Session{ID: "session", CreatedBy: "owner"}
	repo.agentRuns = []domaincopilot.AgentRun{{ID: "original", SessionID: "session", CreatedBy: "owner", ProviderKind: "native", Status: "running", CallbackToken: "private"}}
	run, err := service.FindNativeRequest(t.Context(), principal, "session", "opaque/key+one")
	if err != nil || run.ID != "original" || run.CallbackToken != "" || run.Status != "running" {
		t.Fatalf("original public run not recovered: %+v %v", run, err)
	}
	filter := repo.agentRunFilter
	if !repo.getSessionCalled || filter.CreatedBy != principal.UserID || filter.SessionID != "session" || filter.NativeRequestID != "opaque/key+one" || filter.Limit != 1 || repo.createdAgentRun.ID != "" || len(repo.createdMessages) != 0 {
		t.Fatalf("lookup lost scope or wrote: %+v", filter)
	}
	repo.agentRuns = nil
	if _, err := service.FindNativeRequest(t.Context(), principal, "session", "missing"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("absence must remain unknown to the client: %v", err)
	}
}

func TestNativeRequestRecoveryRejectsInvalidRevokedAndUnownedReads(t *testing.T) {
	for _, test := range []struct {
		session, key, role string
		sessionErr, want   error
	}{
		{"", "key", "reader", nil, apperrors.ErrInvalidArgument},
		{"session", "", "reader", nil, apperrors.ErrInvalidArgument},
		{"session", strings.Repeat("x", 129), "reader", nil, apperrors.ErrInvalidArgument},
		{"session", "key", "revoked", nil, apperrors.ErrAccessDenied},
		{"session", "key", "reader", apperrors.ErrNotFound, apperrors.ErrNotFound},
	} {
		service, repo := newInspectionAuthzTestService(map[string][]string{"reader": {appaccess.PermObserveAIChatUse}})
		repo.session = domaincopilot.Session{ID: "session", CreatedBy: "owner"}
		repo.getSessionErr = test.sessionErr
		_, err := service.FindNativeRequest(t.Context(), domainidentity.Principal{UserID: "owner", Roles: []string{test.role}}, test.session, test.key)
		if !errors.Is(err, test.want) || repo.listAgentRunsCalled || repo.createdAgentRun.ID != "" {
			t.Fatalf("invalid/revoked/unowned read reached run store: %v", err)
		}
	}
}
