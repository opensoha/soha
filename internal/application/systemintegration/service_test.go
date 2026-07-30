package systemintegration

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type integrationRoleReader map[string][]string

func (r integrationRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r, nil
}

type memoryIntegrationRepository struct {
	items       map[string]domain.Integration
	credentials map[string]map[string]string
	creates     int
}

type captureSourceAdapter struct {
	branchSearch string
	branchLimit  int
	tagSearch    string
	tagLimit     int
	commitSearch string
	commitPage   int
	commitLimit  int
}

func (*captureSourceAdapter) TestConnection(context.Context) error { return nil }
func (*captureSourceAdapter) ListRepositories(context.Context, string, string, int) ([]sohaapi.SourceRepository, string, error) {
	return nil, "", nil
}
func (a *captureSourceAdapter) ListRepositoryBranches(_ context.Context, _ string, search string, limit int) ([]sohaapi.SourceBranch, error) {
	a.branchSearch, a.branchLimit = search, limit
	return []sohaapi.SourceBranch{{Name: "main"}}, nil
}
func (a *captureSourceAdapter) ListRepositoryTags(_ context.Context, _ string, search string, limit int) ([]sohaapi.SourceTag, error) {
	a.tagSearch, a.tagLimit = search, limit
	return []sohaapi.SourceTag{{Name: "v1"}}, nil
}
func (*captureSourceAdapter) GetRepositoryFile(context.Context, string, string, string) (sohaapi.SourceFile, error) {
	return sohaapi.SourceFile{}, nil
}
func (a *captureSourceAdapter) ListCommits(_ context.Context, _ string, search string, page, limit int) (domainapp.GitCommitPage, error) {
	a.commitSearch, a.commitPage, a.commitLimit = search, page, limit
	return domainapp.GitCommitPage{Page: page, Limit: limit}, nil
}

type captureSourceFactory struct{ adapter SourceAdapter }

func (f captureSourceFactory) Build(domain.Integration, map[string]string) (SourceAdapter, error) {
	return f.adapter, nil
}

type captureOAuthProvider struct {
	exchanged bool
	refreshed bool
}

func (*captureOAuthProvider) AuthorizationURL(_ OAuthProviderConfig, state string) (string, error) {
	return "https://gitlab.example/oauth/authorize?state=" + url.QueryEscape(state), nil
}
func (p *captureOAuthProvider) Exchange(context.Context, OAuthProviderConfig, string) (OAuthToken, error) {
	p.exchanged = true
	return OAuthToken{AccessToken: "oauth-access", RefreshToken: "oauth-refresh", ExpiresIn: 3600}, nil
}
func (p *captureOAuthProvider) Refresh(context.Context, OAuthProviderConfig, string) (OAuthToken, error) {
	p.refreshed = true
	return OAuthToken{AccessToken: "oauth-access-refreshed", RefreshToken: "oauth-refresh-next", ExpiresIn: 3600}, nil
}

func newMemoryIntegrationRepository() *memoryIntegrationRepository {
	return &memoryIntegrationRepository{items: map[string]domain.Integration{}, credentials: map[string]map[string]string{}}
}

func (r *memoryIntegrationRepository) List(_ context.Context, filter domain.Filter) ([]domain.Integration, error) {
	items := []domain.Integration{}
	for _, item := range r.items {
		if filter.Category != "" && item.Category != filter.Category || filter.ProviderType != "" && item.ProviderType != filter.ProviderType || filter.Enabled != nil && item.Enabled != *filter.Enabled {
			continue
		}
		item.CredentialKeys = sortedMapKeys(r.credentials[item.ID])
		items = append(items, item)
	}
	return items, nil
}
func (r *memoryIntegrationRepository) Get(_ context.Context, id string) (domain.Integration, error) {
	item := r.items[id]
	item.CredentialKeys = sortedMapKeys(r.credentials[id])
	return item, nil
}
func (r *memoryIntegrationRepository) Create(_ context.Context, item domain.Integration, credentials map[string]string) (domain.Integration, error) {
	r.creates++
	r.items[item.ID] = item
	r.credentials[item.ID] = cloneStrings(credentials)
	item.CredentialKeys = sortedMapKeys(credentials)
	return item, nil
}
func (r *memoryIntegrationRepository) Update(_ context.Context, item domain.Integration, _ int64, credentials map[string]string, clear []string) (domain.Integration, error) {
	item.Version++
	r.items[item.ID] = item
	for key, value := range credentials {
		r.credentials[item.ID][key] = value
	}
	for _, key := range clear {
		delete(r.credentials[item.ID], key)
	}
	item.CredentialKeys = sortedMapKeys(r.credentials[item.ID])
	return item, nil
}
func (r *memoryIntegrationRepository) Delete(_ context.Context, id string) error {
	delete(r.items, id)
	delete(r.credentials, id)
	return nil
}
func (r *memoryIntegrationRepository) Credentials(_ context.Context, id string) (map[string]string, error) {
	return cloneStrings(r.credentials[id]), nil
}
func (*memoryIntegrationRepository) UpdateHealth(context.Context, string, string, string, time.Time) error {
	return nil
}

