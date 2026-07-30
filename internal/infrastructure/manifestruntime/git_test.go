package manifestruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

type gitRepositoryReaderStub struct {
	repository domainapp.SourceRepository
}

func (s gitRepositoryReaderStub) GetRepository(context.Context, string) (domainapp.SourceRepository, error) {
	return s.repository, nil
}

type gitCredentialResolverStub struct {
	reference string
	values    map[string]string
}

func (s *gitCredentialResolverStub) ResolveSourceCredentials(_ context.Context, reference string) (map[string]string, error) {
	s.reference = reference
	return s.values, nil
}

func TestReadManifestRepositoryFilesFiltersAndDigests(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "deploy", "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"deploy/api.yaml":          "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: api\n",
		"deploy/worker.yml":        "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: worker\n",
		"deploy/ignored/test.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: ignored\n",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, digest, err := readManifestRepositoryFiles(root, "deploy", []string{"**/*.yaml", "*.yaml", "*.yml"}, []string{"ignored/**"})
	if err != nil {
		t.Fatalf("readManifestRepositoryFiles() error = %v", err)
	}
	if len(files) != 2 || files[0].Path != "api.yaml" || files[1].Path != "worker.yml" {
		t.Fatalf("files = %#v", files)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest = %q", digest)
	}
}

func TestReadManifestRepositoryFilesRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.yaml")
	if err := os.WriteFile(target, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, _, err := readManifestRepositoryFiles(root, "", []string{"*.yaml"}, nil)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("readManifestRepositoryFiles() error = %v, want symbolic link rejection", err)
	}
}

func TestValidateSyncedManifestFilesRejectsInvalidYAML(t *testing.T) {
	_, err := validateSyncedManifestFiles([]domainmanifest.File{{Path: "broken.yaml", Content: "apiVersion: ["}}, domainmanifest.RendererRaw)
	if err == nil {
		t.Fatal("validateSyncedManifestFiles() error = nil, want invalid YAML rejection")
	}
}

func TestValidateSyncedManifestFilesRejectsInlineSecret(t *testing.T) {
	_, err := validateSyncedManifestFiles([]domainmanifest.File{{Path: "secret.yaml", Content: `apiVersion: v1
kind: Secret
metadata:
  name: inline
stringData:
  token: plaintext
`}}, domainmanifest.RendererRaw)
	if err == nil || !strings.Contains(err.Error(), "Secret") {
		t.Fatalf("validateSyncedManifestFiles() error = %v, want inline Secret rejection", err)
	}
}

func TestGitExecutionResolvesRepositoryCredentialWithoutPersistingIt(t *testing.T) {
	resolver := &gitCredentialResolverStub{values: map[string]string{"token": "very-secret-token"}}
	repository := domainapp.SourceRepository{
		ID: "repo-1", Provider: "gitlab", URL: "https://gitlab.example/team/app.git", CredentialRef: "integration-1",
	}
	runtime := NewGit(gitRepositoryReaderStub{repository: repository}, resolver)
	workspace := t.TempDir()
	payload := domainmanifest.TaskPayload{RepositoryID: repository.ID, RepositoryURL: repository.URL}
	environment, err := runtime.executionEnvironment(t.Context(), payload, workspace)
	if err != nil {
		t.Fatalf("executionEnvironment() error = %v", err)
	}
	if resolver.reference != "integration-1" {
		t.Fatalf("credential reference = %q, want integration-1", resolver.reference)
	}
	if strings.Contains(strings.Join(environment, "\n"), "integration-1") {
		t.Fatalf("execution environment leaked credential reference: %#v", environment)
	}
	askpass, err := os.ReadFile(filepath.Join(workspace, "git-askpass.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(askpass), "very-secret-token") {
		t.Fatal("askpass helper persisted the repository token")
	}
	if got := sanitizeGitOutput("failure very-secret-token", environment); strings.Contains(got, "very-secret-token") {
		t.Fatalf("sanitizeGitOutput() = %q, leaked token", got)
	}
}

func TestGitExecutionRejectsChangedRepositoryConfiguration(t *testing.T) {
	runtime := NewGit(gitRepositoryReaderStub{repository: domainapp.SourceRepository{ID: "repo-1", URL: "https://gitlab.example/new.git"}}, &gitCredentialResolverStub{})
	_, err := runtime.executionEnvironment(t.Context(), domainmanifest.TaskPayload{RepositoryID: "repo-1", RepositoryURL: "https://gitlab.example/old.git"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "configuration changed") {
		t.Fatalf("executionEnvironment() error = %v, want stale repository rejection", err)
	}
}

func TestGitCredentialEnvironmentRejectsPlainHTTP(t *testing.T) {
	_, err := prepareGitCredentialEnvironment(domainapp.SourceRepository{URL: "http://git.example/app.git"}, map[string]string{"token": "secret"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "HTTPS or SSH") {
		t.Fatalf("prepareGitCredentialEnvironment() error = %v, want transport rejection", err)
	}
}

func TestValidateGitRepositoryURLRejectsEmbeddedCredentials(t *testing.T) {
	err := validateGitRepositoryURL("https://user:token@git.example/app.git")
	if err == nil || !strings.Contains(err.Error(), "must not embed credentials") {
		t.Fatalf("validateGitRepositoryURL() error = %v, want embedded credential rejection", err)
	}
}
