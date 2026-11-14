package sohacli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
)

// Explicit native acceptance only; ordinary tests never open a user's keychain.
func TestNativeSystemKeyringLifecycle(t *testing.T) {
	if os.Getenv("SOHA_CLI_NATIVE_KEYRING") != "1" {
		t.Skip("requires explicit native keyring acceptance")
	}
	receipt := os.Getenv("SOHA_CLI_NATIVE_RECEIPT")
	if receipt == "" {
		t.Fatal("Native acceptance requires an owned cleanup receipt")
	}
	path := authV2ConfigPath(t)
	store, err := openCredentialStorage(Runtime{ConfigPath: path}, "keyring")
	if err != nil {
		t.Fatal("Native keyring could not be configured")
	}
	// This namespace can never collide with production's config-path hash.
	store.service = "com.opensoha.cli.v2.native-test." + uuid.NewString()
	server, profile := "https://native-test.invalid", "native-test"
	ref := newCredentialRef(server, profile)
	raw, _ := json.Marshal(map[string]string{"service": store.service, "ref": ref})
	if os.WriteFile(receipt, raw, 0o600) != nil {
		t.Fatal("Native cleanup receipt could not be saved")
	}
	t.Cleanup(func() {
		if store.Delete(ref) != nil {
			t.Error("Owned native test entry could not be deleted")
		}
	})
	result := authV2Result()
	p := ProfileConfig{ServerURL: server, runtimeName: profile}
	value := envelopeFromAuth(p, result)
	if store.Save(ref, value) != nil {
		t.Fatal("Native keyring write failed")
	}
	loaded, err := store.Load(ref, server, profile)
	if err != nil || loaded.AccessToken != value.AccessToken || loaded.RefreshToken != value.RefreshToken {
		t.Fatal("Native keyring round trip failed")
	}
	if store.Delete(ref) != nil {
		t.Fatal("Native keyring deletion failed")
	}
	if _, err = store.Load(ref, server, profile); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("Deleted native test entry remains accessible")
	}
}

func TestNativeHeadlessKeyringUnavailable(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("SOHA_CLI_NATIVE_HEADLESS") != "1" {
		t.Skip("requires explicit isolated Linux headless acceptance")
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "missing-bus"))
	t.Setenv("DISPLAY", "")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	path := authV2ConfigPath(t)
	var out, diagnostics bytes.Buffer
	code := Run(t.Context(), []string{"login", "--server", server.URL, "--method", "password", "--login", "test", "--password-stdin"}, Runtime{In: strings.NewReader("test-only\n"), Out: &out, Err: &diagnostics, ConfigPath: path})
	if code != 1 || requests.Load() != 0 || !strings.Contains(diagnostics.String(), "credential_store_unavailable") {
		t.Fatal("Native headless keyring failure did not stop issuance before network access")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "credentials")); !os.IsNotExist(err) {
		t.Fatal("Native headless login silently fell back to plaintext")
	}
}