func TestCreateEncryptsCredentialsAndNeverReturnsSecret(t *testing.T) {
	repo := newMemoryIntegrationRepository()
	service := testIntegrationService(t, repo)
	item, err := service.Create(t.Context(), adminPrincipal(), sohaapi.SystemIntegrationCreateRequest{
		Category: sohaapi.SystemIntegrationCategorySourceControl, ProviderType: "gitlab", Name: "Main GitLab", Enabled: true,
		Configuration: []sohaapi.SystemIntegrationConfigurationField{{Key: "base_url", Value: "https://gitlab.example/api/v4"}},
		Credentials:   []sohaapi.SystemIntegrationCredentialInput{{Key: "token", Value: "raw-secret-token"}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	stored := repo.credentials[item.ID]["token"]
	if !secretcrypto.Encrypted(stored) || strings.Contains(stored, "raw-secret-token") {
		t.Fatalf("stored credential is not encrypted: %q", stored)
	}
	raw, _ := json.Marshal(item)
	if strings.Contains(string(raw), "raw-secret-token") || strings.Contains(string(raw), stored) {
		t.Fatalf("response leaked credential: %s", raw)
	}
	if len(item.CredentialKeys) != 1 || item.CredentialKeys[0] != "token" {
		t.Fatalf("credential keys = %#v", item.CredentialKeys)
	}
	resolved, err := service.ResolveSourceCredentials(t.Context(), item.ID)
	if err != nil {
		t.Fatalf("ResolveSourceCredentials() error = %v", err)
	}
	if resolved["token"] != "raw-secret-token" {
		t.Fatalf("resolved credential = %q, want decrypted token", resolved["token"])
	}
}

func TestGitLabOAuthAuthorizationPersistsEncryptedTokensAndRefreshes(t *testing.T) {
	repo := newMemoryIntegrationRepository()
	service := testIntegrationService(t, repo)
	provider := &captureOAuthProvider{}
	service.RegisterOAuthProvider(domain.ProviderGitLab, provider)
	now := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	item, err := service.Create(t.Context(), adminPrincipal(), sohaapi.SystemIntegrationCreateRequest{
		Category:     sohaapi.SystemIntegrationCategorySourceControl,
		ProviderType: domain.ProviderGitLab,
		Name:         "GitLab OAuth",
		Enabled:      true,
		Configuration: []sohaapi.SystemIntegrationConfigurationField{
			{Key: "base_url", Value: "https://gitlab.example/api/v4"},
			{Key: "auth_mode", Value: gitLabAuthModeOAuth},
			{Key: "client_id", Value: "application-id"},
			{Key: "oauth_redirect_uri", Value: "https://soha.example/api/v1/system-integrations/oauth/gitlab/callback"},
			{Key: "oauth_return_uri", Value: "https://app.example"},
		},
		Credentials: []sohaapi.SystemIntegrationCredentialInput{{Key: "client_secret", Value: "application-secret"}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if connections, err := service.ListSourceConnections(t.Context(), adminPrincipal()); err != nil || len(connections) != 0 {
		t.Fatalf("connection should remain unavailable before OAuth, items=%#v err=%v", connections, err)
	}

	authorization, err := service.BeginOAuth(t.Context(), adminPrincipal(), item.ID)
	if err != nil {
		t.Fatalf("BeginOAuth() error = %v", err)
	}
	authorizationURL, _ := url.Parse(authorization.AuthorizationURL)
	state := authorizationURL.Query().Get("state")
	if state == "" || strings.Contains(authorization.AuthorizationURL, "application-secret") {
		t.Fatalf("authorization URL is invalid or leaked a secret: %q", authorization.AuthorizationURL)
	}
	tamperedSuffix := "A"
	if strings.HasSuffix(state, tamperedSuffix) {
		tamperedSuffix = "B"
	}
	if _, err := service.CompleteOAuth(t.Context(), OAuthCallbackInput{Code: "code", State: state[:len(state)-1] + tamperedSuffix}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("tampered OAuth state error = %v", err)
	}
	redirectURL, err := service.CompleteOAuth(t.Context(), OAuthCallbackInput{Code: "code", State: state})
	if err != nil {
		t.Fatalf("CompleteOAuth() error = %v", err)
	}
	if !provider.exchanged || redirectURL != "https://app.example/settings/source-control/"+item.ID+"?oauth=success" {
		t.Fatalf("oauth completion = exchanged %v redirect %q", provider.exchanged, redirectURL)
	}
	for key, want := range map[string]string{"access_token": "oauth-access", "refresh_token": "oauth-refresh"} {
		stored := repo.credentials[item.ID][key]
		plain, decryptErr := secretcrypto.DecryptStringWithKeyring(testKeyring(t), stored)
		if decryptErr != nil || plain != want || !secretcrypto.Encrypted(stored) {
			t.Fatalf("credential %s = %q, %v", key, plain, decryptErr)
		}
	}
	if connections, err := service.ListSourceConnections(t.Context(), adminPrincipal()); err != nil || len(connections) != 1 {
		t.Fatalf("connection should be available after OAuth, items=%#v err=%v", connections, err)
	}

	now = now.Add(2 * time.Hour)
	updated := repo.items[item.ID]
	credentials, err := service.decryptCredentials(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, refreshed, err := service.refreshOAuthCredentials(t.Context(), updated, credentials)
	if err != nil {
		t.Fatalf("refreshOAuthCredentials() error = %v", err)
	}
	if !provider.refreshed || refreshed["access_token"] != "oauth-access-refreshed" {
		t.Fatalf("oauth refresh = called %v credentials %#v", provider.refreshed, refreshed)
	}
}

func TestLegacyReferenceMethodsForwardSearchAndBoundedLimit(t *testing.T) {
	repo := newMemoryIntegrationRepository()
	service := testIntegrationService(t, repo)
	if _, err := service.Create(t.Context(), adminPrincipal(), sohaapi.SystemIntegrationCreateRequest{
		Category: sohaapi.SystemIntegrationCategorySourceControl, ProviderType: domain.ProviderGitLab,
		Name: "GitLab", Enabled: true,
		Configuration: []sohaapi.SystemIntegrationConfigurationField{
			{Key: "base_url", Value: "https://gitlab.example/api/v4"},
			{Key: "per_page", Value: "50"},
		},
		Credentials: []sohaapi.SystemIntegrationCredentialInput{{Key: "token", Value: "token"}},
	}); err != nil {
		t.Fatal(err)
	}
	adapter := &captureSourceAdapter{}
	service.RegisterSourceAdapter(domain.ProviderGitLab, captureSourceFactory{adapter: adapter})

	if _, err := service.ListBranches(t.Context(), "9", " release ", 12); err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	if adapter.branchSearch != "release" || adapter.branchLimit != 12 {
		t.Fatalf("branch filter = search %q limit %d", adapter.branchSearch, adapter.branchLimit)
	}
	if _, err := service.ListTags(t.Context(), "9", " stable ", 1000); err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if adapter.tagSearch != "stable" || adapter.tagLimit != 50 {
		t.Fatalf("tag filter = search %q limit %d", adapter.tagSearch, adapter.tagLimit)
	}
	if _, err := service.ListCommits(t.Context(), "9", " fix ", 2, 25); err != nil {
		t.Fatalf("ListCommits() error = %v", err)
	}
	if adapter.commitSearch != "fix" || adapter.commitPage != 2 || adapter.commitLimit != 25 {
		t.Fatalf("commit filter = search %q page %d limit %d", adapter.commitSearch, adapter.commitPage, adapter.commitLimit)
	}
}

func testIntegrationService(t *testing.T, repo domain.Repository) *Service {
	t.Helper()
	return New(repo, appaccess.NewPermissionResolver(integrationRoleReader{"admin": {appaccess.PermSettingsSystemIntegrationsView, appaccess.PermSettingsSystemIntegrationsManage}}), nil, nil, testKeyring(t))
}

func testKeyring(t *testing.T) keyring.Ring {
	t.Helper()
	key, err := keyring.NewKey("test-v1", "stable-test-credential-key-32-bytes", time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func adminPrincipal() domainidentity.Principal {
	return domainidentity.Principal{UserID: "admin", Roles: []string{"admin"}}
}

func cloneStrings(values map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range values {
		result[key] = value
	}
	return result
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
