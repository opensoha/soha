package runner

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHermesPrivateCAIsExplicitAndValidated(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	base := &http.Client{}
	client, err := hermesHTTPClient(base, path)
	if err != nil {
		t.Fatal(err)
	}
	h := hermesClient{http: client, endpoint: server.URL, token: "test"}
	if err = h.json(context.Background(), http.MethodGet, "/health", nil, "", nil); err != nil {
		t.Fatal(err)
	}
	if base.Transport != nil {
		t.Fatal("shared HTTP client was mutated")
	}
	if err = os.WriteFile(path, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = hermesHTTPClient(base, path); err == nil {
		t.Fatal("invalid CA accepted")
	}
	if _, err = hermesHTTPClient(base, "relative.pem"); err == nil {
		t.Fatal("relative CA accepted")
	}
}
