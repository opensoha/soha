package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	userrepo "github.com/opensoha/soha/internal/repository/user"
	"golang.org/x/oauth2"
)

func TestJWTKeyringSignsWithKidAndVerifiesPreviousKeys(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	active, err := keyring.NewKey("active-key-id", "active-jwt-secret-value", now, nil)
	if err != nil {
		t.Fatalf("NewKey() active error = %v", err)
	}
	expiresAt := now.Add(time.Hour)
	previous, err := keyring.NewKey("previous-key-id", "previous-jwt-secret-value", now.Add(-time.Hour), &expiresAt)
	if err != nil {
		t.Fatalf("NewKey() previous error = %v", err)
	}
	ring, err := keyring.New(active, []keyring.Key{previous})
	if err != nil {
		t.Fatalf("keyring.New() error = %v", err)
	}
	service := &Service{cfg: cfgpkg.AuthConfig{JWT: cfgpkg.JWTConfig{
		Secret: "active-jwt-secret-value", Keys: ring, Issuer: "soha-test",
		AccessTTL: time.Minute, RefreshTTL: time.Hour,
	}}}

	signed, _, err := service.signAccessToken(domainidentity.Principal{UserID: "user-1"}, "session-1")
	if err != nil {
		t.Fatalf("signAccessToken() error = %v", err)
	}
	unverified, _, err := jwt.NewParser().ParseUnverified(signed, &tokenClaims{})
	if err != nil {
		t.Fatalf("ParseUnverified() error = %v", err)
	}
	if unverified.Header["kid"] != active.ID() {
		t.Fatalf("signed token kid = %#v, want %q", unverified.Header["kid"], active.ID())
	}

	claims := &tokenClaims{
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "user-1", ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		},
	}
	oldToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	oldToken.Header["kid"] = previous.ID()
	oldSigned, err := oldToken.SignedString([]byte(previous.Secret()))
	if err != nil {
		t.Fatalf("SignedString() previous error = %v", err)
	}
	if _, err := service.parseToken(oldSigned, "access"); err != nil {
		t.Fatalf("parseToken() previous error = %v", err)
	}

	legacyToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	legacySigned, err := legacyToken.SignedString([]byte(previous.Secret()))
	if err != nil {
		t.Fatalf("SignedString() legacy error = %v", err)
	}
	if _, err := service.parseToken(legacySigned, "access"); err != nil {
		t.Fatalf("parseToken() kid-less previous error = %v", err)
	}
}

func TestNewRejectsMissingDependency(t *testing.T) {
	t.Parallel()

	_, err := New(Dependencies{})
	if err == nil || !strings.Contains(err.Error(), "accounts dependency is required") {
		t.Fatalf("New() error = %v, want missing accounts error", err)
	}
}

func TestNewRejectsTypedNilDependency(t *testing.T) {
	t.Parallel()

	store := newLoginMappingUserRepo()
	deps := testDependenciesWithUserStore(store)
	var passwords *loginMappingUserRepo
	deps.Passwords = passwords
	_, err := New(deps)
	if err == nil || !strings.Contains(err.Error(), "passwords dependency is required") {
		t.Fatalf("New() error = %v, want typed nil passwords error", err)
	}
}

func TestLoginWithPasswordRejectsWhenLocalLoginIsDisabled(t *testing.T) {
	store := newLoginMappingUserRepo()
	deps := testDependenciesWithUserStore(store)
	deps.Settings = loginProviderSettingsStub{passwordDisabled: true}
	service, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.LoginWithPassword(context.Background(), "opensoha", "secret")
	if !errors.Is(err, apperrors.ErrUnauthorized) || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("LoginWithPassword() error = %v, want disabled unauthorized", err)
	}
}

func TestReconcileExternalUserSyncsLoginRolesAndOrganizations(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.roleRefs["readonly"] = "readonly"
	repo.roleRefs["ops"] = "ops"
	repo.teamRefs["dept-open-1"] = "platform-org"
	service := newTestServiceWithUserStore(repo)

	principal, err := service.reconcileExternalUser(ctx, domainsettings.LoginProviderSettings{
		ID:                "feishu-main",
		Type:              "feishu",
		DefaultRoles:      []string{"readonly"},
		SyncRolesOnLogin:  true,
		RoleField:         "role_ids",
		SyncOrgsOnLogin:   true,
		OrganizationField: "department_ids",
		RoleSyncMode:      "append",
		OrgSyncMode:       "append",
	}, genericProfile{
		ID:        "ou_1",
		Email:     "user@example.com",
		Name:      "User",
		Phone:     "13800138000",
		AvatarURL: "https://example.com/avatar.png",
		Raw: map[string]any{
			"role_ids":       []any{"ops"},
			"department_ids": []any{"dept-open-1"},
		},
	})
	if err != nil {
		t.Fatalf("reconcile external user: %v", err)
	}

	if !hasString(principal.Roles, "readonly") || !hasString(principal.Roles, "ops") {
		t.Fatalf("expected synced roles, got %#v", principal.Roles)
	}
	if !hasString(principal.Teams, "platform-org") {
		t.Fatalf("expected synced organization, got %#v", principal.Teams)
	}
	if principal.AvatarURL != "https://example.com/avatar.png" {
		t.Fatalf("expected external avatar, got %q", principal.AvatarURL)
	}
	user := repo.usersByID[principal.UserID]
	if user.Username != "ou_1" || user.DisplayName != "User" || user.Email != "user@example.com" {
		t.Fatalf("expected external identity profile, got %#v", user)
	}
	if user.Preferences["phone"] != "13800138000" || user.Preferences[avatarURLPreferenceKey] != "https://example.com/avatar.png" {
		t.Fatalf("expected external profile preferences, got %#v", user.Preferences)
	}
	if binding := repo.roleBindings[principal.UserID]["ops"]; binding.source != "feishu" || binding.providerID != "feishu-main" {
		t.Fatalf("expected provider-managed role binding, got %#v", binding)
	}
	if binding := repo.teamBindings[principal.UserID]["platform-org"]; binding.source != "feishu" || binding.providerID != "feishu-main" {
		t.Fatalf("expected provider-managed team binding, got %#v", binding)
	}
}

