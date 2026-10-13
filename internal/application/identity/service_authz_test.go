package identity

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
	userrepo "github.com/opensoha/soha/internal/repository/user"
)

func TestAccessTokenRevocationAcrossSessionRefresh(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "versioned"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			service, repo := newAuthzTestService()
			initial, err := service.loadPrincipal(ctx, "u1")
			if err != nil {
				t.Fatal(err)
			}
			first, err := service.issueAuthResult(ctx, initial, "password")
			if err != nil {
				t.Fatal(err)
			}
			second, err := service.issueAuthResult(ctx, initial, "password")
			if err != nil {
				t.Fatal(err)
			}
			otherIdentity := initial
			otherIdentity.UserID = "u2"
			other, err := service.issueAuthResult(ctx, otherIdentity, "password")
			if err != nil {
				t.Fatal(err)
			}
			old := first.Tokens.AccessToken
			if legacy {
				old = rewriteAccessClaims(t, service, old, func(claims jwt.MapClaims) { delete(claims, "authzVersion") })
			}
			principal, _, err := service.ParseAccessToken(ctx, old)
			if err != nil || !slices.Contains(principal.Roles, "chat-reader") {
				t.Fatalf("initial principal = %+v, %v", principal, err)
			}
			repo.roleBindings["u1"] = map[string]loginMappingBinding{}
			repo.teamBindings["u1"] = map[string]loginMappingBinding{}
			user := repo.usersByID["u1"]
			user.Tags = nil
			user.AuthzVersion++
			repo.usersByID["u1"] = user
			if _, _, err := service.ParseAccessToken(ctx, old); !errors.Is(err, apperrors.ErrUnauthorized) {
				t.Fatalf("before refresh: %v", err)
			}
			refreshed, err := service.RefreshSession(ctx, first.Tokens.RefreshToken)
			if err != nil {
				t.Fatal(err)
			}
			current, access, err := service.ParseAccessToken(ctx, refreshed.Tokens.AccessToken)
			if err != nil {
				t.Fatal(err)
			}
			if len(current.Roles)+len(current.Teams)+len(current.Tags) != 0 {
				t.Fatalf("revoked memberships restored: %+v", current)
			}
			resolver := appaccess.NewPermissionResolver(authzRolePermissions{})
			if err := resolver.Authorize(ctx, current, "observe.ai.chat"); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("refreshed chat authorization: %v", err)
			}
			assertOldAccessAfterRefresh(t, service, resolver, old, legacy)
			if _, _, err := service.ParseAccessToken(ctx, second.Tokens.AccessToken); !errors.Is(err, apperrors.ErrUnauthorized) {
				t.Fatalf("other stale session: %v", err)
			}
			otherPrincipal, _, err := service.ParseAccessToken(ctx, other.Tokens.AccessToken)
			if err != nil || resolver.Authorize(ctx, otherPrincipal, "observe.ai.chat") != nil {
				t.Fatalf("unrelated user's authorization changed: %+v, %v", otherPrincipal, err)
			}
			assertRevokedSession(t, service, repo, access.SessionID, refreshed)
		})
	}
}

func assertRevokedSession(t *testing.T, service *Service, repo *loginMappingUserRepo, sessionID string, refreshed domainidentity.AuthResult) {
	t.Helper()
	ctx := context.Background()
	if err := repo.RevokeSession(ctx, repo.sessionsByID[sessionID].RefreshTokenID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ParseAccessToken(ctx, refreshed.Tokens.AccessToken); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("revoked session accepted: %v", err)
	}
	if _, err := service.RefreshSession(ctx, refreshed.Tokens.RefreshToken); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("revoked session refreshed: %v", err)
	}
}

