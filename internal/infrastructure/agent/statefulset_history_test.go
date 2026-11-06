package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestControllerHistoryTransportKeepsScopeAndRevisionIdentity(t *testing.T) {
	for _, resource := range []string{"statefulsets", "daemonsets"} {
		t.Run(resource, func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Query().Get("namespace") != "team/a" {
					t.Errorf("wrong read scope: %s %s", r.Method, r.URL.String())
				}
				paths = append(paths, r.URL.EscapedPath())
				w.Header().Set("Content-Type", "application/json")
				if len(paths) == 1 {
					_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"name": "db-1", "namespace": "team/a", "revision": "1"}}})
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"kind": "ControllerRevision", "name": "db-1", "namespace": "team/a", "content": "kind: ControllerRevision"}})
				}
			}))
			defer server.Close()
			client := &Client{baseURL: server.URL, httpClient: server.Client()}
			listRead, yamlRead := client.ListStatefulSetRolloutHistory, client.GetStatefulSetRevisionYAML
			if resource == "daemonsets" {
				listRead, yamlRead = client.ListDaemonSetRolloutHistory, client.GetDaemonSetRevisionYAML
			}
			rows, err := listRead(t.Context(), "team/a", "db")
			if err != nil || len(rows) != 1 || rows[0].Name != "db-1" {
				t.Fatalf("history=%#v err=%v", rows, err)
			}
			view, err := yamlRead(t.Context(), "team/a", "db", "db-1")
			if err != nil || view.Name != "db-1" {
				t.Fatalf("revision=%#v err=%v", view, err)
			}
			if len(paths) != 2 || paths[0] != "/api/v1/platform/workloads/"+resource+"/db/rollouts" || paths[1] != "/api/v1/platform/workloads/"+resource+"/db/rollouts/db-1/yaml" {
				t.Fatalf("wrong paths: %#v", paths)
			}

		})
	}
}

func TestControllerHistoryDistinguishesMissingRouteAndObject(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		json   bool
		want   error
	}{
		{"old-agent", http.StatusNotFound, false, apperrors.ErrUnsupportedOperation},
		{"method-unavailable", http.StatusMethodNotAllowed, false, apperrors.ErrUnsupportedOperation},
		{"object-missing", http.StatusNotFound, true, apperrors.ErrNotFound},
		{"rbac-denied", http.StatusForbidden, true, apperrors.ErrAccessDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.json {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
				}
				w.WriteHeader(test.status)
			}))
			defer server.Close()
			client := &Client{baseURL: server.URL, httpClient: server.Client()}
			if _, err := client.ListStatefulSetRolloutHistory(t.Context(), "default", "db"); !errors.Is(err, test.want) {
				t.Errorf("StatefulSet history error = %v, want %v", err, test.want)
			}
			if _, err := client.GetDaemonSetRevisionYAML(t.Context(), "default", "agent", "agent-1"); !errors.Is(err, test.want) {
				t.Errorf("DaemonSet revision error = %v, want %v", err, test.want)
			}
		})
	}
}
