package manifestruntime

import (
	"context"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/cgi" // #nosec G504 -- local test fixture runs the pinned git-http-backend only.
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	contractsapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
)

type deliveryGitAccessStub struct{ access domainapp.GitSourceAccess }

func (s deliveryGitAccessStub) ResolveDeliveryGitAccess(context.Context, domainapp.SourceRepository) (domainapp.GitSourceAccess, error) {
	return s.access, nil
}

func newDeliveryGitFixture(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := runGit(t.Context(), "", nil, "init", "-b", "main", root); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		file := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"config", "user.name", "Git fixture"}, {"config", "user.email", "fixture@example.invalid"}, {"config", "uploadpack.allowFilter", "true"}, {"add", "."}, {"commit", "--allow-empty", "-m", "fixture"}} {
		if err := runGit(t.Context(), root, nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	commit, err := gitOutput(t.Context(), root, nil, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return root, strings.TrimSpace(commit)
}

func deliveryHTTPSFixture(t *testing.T, root string) (*httptest.Server, domainapp.SourceRepository, domainapp.GitSourceAccess, *atomic.Int32) {
	t.Helper()
	execPath, err := gitOutput(t.Context(), root, nil, "--exec-path")
	if err != nil {
		t.Fatal(err)
	}
	backend := &cgi.Handler{Path: filepath.Join(strings.TrimSpace(execPath), "git-http-backend"), Dir: root, Env: []string{"GIT_PROJECT_ROOT=" + filepath.Dir(root), "GIT_HTTP_EXPORT_ALL=1", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_COUNT=0"}}
	requests := &atomic.Int32{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, _ := r.BasicAuth()
		if username != "oauth2" || password != "fixture-git-token" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Git fixture"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		requests.Add(1)
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	endpoint, _ := url.Parse(server.URL)
	// The URL keeps a real TLS DNS hostname while only the approved IP is
	// dialed. This cannot pass by using public DNS or disabling TLS checks.
	host := server.Certificate().DNSNames[0]
	cloneURL := "https://" + net.JoinHostPort(host, endpoint.Port()) + "/" + filepath.Base(root)
	repository := domainapp.SourceRepository{ID: "repo", URL: cloneURL, Provider: "gitlab"}
	access := domainapp.GitSourceAccess{RepositoryURL: cloneURL, Scheme: "https", Host: host, Port: endpoint.Port(), Address: endpoint.Hostname(), CertificateAuthority: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})), Credentials: map[string]string{"token": "fixture-git-token"}}
	return server, repository, access, requests
}

func TestDeliveryGitHTTPSReadsFixedRefsAndIgnoresHostConfiguration(t *testing.T) {
	root, first := newDeliveryGitFixture(t, map[string]string{"catalog/build.soha.yaml": "kind: BuildTemplate\n", "catalog/nested/flow.soha.json": "{\"kind\":\"Workflow\"}", "catalog/ordinary.yaml": "ignored", "catalog/excluded/no.soha.yml": "ignored"})
	if err := runGit(t.Context(), root, nil, "tag", "-a", "main", "-m", "original"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(t.Context(), root, nil, "commit", "--allow-empty", "-m", "second"); err != nil {
		t.Fatal(err)
	}
	second, _ := gitOutput(t.Context(), root, nil, "rev-parse", "HEAD")
	_, repository, access, requests := deliveryHTTPSFixture(t, root)
	// User Git configuration, proxies, SSL bypasses and custom SSH commands
	// must not cross into a credential-bearing source read.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url.file:///untrusted.insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("GIT_SSL_NO_VERIFY", "true")
	t.Setenv("GIT_SSH_COMMAND", "exit 1")
	reader := NewDeliveryGit(deliveryGitAccessStub{access})
	source := domaindocument.Source{RepositoryID: repository.ID, Path: "catalog", RefType: "branch", RefValue: "main", ExcludePatterns: []string{"excluded/**"}}
	for _, tc := range []struct{ kind, ref, commit string }{{"branch", "main", strings.TrimSpace(second)}, {"tag", "main", first}, {"commit", first, first}} {
		source.RefType, source.RefValue = contractsapi.DeliveryTemplateSourceRefType(tc.kind), tc.ref
		result, err := reader.ReadDeliveryDocuments(t.Context(), repository, source)
		if err != nil {
			t.Fatalf("%s read: %v", tc.kind, err)
		}
		if result.ResolvedCommit != tc.commit || !gitCommitPattern.MatchString(result.TreeDigest) || len(result.Files) != 2 || result.Files[0].Path != "build.soha.yaml" || result.Files[1].Path != "nested/flow.soha.json" {
			t.Fatalf("%s unexpected fixed read: %+v", tc.kind, result)
		}
	}
	if requests.Load() == 0 {
		t.Fatal("no authenticated HTTPS transport was exercised")
	}
	access.CertificateAuthority = ""
	if _, err := NewDeliveryGit(deliveryGitAccessStub{access}).ReadDeliveryDocuments(t.Context(), repository, source); err == nil {
		t.Fatal("inherited SSL bypass accepted untrusted certificate")
	}
}

func TestDeliveryGitHTTPSRejectsRedirectAndBadCredentials(t *testing.T) {
	root, _ := newDeliveryGitFixture(t, map[string]string{"template.soha.yaml": "kind: WorkflowTemplate"})
	_, repository, access, _ := deliveryHTTPSFixture(t, root)
	source := domaindocument.Source{RepositoryID: repository.ID, Path: ".", RefType: "branch", RefValue: "main"}
	access.Credentials = map[string]string{"token": "wrong-secret"}
	if _, err := NewDeliveryGit(deliveryGitAccessStub{access}).ReadDeliveryDocuments(t.Context(), repository, source); err == nil || strings.Contains(err.Error(), "wrong-secret") {
		t.Fatal("invalid credential accepted or disclosed")
	}
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusMovedPermanently)
	}))
	defer redirect.Close()
	endpoint, _ := url.Parse(redirect.URL)
	repository.URL = redirect.URL + "/repo.git"
	access.RepositoryURL, access.Host, access.Port, access.Address = repository.URL, endpoint.Hostname(), endpoint.Port(), endpoint.Hostname()
	access.CertificateAuthority = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: redirect.Certificate().Raw}))
	if _, err := NewDeliveryGit(deliveryGitAccessStub{access}).ReadDeliveryDocuments(t.Context(), repository, source); err == nil || reached.Load() != 0 {
		t.Fatal("Git followed a redirect")
	}
}

