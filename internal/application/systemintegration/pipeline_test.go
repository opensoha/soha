package systemintegration

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
)

type pipelineSourceFactory struct{}

func (pipelineSourceFactory) Build(item domain.Integration, credentials map[string]string, client *http.Client) (SourceAdapter, error) {
	return &pipelineTransportAdapter{transportSourceAdapter: &transportSourceAdapter{client: client, base: configurationMap(item.Configuration)["base_url"], token: credentials["token"]}}, nil
}

type pipelineTransportAdapter struct {
	*transportSourceAdapter
	domainbuild.PipelineProvider
}

func (a *pipelineTransportAdapter) ResolvePipelineTag(ctx context.Context, project, tag string) (string, error) {
	var result struct{ Commit struct{ ID string } }
	err := a.get(ctx, "/projects/"+project+"/repository/tags/"+tag, &result)
	return result.Commit.ID, err
}

func (a *pipelineTransportAdapter) RepositoryCloneURLs(ctx context.Context, project string) ([]string, error) {
	var result struct {
		URL string `json:"http_url_to_repo"`
	}
	err := a.get(ctx, "/projects/"+project, &result)
	return []string{result.URL}, err
}

func TestExternalPipelineRechecksConnectionTrustAndCredentials(t *testing.T) {
	var expectedToken atomic.Value
	expectedToken.Store("first-token")
	var hits atomic.Int32
	commit := strings.Repeat("a", 40)
	server := newPipelineTrustServer(&expectedToken, &hits, commit)
	defer server.Close()
	repo := newMemoryIntegrationRepository()
	service := testIntegrationService(t, repo)
	service.RegisterSourceAdapter(domain.ProviderGitLab, pipelineSourceFactory{})
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	item := domain.Integration{ID: "source", Category: domain.CategorySourceControl, ProviderType: domain.ProviderGitLab, Enabled: true, Configuration: []sohaapi.SystemIntegrationConfigurationField{{Key: "base_url", Value: server.URL + "/api/v4"}, {Key: "git_allowed_cidrs", Value: "127.0.0.1/32"}, {Key: "git_ca_certificate", Value: ca}}}
	repo.items[item.ID] = item
	setToken := func(token string) {
		t.Helper()
		var err error
		repo.credentials[item.ID], err = service.encryptCredentialInputs([]sohaapi.SystemIntegrationCredentialInput{{Key: "token", Value: token}})
		if err != nil {
			t.Fatal(err)
		}
		expectedToken.Store(token)
	}
	setToken("first-token")
	spec, err := service.PrepareExternalPipeline(t.Context(), domainapp.SourceRepository{ID: "repository", URL: server.URL + "/app.git", Provider: domain.ProviderGitLab, SourceConnectionID: item.ID, ProviderRepositoryID: "42"}, sohaapi.ExternalPipelineConfiguration{Provider: sohaapi.ExternalPipelineGitLab, PipelineTag: "soha-v1", ArtifactJob: "publish", RegistryID: "registry"}, strings.Repeat("b", 40))
	if err != nil || spec.PipelineCommit != commit || spec.ConnectionEndpoint != server.URL+"/api/v4" {
		t.Fatalf("private TLS pipeline freeze failed: %+v %v", spec, err)
	}
	setToken("rotated-token")
	provider, closeClient, err := service.ExternalPipelineProvider(t.Context(), spec)
	if err != nil {
		t.Fatal("rotated credential was not loaded", err)
	}
	_, err = provider.ResolvePipelineTag(t.Context(), "42", "soha-v1")
	closeClient()
	if err != nil {
		t.Fatal("pipeline request lost its pinned transport or rotated credential", err)
	}
	for _, mutation := range []string{"disabled", "endpoint", "cidr", "ca", "repository"} {
		t.Run(mutation, func(t *testing.T) {
			changed := item
			changed.Configuration = append([]sohaapi.SystemIntegrationConfigurationField(nil), item.Configuration...)
			frozen := spec
			switch mutation {
			case "disabled":
				changed.Enabled = false
			case "endpoint":
				changed.Configuration[0].Value += "/changed"
			case "cidr":
				changed.Configuration[1].Value = "192.0.2.0/24"
			case "ca":
				changed.Configuration[2].Value = ""
			case "repository":
				frozen.RepositoryURL = server.URL + "/other.git"
			}
			repo.items[item.ID] = changed
			before := hits.Load()
			provider, closeClient, err := service.ExternalPipelineProvider(t.Context(), frozen)
			if closeClient != nil {
				closeClient()
			}
			if err == nil || provider != nil {
				t.Fatal("changed connection or repository identity was accepted")
			}
			if mutation != "repository" && hits.Load() != before {
				t.Fatal("credential reached a revoked or untrusted endpoint")
			}
			repo.items[item.ID] = item
		})
	}
}

func newPipelineTrustServer(expectedToken *atomic.Value, hits *atomic.Int32, commit string) *httptest.Server {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		token, _ := expectedToken.Load().(string)
		if r.Header.Get("PRIVATE-TOKEN") != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v4/projects/42":
			_ = json.NewEncoder(w).Encode(map[string]any{"http_url_to_repo": server.URL + "/app.git"})
		case "/api/v4/projects/42/repository/tags/soha-v1":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "soha-v1", "protected": true, "commit": map[string]string{"id": commit}})
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}