func TestReconcileExternalUserRefreshesExistingIdentityProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.usersByID["user-1"] = userrepo.User{
		ID:          "user-1",
		Username:    "user-ea5ff349",
		Email:       "ou_1@feishu.login.local",
		DisplayName: "旧名称",
		Status:      "active",
		Preferences: map[string]any{},
	}
	repo.identities["feishu|feishu-main|ou_1"] = userrepo.OIDCIdentity{
		ID:             "identity-1",
		UserID:         "user-1",
		ProviderType:   "feishu",
		ProviderID:     "feishu-main",
		ProviderUserID: "ou_1",
	}
	service := newTestServiceWithUserStore(repo)

	_, err := service.reconcileExternalUser(ctx, domainsettings.LoginProviderSettings{
		ID:   "feishu-main",
		Type: "feishu",
	}, genericProfile{
		ID:        "ou_1",
		Email:     "user@example.com",
		Name:      "山吹",
		AvatarURL: "https://example.com/avatar.png",
		Raw:       map[string]any{"open_id": "ou_1"},
	})
	if err != nil {
		t.Fatalf("reconcile external user: %v", err)
	}

	user := repo.usersByID["user-1"]
	if user.Username != "ou_1" || user.DisplayName != "山吹" || user.Email != "user@example.com" {
		t.Fatalf("expected refreshed external identity profile, got %#v", user)
	}
	if user.Preferences[avatarURLPreferenceKey] != "https://example.com/avatar.png" {
		t.Fatalf("expected refreshed avatar preference, got %#v", user.Preferences)
	}
}

func TestFetchOAuth2ProfileUsesConfiguredProfileFields(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"open_id":          "ou_1",
				"name":             "Ada",
				"enterprise_email": "ada@example.com",
				"contact":          map[string]any{"mobile": "13800138000"},
				"avatar":           map[string]any{"url": "https://example.com/ada.png"},
			},
		})
	}))
	defer server.Close()

	profile, err := (&Service{}).fetchOAuth2Profile(context.Background(), domainsettings.LoginProviderSettings{
		ID:            "feishu-main",
		Type:          "feishu",
		UserInfoURL:   server.URL,
		UserIDField:   "open_id",
		UserNameField: "name",
		EmailField:    "enterprise_email",
		PhoneField:    "contact.mobile",
		AvatarField:   "avatar.url",
	}, (&oauth2.Token{AccessToken: "access-token"}))
	if err != nil {
		t.Fatalf("fetchOAuth2Profile() error = %v", err)
	}
	if profile.ID != "ou_1" || profile.Email != "ada@example.com" || profile.Phone != "13800138000" || profile.AvatarURL != "https://example.com/ada.png" {
		t.Fatalf("unexpected profile: %#v", profile)
	}
}

