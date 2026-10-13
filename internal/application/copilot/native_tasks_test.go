package copilot

import (
	"context"
	"errors"
	"testing"
	"time"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type nativeTaskStoreFixture struct {
	*nativeQueueCapture
	parent domaincopilot.NativeTaskParent
	stop   string
}

func (f *nativeTaskStoreFixture) WithNativeTaskCreation(ctx context.Context, _ domaincopilot.NativeLease, register func(context.Context, domaincopilot.NativeTaskParent) error) error {
	if f.stop != "" {
		return apperrors.ErrConflict
	}
	return register(ctx, f.parent)
}
func (f *nativeTaskStoreFixture) NativeTaskStopReason(context.Context, domaincopilot.NativeTaskParent) (string, error) {
	return f.stop, nil
}
func (f *nativeTaskStoreFixture) WithNativeTaskParent(ctx context.Context, _ domaincopilot.NativeTaskParent, cancellation bool, execute func(context.Context) error) error {
	if !cancellation && f.stop != "" {
		return apperrors.ErrConflict
	}
	ctx, cancel := context.WithDeadline(ctx, f.parent.Deadline)
	defer cancel()
	return execute(ctx)
}

func TestNativeChildGuardRefreshesStoredIdentityAndEvidence(t *testing.T) {
	for _, scenario := range []string{"active", "knowledge_revoked", "token_revoked", "stopped"} {
		t.Run(scenario, func(t *testing.T) {
			service, loop, _, _ := nativeGovernedTaskFixture(t)
			resolver := &nativeTokenPrincipalResolver{chatToolPrincipalResolver: chatToolPrincipalResolver{principal: domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}}}
			service.agentPrincipals = resolver
			mapValue(loop.lease.Run.Input["_sohaPrincipal"])["accessTokenId"] = "frozen-token"
			loop.lease.Run.Output["contextSnapshot"] = map[string]any{"citations": []map[string]any{{"knowledgeBaseId": "base", "documentId": "doc", "chunkId": "chunk", "contentHash": "hash"}}}
			parent := domaincopilot.NativeTaskParent{RunID: loop.lease.Run.ID, OwnerID: "reader", SessionID: "session", RequestHash: "frozen", Deadline: time.Now().Add(time.Minute)}
			store := &nativeTaskStoreFixture{nativeQueueCapture: &nativeQueueCapture{inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}, nativeLoopStore: loop}, parent: parent}
			service.agentRuns = store
			switch scenario {
			case "knowledge_revoked":
				nativeLoopCitations(t, service).denied = true
			case "token_revoked":
				resolver.err = apperrors.ErrUnauthorized
			case "stopped":
				store.stop = "user"
			}
			// The caller's stale payload must never replace the stored evidence or token.
			expected := loop.lease
			expected.Run.Input, expected.Run.Output = map[string]any{}, map[string]any{}
			created := false
			err := service.WithNativeTaskCreation(t.Context(), expected, func(_ context.Context, got domaincopilot.NativeTaskParent, principal domainidentity.Principal) error {
				created = true
				if got != parent || principal.UserID != "reader" || resolver.tokenID != "frozen-token" {
					t.Fatal("child did not use the stored original identity")
				}
				return nil
			})
			if scenario == "active" {
				if err != nil || !created {
					t.Fatalf("stored identity unavailable: %v", err)
				}
			} else if err == nil || created {
				t.Fatalf("revoked evidence created a child: %v", err)
			}
			reason, err := service.NativeTaskStopReason(t.Context(), parent)
			want := map[string]string{"active": "", "knowledge_revoked": "failure", "token_revoked": "failure", "stopped": "user"}[scenario]
			if err != nil || reason != want {
				t.Fatalf("stop reason=%q want=%q err=%v", reason, want, err)
			}
			for _, cancellation := range []bool{false, true} {
				allowed := scenario == "active" || (cancellation && scenario != "token_revoked")
				assertNativeParentAccess(t, service, parent, cancellation, allowed)
			}
		})
	}
}

func assertNativeParentAccess(t *testing.T, service *Service, parent domaincopilot.NativeTaskParent, cancellation, allowed bool) {
	t.Helper()
	called := false
	err := service.WithNativeTaskParent(t.Context(), parent, cancellation, func(ctx context.Context) error {
		called = true
		if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(parent.Deadline) {
			t.Fatal("parent deadline was dropped")
		}
		return nil
	})
	if allowed && (err != nil || !called) || !allowed && (err == nil || called) {
		t.Fatalf("cancellation=%t authorized=%t called=%t err=%v", cancellation, allowed, called, err)
	}
	if !allowed && !errors.Is(err, apperrors.ErrConflict) && !errors.Is(err, apperrors.ErrAccessDenied) && !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("unexpected authorization error: %v", err)
	}
}
