package systemintegration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type gitIdentityAdapter struct {
	SourceAdapter
	domainapp.SourceMetadataReader
	url, repositoryID string
}

func (a *gitIdentityAdapter) RepositoryCloneURLs(_ context.Context, id string) ([]string, error) {
	if id != a.repositoryID {
		return nil, apperrors.ErrNotFound
	}
	return []string{a.url}, nil
}

type gitIdentityFactory struct{ adapter *gitIdentityAdapter }

func (f gitIdentityFactory) Build(_ domain.Integration, _ map[string]string, client *http.Client) (SourceAdapter, error) {
	if client == nil || client.Transport == nil || client.CheckRedirect == nil {
		return nil, errors.New("identity request must use the constrained client")
	}
	return f.adapter, nil
}

func TestDeliveryGitPolicyRequiresConnectionEndpoint(t *testing.T) {
	item := domain.Integration{Configuration: []sohaapi.SystemIntegrationConfigurationField{{Key: "base_url", Value: "https://git.example/api/v4"}}}
	for _, value := range []string{"https://git.example/team/repo.git", "git@git.example:team/repo.git", "ssh://git@git.example/team/repo.git"} {
		if _, _, _, err := deliveryGitPolicy(item, value); err != nil {
			t.Fatalf("registered endpoint rejected: %s: %v", value, err)
		}
	}
	for _, value := range []string{"https://other.example/repo.git", "https://git.example:444/repo.git", "ssh://git@git.example:2222/repo.git", "http://git.example/repo.git", "https://user:token@git.example/repo.git", "file:///tmp/repo", "ssh://-option@git.example/repo.git", "https://git.example/repo.git?token=secret", "git@-option:repo.git"} {
		if _, _, _, err := deliveryGitPolicy(item, value); err == nil {
			t.Fatalf("unapproved endpoint accepted: %s", value)
		}
	}
	item.Configuration = append(item.Configuration, sohaapi.SystemIntegrationConfigurationField{Key: "git_allowed_endpoints", Value: "ssh://ssh.example:2222, https://clone.example"}, sohaapi.SystemIntegrationConfigurationField{Key: "git_allowed_cidrs", Value: "10.10.0.0/16\nfd12::/48"})
	access, _, prefixes, err := deliveryGitPolicy(item, "ssh://git@ssh.example:2222/team/repo.git")
	if err != nil || access.Host != "ssh.example" || access.Port != "2222" || len(prefixes) != 2 {
		t.Fatalf("explicit endpoint policy: %+v %v", access, err)
	}
	for _, config := range []map[string]string{{"git_allowed_cidrs": "invalid"}, {"git_allowed_endpoints": "https://clone.example/repo"}, {"git_allowed_endpoints": "https://clone.example?token=secret"}} {
		if _, _, err := sourceGitAllowlist(config); err == nil {
			t.Fatal("invalid allowlist accepted")
		}
	}
}

func TestDeliveryGitAccessChecksBindingAndKeepsCredentialsPrivate(t *testing.T) {
	repo := newMemoryIntegrationRepository()
	service := testIntegrationService(t, repo)
	item := domain.Integration{ID: "connection", ProviderType: "gitlab", Category: domain.CategorySourceControl, Enabled: true, Configuration: []sohaapi.SystemIntegrationConfigurationField{{Key: "base_url", Value: "https://127.0.0.1"}, {Key: "git_allowed_cidrs", Value: "127.0.0.1/32"}}}
	repo.items[item.ID] = item
	var err error
	repo.credentials[item.ID], err = service.encryptCredentialInputs([]sohaapi.SystemIntegrationCredentialInput{{Key: "token", Value: "fixture-private-token"}})
	if err != nil {
		t.Fatal(err)
	}
	repository := domainapp.SourceRepository{ID: "repo", URL: "https://127.0.0.1/team/repo.git", Provider: "gitlab", SourceConnectionID: item.ID, ProviderRepositoryID: "project-1", CredentialRef: item.ID}
	identity := &gitIdentityAdapter{url: repository.URL, repositoryID: repository.ProviderRepositoryID}
	service.RegisterSourceAdapter("gitlab", gitIdentityFactory{adapter: identity})
	access, err := service.ResolveDeliveryGitAccess(t.Context(), repository)
	if err != nil || access.Address != "127.0.0.1" || access.Credentials["token"] != "fixture-private-token" {
		t.Fatalf("approved repository read failed: %v", err)
	}
	encoded, _ := json.Marshal(access)
	if strings.Contains(string(encoded), "fixture") || string(encoded) != "{}" {
		t.Fatal("execution grant leaked through JSON")
	}
	for _, change := range []func(*domainapp.SourceRepository){
		func(r *domainapp.SourceRepository) { r.CredentialRef = "other" },
		func(r *domainapp.SourceRepository) { r.SourceConnectionID = "" },
		func(r *domainapp.SourceRepository) { r.ProviderRepositoryID = "" },
		func(r *domainapp.SourceRepository) { r.Provider = "other" },
		func(r *domainapp.SourceRepository) { r.URL = "https://other.example/repo.git" },
		func(r *domainapp.SourceRepository) { r.URL = "https://127.0.0.1/team/other.git" },
		func(r *domainapp.SourceRepository) { r.ProviderRepositoryID = "different-project" },
	} {
		changed := repository
		change(&changed)
		if _, err := service.ResolveDeliveryGitAccess(t.Context(), changed); err == nil {
			t.Fatal("invalid repository binding accepted")
		}
	}
	public := repository
	public.CredentialRef = ""
	if access, err := service.ResolveDeliveryGitAccess(t.Context(), public); err != nil || len(access.Credentials) != 0 {
		t.Fatalf("public clone received connection credentials: %v", err)
	}
	// The stored path is reused by another project after a provider-side rename.
	identity.url = "https://127.0.0.1/team/renamed.git"
	for _, repository := range []domainapp.SourceRepository{repository, public} {
		if _, err := service.ResolveDeliveryGitAccess(t.Context(), repository); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("renamed/reassigned repository path accepted: %v", err)
		}
	}
	identity.url = repository.URL
	item.Enabled = false
	repo.items[item.ID] = item
	if _, err := service.ResolveDeliveryGitAccess(t.Context(), repository); err == nil {
		t.Fatal("disabled connection accepted")
	}
}
