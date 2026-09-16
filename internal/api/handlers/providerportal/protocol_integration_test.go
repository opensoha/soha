package providerportal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appprovider "github.com/opensoha/soha/internal/application/identityprovider"
	appportal "github.com/opensoha/soha/internal/application/providerportal"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	infrasaml "github.com/opensoha/soha/internal/infrastructure/saml"
	"github.com/opensoha/soha/internal/platform/keyring"
	providerrepo "github.com/opensoha/soha/internal/repository/identityprovider"
	portalrepo "github.com/opensoha/soha/internal/repository/providerportal"
	userrepo "github.com/opensoha/soha/internal/repository/user"
)

type ssoProtocolFixture struct {
	server       *httptest.Server
	client       *http.Client
	providers    *appprovider.Service
	applications *appportal.Service
	onboarding   *appportal.OnboardingService
	repo         *portalrepo.Repository
	users        *userrepo.Repository
	principal    domainidentity.Principal
	publicURL    string
}

// Only platform login is a fixture. Protocol handlers, crypto and persistence are real.
func newSSOProtocolFixture(t *testing.T) *ssoProtocolFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := ssoTestDatabase(t)
	permissions := appaccess.NewPermissionResolver(ssoProtocolPermissions{})
	repo := portalrepo.New(db)
	users := userrepo.New(db)
	principal := domainidentity.Principal{UserID: uuid.NewString(), UserName: "Protocol User", Email: "protocol-" + uuid.NewString() + "@example.test", Roles: []string{"protocol-admin"}}
	if err := users.UpsertUser(context.Background(), domainidentity.User{ID: principal.UserID, Username: principal.UserID, DisplayName: principal.UserName, Email: principal.Email, Status: "active", AuthzVersion: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Exec("DELETE FROM users WHERE id = ?", principal.UserID).Error })
	key, err := keyring.NewKey("sso-protocol", "test-encryption-key-32-bytes-long", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	providers := appprovider.NewWithEncryptionKeysAndSAML(providerrepo.New(db), users, permissions, nil, keys, infrasaml.NewProviderRuntime())
	applications := appportal.New(repo, permissions, nil)
	f := &ssoProtocolFixture{providers: providers, applications: applications, onboarding: appportal.NewOnboarding(repo, applications, providers), repo: repo, users: users, principal: principal}
	h := New(Services{OIDC: providers, OIDCLogout: providers, SAML: providers, Proxy: providers, OutpostContractRuntime: providers})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if cookie, err := c.Request.Cookie("test-platform-session"); err == nil {
			if session, err := users.GetAuthSessionByID(c.Request.Context(), cookie.Value); err == nil && session.Status == "active" {
				c.Set("principal", principal)
				c.Set("access_context", domainidentity.AccessContext{TokenKind: "session_access", SessionID: session.ID})
			}
		}
	})
	router.GET("/.well-known/openid-configuration", h.OIDCDiscovery)
	router.GET("/oauth2/authorize", h.OIDCAuthorize)
	router.POST("/oauth2/token", h.OIDCToken)
	router.GET("/oauth2/userinfo", h.OIDCUserInfo)
	router.GET("/oauth2/jwks", h.OIDCJWKS)
	router.POST("/oauth2/introspect", h.OIDCIntrospect)
	router.POST("/oauth2/revoke", h.OIDCRevoke)
	router.GET("/oauth2/logout", h.OIDCEndSession)
	router.GET("/saml2/idp/:providerID/metadata", h.SAMLMetadata)
	router.GET("/saml2/idp/:providerID/sso", h.SAMLSSO)
	router.POST("/saml2/idp/:providerID/sso", h.SAMLSSO)
	router.POST("/api/v1/identity/outposts/runtime/claim", h.ClaimIdentityOutpostRuntime)
	router.POST("/api/v1/identity/outposts/:outpostID/heartbeat", h.HeartbeatIdentityOutpostRuntime)
	router.POST("/api/v1/identity/outposts/:outpostID/check", h.CheckIdentityOutpostAccess)
	router.POST("/api/v1/identity/outposts/:outpostID/events", h.RecordIdentityOutpostRuntimeEvents)
	router.GET("/api/v1/provider/proxy/auth", h.ProxyAuth)
	f.server = httptest.NewServer(router)
	t.Cleanup(f.server.Close)
	f.publicURL = f.server.URL
	providers.SetPublicAccessURL(func() string { return f.publicURL })
	f.client = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return f
}

func (f *ssoProtocolFixture) onboard(t *testing.T, input appportal.OnboardingInput) appportal.OnboardingResult {
	t.Helper()
	input.Application.Assignments = []domainportal.ApplicationAssignmentInput{{SubjectType: "user", SubjectID: f.principal.UserID, Effect: "allow"}}
	result, err := f.onboarding.OnboardApplication(context.Background(), f.principal, input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.repo.DeleteApplication(context.Background(), result.Application.ID) })
	input.Application.Status, input.Application.ProviderID = "enabled", result.Application.ProviderID
	if _, err := f.applications.UpdateApplication(context.Background(), f.principal, result.Application.ID, input.Application); err != nil {
		t.Fatal(err)
	}
	return result
}

type ssoProtocolPermissions struct{}

func (ssoProtocolPermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"protocol-admin": {"identity.applications.create", "identity.applications.update", "identity.providers.create", "identity.providers.update", "identity.providers.view", "identity.providers.rotate", "identity.outposts.create", "identity.outposts.view", "identity.outposts.rotate", "identity.outposts.delete"}}, nil
}

func (f *ssoProtocolFixture) sessionID(name string) string { return f.principal.UserID + "-" + name }

func (f *ssoProtocolFixture) login(t *testing.T, name string) string {
	t.Helper()
	id := f.sessionID(name)
	if _, err := f.users.GetAuthSessionByID(context.Background(), id); err == nil {
		return id
	}
	if err := f.users.CreateSession(context.Background(), domainidentity.Session{ID: id, UserID: f.principal.UserID, RefreshTokenID: uuid.NewString(), ProviderType: "local", Status: "active", ExpiresAt: time.Now().Add(time.Hour), LastSeenAt: time.Now(), AuthzVersion: 1, Metadata: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	return id
}

func ssoHTTP(t *testing.T, client *http.Client, request *http.Request, status int) ([]byte, http.Header) {
	t.Helper()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		var problem struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if response.StatusCode >= 400 {
			_ = json.Unmarshal(body, &problem)
		}
		t.Fatalf("%s %s: status %d, want %d; %s: %s", request.Method, request.URL.Path, response.StatusCode, status, problem.Error, problem.Description)
	}
	return body, response.Header
}

// Keep protocol assertions at the caller without adding a test framework.
func ssoCheck(t *testing.T, condition bool, format string, args ...any) {
	t.Helper()
	if !condition {
		t.Fatalf(format, args...)
	}
}

func ssoNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
