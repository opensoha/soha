package copilot

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appidentity "github.com/opensoha/soha/internal/application/identity"
	"github.com/opensoha/soha/internal/platform/appconfig"
	"github.com/opensoha/soha/internal/platform/apperrors"
	policyrepo "github.com/opensoha/soha/internal/repository/policy"
	userrepo "github.com/opensoha/soha/internal/repository/user"
)

func TestPersistentIdentitySurvivesServiceRestartWithPostgres(t *testing.T) {
	_, store, ctx := newCopilotPostgresTestRepository(t)
	users := userrepo.New(store.DB())
	policies := policyrepo.New(store.DB())
	owner, other := uuid.NewString(), uuid.NewString()
	password := "isolated-restart-test-only"
	createAuthzTestUsers(t, ctx, users, policies, owner, other, password)
	cfg := appconfig.Auth{JWT: appconfig.JWT{Secret: uuid.NewString(), Issuer: "task115-restart", AccessTTL: time.Minute, RefreshTTL: time.Hour}}
	start := func(config appconfig.Auth) *appidentity.Service {
		t.Helper()
		// New adapters and a new service intentionally share only durable DB and keys.
		repo := userrepo.New(store.DB())
		service, err := appidentity.New(appidentity.Dependencies{
			Config: config, Accounts: repo, Passwords: repo, Authorization: repo,
			RoleBindings: repo, TeamBindings: repo, Identities: repo, Sessions: repo,
			SessionAdmin: repo, EphemeralTokens: repo, Permissions: appaccess.NewPermissionResolver(policies),
		})
		if err != nil {
			t.Fatal(err)
		}
		return service
	}
	first := start(cfg)
	result, err := first.LoginWithPassword(ctx, owner, password)
	if err != nil {
		t.Fatal(err)
	}
	restarted := start(cfg)
	principal, _, err := restarted.ParseAccessToken(ctx, result.Tokens.AccessToken)
	if err != nil || principal.UserID != owner {
		t.Fatal("persisted access session did not survive service reconstruction")
	}
	refreshed, err := restarted.RefreshSession(ctx, result.Tokens.RefreshToken)
	if err != nil || refreshed.Tokens.AccessToken == "" {
		t.Fatal("persisted refresh session did not survive service reconstruction")
	}
	changed := cfg
	changed.JWT.Secret = uuid.NewString()
	if _, err := start(changed).RefreshSession(ctx, result.Tokens.RefreshToken); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("changing signing keys should reject the previous refresh credential")
	}
	if err := restarted.Logout(ctx, refreshed.Tokens.AccessToken, refreshed.Tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := start(cfg).RefreshSession(ctx, result.Tokens.RefreshToken); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("service restart revived a revoked session")
	}
}
