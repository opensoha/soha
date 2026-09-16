package systemintegration

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
)

type transportSourceFactory struct{}

func (transportSourceFactory) Build(item domain.Integration, credentials map[string]string, client *http.Client) (SourceAdapter, error) {
	if client == nil {
		return nil, fmt.Errorf("configured source transport was lost")
	}
	return &transportSourceAdapter{client: client, base: configurationMap(item.Configuration)["base_url"], token: credentials["token"]}, nil
}

type transportSourceAdapter struct {
	SourceAdapter
	domainapp.SourceMetadataReader
	client      *http.Client
	base, token string
}

func (a *transportSourceAdapter) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", a.token)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return json.NewDecoder(resp.Body).Decode(out)
}

func (a *transportSourceAdapter) ListRepositories(ctx context.Context, _, _ string, _ int) ([]sohaapi.SourceRepository, string, error) {
	var result []sohaapi.SourceRepository
	err := a.get(ctx, "/projects", &result)
	return result, "", err
}

func (a *transportSourceAdapter) RepositoryCloneURLs(ctx context.Context, id string) ([]string, error) {
	var result []string
	err := a.get(ctx, "/projects/"+id, &result)
	return result, err
}

func TestConfiguredSourceTLSWorksBeforeRepositoryRegistration(t *testing.T) {
	var hits []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		if r.Header.Get("PRIVATE-TOKEN") != "test-token" {
			t.Error("missing source credential")
		}
		switch r.URL.Path {
		case "/api/v4/projects":
			_, _ = w.Write([]byte(`[{"id":"42","name":"Template repository"}]`))
		case "/api/v4/projects/42":
			_, _ = w.Write([]byte(`["https://git.example/templates.git"]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	repo := newMemoryIntegrationRepository()
	service := testIntegrationService(t, repo)
	service.RegisterSourceAdapter("gitlab", transportSourceFactory{})
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	item := domain.Integration{ID: "source", Category: domain.CategorySourceControl, ProviderType: "gitlab", Enabled: true, Configuration: []sohaapi.SystemIntegrationConfigurationField{{Key: "base_url", Value: server.URL + "/api/v4"}, {Key: "git_allowed_cidrs", Value: "127.0.0.1/32"}, {Key: "git_ca_certificate", Value: ca}}}
	repo.items[item.ID] = item
	var err error
	repo.credentials[item.ID], err = service.encryptCredentialInputs([]sohaapi.SystemIntegrationCredentialInput{{Key: "token", Value: "test-token"}})
	if err != nil {
		t.Fatal(err)
	}
	items, _, err := service.ListSourceRepositories(t.Context(), adminPrincipal(), item.ID, "", "", 50)
	if err != nil || len(items) != 1 || items[0].ID != "42" {
		t.Fatalf("private CA discovery failed: %v", err)
	}
	if err := service.ValidateSourceRepositoryBinding(t.Context(), domainapp.SourceRepositoryInput{SourceConnectionID: item.ID, Provider: "gitlab", ProviderRepositoryID: "42", URL: "https://git.example/templates.git"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(hits, ",") != "/api/v4/projects,/api/v4/projects/42" {
		t.Fatalf("unexpected requests: %v", hits)
	}
	item.Configuration[2].Value = ""
	repo.items[item.ID] = item
	if _, _, err := service.ListSourceRepositories(t.Context(), adminPrincipal(), item.ID, "", "", 50); err == nil {
		t.Fatal("private CA source accepted without trust")
	}
	item.Configuration = []sohaapi.SystemIntegrationConfigurationField{{Key: "base_url", Value: "http://legacy.example/api/v4"}}
	if client, err := sourceConnectionHTTPClient(t.Context(), item); client != nil || err != nil {
		t.Fatal("legacy connection transport changed")
	}
}
