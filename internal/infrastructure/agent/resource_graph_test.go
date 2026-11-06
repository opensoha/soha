package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestResourceGraphAgentTransport(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		want        error
	}{
		{"supported", 200, "application/json", nil},
		{"old Agent", 404, "text/plain", apperrors.ErrUnsupportedOperation},
		{"unsupported root", 501, "application/json", apperrors.ErrUnsupportedOperation},
		{"missing object", 404, "application/json", apperrors.ErrNotFound},
		{"invalid", 400, "application/json", apperrors.ErrInvalidArgument},
		{"forbidden", 403, "application/json", apperrors.ErrAccessDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/platform/resources/graph" || r.URL.Query().Get("namespace") != "team + one" || r.URL.Query().Get("kind") != "Pod" {
					t.Errorf("wrong URL %s", r.URL)
				}
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				if tc.status == 200 {
					_, _ = w.Write([]byte(`{"data":{"clusterId":"agent-local","nodes":[],"edges":[]}}`))
				} else {
					_, _ = w.Write([]byte(`{"error":"private-provider-detail"}`))
				}
			}))
			defer server.Close()
			_, err := (&Client{baseURL: server.URL, httpClient: server.Client()}).GetResourceGraph(context.Background(), "team + one", "Pod", "api")
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), "private-provider-detail") {
				t.Fatal("provider error leaked")
			}
		})
	}
}

func TestResourceGraphAgentRejectsGETRedirect(t *testing.T) {
	for _, tc := range []struct{ kind, namespace string }{
		{"Pod", "team-a"}, {"Deployment", "team-a"}, {"Secret", "team-a"}, {"StorageClass", ""}, {"Role", "team-a"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			var probes, reads, redirects atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer graph-test-token" {
					t.Error("initial Graph request is missing its authorization")
				}
				switch {
				case r.URL.Path == "/unrelated":
					redirects.Add(1)
					w.WriteHeader(http.StatusOK)
				case r.Method == http.MethodHead && r.URL.RequestURI() == resourceGraphCapabilityPath(tc.kind):
					probes.Add(1)
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodGet && r.URL.Path == resourceGraphPath && r.URL.Query().Get("kind") == tc.kind:
					reads.Add(1)
					http.Redirect(w, r, "/unrelated", http.StatusTemporaryRedirect)
				default:
					t.Errorf("unexpected Graph request: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			client := &Client{baseURL: server.URL, token: "graph-test-token", httpClient: server.Client()}
			_, err := client.GetResourceGraph(context.Background(), tc.namespace, tc.kind, "api")
			wantProbes := int32(0)
			if resourceGraphCapabilityPath(tc.kind) != "" {
				wantProbes = 1
			}
			if !errors.Is(err, apperrors.ErrUnsupportedOperation) || probes.Load() != wantProbes || reads.Load() != 1 || redirects.Load() != 0 {
				t.Fatalf("Graph redirect was not blocked: probes=%d reads=%d redirects=%d err=%v", probes.Load(), reads.Load(), redirects.Load(), err)
			}
		})
	}
}

func TestResourceGraphRequestErrorDoesNotChangeOtherEndpoints(t *testing.T) {
	for _, path := range []string{"/api/v1/platform/resources/graphical", "/api/v1/platform/resources/graph/stream", "/api/v1/platform/resources/yaml"} {
		resp := &http.Response{StatusCode: http.StatusNotImplemented, Header: make(http.Header)}
		if resourceGraphNoRedirect(http.MethodGet, path) {
			t.Fatalf("Graph redirect policy applied to %s", path)
		}
		if err := resourceGraphRequestError(path, resp); err != nil {
			t.Fatalf("Graph mapping applied to %s: %v", path, err)
		}
	}
}
