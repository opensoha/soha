package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListCommitsSendsPaginationAndSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/projects/group%2Frepo/repository/commits" {
			t.Fatalf("path = %q", r.URL.EscapedPath())
		}
		if got := r.URL.Query().Get("page"); got != "3" {
			t.Fatalf("page = %q", got)
		}
		if got := r.URL.Query().Get("per_page"); got != "26" {
			t.Fatalf("per_page = %q", got)
		}
		if got := r.URL.Query().Get("search"); got != "fix release" {
			t.Fatalf("search = %q", got)
		}
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "secret" {
			t.Fatalf("PRIVATE-TOKEN = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"abc123","title":"fix release","committed_date":"2026-07-19T02:03:04Z"}]`))
	}))
	defer server.Close()

	client := &Client{baseURL: server.URL, token: "secret", perPage: 50, http: server.Client(), enabled: true}
	items, err := client.ListCommits(context.Background(), "group/repo", " fix release ", 3, 25)
	if err != nil {
		t.Fatalf("ListCommits returned error: %v", err)
	}
	wantTime := time.Date(2026, 7, 19, 2, 3, 4, 0, time.UTC)
	if len(items.Items) != 1 || items.Items[0].ID != "abc123" || items.Items[0].Title != "fix release" || !items.Items[0].CommittedAt.Equal(wantTime) || items.Page != 3 || items.Limit != 25 || items.HasMore {
		t.Fatalf("items = %#v", items)
	}
}
