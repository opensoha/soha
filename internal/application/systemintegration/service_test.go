package systemintegration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
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

type captureSourceFactory struct{ adapter SourceAdapter }

func (f captureSourceFactory) Build(domain.Integration, map[string]string) (SourceAdapter, error) {
	return f.adapter, nil
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
}

func TestImportLegacyGitLabOnlyWhenNoConnectionExists(t *testing.T) {
	repo := newMemoryIntegrationRepository()
	service := testIntegrationService(t, repo)
	legacy := LegacyGitLabConfig{Enabled: true, BaseURL: "https://gitlab.example/api/v4", Token: "legacy-token", PerPage: 50, Timeout: 10 * time.Second}
	if err := service.ImportLegacyGitLab(t.Context(), legacy); err != nil {
		t.Fatalf("first import error = %v", err)
	}
	if err := service.ImportLegacyGitLab(t.Context(), legacy); err != nil {
		t.Fatalf("second import error = %v", err)
	}
	if repo.creates != 1 {
		t.Fatalf("create count = %d, want 1", repo.creates)
	}
	for id := range repo.items {
		if plain, err := secretcrypto.DecryptStringWithKeyring(testKeyring(t), repo.credentials[id]["token"]); err != nil || plain != "legacy-token" {
			t.Fatalf("legacy token round trip = %q, %v", plain, err)
		}
	}
}

func TestLegacyReferenceMethodsForwardSearchAndBoundedLimit(t *testing.T) {
	repo := newMemoryIntegrationRepository()
	service := testIntegrationService(t, repo)
	if err := service.ImportLegacyGitLab(t.Context(), LegacyGitLabConfig{
		Enabled: true, BaseURL: "https://gitlab.example/api/v4", Token: "legacy-token", PerPage: 50,
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