func TestDeliveryGitFilesFailClosedWithoutCheckout(t *testing.T) {
	for _, tc := range []struct {
		name, sourcePath string
		files            map[string]string
		wantError        bool
	}{
		{"empty-selection", ".", map[string]string{"README.md": "no templates"}, false},
		{"missing-directory", "catalog", map[string]string{"README.md": "no templates"}, true},
		{"path-escape", "../outside", nil, true},
		{"binary", ".", map[string]string{"invalid.soha.json": "\x00"}, true},
		{"large-file", ".", map[string]string{"large.soha.json": strings.Repeat("x", (1<<20)+1)}, true},
		{"total-size", ".", map[string]string{"one.soha.yaml": strings.Repeat("x", 1<<20), "two.soha.yaml": strings.Repeat("x", 1<<20), "three.soha.yaml": "x"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, commit := newDeliveryGitFixture(t, tc.files)
			reader := deliveryGitReader{directory: root, environment: os.Environ()}
			result, err := reader.files(t.Context(), commit, domaindocument.Source{Path: tc.sourcePath})
			if (err != nil) != tc.wantError || !tc.wantError && len(result) != 0 {
				t.Fatalf("files = %d, error = %v", len(result), err)
			}
		})
	}
	for _, mode := range []string{"120000", "160000"} {
		if _, _, err := selectedGitBlob(mode+" blob "+strings.Repeat("a", 40)+"\tignored.txt", []string{"*.soha.yaml"}, nil); err == nil {
			t.Fatal("link/submodule accepted in source tree")
		}
	}
	access := domainapp.GitSourceAccess{Scheme: "ssh", Credentials: map[string]string{"private_key": "fixture-key"}}
	if _, err := deliveryGitEnvironment(domainapp.SourceRepository{URL: "git@git.example:repo.git"}, access, t.TempDir()); err == nil {
		t.Fatal("SSH source accepted without known_hosts")
	}
}
