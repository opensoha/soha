package agent

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestRBACResourceGraphRequiresAuthenticatedAgentCapability(t *testing.T) {
	for _, tc := range []struct {
		name, kind, namespace  string
		probeStatus, getStatus int
		want                   error
	}{
		{"service account", "ServiceAccount", "team-a", 204, 200, nil},
		{"role", "Role", "team-a", 204, 200, nil},
		{"role binding", "RoleBinding", "team-a", 204, 200, nil},
		{"cluster role", "ClusterRole", "", 204, 200, nil},
		{"cluster role binding", "ClusterRoleBinding", "", 204, 200, nil},
		{"old Agent", "Role", "team-a", 404, 0, apperrors.ErrUnsupportedOperation},
		{"unknown capability", "Role", "team-a", 400, 0, apperrors.ErrUnsupportedOperation},
		{"unsupported", "Role", "team-a", 501, 0, apperrors.ErrUnsupportedOperation},
		{"HEAD missing", "Role", "team-a", 405, 0, apperrors.ErrUnsupportedOperation},
		{"generic HEAD", "Role", "team-a", 200, 0, apperrors.ErrUnsupportedOperation},
		{"redirect HEAD", "Role", "team-a", 302, 0, apperrors.ErrUnsupportedOperation},
		{"denied HEAD", "Role", "team-a", 403, 0, apperrors.ErrAccessDenied},
		{"unauthenticated HEAD", "Role", "team-a", 401, 0, apperrors.ErrUnauthorized},
		{"redirect graph", "Role", "team-a", 204, 307, apperrors.ErrUnsupportedOperation},
		{"missing root", "Role", "team-a", 204, 404, apperrors.ErrNotFound},
		{"denied graph", "Role", "team-a", 204, 403, apperrors.ErrAccessDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := rand.Text()
			var mu sync.Mutex
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				calls = append(calls, r.Method)
				if got := r.Header.Get("Authorization"); got != "Bearer "+token {
					t.Errorf("missing Agent authorization")
				}
				switch {
				case r.Method == http.MethodHead && r.URL.RequestURI() == "/api/v1/platform/resources/graph?capability=resource.graph.rbac.v1":
					w.Header().Set("Location", "/unexpected-redirect")
					w.WriteHeader(tc.probeStatus)
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platform/resources/graph" && tc.getStatus != 0:
					query := r.URL.Query()
					if len(query) != 3 || query.Get("namespace") != tc.namespace || query.Get("kind") != tc.kind || query.Get("name") != "system:sample" {
						t.Errorf("incorrect graph query: %v", query)
					}
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Location", "/unexpected-redirect")
					w.WriteHeader(tc.getStatus)
					_, _ = w.Write([]byte(`{"data":{"rootId":"root"}}`))
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := &Client{baseURL: server.URL, token: token, httpClient: server.Client()}
			graph, err := client.GetResourceGraph(context.Background(), tc.namespace, tc.kind, "system:sample")
			if !errors.Is(err, tc.want) || (tc.want == nil && graph.RootID != "root") {
				t.Fatalf("graph=%+v error=%v, want %v", graph, err, tc.want)
			}
			wantCalls := []string{http.MethodHead}
			if tc.getStatus != 0 {
				wantCalls = append(wantCalls, http.MethodGet)
			}
			mu.Lock()
			defer mu.Unlock()
			if !slices.Equal(calls, wantCalls) {
				t.Fatalf("calls=%v, want %v", calls, wantCalls)
			}
		})
	}
}
