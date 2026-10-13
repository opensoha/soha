package copilot

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/api/handlers"
	"github.com/opensoha/soha/internal/api/middleware"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	appidentity "github.com/opensoha/soha/internal/application/identity"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/appconfig"
	"github.com/opensoha/soha/internal/platform/keyring"
	policyrepo "github.com/opensoha/soha/internal/repository/policy"
	userrepo "github.com/opensoha/soha/internal/repository/user"
	"golang.org/x/crypto/bcrypt"
)

func TestSessionAuthorizationWithPostgres(t *testing.T) {
	repo, store, ctx := newCopilotPostgresTestRepository(t)
	users := userrepo.New(store.DB())
	policies := policyrepo.New(store.DB())
	owner, other := uuid.NewString(), uuid.NewString()
	password := "test-only-password"
	createAuthzTestUsers(t, ctx, users, policies, owner, other, password)
	resolver := appaccess.NewPermissionResolver(policies)
	cfg := appconfig.Auth{JWT: appconfig.JWT{Secret: uuid.NewString(), Issuer: "soha-authz-test", AccessTTL: time.Minute, RefreshTTL: time.Hour}}
	identity, err := appidentity.New(appidentity.Dependencies{
		Config: cfg, Accounts: users, Passwords: users, Authorization: users,
		RoleBindings: users, TeamBindings: users, Identities: users,
		Sessions: users, SessionAdmin: users, EphemeralTokens: users, Permissions: resolver,
	})
	if err != nil {
		t.Fatal(err)
	}
	login := func(id string) domainidentity.AuthResult {
		t.Helper()
		result, err := identity.LoginWithPassword(ctx, id, password)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first, second, unrelated := login(owner), login(owner), login(other)
	session := func(id string) domain.Session {
		t.Helper()
		now := time.Now().UTC()
		item, err := repo.CreateSession(ctx, domain.Session{ID: uuid.NewString(), Title: "Authz isolation", CreatedBy: id, Metadata: map[string]any{}, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	ownSession, otherSession := session(owner), session(other)
	service, err := appcopilot.New(appcopilot.Dependencies{
		Sessions: repo, Messages: repo, DataSources: repo, AnalysisProfiles: repo,
		AutomationPolicies: repo, RootCauseRuns: repo, AgentRuns: repo,
		InspectionTasks: repo, InspectionRuns: repo, Permissions: resolver,
	})
	if err != nil {
		t.Fatal(err)
	}
	read := newAuthzHTTPReader(t, ctx, cfg, identity, service)
	ownPath := "/api/v1/copilot/sessions/" + ownSession.ID
	otherPath := "/api/v1/copilot/sessions/" + otherSession.ID
	legacy := legacySessionAccess(t, first.Tokens.AccessToken, cfg.JWT.Secret)
	t.Run("current_permission_and_owner", func(t *testing.T) {
		read(first.Tokens.AccessToken, ownPath, http.StatusOK)
		read(unrelated.Tokens.AccessToken, otherPath, http.StatusOK)
		read(unrelated.Tokens.AccessToken, ownPath, http.StatusNotFound)
		read(legacy, ownPath, http.StatusOK)
	})
	if err := users.ReplaceRoleBindings(ctx, owner, nil); err != nil {
		t.Fatal(err)
	}
	t.Run("revoked_before_refresh", func(t *testing.T) {
		read(first.Tokens.AccessToken, ownPath, http.StatusUnauthorized)
		read(second.Tokens.AccessToken, ownPath, http.StatusUnauthorized)
		read(legacy, ownPath, http.StatusUnauthorized)
	})
	refreshed, err := identity.RefreshSession(ctx, first.Tokens.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("old_version_stays_revoked", func(t *testing.T) {
		read(first.Tokens.AccessToken, ownPath, http.StatusUnauthorized)
		read(second.Tokens.AccessToken, ownPath, http.StatusUnauthorized)
	})
	t.Run("current_and_legacy_deny_resource_reads", func(t *testing.T) {
		for _, token := range []string{refreshed.Tokens.AccessToken, legacy} {
			read(token, ownPath, http.StatusForbidden)
			read(token, ownPath+"/messages", http.StatusForbidden)
			read(token, "/api/v1/copilot/assistant/tasks", http.StatusForbidden)
		}
	})
	t.Run("unrelated_user_and_history_preserved", func(t *testing.T) {
		read(unrelated.Tokens.AccessToken, otherPath, http.StatusOK)
		read(unrelated.Tokens.AccessToken, ownPath, http.StatusNotFound)
		if _, err := repo.GetSession(ctx, owner, ownSession.ID); err != nil {
			t.Fatalf("revocation deleted history: %v", err)
		}
	})
	t.Run("revoked_session_cannot_refresh", func(t *testing.T) {
		if err := identity.Logout(ctx, refreshed.Tokens.AccessToken, refreshed.Tokens.RefreshToken); err != nil {
			t.Fatal(err)
		}
		read(refreshed.Tokens.AccessToken, ownPath, http.StatusUnauthorized)
		if _, err := identity.RefreshSession(context.Background(), refreshed.Tokens.RefreshToken); err == nil {
			t.Fatal("revoked session refreshed")
		}
	})
}

func createAuthzTestUsers(t *testing.T, ctx context.Context, users *userrepo.Repository, policies *policyrepo.Repository, owner, other, password string) {
	t.Helper()
	role := "authz-chat-" + uuid.NewString()
	if _, err := policies.CreateRole(ctx, domainaccess.RoleInput{ID: role, Name: role, Scope: "global", PermissionKeys: []string{"observe.ai.chat"}}); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{owner, other} {
		if err := users.UpsertUser(ctx, domainidentity.User{ID: id, Username: id, Email: id + "@example.test", Status: "active"}); err != nil {
			t.Fatal(err)
		}
		if err := users.SetPasswordHash(ctx, id, string(hash)); err != nil {
			t.Fatal(err)
		}
		if err := users.ReplaceRoleBindings(ctx, id, []string{role}); err != nil {
			t.Fatal(err)
		}
	}
}

func newAuthzHTTPReader(t *testing.T, ctx context.Context, cfg appconfig.Auth, identity *appidentity.Service, service *appcopilot.Service) func(string, string, int) {
	t.Helper()
	handler := handlers.NewCopilotHandlerWithServices(handlers.CopilotServices{Sessions: service, Messages: service, PersonalAssistant: service}, keyring.Ring{})
	router := gin.New()
	router.Use(middleware.BuildPrincipalMiddleware(cfg, identity), middleware.RequireAuth())
	router.GET("/api/v1/copilot/sessions/:sessionID", handler.GetSession)
	router.GET("/api/v1/copilot/sessions/:sessionID/messages", handler.ListMessages)
	router.GET("/api/v1/copilot/assistant/tasks", handler.ListPersonalAssistantTasks)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 5 * time.Second
	return func(token, path string, want int) {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
		}()
		if _, err := io.Copy(io.Discard, response.Body); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("GET %s: got %d, want %d", path, response.StatusCode, want)
		}
	}
}

func legacySessionAccess(t *testing.T, signed, secret string) string {
	t.Helper()
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(signed, claims); err != nil {
		t.Fatal(err)
	}
	delete(claims, "authzVersion")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	legacy, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return legacy
}
