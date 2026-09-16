package manifestruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domainapp "github.com/opensoha/soha/internal/domain/application"
)

type registeredGitAccessStub struct{ deliveryGitAccessStub }

func (s registeredGitAccessStub) ResolveSourceCredentials(context.Context, string) (map[string]string, error) {
	return s.access.Credentials, nil
}

func TestResolveRegisteredRepositoryCommitUsesConnectionTrust(t *testing.T) {
	root, commit := newDeliveryGitFixture(t, map[string]string{"source.txt": "source"})
	_, repository, access, requests := deliveryHTTPSFixture(t, root)
	repository.SourceConnectionID, repository.CredentialRef = "registered", "registered"
	reader := gitRepositoryReaderStub{repository: repository}
	resolver := NewGit(reader, registeredGitAccessStub{deliveryGitAccessStub{access}})
	for _, ref := range []struct{ kind, value string }{{"branch", "main"}, {"commit", commit}} {
		got, err := resolver.ResolveRepositoryCommit(t.Context(), repository.ID, ref.kind, ref.value)
		if err != nil || got != commit {
			t.Fatalf("registered %s resolution = %q, %v", ref.kind, got, err)
		}
	}
	if requests.Load() == 0 {
		t.Fatal("registered source did not use authenticated TLS")
	}
	t.Setenv("GIT_SSL_NO_VERIFY", "true")
	access.CertificateAuthority = ""
	if _, err := NewGit(reader, registeredGitAccessStub{deliveryGitAccessStub{access}}).ResolveRepositoryCommit(t.Context(), repository.ID, "commit", commit); err == nil {
		t.Fatal("registered source bypassed certificate trust")
	}
	if _, err := NewGit(reader, nil).ResolveRepositoryCommit(t.Context(), repository.ID, "commit", commit); err == nil {
		t.Fatal("registered source fell back to legacy credentials")
	}
}

func TestResolveRepositoryCommitUsesExactRefsAndPeelsAnnotatedTag(t *testing.T) {
	if os.Getenv("GIT_DIR") != "" || os.Getenv("GIT_WORK_TREE") != "" {
		t.Fatal("Git integration tests require an isolated Git environment")
	}
	ctx := context.Background()
	root := t.TempDir()
	repository := filepath.Join(root, "source")
	if err := runGit(ctx, "", nil, "init", "-b", "release", repository); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "first"}} {
		if err := runGit(ctx, repository, nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	first, err := gitOutput(ctx, repository, nil, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	first = strings.TrimSpace(first)
	if err := runGit(ctx, repository, nil, "tag", "-a", "release", "-m", "pinned"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, repository, nil, "commit", "--allow-empty", "-m", "second"); err != nil {
		t.Fatal(err)
	}
	second, err := gitOutput(ctx, repository, nil, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	second = strings.TrimSpace(second)
	tree, err := gitOutput(ctx, repository, nil, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, repository, nil, "tag", "tree-tag", strings.TrimSpace(tree)); err != nil {
		t.Fatal(err)
	}
	// A process-local URL rewrite keeps the actual Git protocol test offline;
	// the production URL validator still rejects local paths and credentials.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url.file://"+root+"/.insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://git.example.invalid/")
	git := NewGit(gitRepositoryReaderStub{repository: domainapp.SourceRepository{ID: "repo", URL: "https://git.example.invalid/source"}}, nil)
	for _, tc := range []struct{ kind, ref, commit string }{{"branch", "release", second}, {"tag", "release", first}, {"commit", first, first}} {
		got, err := git.ResolveRepositoryCommit(ctx, "repo", tc.kind, tc.ref)
		if err != nil || got != tc.commit {
			t.Fatalf("resolve %s %s: %s %v", tc.kind, tc.ref, got, err)
		}
	}
	for _, tc := range []struct{ kind, ref string }{{"commit", "abc123"}, {"branch", "--upload-pack=bad"}, {"tag", "missing"}, {"tag", "tree-tag"}, {"branch", "release:other"}} {
		if _, err := git.ResolveRepositoryCommit(ctx, "repo", tc.kind, tc.ref); err == nil {
			t.Fatalf("invalid ref accepted: %+v", tc)
		}
	}
}
