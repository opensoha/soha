package networkaccess

import (
	"context"
	"errors"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type sessionCommandLookupStore struct {
	Store
	session, key string
	calls        int
	parentReads  int
	parent       domainnetworkaccess.Session
}

func (s *sessionCommandLookupStore) GetSession(_ context.Context, id string) (domainnetworkaccess.Session, error) {
	s.parentReads++
	if s.parent.ID != "" {
		return s.parent, nil
	}
	return domainnetworkaccess.Session{ID: id, SiteID: "site-1"}, nil
}

func (s *sessionCommandLookupStore) GetSessionCommandForSession(_ context.Context, session, id string) (domainnetworkaccess.SessionCommand, error) {
	s.session, s.key, s.calls = session, id, s.calls+1
	return domainnetworkaccess.SessionCommand{ID: id, SessionID: session}, nil
}
func (s *sessionCommandLookupStore) GetSessionCommandForPlan(_ context.Context, session, hash string) (domainnetworkaccess.SessionCommand, error) {
	s.session, s.key, s.calls = session, hash, s.calls+1
	return domainnetworkaccess.SessionCommand{ID: "original", SessionID: session}, nil
}

func TestSessionCommandLookupUsesCurrentPermissionAndExactCoordinates(t *testing.T) {
	store := &sessionCommandLookupStore{}
	roles := enrollmentRoleReader{"task115-command-reader": {appaccess.PermNetworkAccessSitesView}, "unrelated": {appaccess.PermNetworkAccessSitesUpdate}}
	service := &Service{store: store, permissions: appaccess.NewPermissionResolver(roles), scope: allowTestNetworkScope()}
	actor := domainidentity.Principal{UserID: "actor", Roles: []string{"task115-command-reader"}}
	command, err := service.GetSessionCommandForSession(t.Context(), actor, "session-1", "command-1")
	if err != nil || command.ID != "command-1" || store.session != "session-1" || store.key != "command-1" || store.calls != 1 {
		t.Fatalf("lookup=%+v err=%v store=%+v", command, err, store)
	}
	hash := "sha256:" + strings.Repeat("a", 64)
	command, err = service.GetOriginalSessionCommand(t.Context(), actor, "session-1", hash)
	if err != nil || command.ID != "original" || store.key != hash || store.calls != 2 {
		t.Fatalf("original=%+v err=%v", command, err)
	}
	actor.PermissionKeys = []string{appaccess.PermNetworkAccessSitesUpdate}
	if _, err := service.GetOriginalSessionCommand(t.Context(), actor, "session-1", hash); !errors.Is(err, apperrors.ErrAccessDenied) || store.calls != 2 {
		t.Fatalf("key ceiling err=%v calls=%d", err, store.calls)
	}
	actor.PermissionKeys = nil
	delete(roles, "task115-command-reader")
	if _, err := service.GetSessionCommandForSession(t.Context(), actor, "session-1", "command-1"); !errors.Is(err, apperrors.ErrAccessDenied) || store.calls != 2 {
		t.Fatalf("current role err=%v calls=%d", err, store.calls)
	}
	service.permissions = nil
	if _, err := service.GetSessionCommandForSession(t.Context(), actor, "session-1", "command-1"); !errors.Is(err, apperrors.ErrAccessDenied) || store.calls != 2 {
		t.Fatalf("missing resolver err=%v calls=%d", err, store.calls)
	}
}

func TestSessionCommandLookupRejectsInvalidCoordinatesBeforeStorage(t *testing.T) {
	store := &sessionCommandLookupStore{}
	service := &Service{store: store, permissions: appaccess.NewPermissionResolver(enrollmentRoleReader{"reader": {appaccess.PermNetworkAccessSitesView}}), scope: allowTestNetworkScope()}
	actor := domainidentity.Principal{UserID: "actor", Roles: []string{"reader"}}
	for _, tc := range []struct {
		session, key string
		plan         bool
	}{
		{"", "command", false}, {"session", "", false}, {"../session", "command", false}, {"session", "a\n", false}, {"session", strings.Repeat("x", 129), false},
		{"session", "sha256:short", true}, {"session", "sha256:" + strings.Repeat("A", 64), true}, {"session", "sha256:" + strings.Repeat("a", 65), true},
	} {
		var err error
		if tc.plan {
			_, err = service.GetOriginalSessionCommand(t.Context(), actor, tc.session, tc.key)
		} else {
			_, err = service.GetSessionCommandForSession(t.Context(), actor, tc.session, tc.key)
		}
		if !errors.Is(err, apperrors.ErrInvalidArgument) || store.calls != 0 || store.parentReads != 0 {
			t.Fatalf("invalid lookup err=%v calls=%d", err, store.calls)
		}
	}
}

func TestSessionQualifiedCommandReadsEnforceCurrentParentScope(t *testing.T) {
	for _, original := range []bool{false, true} {
		for _, unavailable := range []bool{false, true} {
			store := &sessionCommandLookupStore{parent: domainnetworkaccess.Session{ID: "session-1", SiteID: "site-hidden"}}
			service := &Service{store: store, permissions: appaccess.NewPermissionResolver(enrollmentRoleReader{"reader": {appaccess.PermNetworkAccessSitesView}})}
			service.scope = scopeAuthorizerFunc(func(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
				if request.Resource.Labels["network.siteId"] != "site-hidden" {
					t.Fatal("scope did not use the actual parent session")
				}
				if unavailable {
					return domainaccess.Decision{}, apperrors.ErrServiceUnavailable
				}
				return domainaccess.Decision{Allowed: false}, nil
			})
			actor := domainidentity.Principal{UserID: "actor", Roles: []string{"reader"}}
			var command domainnetworkaccess.SessionCommand
			var err error
			if original {
				command, err = service.GetOriginalSessionCommand(t.Context(), actor, "session-1", "sha256:"+strings.Repeat("a", 64))
			} else {
				command, err = service.GetSessionCommandForSession(t.Context(), actor, "session-1", "command-1")
			}
			if err == nil || command.ID != "" || store.calls != 0 || store.parentReads != 1 {
				t.Fatalf("session scope must fail before command data: original=%v unavailable=%v error=%v calls=%d", original, unavailable, err, store.calls)
			}
		}
	}
}
