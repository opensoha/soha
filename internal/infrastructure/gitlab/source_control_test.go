package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestNewPreservesLegacyConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects" {
			t.Fatalf("path = %q, want /projects", r.URL.Path)
		}
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "legacy-token" {
			t.Fatalf("PRIVATE-TOKEN = %q, want legacy-token", got)
		}
		_, _ = fmt.Fprint(w, `[{"id":7,"name":"demo","path":"demo","path_with_namespace":"team/demo","default_branch":"main","web_url":"https://gitlab.example/team/demo"}]`)
	}))
	defer server.Close()

	client := New(cfgpkg.GitLabConfig{
		Enabled: true,
		BaseURL: server.URL + "/",
		Token:   " legacy-token ",
		PerPage: 20,
		Timeout: time.Second,
	})
	items, err := client.ListProjects(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(items) != 1 || items[0].PathWithNamespace != "team/demo" {
		t.Fatalf("ListProjects() = %#v", items)
	}
}

func TestClientTestConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Fatalf("path = %q, want /user", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"id":42}`)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	if err := client.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
}

func TestListRepositoriesMapsFieldsAndPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/groups/platform%2Fteam/projects" {
			t.Fatalf("escaped path = %q", r.URL.EscapedPath())
		}
		if got := r.URL.Query().Get("search"); got != "api" {
			t.Fatalf("search = %q, want api", got)
		}
		if got := r.URL.Query().Get("page"); got != "2" {
			t.Fatalf("page = %q, want 2", got)
		}
		if got := r.URL.Query().Get("per_page"); got != "25" {
			t.Fatalf("per_page = %q, want 25", got)
		}
		if got := r.URL.Query().Get("simple"); got != "" {
			t.Fatalf("simple = %q, want empty for group projects", got)
		}
		w.Header().Set("X-Next-Page", "3")
		_, _ = fmt.Fprint(w, `[{"id":9,"name":"api","path_with_namespace":"platform/team/api","default_branch":"main","web_url":"https://gitlab.example/platform/team/api","archived":true,"namespace":{"full_path":"platform/team"}}]`)
	}))
	defer server.Close()

	client := NewWithOptions(Options{Enabled: true, BaseURL: server.URL, Token: "token", GroupID: "platform/team"})
	items, next, err := client.ListRepositories(context.Background(), " api ", "2", 25)
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	want := sohaapi.SourceRepository{
		ID: "9", Name: "api", FullName: "platform/team/api", Namespace: "platform/team",
		WebURL: "https://gitlab.example/platform/team/api", DefaultBranch: "main", Archived: true,
	}
	if len(items) != 1 || items[0] != want {
		t.Fatalf("ListRepositories() items = %#v, want %#v", items, want)
	}
	if next != "3" {
		t.Fatalf("ListRepositories() next = %q, want 3", next)
	}
}

func TestListRepositoryReferencesPreservesFiltersAndUsesOneBoundedRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.EscapedPath() != "/projects/team%2Fapi/repository/branches" {
			t.Fatalf("escaped path = %q", r.URL.EscapedPath())
		}
		if got := r.URL.Query().Get("search"); got != "release" {
			t.Fatalf("search = %q, want release", got)
		}
		if got := r.URL.Query().Get("per_page"); got != "12" {
			t.Fatalf("per_page = %q, want 12", got)
		}
		w.Header().Set("X-Next-Page", "2")
		_, _ = fmt.Fprint(w, `[{"name":"release","commit":{"id":"sha-release"}}]`)
	}))
	defer server.Close()

	items, err := newTestClient(server.URL).ListRepositoryBranches(context.Background(), "team/api", " release ", 12)
	if err != nil {
		t.Fatalf("ListRepositoryBranches() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one bounded request", requests)
	}
	if len(items) != 1 || items[0].CommitID != "sha-release" || items[0].Name != "release" {
		t.Fatalf("ListRepositoryBranches() = %#v", items)
	}
}

func TestListRepositoryTagsMapsProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `[{"name":"v1.0.0","commit":{"id":"sha-tag"}}]`)
	}))
	defer server.Close()

	items, err := newTestClient(server.URL).ListRepositoryTags(context.Background(), "9", "", 25)
	if err != nil {
		t.Fatalf("ListRepositoryTags() error = %v", err)
	}
	if len(items) != 1 || items[0] != (sohaapi.SourceTag{Name: "v1.0.0", CommitID: "sha-tag"}) {
		t.Fatalf("ListRepositoryTags() = %#v", items)
	}
}

func TestGetRepositoryFileMapsBase64Content(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/projects/team%2Fapi/repository/files/deploy%2Fvalues.yaml" {
			t.Fatalf("escaped path = %q", r.URL.EscapedPath())
		}
		if got := r.URL.Query().Get("ref"); got != "release/v1" {
			t.Fatalf("ref = %q, want release/v1", got)
		}
		_, _ = fmt.Fprint(w, `{"blob_id":"blob-1","file_path":"deploy/values.yaml","encoding":"base64","content":"aGVsbG8=","size":5}`)
	}))
	defer server.Close()

	item, err := newTestClient(server.URL).GetRepositoryFile(context.Background(), "team/api", "release/v1", "deploy/values.yaml")
	if err != nil {
		t.Fatalf("GetRepositoryFile() error = %v", err)
	}
	if item.RepositoryID != "team/api" || item.Path != "deploy/values.yaml" || item.Encoding != sohaapi.SourceFileEncodingBase64 || item.Content != "aGVsbG8=" || item.SizeBytes != 5 {
		t.Fatalf("GetRepositoryFile() = %#v", item)
	}
}

func TestProviderNeutralMethodsValidateConfigurationAndInputs(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{
			name: "disabled",
			run:  func() error { return NewWithOptions(Options{}).TestConnection(context.Background()) },
			want: apperrors.ErrAccessDenied,
		},
		{
			name: "invalid cursor",
			run: func() error {
				_, _, err := NewWithOptions(Options{Enabled: true, BaseURL: "http://gitlab.example", Token: "token"}).ListRepositories(context.Background(), "", "next", 10)
				return err
			},
			want: apperrors.ErrInvalidArgument,
		},
		{
			name: "missing file ref",
			run: func() error {
				_, err := NewWithOptions(Options{Enabled: true, BaseURL: "http://gitlab.example", Token: "token"}).GetRepositoryFile(context.Background(), "9", "", "Dockerfile")
				return err
			},
			want: apperrors.ErrInvalidArgument,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, tt.want)
			}
		})
	}
}

func newTestClient(baseURL string) *Client {
	return NewWithOptions(Options{Enabled: true, BaseURL: baseURL, Token: "token", PerPage: 2, Timeout: time.Second})
}