func assertOldAccessAfterRefresh(t *testing.T, service *Service, resolver *appaccess.PermissionResolver, old string, legacy bool) {
	t.Helper()
	ctx := context.Background()
	principal, _, err := service.ParseAccessToken(ctx, old)
	if !legacy {
		if !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatalf("old token revived after refresh: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("legacy authentication: %v", err)
	}
	if len(principal.Roles)+len(principal.Teams)+len(principal.Tags) != 0 {
		t.Fatalf("legacy stale memberships: %+v", principal)
	}
	if err := resolver.Authorize(ctx, principal, "observe.ai.chat"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("legacy revoked chat authorization: %v", err)
	}
}

func TestAccessTokenRejectsInvalidAuthorizationVersion(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "zero", value: 0},
		{name: "negative", value: -1},
		{name: "future", value: 2},
		{name: "string", value: "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, _ := newAuthzTestService()
			result, err := service.issueAuthResult(context.Background(), domainidentity.Principal{UserID: "u1"}, "password")
			if err != nil {
				t.Fatal(err)
			}
			token := rewriteAccessClaims(t, service, result.Tokens.AccessToken, func(claims jwt.MapClaims) {
				claims["authzVersion"] = test.value
			})
			if _, _, err := service.ParseAccessToken(context.Background(), token); !errors.Is(err, apperrors.ErrUnauthorized) {
				t.Fatalf("invalid version accepted: %v", err)
			}
		})
	}
}

func TestSessionTokensUseCurrentIdentity(t *testing.T) {
	ctx := context.Background()
	service, repo := newAuthzTestService()
	stale := domainidentity.Principal{UserID: "u1", Roles: []string{"removed-role"}, Teams: []string{"removed-team"}, Tags: []string{"removed-tag"}}
	result, err := service.issueAuthResult(ctx, stale, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.User.Roles, []string{"chat-reader"}) || !slices.Equal(result.User.Teams, []string{"current-team"}) || !slices.Equal(result.User.Tags, []string{"current-tag"}) {
		t.Fatalf("stale handoff principal signed: %+v", result.User)
	}
	claims, err := service.parseToken(result.Tokens.AccessToken, "access")
	if err != nil || !slices.Equal(claims.Roles, result.User.Roles) {
		t.Fatalf("signed claims = %+v, %v", claims, err)
	}
	if claims.AuthzVersion == nil || *claims.AuthzVersion != 1 {
		t.Fatalf("missing issuance authorization version")
	}
	// Current membership remains authoritative even if an older writer missed a version bump.
	repo.roleBindings["u1"] = map[string]loginMappingBinding{}
	principal, _, err := service.ParseAccessToken(ctx, result.Tokens.AccessToken)
	if err != nil || len(principal.Roles) != 0 {
		t.Fatalf("stale claims trusted: %+v, %v", principal, err)
	}
	user := repo.usersByID["u1"]
	user.Status = "disabled"
	repo.usersByID["u1"] = user
	if _, _, err := service.ParseAccessToken(ctx, result.Tokens.AccessToken); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("disabled account accepted: %v", err)
	}
	if _, err := service.RefreshSession(ctx, result.Tokens.RefreshToken); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("disabled account refreshed: %v", err)
	}
}

func newAuthzTestService() (*Service, *loginMappingUserRepo) {
	repo := newLoginMappingUserRepo()
	for _, id := range []string{"u1", "u2"} {
		repo.usersByID[id] = userrepo.User{ID: id, Username: id, Status: "active", AuthzVersion: 1, Tags: []string{"current-tag"}}
		repo.roleBindings[id] = map[string]loginMappingBinding{"chat-reader": {source: "local"}}
		repo.teamBindings[id] = map[string]loginMappingBinding{"current-team": {source: "local"}}
	}
	service := newTestServiceWithUserStore(repo)
	service.cfg = cfgpkg.AuthConfig{JWT: cfgpkg.JWTConfig{Secret: uuid.NewString(), Issuer: "soha-test", AccessTTL: time.Minute, RefreshTTL: time.Hour}}
	return service, repo
}

func rewriteAccessClaims(t *testing.T, service *Service, signed string, update func(jwt.MapClaims)) string {
	t.Helper()
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(signed, claims); err != nil {
		t.Fatal(err)
	}
	update(claims)
	ring, err := service.jwtKeyring()
	if err != nil {
		t.Fatal(err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = ring.Active().ID()
	result, err := token.SignedString([]byte(ring.Active().Secret()))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type authzRolePermissions struct{}

func (authzRolePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"chat-reader": {"observe.ai.chat"}}, nil
}