func TestFetchOAuth2ProfileEnrichesFeishuDepartmentsWhenOrganizationSyncEnabled(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer access-token" {
				t.Fatalf("user info authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"open_id": "ou_1", "name": "Ada", "email": "ada@example.com",
			}})
		case "/open-apis/auth/v3/tenant_access_token/internal":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "tenant_access_token": "tenant-token"})
		case "/open-apis/contact/v3/users/ou_1":
			if r.Header.Get("Authorization") != "Bearer tenant-token" {
				t.Fatalf("contact authorization = %q", r.Header.Get("Authorization"))
			}
			if got := r.URL.Query().Get("user_id_type"); got != "open_id" {
				t.Fatalf("user_id_type = %q", got)
			}
			if got := r.URL.Query().Get("department_id_type"); got != "open_department_id" {
				t.Fatalf("department_id_type = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"user": map[string]any{
				"open_id": "ou_1", "department_ids": []string{"od-1", "od-2"},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	profile, err := (&Service{}).fetchOAuth2Profile(context.Background(), domainsettings.LoginProviderSettings{
		ID:                "feishu-main",
		Type:              "feishu",
		ClientID:          "app-id",
		ClientSecret:      "app-secret",
		UserInfoURL:       server.URL + "/userinfo",
		UserIDField:       "open_id",
		UserNameField:     "name",
		EmailField:        "email",
		SyncOrgsOnLogin:   true,
		OrganizationField: "department_ids",
	}, &oauth2.Token{AccessToken: "access-token"})
	if err != nil {
		t.Fatalf("fetchOAuth2Profile() error = %v", err)
	}
	if got := nestedStrings(profile.Raw, "department_ids"); !slices.Equal(got, []string{"od-1", "od-2"}) {
		t.Fatalf("department_ids = %#v", got)
	}
}

func TestListProvidersDoesNotFallbackToConfiguredOIDCWhenSettingsProvidersAreEmpty(t *testing.T) {
	service := &Service{
		cfg: cfgpkg.AuthConfig{
			OIDC: cfgpkg.OIDCConfig{
				Enabled:      true,
				ProviderName: "default",
			},
		},
		settings: loginProviderSettingsStub{providers: map[string]domainsettings.LoginProviderSettings{}},
	}

	providers := service.ListProviders(context.Background())
	if len(providers) != 1 || providers[0].Type != "password" {
		t.Fatalf("providers = %#v, want password only", providers)
	}
}

func TestUpdateCurrentProfileStoresAvatarPreferences(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.usersByID["u-1"] = userrepo.User{
		ID:          "u-1",
		Username:    "opensoha",
		Email:       "old@example.com",
		DisplayName: "Old",
		Status:      "active",
		Preferences: map[string]any{},
	}
	repo.emailToID["old@example.com"] = "u-1"
	service := newTestServiceWithUserStore(repo)

	profile, err := service.UpdateCurrentProfile(ctx, domainidentity.Principal{UserID: "u-1"}, domainidentity.ProfileUpdate{
		DisplayName: "OpenSoha",
		Email:       "opensoha@soha.local",
		Phone:       "13800000000",
		AvatarURL:   "https://example.com/avatar.png",
		AvatarFit:   "contain",
	})
	if err != nil {
		t.Fatalf("UpdateCurrentProfile returned error: %v", err)
	}
	if profile.AvatarURL != "https://example.com/avatar.png" || profile.AvatarFit != "contain" {
		t.Fatalf("profile avatar = %q/%q, want stored avatar", profile.AvatarURL, profile.AvatarFit)
	}
	stored := repo.usersByID["u-1"].Preferences
	if stored[avatarURLPreferenceKey] != "https://example.com/avatar.png" || stored[avatarFitPreferenceKey] != "contain" {
		t.Fatalf("stored avatar preferences = %#v", stored)
	}
}

func TestUpdateCurrentProfileRejectsDuplicateEmail(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.usersByID["u-1"] = userrepo.User{
		ID:          "u-1",
		Username:    "opensoha",
		Email:       "old@example.com",
		DisplayName: "OpenSoha",
		Status:      "active",
	}
	repo.usersByID["u-2"] = userrepo.User{
		ID:          "u-2",
		Username:    "taken",
		Email:       "taken@example.com",
		DisplayName: "Taken",
		Status:      "active",
	}
	repo.emailToID["old@example.com"] = "u-1"
	repo.emailToID["taken@example.com"] = "u-2"
	service := newTestServiceWithUserStore(repo)

	_, err := service.UpdateCurrentProfile(ctx, domainidentity.Principal{UserID: "u-1"}, domainidentity.ProfileUpdate{
		DisplayName: "OpenSoha",
		Email:       "taken@example.com",
	})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("UpdateCurrentProfile error = %v, want conflict", err)
	}
	if got := repo.usersByID["u-1"].Email; got != "old@example.com" {
		t.Fatalf("stored email = %q, want unchanged old@example.com", got)
	}
}

func TestNormalizeAvatarURLRejectsUnsafeScheme(t *testing.T) {
	if _, err := normalizeAvatarURL("javascript:alert(1)"); err == nil {
		t.Fatalf("normalizeAvatarURL accepted javascript scheme")
	}
}

func TestSyncLoginProviderBindingsReplaceExternalOnly(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.roleRefs["ops"] = "ops"
	repo.teamRefs["dept-open-1"] = "platform-org"
	repo.roleBindings["u1"] = map[string]loginMappingBinding{
		"local-role": {source: "local"},
		"stale-role": {source: "feishu", providerID: "feishu-main"},
	}
	repo.teamBindings["u1"] = map[string]loginMappingBinding{
		"local-team": {source: "local"},
		"stale-team": {source: "feishu", providerID: "feishu-main"},
	}
	service := newTestServiceWithUserStore(repo)

	err := service.syncLoginProviderBindings(ctx, "u1", domainsettings.LoginProviderSettings{
		ID:                "feishu-main",
		Type:              "feishu",
		SyncRolesOnLogin:  true,
		RoleField:         "role_ids",
		RoleSyncMode:      "replace_external",
		SyncOrgsOnLogin:   true,
		OrganizationField: "department_ids",
		OrgSyncMode:       "replace_external",
	}, map[string]any{
		"role_ids":       []any{"ops"},
		"department_ids": []any{"dept-open-1"},
	})
	if err != nil {
		t.Fatalf("sync login provider bindings: %v", err)
	}

	if !repo.hasRoleBinding("u1", "local-role") || !repo.hasTeamBinding("u1", "local-team") {
		t.Fatalf("expected local bindings to remain, roles=%#v teams=%#v", repo.roleBindings["u1"], repo.teamBindings["u1"])
	}
	if repo.hasRoleBinding("u1", "stale-role") || repo.hasTeamBinding("u1", "stale-team") {
		t.Fatalf("expected stale external bindings removed, roles=%#v teams=%#v", repo.roleBindings["u1"], repo.teamBindings["u1"])
	}
	if !repo.hasRoleBinding("u1", "ops") || !repo.hasTeamBinding("u1", "platform-org") {
		t.Fatalf("expected current external bindings added, roles=%#v teams=%#v", repo.roleBindings["u1"], repo.teamBindings["u1"])
	}
}

