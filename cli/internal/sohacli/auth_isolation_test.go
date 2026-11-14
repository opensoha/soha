package sohacli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEnvironmentCredentialsWithoutProfile(t *testing.T) {
	t.Setenv("SOHA_TOKEN", "test-environment-only")
	t.Setenv("SOHA_SERVER", "https://example.invalid/soha")
	path := filepath.Join(t.TempDir(), "missing.json")
	_, _, profile, err := loadRuntimeProfile(context.Background(), Runtime{ConfigPath: path}, "ci")
	if err != nil {
		t.Fatal(err)
	}
	if profile.ServerURL != "https://example.invalid/soha" || profile.AccessToken == "" || profile.RefreshToken != "" || profile.refreshEnabled {
		t.Fatal("environment credentials were not isolated")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("environment authentication wrote a config")
	}
}

func TestServerOverrideNeverUsesStoredCredential(t *testing.T) {
	t.Setenv("SOHA_TOKEN", "")
	t.Setenv("SOHA_SERVER", "https://other.invalid")
	path := writeTestConfigWithProfile(t, ProfileConfig{
		ServerURL: "https://original.invalid", AccessToken: "test-access", RefreshToken: "test-refresh",
		ExpiresAt: time.Now().Add(-time.Hour),
	})
	_, _, _, err := loadRuntimeProfile(context.Background(), Runtime{ConfigPath: path}, "dev")
	if err == nil {
		t.Fatal("cross-server credential reuse was accepted")
	}
	stored, readErr := loadConfig(path)
	if readErr != nil || stored.Profiles["dev"].AccessToken != "test-access" {
		t.Fatal("rejected override modified stored credentials")
	}
}

func TestCanonicalServerIsolation(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		same bool
	}{
		{"https://EXAMPLE.invalid:443/soha/", "https://example.invalid/soha", true},
		{"http://example.invalid:80", "http://example.invalid", true},
		{"https://example.invalid/a", "https://example.invalid/b", false},
		{"https://example.invalid", "http://example.invalid", false},
		{"https://example.invalid", "https://example.invalid:8443", false},
		{"https://example.invalid", "https://alias.invalid", false},
		{"https://user:secret@example.invalid", "https://user:secret@example.invalid", false},
	} {
		if sameServerURL(tc.a, tc.b) != tc.same {
			t.Fatalf("unexpected server identity for %s", tc.a)
		}
	}
}

func TestPrivateConfigRejectsSymlinkAndTightensFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes and unprivileged symlink behavior")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(path, Config{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("config permissions were not tightened")
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(link); err == nil {
		t.Fatal("symlink config was read")
	}
	if err := saveConfig(link, Config{}); err == nil {
		t.Fatal("symlink config was replaced")
	}
}

func TestConfigDoesNotChangeSharedDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions")
	}
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(filepath.Join(dir, "config.json"), Config{}); err == nil {
		t.Fatal("shared directory accepted for credentials")
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatal("unrelated directory permissions changed")
	}
}

func TestRefreshFailureRetainsProfileWithoutEchoingSecrets(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{
		{401, "auth_refresh_rejected"}, {403, "auth_refresh_forbidden"}, {503, "auth_refresh_unavailable"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/auth/refresh" || r.Header.Get("Authorization") != "" {
					t.Error("unexpected authenticated refresh request")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":{"message":"test-refresh-secret"}}`))
			}))
			defer server.Close()
			path := writeTestConfigWithProfile(t, ProfileConfig{ServerURL: server.URL, AccessToken: "test-access-secret", RefreshToken: "test-refresh-secret", ExpiresAt: time.Now().Add(-time.Hour)})
			before, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			var out, errOut bytes.Buffer
			if code := Run(context.Background(), []string{"capabilities", "--profile", "dev"}, Runtime{ConfigPath: path, Out: &out, Err: &errOut}); code != 1 {
				t.Fatalf("exit=%d", code)
			}
			after, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(before, after) || calls != 1 {
				t.Fatal("failed refresh changed credentials or retried")
			}
			if !strings.Contains(errOut.String(), tc.code) || strings.Contains(out.String()+errOut.String(), "test-refresh-secret") {
				t.Fatal("refresh error was unclassified or leaked a credential")
			}
		})
	}
}

