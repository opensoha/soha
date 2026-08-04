package vaultsecret

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainsecret "github.com/opensoha/soha/internal/domain/secret"
)

func TestClientReadsPinnedVaultKV2Version(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/team/kv/data/demo/app" || r.URL.Query().Get("version") != "7" {
			t.Fatalf("request URL = %s", r.URL.String())
		}
		if r.Header.Get("X-Vault-Token") != "vault-token" || r.Header.Get("X-Vault-Namespace") != "tenant-a" {
			t.Fatalf("request headers = %#v", r.Header)
		}
		_, _ = fmt.Fprint(w, `{"data":{"data":{"token":"resolved-value"},"metadata":{"version":7}}}`)
	}))
	defer server.Close()

	client, err := New(server.URL, "vault-token", "tenant-a", time.Second, 4096)
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.Read(context.Background(), domainsecret.VaultKV2Reference{Mount: "team/kv", Path: "demo/app", Key: "token", Version: 7})
	if err != nil || value != "resolved-value" {
		t.Fatalf("Read() = %q, %v", value, err)
	}
}

func TestClientDoesNotExposeVaultResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "sensitive provider detail", http.StatusForbidden)
	}))
	defer server.Close()
	client, err := New(server.URL, "vault-token", "", time.Second, 4096)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Read(context.Background(), domainsecret.VaultKV2Reference{Mount: "secret", Path: "demo", Key: "token", Version: 1})
	if err == nil || strings.Contains(err.Error(), "sensitive provider detail") {
		t.Fatalf("Read() error = %v", err)
	}
}
