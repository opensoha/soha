package networkproxy

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type proxyTestRoles struct{ keys []string }

func (r proxyTestRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"viewer": r.keys}, nil
}

type closeTestStore struct {
	ManagementStore
	queued bool
	now    time.Time
}

func (s *closeTestStore) Get(context.Context, string) (domain.Instance, error) {
	return domain.Instance{Capabilities: []string{"close_connection"}}, nil
}

func (s *closeTestStore) Connections(context.Context, string) (domain.ConnectionsSnapshot, error) {
	return domain.ConnectionsSnapshot{ObservedAt: &s.now,
		Connections: []domain.Connection{{ID: "connection-1"}}}, nil
}

func (s *closeTestStore) QueueClose(context.Context, string, string, string, time.Time) (domain.CloseCommand, error) {
	s.queued = true
	return domain.CloseCommand{}, nil
}

type closeTestAudit struct{ err error }

func (a closeTestAudit) Record(context.Context, domainaudit.Entry) error { return a.err }

type closeReplayTestStore struct {
	closeTestStore
	receipt domain.CloseCommand
}

func (s *closeReplayTestStore) ReplayClose(context.Context, string, string, string, domain.CloseInput, time.Time) (domain.CloseCommand, bool, error) {
	return s.receipt, s.receipt.ID != "", nil
}

func (s *closeReplayTestStore) QueueCloseRequest(_ context.Context, _, _, _ string, input domain.CloseInput, _ time.Time) (domain.CloseCommand, error) {
	s.queued = true
	return domain.CloseCommand{ID: input.RequestID, ExpiresAt: input.ExpiresAt}, nil
}

func TestCloseRecoversExistingReceiptAfterWindowWithoutQueueing(t *testing.T) {
	now := time.Now().UTC()
	store := &closeReplayTestStore{}
	principal := domainidentity.Principal{UserID: "viewer-1", Roles: []string{"viewer"}}
	service := &Management{store: store, permissions: appaccess.NewPermissionResolver(proxyTestRoles{
		keys: []string{appaccess.PermNetworkAccessProxyConnectionsClose},
	}), now: func() time.Time { return now }, scope: commandScope{allowed: true}, audit: closeTestAudit{}, operations: commandOperations{}}
	input := domain.CloseInput{RequestID: "caller-id", ProcessEpoch: "epoch-1", IssuedAt: now.Add(-2 * time.Minute), ExpiresAt: now.Add(-100 * time.Second)}
	if _, err := service.CloseWithInput(context.Background(), principal, "proxy-1", "connection-1", input); !errors.Is(err, apperrors.ErrInvalidArgument) || store.queued {
		t.Fatalf("new expired intent error = %v, queued = %v", err, store.queued)
	}
	store.receipt = domain.CloseCommand{ID: input.RequestID, Status: "closed", ExpiresAt: input.ExpiresAt}
	if got, err := service.CloseWithInput(context.Background(), principal, "proxy-1", "connection-1", input); err != nil || got != store.receipt || store.queued {
		t.Fatalf("replayed = %+v, error = %v, queued = %v", got, err, store.queued)
	}
}

func TestCloseRequiresPermissionAndAuditBeforeQueue(t *testing.T) {
	now := time.Now().UTC()
	store := &closeTestStore{now: now}
	principal := domainidentity.Principal{UserID: "viewer-1", Roles: []string{"viewer"}}
	service := &Management{store: store, permissions: appaccess.NewPermissionResolver(proxyTestRoles{
		keys: []string{appaccess.PermNetworkAccessProxyConnectionsView},
	}), now: func() time.Time { return now }, scope: commandScope{allowed: true}}

	if _, err := service.Close(context.Background(), principal, "proxy-1", "connection-1"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("close without permission error = %v", err)
	}
	if store.queued {
		t.Fatal("close was queued without permission")
	}

	service.permissions = appaccess.NewPermissionResolver(proxyTestRoles{
		keys: []string{appaccess.PermNetworkAccessProxyConnectionsClose},
	})
	auditErr := errors.New("audit unavailable")
	service.audit = closeTestAudit{err: auditErr}
	if _, err := service.Close(context.Background(), principal, "proxy-1", "connection-1"); !errors.Is(err, auditErr) {
		t.Fatalf("close audit error = %v", err)
	}
	if store.queued {
		t.Fatal("close was queued before the audit record")
	}
}
