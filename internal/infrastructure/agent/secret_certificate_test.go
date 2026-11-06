package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestSecretCertificateAgentTransport(t *testing.T) {
	for _, tc := range []struct {
		name              string
		status            int
		contentType, body string
		want              error
	}{
		{"metadata", 200, "application/json", `{"data":{"name":"web-tls","namespace":"team + one","status":"available","notAfter":"2026-12-02T08:00:00Z"}}`, nil},
		{"older Agent", 404, "text/plain", "private-key-sentinel", apperrors.ErrUnsupportedOperation},
		{"RBAC denial", 403, "application/json", `{"error":"private-key-sentinel"}`, apperrors.ErrAccessDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/platform/ownership-v2/configuration/secrets/web-tls/certificate" || r.URL.Query().Get("namespace") != "team + one" {
					t.Errorf("wrong request %s", r.URL)
				}
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			view, err := (&Client{baseURL: server.URL, httpClient: server.Client()}).GetSecretCertificate(context.Background(), "team + one", "web-tls")
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), "private-key-sentinel") {
				t.Fatal("private provider body leaked")
			}
			if err == nil && view.NotAfter != "2026-12-02T08:00:00Z" {
				t.Fatal("metadata not decoded")
			}
		})
	}
}
