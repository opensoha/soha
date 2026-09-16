package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestSourceMetadataUsesExactReferenceAndBoundsReads(t *testing.T) {
	commit := strings.Repeat("a", 40)
	server := sourceMetadataTestServer(t, commit)
	defer server.Close()
	client := NewWithOptions(Options{BaseURL: server.URL, Token: "test-token", Enabled: true})
	urls, err := client.RepositoryCloneURLs(context.Background(), "42")
	if err != nil || len(urls) != 2 {
		t.Fatalf("identity: %v %v", urls, err)
	}
	resolved, err := client.ResolveRepositoryRef(context.Background(), "42", "branch", "feature/one")
	if err != nil || resolved != commit {
		t.Fatalf("reference: %s %v", resolved, err)
	}
	data, err := client.ReadRepositoryFile(context.Background(), "42", commit, "services/api/go.mod", 128)
	if err != nil || !strings.Contains(string(data), "module example.test/api") {
		t.Fatalf("nested file: %q %v", data, err)
	}
	for _, tc := range []struct {
		name string
		want error
	}{{"large", domainapp.ErrSourceFileTooLarge}, {"denied", apperrors.ErrAccessDenied}, {"missing", apperrors.ErrNotFound}, {"redirect", apperrors.ErrClusterUnready}} {
		if _, err := client.ReadRepositoryFile(context.Background(), "42", commit, tc.name, 64); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.ReadRepositoryFile(ctx, "42", commit, "slow", 64); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
}

func sourceMetadataTestServer(t *testing.T, commit string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "test-token" {
			t.Error("missing provider authentication")
		}
		switch r.URL.EscapedPath() {
		case "/projects/42":
			_, _ = fmt.Fprint(w, `{"http_url_to_repo":"https://git.example/team/app.git","ssh_url_to_repo":"git@git.example:team/app.git"}`)
		case "/projects/42/repository/branches/feature%2Fone":
			_, _ = fmt.Fprintf(w, `{"commit":{"id":%q}}`, commit)
		case "/projects/42/repository/files/services%2Fapi%2Fgo.mod/raw":
			if r.URL.Query().Get("ref") != commit {
				t.Error("file read did not use resolved commit")
			}
			_, _ = fmt.Fprint(w, "module example.test/api\ngo 1.26.0\n")
		case "/projects/42/repository/files/large/raw":
			w.WriteHeader(http.StatusOK)
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Error(err)
			}
			_, _ = fmt.Fprint(w, strings.Repeat("x", 128))
		case "/projects/42/repository/files/denied/raw":
			w.WriteHeader(http.StatusForbidden)
		case "/projects/42/repository/files/slow/raw":
			<-r.Context().Done()
		case "/projects/42/repository/files/redirect/raw":
			http.Redirect(w, r, "/unexpected", http.StatusFound)
		case "/unexpected":
			t.Error("metadata followed redirect")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}
