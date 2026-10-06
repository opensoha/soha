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

func TestCloseRequiresPermissionAndAuditBeforeQueue(t *testing.T) {
	now := time.Now().UTC()
	store := &closeTestStore{now: now}
	principal := domainidentity.Principal{UserID: "viewer-1", Roles: []string{"viewer"}}
	service := &Management{store: store, permissions: appaccess.NewPermissionResolver(proxyTestRoles{
		keys: []string{appaccess.PermNetworkAccessProxyConnectionsView},
	}), now: func() time.Time { return now }}

	if _, err := service.Close(context.Background(), principal, "proxy-1", "connection-1"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("close without permission error = %v", err)
	}
	if store.queued {
		t.Fatal("close was queued without permission")
	}

	service.permissions = appaccess.NewPermissionResolver(proxyTestRoles{
		keys: []string{appaccess.PermNetworkAccessProxyConnectionsClose},
	})
	service.audit = closeTestAudit{err: errors.New("audit unavailable")}
	if _, err := service.Close(context.Background(), principal, "proxy-1", "connection-1"); err == nil {
		t.Fatal("close succeeded without an audit record")
	}
	if store.queued {
		t.Fatal("close was queued before the audit record")
	}
}