func TestMCPServerMismatchDoesNotRefreshOrForwardCredentials(t *testing.T) {
	t.Setenv("SOHA_TOKEN", "")
	t.Setenv("SOHA_SERVER", "")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		t.Error("mismatched MCP target caused a request")
	}))
	defer server.Close()
	path := writeTestConfigWithProfile(t, ProfileConfig{ServerURL: server.URL, AccessToken: "test-access", RefreshToken: "test-refresh", ExpiresAt: time.Now().Add(-time.Hour)})
	if _, err := loadMCPRuntimeProfile(context.Background(), Runtime{ConfigPath: path}, "dev", "https://other.invalid", ""); err == nil {
		t.Fatal("MCP forwarded the stored credential to another server")
	}
	if calls != 0 {
		t.Fatal("MCP refreshed before rejecting a different target")
	}
}

func TestExplicitEnvironmentCredentialsIgnoreBrokenConfig(t *testing.T) {
	t.Setenv("SOHA_TOKEN", "test-environment-token")
	t.Setenv("SOHA_SERVER", "https://example.invalid")
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, profile, err := loadRuntimeProfile(context.Background(), Runtime{ConfigPath: path}, "ci")
	if err != nil || profile.AccessToken != "test-environment-token" || profile.RefreshToken != "" {
		t.Fatal("explicit stateless credentials depended on stored config", err)
	}
	mcpProfile, err := loadMCPRuntimeProfile(context.Background(), Runtime{ConfigPath: path}, "ci", "https://example.invalid", "")
	if err != nil || mcpProfile.ServerURL != "https://example.invalid" {
		t.Fatal("MCP environment login required a persisted profile", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "not json" {
		t.Fatal("stateless authentication changed config")
	}
}

func TestAPIRedirectNeverForwardsCredentialBodyOrBearer(t *testing.T) {
	for _, token := range []string{"", "test-bearer"} {
		t.Run("bearer-present-"+fmt.Sprint(token != ""), func(t *testing.T) {
			received := 0
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received++ }))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL+"/api/v1/auth/login", http.StatusTemporaryRedirect)
			}))
			defer source.Close()
			client := APIClient{ServerURL: source.URL, Token: token}
			if err := client.doJSON(context.Background(), http.MethodPost, "/api/v1/auth/login", token, nil, map[string]string{"login": "test-user", "password": "test-password"}, nil); err == nil {
				t.Fatal("cross-server API redirect succeeded")
			}
			if received != 0 {
				t.Fatal("API credentials reached redirect target")
			}
		})
	}
}

func TestAPIRedirectWithinConfiguredBasePath(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/soha/api/old" {
			http.Redirect(w, r, "/soha/api/new", http.StatusTemporaryRedirect)
			return
		}
		calls++
		if r.URL.Path != "/soha/api/new" || r.Header.Get("Authorization") != "Bearer test-bearer" {
			t.Error("same-server redirect lost its boundary or credential")
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := APIClient{ServerURL: server.URL + "/soha", Token: "test-bearer"}
	if err := client.doJSON(context.Background(), http.MethodGet, "/api/old", client.Token, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("same-server redirect was not followed")
	}
}

func TestCLIAuthCredentialErrorsAreRedacted(t *testing.T) {
	for _, key := range []string{"deviceCode", "userCode", "verificationHandle", "codeVerifier", "confirmationId"} {
		if strings.Contains(redactSensitiveText(key+"=test-only-credential"), "test-only-credential") {
			t.Fatal("CLI authentication credential escaped redaction")
		}
	}
}