func TestAccessTokenRequiresFreshAuthzVersion(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.usersByID["u1"] = userrepo.User{
		ID:           "u1",
		Username:     "user1",
		Email:        "user1@example.com",
		DisplayName:  "User One",
		Status:       "active",
		AuthzVersion: 1,
	}
	repo.roleBindings["u1"] = map[string]loginMappingBinding{"readonly": {source: "local"}}

	service := newTestServiceWithUserStore(repo)
	service.cfg = cfgpkg.AuthConfig{JWT: cfgpkg.JWTConfig{
		Secret:     "test-secret",
		Issuer:     "soha-test",
		AccessTTL:  time.Minute,
		RefreshTTL: time.Hour,
	}}
	result, err := service.issueAuthResult(ctx, domainidentity.Principal{
		UserID:   "u1",
		UserName: "User One",
		Email:    "user1@example.com",
		Roles:    []string{"readonly"},
	}, "password")
	if err != nil {
		t.Fatalf("issue auth result: %v", err)
	}

	user := repo.usersByID["u1"]
	user.AuthzVersion = 2
	repo.usersByID["u1"] = user

	if _, _, err := service.ParseAccessToken(ctx, result.Tokens.AccessToken); err == nil {
		t.Fatalf("ParseAccessToken error = nil, want stale authz rejection")
	}

	refreshed, err := service.RefreshSession(ctx, result.Tokens.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshSession after authz bump returned error: %v", err)
	}
	principal, _, err := service.ParseAccessToken(ctx, refreshed.Tokens.AccessToken)
	if err != nil {
		t.Fatalf("ParseAccessToken for refreshed token returned error: %v", err)
	}
	if principal.UserID != "u1" || !hasString(principal.Roles, "readonly") {
		t.Fatalf("unexpected refreshed principal: %#v", principal)
	}
}

func TestBeginProviderLoginStoresSafeReturnToInState(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	provider := domainsettings.LoginProviderSettings{
		ID:           "oauth-main",
		Name:         "OAuth Main",
		Type:         "oauth2",
		Enabled:      true,
		ClientID:     "client-1",
		ClientSecret: "secret-1",
		AuthorizeURL: "https://idp.example/authorize",
		TokenURL:     "https://idp.example/token",
		RedirectURL:  "https://soha.example/api/v1/auth/login/oauth-main/callback",
		Scopes:       []string{"profile"},
	}
	service := newTestServiceWithUserStore(repo)
	service.settings = loginProviderSettingsStub{providers: map[string]domainsettings.LoginProviderSettings{provider.ID: provider}}
	returnTo := "/oauth2/authorize?client_id=portal#resume"

	loginURL, err := service.BeginProviderLogin(ctx, provider.ID, returnTo)
	if err != nil {
		t.Fatalf("BeginProviderLogin returned error: %v", err)
	}
	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatalf("parse login url: %v", err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatalf("login URL missing state: %s", loginURL)
	}
	stateToken, ok := repo.ephemeral[oauthStateKind+"|"+state]
	if !ok {
		t.Fatalf("state token %q was not stored", state)
	}
	if got := stateToken.Payload["returnTo"]; got != returnTo {
		t.Fatalf("returnTo payload = %#v, want %q", got, returnTo)
	}
}

func TestBeginProviderLinkBindsStateToCurrentUser(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	provider := domainsettings.LoginProviderSettings{
		ID: "oauth-main", Name: "OAuth Main", Type: "oauth2", Enabled: true,
		ClientID: "client-1", AuthorizeURL: "https://idp.example/authorize",
		RedirectURL: "https://soha.example/api/v1/auth/login/oauth-main/callback",
	}
	service := newTestServiceWithUserStore(repo)
	service.settings = loginProviderSettingsStub{providers: map[string]domainsettings.LoginProviderSettings{provider.ID: provider}}

	loginURL, err := service.BeginProviderLink(ctx, domainidentity.Principal{UserID: "user-1"}, provider.ID, "/account/profile")
	if err != nil {
		t.Fatalf("BeginProviderLink returned error: %v", err)
	}
	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatalf("parse login url: %v", err)
	}
	stateToken := repo.ephemeral[oauthStateKind+"|"+parsed.Query().Get("state")]
	if got := stateToken.Payload["linkUserId"]; got != "user-1" {
		t.Fatalf("linkUserId payload = %#v, want user-1", got)
	}
}

