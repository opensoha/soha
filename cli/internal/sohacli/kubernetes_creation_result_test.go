package sohacli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKubernetesCreationReportsBatchFailureAndRetainsOriginalReceipt(t *testing.T) {
	for _, status := range []string{"succeeded", "partial", "failed", "unknown"} {
		t.Run(status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"data":{"operationId":"original-operation","contentHash":"%s","status":"%s","items":[]}}`, strings.Repeat("a", 64), status)
			}))
			defer server.Close()
			var out, errOut bytes.Buffer
			code := Run(t.Context(), []string{"k8s", "create", "execute", "--cluster-id", "cluster-a", "--file", "-", "--idempotency-key", "original-key", "--retry-original", "--yes", "--profile", "dev"}, Runtime{In: strings.NewReader("kind: ConfigMap"), Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			if (code == 0) != (status == "succeeded") || !strings.Contains(out.String(), "original-operation") {
				t.Fatalf("status=%s code=%d stdout=%s stderr=%s", status, code, out.String(), errOut.String())
			}
		})
	}
}
