package sohacli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestKnowledgeConnectorReferenceFormats(t *testing.T) {
	for _, tc := range []struct {
		name, ref string
		valid     bool
	}{
		{"canonical", "soha://secrets/source-id", true},
		{"pinned", "soha://secrets/source-id/versions/2", true},
		{"legacy", "secret:knowledge/http", true},
		{"zero-version", "soha://secrets/source-id/versions/0", false},
		{"traversal", "soha://secrets/source-id/../foreign", false},
		{"plaintext", "plaintext-token", false},
		{"oversize", "soha://secrets/" + strings.Repeat("a", 513), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assertAICommandHeaders(t, r, "knowledge-connector-operator")
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/ai/knowledge/connectors" {
					t.Fatal("unexpected connector endpoint")
				}
				var input KnowledgeConnectorInput
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Fatal(err)
				}
				if input.SecretRef != tc.ref {
					t.Fatal("credential identity or version changed")
				}
				writeJSON(t, w, map[string]any{"data": map[string]any{"id": "connector-1", "secretRef": input.SecretRef}})
			}))
			defer server.Close()
			var out, errOut bytes.Buffer
			args := []string{"knowledge", "connectors", "create", "--profile", "dev", "--base-id", "base", "--name", "source", "--kind", "http", "--config-ref", tc.ref, "--config-json", `{"url":"https://docs.example.com/","allowedHosts":["docs.example.com"],"maxBytes":1024}`}
			code := Run(t.Context(), args, Runtime{Out: &out, Err: &errOut, ConfigPath: writeAICommandProfile(t, server.URL)})
			if !tc.valid {
				if code == 0 || calls.Load() != 0 {
					t.Fatal("invalid reference reached HTTP")
				}
				return
			}
			if code != 0 || calls.Load() != 1 {
				t.Fatalf("valid reference rejected: code=%d calls=%d stderr=%s", code, calls.Load(), errOut.String())
			}
			if strings.Contains(out.String(), tc.ref) {
				t.Fatal("summary exposed credential reference")
			}
		})
	}
}