func TestLinkExternalIdentityRejectsIdentityOwnedByAnotherUser(t *testing.T) {
	repo := newLoginMappingUserRepo()
	repo.identities["oauth2|oauth-main|external-1"] = userrepo.OIDCIdentity{
		UserID: "other-user", ProviderType: "oauth2", ProviderID: "oauth-main", ProviderUserID: "external-1",
	}
	service := newTestServiceWithUserStore(repo)
	err := service.linkExternalIdentity(context.Background(), "current-user", domainsettings.LoginProviderSettings{
		ID: "oauth-main", Type: "oauth2",
	}, genericProfile{ID: "external-1"})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("linkExternalIdentity error = %v, want conflict", err)
	}
	if got := repo.identities["oauth2|oauth-main|external-1"].UserID; got != "other-user" {
		t.Fatalf("identity owner = %q, want other-user", got)
	}
}

func TestBeginProviderLoginRejectsUnsafeReturnTo(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	provider := domainsettings.LoginProviderSettings{
		ID:           "oauth-main",
		Type:         "oauth2",
		Enabled:      true,
		ClientID:     "client-1",
		AuthorizeURL: "https://idp.example/authorize",
		RedirectURL:  "https://soha.example/api/v1/auth/login/oauth-main/callback",
	}
	service := newTestServiceWithUserStore(repo)
	service.settings = loginProviderSettingsStub{providers: map[string]domainsettings.LoginProviderSettings{provider.ID: provider}}

	for _, value := range []string{
		"//evil.example/path",
		"https://evil.example/path",
		"/portal\nnext",
		"/portal?next=%0A",
		"/\\evil",
		"/%5Cevil",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := service.BeginProviderLogin(ctx, provider.ID, value); err == nil {
				t.Fatalf("BeginProviderLogin return_to %q error = nil, want rejection", value)
			}
		})
	}
}

func TestAddReturnToQueryPreservesCallbackCode(t *testing.T) {
	returnTo := "/api/v1/provider/proxy/callback?state=resume"

	redirectURL, err := addReturnToQuery("http://ui.example/login/callback?code=exchange-1", returnTo)
	if err != nil {
		t.Fatalf("addReturnToQuery returned error: %v", err)
	}
	parsed, err := url.Parse(redirectURL)
	if err != nil {
		t.Fatalf("parse redirect url: %v", err)
	}
	if got := parsed.Query().Get("code"); got != "exchange-1" {
		t.Fatalf("code = %q, want exchange-1", got)
	}
	if got := parsed.Query().Get("return_to"); got != returnTo {
		t.Fatalf("return_to = %q, want %q", got, returnTo)
	}
}

func TestStreamTicketIsSingleUseAndPathBound(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.usersByID["u1"] = userrepo.User{
		ID:           "u1",
		Username:     "user1",
		Email:        "user1@example.com",
		DisplayName:  "User One",
		Status:       "active",
		AuthzVersion: 1,
	}
	repo.roleBindings["u1"] = map[string]loginMappingBinding{"ops": {source: "local"}}
	repo.sessionsByID["s1"] = userrepo.Session{
		ID:           "s1",
		UserID:       "u1",
		Status:       "active",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		AuthzVersion: 1,
	}
	service := newTestServiceWithUserStore(repo)

	path := "/api/v1/clusters/cluster-a/workloads/pods/pod-a/logs/stream"
	ticket, err := service.IssueStreamTicket(ctx, domainidentity.Principal{UserID: "u1"}, domainidentity.AccessContext{TokenKind: "session_access", SessionID: "s1"}, domainidentity.StreamTicketRequest{Path: path})
	if err != nil {
		t.Fatalf("IssueStreamTicket returned error: %v", err)
	}
	if ticket.Ticket == "" || ticket.ExpiresAt.Before(time.Now().UTC()) {
		t.Fatalf("unexpected stream ticket: %#v", ticket)
	}

	principal, accessCtx, err := service.ParseStreamTicket(ctx, ticket.Ticket, path)
	if err != nil {
		t.Fatalf("ParseStreamTicket returned error: %v", err)
	}
	if principal.UserID != "u1" || !hasString(principal.Roles, "ops") {
		t.Fatalf("unexpected stream principal: %#v", principal)
	}
	if accessCtx.TokenKind != "stream_ticket" || accessCtx.SessionID != "s1" {
		t.Fatalf("unexpected stream access context: %#v", accessCtx)
	}
	if _, _, err := service.ParseStreamTicket(ctx, ticket.Ticket, path); err == nil {
		t.Fatal("second ParseStreamTicket error = nil, want single-use rejection")
	}
}

func TestStreamTicketAllowsDockerRuntimeStreams(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.usersByID["u1"] = userrepo.User{
		ID:           "u1",
		Username:     "user1",
		Email:        "user1@example.com",
		DisplayName:  "User One",
		Status:       "active",
		AuthzVersion: 1,
	}
	repo.sessionsByID["s1"] = userrepo.Session{
		ID:           "s1",
		UserID:       "u1",
		Status:       "active",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		AuthzVersion: 1,
	}
	service := newTestServiceWithUserStore(repo)

	for _, path := range []string{
		"/api/v1/docker/projects/project-1/runtime/logs/stream",
		"/api/v1/docker/projects/project-1/runtime/terminal",
	} {
		ticket, err := service.IssueStreamTicket(ctx, domainidentity.Principal{UserID: "u1"}, domainidentity.AccessContext{TokenKind: "session_access", SessionID: "s1"}, domainidentity.StreamTicketRequest{Path: path})
		if err != nil {
			t.Fatalf("IssueStreamTicket(%q) returned error: %v", path, err)
		}
		if _, accessCtx, err := service.ParseStreamTicket(ctx, ticket.Ticket, path); err != nil {
			t.Fatalf("ParseStreamTicket(%q) returned error: %v", path, err)
		} else if accessCtx.TokenKind != "stream_ticket" {
			t.Fatalf("unexpected stream access context: %#v", accessCtx)
		}
	}
}

func TestStreamTicketRejectsMismatchedPath(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.usersByID["u1"] = userrepo.User{
		ID:           "u1",
		Username:     "user1",
		Email:        "user1@example.com",
		DisplayName:  "User One",
		Status:       "active",
		AuthzVersion: 1,
	}
	repo.sessionsByID["s1"] = userrepo.Session{
		ID:           "s1",
		UserID:       "u1",
		Status:       "active",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		AuthzVersion: 1,
	}
	service := newTestServiceWithUserStore(repo)

	ticket, err := service.IssueStreamTicket(ctx, domainidentity.Principal{UserID: "u1"}, domainidentity.AccessContext{TokenKind: "session_access", SessionID: "s1"}, domainidentity.StreamTicketRequest{Path: "/api/v1/virtualization/operations/task-1/stream"})
	if err != nil {
		t.Fatalf("IssueStreamTicket returned error: %v", err)
	}
	if _, _, err := service.ParseStreamTicket(ctx, ticket.Ticket, "/api/v1/virtualization/operations/task-2/stream"); err == nil {
		t.Fatal("ParseStreamTicket error = nil, want path mismatch rejection")
	}
}

func TestReconcileOIDCUserMigratesLegacyProviderIdentity(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.usersByID["u1"] = userrepo.User{
		ID:          "u1",
		Username:    "user1",
		Email:       "user@example.com",
		DisplayName: "User One",
		Status:      "active",
	}
	repo.identities["oidc|legacy-provider|sub-1"] = userrepo.OIDCIdentity{
		ID:             "identity-1",
		UserID:         "u1",
		ProviderType:   "oidc",
		ProviderID:     "legacy-provider",
		ProviderUserID: "sub-1",
		Profile:        map[string]any{"email": "user@example.com", "name": "User One"},
		LastLoginAt:    time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC),
	}
	service := newTestServiceWithUserStore(repo)

	principal, err := service.reconcileOIDCUser(ctx, oidcProfile{
		Sub:   "sub-1",
		Email: "user@example.com",
		Name:  "User One",
		Raw:   map[string]any{"sub": "sub-1"},
	}, cfgpkg.OIDCConfig{
		Enabled:      true,
		ProviderName: "legacy-provider",
	}, domainsettings.LoginProviderSettings{
		ID:   "new-provider",
		Type: "oidc",
	})
	if err != nil {
		t.Fatalf("reconcile oidc user: %v", err)
	}
	if principal.UserID != "u1" {
		t.Fatalf("expected existing user to be reused, got %#v", principal)
	}
	if _, ok := repo.identities["oidc|legacy-provider|sub-1"]; ok {
		t.Fatalf("expected old provider identity key to be removed")
	}
	migrated, ok := repo.identities["oidc|new-provider|sub-1"]
	if !ok {
		t.Fatalf("expected migrated identity under new provider id")
	}
	if migrated.ProviderID != "new-provider" || len(repo.migrations) != 1 {
		t.Fatalf("expected migrated identity to be recorded, got %#v %#v", migrated, repo.migrations)
	}
}

type loginMappingBinding struct {
	source     string
	providerID string
}

func newTestServiceWithUserStore(store *loginMappingUserRepo) *Service {
	service, err := New(testDependenciesWithUserStore(store))
	if err != nil {
		panic(err)
	}
	return service
}

func testDependenciesWithUserStore(store *loginMappingUserRepo) Dependencies {
	return Dependencies{
		Accounts: store, Passwords: store, Authorization: store,
		RoleBindings: store, TeamBindings: store, Identities: store,
		Sessions: store, SessionAdmin: store, EphemeralTokens: store,
	}
}

type loginProviderSettingsStub struct {
	providers        map[string]domainsettings.LoginProviderSettings
	passwordDisabled bool
}

func (s loginProviderSettingsStub) LocalPasswordLoginEnabled(context.Context) (bool, error) {
	return !s.passwordDisabled, nil
}

func (s loginProviderSettingsStub) ResolveLoginProviders(context.Context) ([]domainsettings.LoginProviderSettings, string, error) {
	items := make([]domainsettings.LoginProviderSettings, 0, len(s.providers))
	for _, provider := range s.providers {
		items = append(items, provider)
	}
	return items, "", nil
}

func (s loginProviderSettingsStub) ResolveLoginProvider(_ context.Context, providerID string) (domainsettings.LoginProviderSettings, error) {
	if provider, ok := s.providers[strings.TrimSpace(providerID)]; ok {
		return provider, nil
	}
	return domainsettings.LoginProviderSettings{}, userrepo.ErrNotFound
}

type loginMappingUserRepo struct {
	usersByID    map[string]userrepo.User
	emailToID    map[string]string
	identities   map[string]userrepo.OIDCIdentity
	migrations   []userrepo.OIDCIdentity
	roleRefs     map[string]string
	teamRefs     map[string]string
	roleBindings map[string]map[string]loginMappingBinding
	teamBindings map[string]map[string]loginMappingBinding
	sessionsByID map[string]userrepo.Session
	refreshToID  map[string]string
	ephemeral    map[string]userrepo.EphemeralToken
}

func newLoginMappingUserRepo() *loginMappingUserRepo {
	return &loginMappingUserRepo{
		usersByID:    map[string]userrepo.User{},
		emailToID:    map[string]string{},
		identities:   map[string]userrepo.OIDCIdentity{},
		roleRefs:     map[string]string{},
		teamRefs:     map[string]string{},
		roleBindings: map[string]map[string]loginMappingBinding{},
		teamBindings: map[string]map[string]loginMappingBinding{},
		sessionsByID: map[string]userrepo.Session{},
		refreshToID:  map[string]string{},
		ephemeral:    map[string]userrepo.EphemeralToken{},
	}
}

func (r *loginMappingUserRepo) FindByLogin(context.Context, string) (userrepo.User, error) {
	return userrepo.User{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) FindByEmail(_ context.Context, email string) (userrepo.User, error) {
	if userID := r.emailToID[strings.ToLower(email)]; userID != "" {
		return r.usersByID[userID], nil
	}
	return userrepo.User{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) GetByID(_ context.Context, userID string) (userrepo.User, error) {
	if user, ok := r.usersByID[userID]; ok {
		if user.AuthzVersion < 1 {
			user.AuthzVersion = 1
		}
		return user, nil
	}
	return userrepo.User{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) GetAuthzState(_ context.Context, userID string) (userrepo.AuthzState, error) {
	if user, ok := r.usersByID[userID]; ok {
		authzVersion := user.AuthzVersion
		if authzVersion < 1 {
			authzVersion = 1
		}
		return userrepo.AuthzState{UserID: user.ID, Status: user.Status, AuthzVersion: authzVersion}, nil
	}
	return userrepo.AuthzState{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) UpsertUser(_ context.Context, user userrepo.User) error {
	if user.AuthzVersion < 1 {
		user.AuthzVersion = 1
	}
	r.usersByID[user.ID] = user
	r.emailToID[strings.ToLower(user.Email)] = user.ID
	return nil
}

func (r *loginMappingUserRepo) SetPasswordHash(context.Context, string, string) error {
	return nil
}

func (r *loginMappingUserRepo) GetPasswordHash(context.Context, string) (string, error) {
	return "", userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) ListRoles(_ context.Context, userID string) ([]string, error) {
	return sortedBindingIDs(r.roleBindings[userID]), nil
}

func (r *loginMappingUserRepo) ReplaceRoleBindings(_ context.Context, userID string, roleIDs []string) error {
	r.roleBindings[userID] = map[string]loginMappingBinding{}
	for _, roleID := range roleIDs {
		r.roleBindings[userID][roleID] = loginMappingBinding{source: "local"}
	}
	return nil
}

func (r *loginMappingUserRepo) ResolveRoleIDs(_ context.Context, refs []string) ([]string, error) {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if roleID := r.roleRefs[strings.TrimSpace(ref)]; roleID != "" {
			out = append(out, roleID)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (r *loginMappingUserRepo) SyncExternalRoleBindings(_ context.Context, userID, source, providerID string, roleIDs []string, replace bool) error {
	if r.roleBindings[userID] == nil {
		r.roleBindings[userID] = map[string]loginMappingBinding{}
	}
	if replace {
		for roleID, binding := range r.roleBindings[userID] {
			if binding.source == source && binding.providerID == providerID {
				delete(r.roleBindings[userID], roleID)
			}
		}
	}
	for _, roleID := range roleIDs {
		if _, exists := r.roleBindings[userID][roleID]; exists {
			continue
		}
		r.roleBindings[userID][roleID] = loginMappingBinding{source: source, providerID: providerID}
	}
	return nil
}

func (r *loginMappingUserRepo) ListTeams(_ context.Context, userID string) ([]string, error) {
	return sortedBindingIDs(r.teamBindings[userID]), nil
}

func (r *loginMappingUserRepo) ResolveTeamIDsForExternalRefs(_ context.Context, _, _ string, refs []string) ([]string, error) {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if teamID := r.teamRefs[strings.TrimSpace(ref)]; teamID != "" {
			out = append(out, teamID)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (r *loginMappingUserRepo) SyncExternalTeamBindings(_ context.Context, userID, source, providerID string, teamIDs []string, replace bool) error {
	if r.teamBindings[userID] == nil {
		r.teamBindings[userID] = map[string]loginMappingBinding{}
	}
	if replace {
		for teamID, binding := range r.teamBindings[userID] {
			if binding.source == source && binding.providerID == providerID {
				delete(r.teamBindings[userID], teamID)
			}
		}
	}
	for _, teamID := range teamIDs {
		if _, exists := r.teamBindings[userID][teamID]; exists {
			continue
		}
		r.teamBindings[userID][teamID] = loginMappingBinding{source: source, providerID: providerID}
	}
	return nil
}

func (r *loginMappingUserRepo) ListProjects(context.Context, string) ([]string, error) {
	return []string{}, nil
}

func (r *loginMappingUserRepo) FindIdentity(_ context.Context, providerType, providerID, providerUserID string) (userrepo.OIDCIdentity, error) {
	if identity, ok := r.identities[providerType+"|"+providerID+"|"+providerUserID]; ok {
		return identity, nil
	}
	return userrepo.OIDCIdentity{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) MigrateOIDCIdentity(_ context.Context, identity userrepo.OIDCIdentity, providerID string) error {
	key := identity.ProviderType + "|" + identity.ProviderID + "|" + identity.ProviderUserID
	delete(r.identities, key)
	identity.ProviderID = providerID
	r.identities[identity.ProviderType+"|"+identity.ProviderID+"|"+identity.ProviderUserID] = identity
	r.migrations = append(r.migrations, identity)
	return nil
}

func (r *loginMappingUserRepo) ListIdentitiesByUserID(context.Context, string) ([]userrepo.OIDCIdentity, error) {
	return []userrepo.OIDCIdentity{}, nil
}

func (r *loginMappingUserRepo) UpsertOIDCIdentity(_ context.Context, identity userrepo.OIDCIdentity) error {
	r.identities[identity.ProviderType+"|"+identity.ProviderID+"|"+identity.ProviderUserID] = identity
	return nil
}

func (r *loginMappingUserRepo) CreateSession(_ context.Context, session userrepo.Session) error {
	if session.AuthzVersion < 1 {
		session.AuthzVersion = 1
	}
	r.sessionsByID[session.ID] = session
	r.refreshToID[session.RefreshTokenID] = session.ID
	return nil
}

func (r *loginMappingUserRepo) GetSessionByRefreshID(_ context.Context, refreshID string) (userrepo.Session, error) {
	if sessionID := r.refreshToID[refreshID]; sessionID != "" {
		return r.sessionsByID[sessionID], nil
	}
	return userrepo.Session{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) GetAuthSessionByID(_ context.Context, sessionID string) (userrepo.Session, error) {
	if session, ok := r.sessionsByID[sessionID]; ok {
		return session, nil
	}
	return userrepo.Session{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) GetSessionByID(context.Context, string) (domainidentity.SessionRecord, error) {
	return domainidentity.SessionRecord{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) ListSessionRecords(context.Context, int) ([]domainidentity.SessionRecord, error) {
	return []domainidentity.SessionRecord{}, nil
}

func (r *loginMappingUserRepo) ListSessionRecordsByUserID(context.Context, string, int) ([]domainidentity.SessionRecord, error) {
	return []domainidentity.SessionRecord{}, nil
}

func (r *loginMappingUserRepo) RevokeSessionByID(context.Context, string) error {
	return nil
}

func (r *loginMappingUserRepo) TouchSession(_ context.Context, refreshID string, lastSeenAt time.Time, authzVersion int64) error {
	sessionID := r.refreshToID[refreshID]
	if sessionID == "" {
		return userrepo.ErrNotFound
	}
	session := r.sessionsByID[sessionID]
	session.LastSeenAt = lastSeenAt
	session.AuthzVersion = authzVersion
	r.sessionsByID[sessionID] = session
	return nil
}

func (r *loginMappingUserRepo) RevokeSession(_ context.Context, refreshID string) error {
	sessionID := r.refreshToID[refreshID]
	if sessionID != "" {
		session := r.sessionsByID[sessionID]
		session.Status = "revoked"
		r.sessionsByID[sessionID] = session
	}
	return nil
}

func (r *loginMappingUserRepo) CreateEphemeralToken(_ context.Context, token userrepo.EphemeralToken) error {
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now().UTC()
	}
	r.ephemeral[token.Kind+"|"+token.Token] = token
	return nil
}

func (r *loginMappingUserRepo) ConsumeEphemeralToken(_ context.Context, token, kind string) (userrepo.EphemeralToken, error) {
	key := kind + "|" + token
	item, ok := r.ephemeral[key]
	if !ok || item.ExpiresAt.Before(time.Now().UTC()) {
		return userrepo.EphemeralToken{}, userrepo.ErrNotFound
	}
	delete(r.ephemeral, key)
	return item, nil
}

func (r *loginMappingUserRepo) GetPersonalAccessTokenByHash(context.Context, string) (domainaigateway.PersonalAccessToken, error) {
	return domainaigateway.PersonalAccessToken{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) TouchPersonalAccessToken(context.Context, string, time.Time) error {
	return nil
}

func (r *loginMappingUserRepo) GetServiceAccountTokenByHash(context.Context, string) (domainaigateway.ServiceAccountToken, error) {
	return domainaigateway.ServiceAccountToken{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) TouchServiceAccountToken(context.Context, string, time.Time) error {
	return nil
}

func (r *loginMappingUserRepo) GetServiceAccount(context.Context, string) (domainaigateway.ServiceAccount, error) {
	return domainaigateway.ServiceAccount{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) hasRoleBinding(userID, roleID string) bool {
	_, ok := r.roleBindings[userID][roleID]
	return ok
}

func (r *loginMappingUserRepo) hasTeamBinding(userID, teamID string) bool {
	_, ok := r.teamBindings[userID][teamID]
	return ok
}

func sortedBindingIDs(items map[string]loginMappingBinding) []string {
	if len(items) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(items))
	for item := range items {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func hasString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
