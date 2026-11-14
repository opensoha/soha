package sohacli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

type memorySecretKeyring struct {
	mu          sync.Mutex
	values      map[string]string
	unavailable bool
}

func TestUnauthorizedRequestCannotAdoptReplacementProfile(t *testing.T) {
	for _, replacement := range []string{"different-user", "new-session", "service-key"} {
		t.Run(replacement, func(t *testing.T) {
			path := authV2ConfigPath(t)
			rt := Runtime{ConfigPath: path, Err: &bytes.Buffer{}}
			var writes, refreshes atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/test-write" {
					refreshes.Add(1)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				writes.Add(1)
				if r.Header.Get("Authorization") != "Bearer test-access-new" {
					t.Error("Replacement identity was sent by an already-started request")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				result := authV2Result()
				result.Data.Tokens.AccessToken = "replacement-access"
				result.Data.Tokens.RefreshToken = "replacement-refresh"
				result.Data.Tokens.SessionID = uuid.New()
				if replacement == "different-user" {
					result.Data.User.UserID = "replacement-user"
				}
				p := ProfileConfig{ServerURL: server.URL, UserID: result.Data.User.UserID, runtimeName: "local"}
				value := envelopeFromAuth(p, result)
				if replacement == "service-key" {
					value.Kind, value.ProtocolVersion, value.SessionID, value.RefreshToken = "service_account_token", 0, "", ""
				}
				if err := saveAuthenticatedProfile(r.Context(), rt, "local", p, value, "file", true); err != nil {
					t.Error("Failed to install the owned replacement profile")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer server.Close()
			writeTestV2Profile(t, path, server.URL, false)
			_, _, original, err := loadRuntimeProfile(t.Context(), rt, "local")
			if err != nil {
				t.Fatal(err)
			}
			client := gatewayClient(rt, original)
			err = client.doJSON(t.Context(), http.MethodPost, "/api/v1/test-write", original.AccessToken, nil, map[string]string{"operation": "test-only"}, nil)
			var classified *authenticationError
			if !errors.As(err, &classified) || classified.Code != "profile_changed" || writes.Load() != 1 || refreshes.Load() != 0 {
				t.Fatal("A replacement login/Key was adopted, refreshed or replayed")
			}
			cfg, err := loadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			current, err := hydrateProfile(rt, "local", cfg.Profiles["local"])
			if err != nil || current.AccessToken != "replacement-access" {
				t.Fatal("Rejecting a stale request changed the replacement credentials")
			}
		})
	}
}

func TestLegacyRefreshRetainsAuthenticationLineage(t *testing.T) {
	for _, migrated := range []bool{false, true} {
		t.Run(fmt.Sprint("migrated=", migrated), func(t *testing.T) {
			var refreshes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				refreshes.Add(1)
				if r.URL.Path != "/api/v1/auth/refresh" {
					t.Error("Unexpected legacy refresh endpoint")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				writeJSON(t, w, map[string]any{"data": map[string]any{"user": map[string]string{"userId": "legacy-user"}, "tokens": map[string]any{"accessToken": "legacy-access-next", "refreshToken": "legacy-refresh-next", "expiresIn": 3600}}})
			}))
			defer server.Close()
			path := writeTestConfigWithProfile(t, ProfileConfig{ServerURL: server.URL, AccessToken: "legacy-access-first", RefreshToken: "legacy-refresh-first", ExpiresAt: time.Now().Add(time.Hour), UserID: "legacy-user"})
			rt := Runtime{ConfigPath: path, Err: &bytes.Buffer{}}
			_, _, expected, err := loadRuntimeProfile(t.Context(), rt, "dev")
			if err != nil {
				t.Fatal(err)
			}
			if migrated {
				value := credentialEnvelope{Version: 2, Kind: "session", AccessToken: expected.AccessToken, RefreshToken: expected.RefreshToken, ExpiresAt: expected.ExpiresAt, Generation: 1}
				if err = saveAuthenticatedProfile(t.Context(), rt, "dev", expected, value, "file", true); err != nil {
					t.Fatal(err)
				}
				_, _, expected, err = loadRuntimeProfile(t.Context(), rt, "dev")
				if err != nil {
					t.Fatal(err)
				}
			}
			// Both callers captured the same old generation before either refresh.
			results := make(chan error, 2)
			for range 2 {
				go func() {
					updated, refreshErr := refreshStoredProfile(t.Context(), rt, "dev", expected, true)
					if refreshErr == nil && updated.AccessToken != "legacy-access-next" {
						refreshErr = errors.New("wrong legacy refresh generation")
					}
					results <- refreshErr
				}()
			}
			for range 2 {
				if err = <-results; err != nil {
					t.Fatal(err)
				}
			}
			if refreshes.Load() != 1 {
				t.Fatal("Same legacy authentication refreshed more than once")
			}
			// A fresh login for the same user gets a different lineage.
			var login loginResponse
			login.Data.Tokens.AccessToken = "legacy-relogin-access"
			login.Data.Tokens.RefreshToken = "legacy-relogin-refresh"
			login.Data.Tokens.ExpiresIn = 3600
			value := envelopeFromAuth(expected, login)
			if err = saveAuthenticatedProfile(t.Context(), rt, "dev", expected, value, "file", true); err != nil {
				t.Fatal(err)
			}
			_, err = refreshStoredProfile(t.Context(), rt, "dev", expected, true)
			var classified *authenticationError
			if !errors.As(err, &classified) || classified.Code != "profile_changed" || refreshes.Load() != 1 {
				t.Fatal("Stale legacy request adopted a fresh login")
			}
		})
	}
}

func TestOldServerDeviceLoginRequiresExplicitLegacyFallback(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var capabilities, writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/cli/options" {
					capabilities.Add(1)
					w.WriteHeader(status)
					return
				}
				writes.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()
			path := authV2ConfigPath(t)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := requirePrivateConfigDirectory(filepath.Dir(path), true); err != nil {
				t.Fatal(err)
			}
			original := []byte("{\"currentProfile\":\"other\",\"profiles\":{\"other\":{\"serverUrl\":\"https://other.example.test\"}}}\n")
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			var out, diagnostic bytes.Buffer
			code := Run(t.Context(), []string{"login", "--server", server.URL, "--profile", "unsupported", "--device-code", "--no-browser", "--credential-store", "file", "--json"}, Runtime{In: strings.NewReader(""), Out: &out, Err: &diagnostic, ConfigPath: path})
			if code != 1 || capabilities.Load() != 1 || writes.Load() != 0 || !strings.Contains(diagnostic.String(), "cli_auth_unsupported") || !strings.Contains(diagnostic.String(), "legacy-password") || out.Len() != 0 {
				t.Fatal("Old-server device login did not stop with explicit, side-effect-free compatibility guidance")
			}
			current, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(original, current) {
				t.Fatal("Unsupported device login changed an existing profile")
			}
		})
	}
}

func TestDevicePollingPreservesServerTerminalReason(t *testing.T) {
	for _, terminal := range []struct {
		code   string
		status int
	}{
		{"device_authorization_denied", http.StatusForbidden},
		{"device_authorization_cancelled", http.StatusConflict},
		{"expired_token", http.StatusBadRequest},
	} {
		t.Run(terminal.code, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(terminal.status)
				writeJSON(t, w, map[string]any{"error": map[string]string{"code": terminal.code, "message": "fixture-sensitive-response"}})
			}))
			defer server.Close()
			_, err := pollDeviceLogin(t.Context(), APIClient{ServerURL: server.URL}, pendingDeviceLogin{DeviceCode: strings.Repeat("d", 43), CodeVerifier: strings.Repeat("v", 43), ClaimRequestID: uuid.NewString(), ExpiresAt: time.Now().Add(10 * time.Second), Interval: 1})
			var classified *authenticationError
			if !errors.As(err, &classified) || classified.Code != terminal.code || requests.Load() != 1 || strings.Contains(err.Error(), "fixture-sensitive-response") {
				t.Fatal("Device polling lost the terminal reason, retried a closed request or exposed server text")
			}
		})
	}
}

func (k *memorySecretKeyring) Set(s, u, p string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.unavailable {
		return errors.New("test keyring locked")
	}
	if k.values == nil {
		k.values = map[string]string{}
	}
	k.values[s+":"+u] = p
	return nil
}
func (k *memorySecretKeyring) Get(s, u string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.unavailable {
		return "", errors.New("test keyring locked")
	}
	v, ok := k.values[s+":"+u]
	if !ok {
		return "", errors.New("missing test key")
	}
	return v, nil
}
func (k *memorySecretKeyring) Delete(s, u string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.unavailable {
		return errors.New("test keyring locked")
	}
	delete(k.values, s+":"+u)
	return nil
}
func authV2Result() loginResponse {
	var r loginResponse
	r.Data.User.UserID = "user-test"
	r.Data.User.UserName = "Test user"
	r.Data.Tokens.AccessToken = "test-access-new"
	r.Data.Tokens.RefreshToken = "test-refresh-new"
	r.Data.Tokens.ExpiresAt = time.Now().Add(time.Minute)
	r.Data.Tokens.TokenType = "Bearer"
	r.Data.Tokens.ProtocolVersion = 2
	r.Data.Tokens.CredentialKind = "session"
	r.Data.Tokens.SessionID = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	idle, absolute := time.Now().Add(7*24*time.Hour), time.Now().Add(30*24*time.Hour)
	r.Data.Tokens.RefreshExpiresAt = &idle
	r.Data.Tokens.SessionAbsoluteExpiresAt = &absolute
	return r
}
func authV2ConfigPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "private", "config.json")
}
func writeTestV2Profile(t *testing.T, path, server string, expired bool) {
	t.Helper()
	rt := Runtime{ConfigPath: path, Err: &bytes.Buffer{}}
	r := authV2Result()
	if expired {
		r.Data.Tokens.AccessToken = "test-access-old"
		r.Data.Tokens.RefreshToken = "test-refresh-old"
		r.Data.Tokens.ExpiresAt = time.Now().Add(-time.Minute)
	}
	p := ProfileConfig{ServerURL: server, UserID: "user-test", UserName: "Test user", runtimeName: "local"}
	if err := saveAuthenticatedProfile(context.Background(), rt, "local", p, envelopeFromAuth(p, r), "file", true); err != nil {
		t.Fatal(err)
	}
}
func TestV2CredentialStorageAndKeyringFailure(t *testing.T) {
	t.Run("private-reference", func(t *testing.T) {
		path := authV2ConfigPath(t)
		writeTestV2Profile(t, path, "https://soha.example.test", false)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("test-access")) || bytes.Contains(raw, []byte("test-refresh")) {
			t.Fatal("config contains secret material")
		}
		cfg, err := loadConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		p := cfg.Profiles["local"]
		loaded, err := hydrateProfile(Runtime{ConfigPath: path}, "local", p)
		if err != nil || loaded.AccessToken != "test-access-new" || loaded.ProtocolVersion != 2 {
			t.Fatal("credential reference could not be resolved", err)
		}
		p.ServerURL = "https://another.example.test"
		if _, err = hydrateProfile(Runtime{ConfigPath: path}, "local", p); err == nil {
			t.Fatal("credential reference crossed server binding")
		}
	})
	t.Run("no-plaintext-fallback", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
		defer server.Close()
		path := authV2ConfigPath(t)
		var out, diagnostics bytes.Buffer
		code := Run(context.Background(), []string{"login", "--server", server.URL, "--device-code", "--no-browser"}, Runtime{In: strings.NewReader(""), Out: &out, Err: &diagnostics, ConfigPath: path, Keyring: &memorySecretKeyring{unavailable: true}})
		if code != 1 || requests.Load() != 0 || !strings.Contains(diagnostics.String(), "credential_store_unavailable") {
			t.Fatal("unavailable keyring did not stop issuance")
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "credentials")); !os.IsNotExist(err) {
			t.Fatal("auto store silently created a plaintext credential directory")
		}
	})
}

func TestEphemeralLoginRevokesOnlyItsTemporarySession(t *testing.T) {
	for _, method := range []string{"password", "legacy-password", "device", "key", "cleanup-failure"} {
		t.Run(method, func(t *testing.T) {
			var revocations atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/auth/cli/options":
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"protocolVersion": 2, "supportedMethods": []string{"password", "device_code"}, "verificationOrigin": server.URL}})
				case "/api/v1/auth/cli/device-authorizations":
					_ = json.NewEncoder(w).Encode(sohaapi.CLIDeviceAuthorizationEnvelope{Data: sohaapi.CLIDeviceAuthorization{GrantID: uuid.New(), DeviceCode: strings.Repeat("d", 43), UserCode: "000042", VerificationURI: server.URL + "/auth/device#handle=" + strings.Repeat("h", 43), ExpiresAt: time.Now().Add(time.Minute), ExpiresIn: 60, Interval: 1}})
				case "/api/v1/auth/cli/password", "/api/v1/auth/login", "/api/v1/auth/cli/device-token":
					result := authV2Result()
					result.Data.Tokens.AccessToken = "temporary-access"
					result.Data.Tokens.RefreshToken = "temporary-refresh"
					if method == "legacy-password" {
						result.Data.Tokens.ProtocolVersion = 0
					}
					_ = json.NewEncoder(w).Encode(result)
				case "/api/v1/auth/context":
					if r.Header.Get("Authorization") != "Bearer imported-key" {
						t.Error("Key validation did not use the supplied Key")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"subjectId": "test-user", "subjectType": "user", "credentialKind": "personal_access_token", "displayName": "Test user"}})
				case "/api/v1/auth/cli/logout", "/api/v1/auth/logout":
					revocations.Add(1)
					var input struct {
						RefreshToken string `json:"refreshToken"`
					}
					if json.NewDecoder(r.Body).Decode(&input) != nil || input.RefreshToken != "temporary-refresh" {
						t.Error("Revocation did not target the new temporary session")
					}
					if method == "cleanup-failure" {
						w.WriteHeader(http.StatusServiceUnavailable)
						_, _ = w.Write([]byte("temporary-refresh should never appear in diagnostics"))
					}
				default:
					t.Error("Unexpected ephemeral request path")
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			path := authV2ConfigPath(t)
			writeTestV2Profile(t, path, server.URL, false)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"login", "--server", server.URL, "--profile", "local", "--credential-store", "ephemeral", "--json"}
			input := "test-password\n"
			switch method {
			case "key":
				args = append(args, "--with-token")
				input = "imported-key\n"
			case "device":
				args = append(args, "--device-code", "--no-browser")
			default:
				selected := method
				if selected == "cleanup-failure" {
					selected = "password"
				}
				args = append(args, "--method", selected, "--login", "test", "--password-stdin")
			}
			var out, diagnostics bytes.Buffer
			code := Run(context.Background(), args, Runtime{In: strings.NewReader(input), Out: &out, Err: &diagnostics, ConfigPath: path})
			if method == "cleanup-failure" {
				if code != 1 || !strings.Contains(diagnostics.String(), "ephemeral_cleanup_unconfirmed") || strings.Contains(diagnostics.String(), "temporary-refresh") || out.Len() != 0 {
					t.Fatal("Unconfirmed temporary revocation did not return a secret-free failure")
				}
			} else {
				var result map[string]any
				if code != 0 || json.Unmarshal(out.Bytes(), &result) != nil || result["persisted"] != false || result["authenticated"] != false || result["verified"] != true {
					t.Fatal("Ephemeral validation did not return its non-persistent result")
				}
				if method != "key" && result["temporarySessionRevoked"] != true {
					t.Fatal("Temporary session revocation was not reported")
				}
			}
			expected := int32(1)
			if method == "key" {
				expected = 0
			}
			if revocations.Load() != expected {
				t.Fatal("Ephemeral login revoked an incorrect number of sessions or Keys")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("Ephemeral login changed the existing profile")
			}
		})
	}
}

func TestKeyLoginHumanOutputNeutralizesTerminalControls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/context" {
			t.Error("Unexpected Key login request")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"subjectId": "user-test", "subjectType": "user", "credentialKind": "personal_access_token", "displayName": "External\x1b[2J\u009b31m\r\nUser"}})
	}))
	defer server.Close()
	var out, diagnostics bytes.Buffer
	code := Run(context.Background(), []string{"login", "--server", server.URL, "--with-token", "--credential-store", "ephemeral"}, Runtime{In: strings.NewReader("imported-test-key\n"), Out: &out, Err: &diagnostics, ConfigPath: authV2ConfigPath(t)})
	if code != 0 || strings.ContainsAny(out.String(), "\x1b\u009b\r") || strings.Count(out.String(), "\n") != 1 || strings.Contains(out.String(), "imported-test-key") {
		t.Fatal("Human login output emitted controls, secrets or forged extra lines")
	}
}

func TestDeviceLoginResumesOriginalClaim(t *testing.T) {
	path := authV2ConfigPath(t)
	var server *httptest.Server
	var challenge, claimID, verifier string
	var creates, claims atomic.Int32
	var mu sync.Mutex
	var approved atomic.Bool
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "" {
			t.Error("public authentication request used profile bearer")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/cli/options":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"protocolVersion": 2, "enabled": true, "supportedMethods": []string{"device_code", "key"}, "verificationOrigin": server.URL}})
		case "/api/v1/auth/cli/device-authorizations":
			creates.Add(1)
			var input sohaapi.CLIDeviceAuthorizationCreateRequest
			if json.NewDecoder(r.Body).Decode(&input) != nil {
				t.Error("invalid create request")
			}
			challenge = input.CodeChallenge
			_ = json.NewEncoder(w).Encode(sohaapi.CLIDeviceAuthorizationEnvelope{Data: sohaapi.CLIDeviceAuthorization{GrantID: uuid.New(), DeviceCode: strings.Repeat("d", 43), UserCode: "000042", VerificationURI: server.URL + "/auth/device#handle=" + strings.Repeat("h", 43), ExpiresAt: time.Now().Add(time.Minute), ExpiresIn: 60, Interval: 1}})
		case "/api/v1/auth/cli/device-token":
			claims.Add(1)
			var input sohaapi.CLIDeviceTokenRequest
			_ = json.NewDecoder(r.Body).Decode(&input)
			sum := sha256.Sum256([]byte(input.CodeVerifier))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
				t.Error("claim did not prove original PKCE")
			}
			if claimID == "" {
				claimID = input.ClaimRequestID.String()
				verifier = input.CodeVerifier
			} else if claimID != input.ClaimRequestID.String() || verifier != input.CodeVerifier {
				t.Error("resume changed original claim identity")
			}
			if !approved.Load() {
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`{"error":{"code":"unavailable","message":"test"}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(authV2Result())
		default:
			t.Error("unexpected device path")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	args := []string{"login", "--server", server.URL, "--profile", "local", "--device-code", "--no-browser", "--credential-store", "file"}
	first, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var out, diagnostics bytes.Buffer
	code := Run(first, args, Runtime{In: strings.NewReader(""), Out: &out, Err: &diagnostics, ConfigPath: path})
	if code != 1 || creates.Load() != 1 || claims.Load() != 1 {
		t.Fatal("initial interrupted claim was not retained")
	}
	approved.Store(true)
	out.Reset()
	diagnostics.Reset()
	code = Run(context.Background(), args[:len(args)-2], Runtime{In: strings.NewReader(""), Out: &out, Err: &diagnostics, ConfigPath: path, Keyring: &memorySecretKeyring{unavailable: true}})
	if code != 0 || creates.Load() != 1 || claims.Load() != 2 {
		t.Fatalf("resume failed: %s", diagnostics.String())
	}
	if !strings.Contains(out.String(), "000 042") || strings.Contains(out.String(), "test-access") || strings.Contains(diagnostics.String(), "test-refresh") {
		t.Fatal("verification formatting or secret-free output failed")
	}
	pendingPath, err := pendingLoginFile(Runtime{ConfigPath: path}, server.URL, "local")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pendingPath); !os.IsNotExist(err) {
		t.Fatal("pending login reference was not cleared")
	}
}
func TestV2RefreshProcessHelper(t *testing.T) {
	if os.Getenv("SOHA_CLI_TEST_REFRESH_HELPER") != "1" {
		return
	}
	var out, diagnostics bytes.Buffer
	code := Run(context.Background(), []string{"whoami", "--profile", "local", "--json"}, Runtime{In: strings.NewReader(""), Out: &out, Err: &diagnostics, ConfigPath: os.Getenv("SOHA_CLI_TEST_CONFIG")})
	if code != 0 {
		t.Fatal("child authentication failed:", diagnostics.String())
	}
}
func TestV2RefreshSerializesAcrossProcesses(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/cli/refresh":
			if r.Header.Get("Authorization") != "" {
				t.Error("refresh sent bearer")
			}
			var input sohaapi.CLIRefreshRequest
			_ = json.NewDecoder(r.Body).Decode(&input)
			if input.RefreshToken != "test-refresh-old" || input.RefreshRequestID == uuid.Nil {
				t.Error("refresh credential or durable request ID missing")
			}
			if refreshes.Add(1) != 1 {
				w.WriteHeader(401)
				return
			}
			time.Sleep(100 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(authV2Result())
		case "/api/v1/auth/context":
			if r.Header.Get("Authorization") != "Bearer test-access-new" {
				t.Error("child used stale access credential")
			}
			_ = json.NewEncoder(w).Encode(sohaapi.CLIAuthContextEnvelope{Data: sohaapi.CLIAuthContext{SubjectType: "user", SubjectID: "user-test", DisplayName: "Test user", CredentialKind: "session", PermissionKeys: []string{"ai.gateway.view"}, Scopes: []string{}}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	path := authV2ConfigPath(t)
	writeTestV2Profile(t, path, server.URL, true)
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestV2RefreshProcessHelper$")
			cmd.Env = append(os.Environ(), "SOHA_CLI_TEST_REFRESH_HELPER=1", "SOHA_CLI_TEST_CONFIG="+path, "SOHA_TOKEN=", "SOHA_SERVER=")
			raw, err := cmd.CombinedOutput()
			if err != nil {
				errors <- fmt.Errorf("child refresh failed: %w: %s", err, raw)
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if refreshes.Load() != 1 {
		t.Fatal("concurrent processes refreshed the same generation more than once")
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := hydrateProfile(Runtime{ConfigPath: path}, "local", cfg.Profiles["local"])
	if err != nil || p.RefreshToken != "test-refresh-new" || p.credential.PendingRefreshRequestID != "" {
		t.Fatal("rotated generation was not committed", err)
	}
}
func TestStatelessKeyAndOfflineStatus(t *testing.T) {
	t.Setenv("SOHA_TOKEN", "test-key")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/api/v1/auth/context" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("service account verification used user profile lookup")
		}
		_ = json.NewEncoder(w).Encode(sohaapi.CLIAuthContextEnvelope{Data: sohaapi.CLIAuthContext{SubjectType: "service_account", SubjectID: "ci", DisplayName: "CI", CredentialKind: "service_account_token", PermissionKeys: []string{}, Scopes: []string{}}})
	}))
	defer server.Close()
	t.Setenv("SOHA_SERVER", server.URL)
	path := authV2ConfigPath(t)
	var out, diagnostics bytes.Buffer
	rt := Runtime{ConfigPath: path, In: strings.NewReader(""), Out: &out, Err: &diagnostics}
	if code := Run(context.Background(), []string{"auth", "status", "--json"}, rt); code != 0 {
		t.Fatal("stateless status failed")
	}
	if !strings.Contains(out.String(), `"authenticated": true`) || strings.Contains(out.String(), "test-key") {
		t.Fatal("online status is not verified and credential-free")
	}
	out.Reset()
	if code := Run(context.Background(), []string{"auth", "status", "--offline", "--json"}, rt); code != 0 {
		t.Fatal("offline metadata failed")
	}
	if requests.Load() != 1 || !strings.Contains(out.String(), `"authenticated": null`) {
		t.Fatal("offline metadata claimed authentication or contacted server")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("environment credentials were persisted")
	}
}

func TestWriteRequestOnlyRetriesPreHandlerAuthenticationRejection(t *testing.T) {
	for _, marked := range []bool{false, true} {
		t.Run(fmt.Sprint(marked), func(t *testing.T) {
			var writes, refreshes int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/auth/refresh" {
					refreshes++
					_ = json.NewEncoder(w).Encode(authV2Result())
					return
				}
				writes++
				if writes == 1 {
					if marked {
						w.Header().Set("WWW-Authenticate", `Bearer realm="soha", error="invalid_token"`)
					}
					w.WriteHeader(401)
					_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"rejected"}}`))
					return
				}
				w.WriteHeader(200)
			}))
			defer server.Close()
			path := authV2ConfigPath(t)
			profile := ProfileConfig{ServerURL: server.URL, AccessToken: "old", RefreshToken: "legacy-refresh", ExpiresAt: time.Now().Add(time.Hour), runtimeName: "local", refreshEnabled: true, refreshPersist: true}
			if err := saveConfig(path, Config{CurrentProfile: "local", Profiles: map[string]ProfileConfig{"local": profile}}); err != nil {
				t.Fatal(err)
			}
			client := gatewayClient(Runtime{ConfigPath: path, Err: &bytes.Buffer{}}, profile)
			err := client.doJSON(context.Background(), http.MethodPost, "/api/v1/test", client.Token, map[string]string{"Idempotency-Key": "test-request"}, map[string]any{"operation": "create"}, nil)
			if marked {
				if err != nil || writes != 2 || refreshes != 1 {
					t.Fatal("pre-handler rejection was not safely renewed", err)
				}
			} else if err == nil || writes != 1 || refreshes != 0 {
				t.Fatal("ambiguous write rejection was replayed")
			}
		})
	}
}
func TestAPIErrorNeverEchoesKnownCredentialValues(t *testing.T) {
	message := safeAPIErrorMessage("400 Bad Request", []byte(`{"error":{"message":"echo test-password and test-access and test-device"}}`), []byte(`{"password":"test-password","deviceCode":"test-device"}`), "test-access")
	if strings.Contains(message, "test-password") || strings.Contains(message, "test-access") || strings.Contains(message, "test-device") {
		t.Fatal("server error echoed credentials without a field label")
	}
}

func TestLoginCredentialCommitFailureDoesNotRevokeCommittedSession(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "before-rename"
		if committed {
			name = "after-rename-sync-unknown"
		}
		t.Run(name, func(t *testing.T) {
			var logouts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/auth/cli/options":
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"protocolVersion": 2, "enabled": true, "supportedMethods": []string{"password", "key"}}})
				case "/api/v1/auth/cli/password":
					_ = json.NewEncoder(w).Encode(authV2Result())
				case "/api/v1/auth/cli/logout":
					logouts.Add(1)
					w.WriteHeader(200)
				default:
					t.Error("unexpected authentication path")
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			path := authV2ConfigPath(t)
			prior := Config{CurrentProfile: "local", Profiles: map[string]ProfileConfig{"local": {ServerURL: server.URL, AccessToken: "old-test-access", RefreshToken: "old-test-refresh"}, "other": {ServerURL: "https://another.example.test", UserName: "Other profile"}}}
			if err := saveConfig(path, prior); err != nil {
				t.Fatal(err)
			}
			var out, diagnostics bytes.Buffer
			rt := Runtime{In: strings.NewReader("test-password\n"), Out: &out, Err: &diagnostics, ConfigPath: path, credentialConfigWrite: func(path string, cfg Config) error {
				if committed {
					if err := saveConfig(path, cfg); err != nil {
						return err
					}
				}
				return errors.New("injected config commit failure")
			}}
			code := Run(context.Background(), []string{"login", "--server", server.URL, "--profile", "local", "--method", "password", "--login", "test", "--password-stdin", "--credential-store", "file"}, rt)
			if code != 1 {
				t.Fatal("save failure must not claim successful login")
			}
			cfg, err := loadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Profiles["other"].UserName != "Other profile" {
				t.Fatal("unrelated profile changed")
			}
			if committed {
				if logouts.Load() != 0 || cfg.Profiles["local"].CredentialRef == "" || !strings.Contains(diagnostics.String(), "credential_save_unconfirmed") {
					t.Fatal("uncertain committed credential was revoked or lost")
				}
				loaded, err := hydrateProfile(rt, "local", cfg.Profiles["local"])
				if err != nil || loaded.AccessToken != "test-access-new" {
					t.Fatal("committed credential reference is not recoverable")
				}
			} else {
				if logouts.Load() != 1 || cfg.Profiles["local"].AccessToken != "old-test-access" {
					t.Fatal("uncommitted login replaced old credentials or failed to revoke new session")
				}
				entries, err := os.ReadDir(filepath.Join(filepath.Dir(path), "credentials"))
				if err != nil || len(entries) != 0 {
					t.Fatal("uncommitted credential entry remains")
				}
			}
			if strings.Contains(out.String()+diagnostics.String(), "test-access-new") || strings.Contains(out.String()+diagnostics.String(), "test-refresh-new") || strings.Contains(out.String()+diagnostics.String(), "test-password") {
				t.Fatal("commit diagnostics exposed credentials")
			}
		})
	}
}

func TestKeyRotationRecoveryUsesIndependentAuthAndDurableRequest(t *testing.T) {
	var rotations atomic.Int32
	var requestID string
	var expiry time.Time
	path := authV2ConfigPath(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer independent-test-session" {
			t.Error("rotation used ambient or target credential")
		}
		switch {
		case r.URL.Path == "/api/v1/auth/context":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"credentialKind": "session", "subjectType": "user", "subjectId": "test-user", "displayName": "Test", "permissionKeys": []string{"ai.gateway.invoke"}, "scopes": []string{}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/ai-gateway/personal-access-tokens":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"id": "target-test"}}})
		case strings.HasSuffix(r.URL.Path, "/rotate"):
			var input sohaapi.TokenRotationInput
			if json.NewDecoder(r.Body).Decode(&input) != nil {
				t.Error("invalid rotation request")
			}
			raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), "token-rotations", input.RotationRequestID.String()+".json"))
			if err != nil || !bytes.Contains(raw, []byte(input.RotationRequestID.String())) {
				t.Error("rotation sent before durable local recovery record")
			}
			if rotations.Add(1) == 1 {
				requestID = input.RotationRequestID.String()
				expiry = *input.ExpiresAt
				if expiry.Before(time.Now().Add(89*24*time.Hour)) || expiry.After(time.Now().Add(91*24*time.Hour)) {
					t.Error("rotation did not set finite 90-day default")
				}
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`{"error":{"code":"unavailable","message":"response lost"}}`))
				return
			}
			if input.RotationRequestID.String() != requestID || !input.ExpiresAt.Equal(expiry) {
				t.Error("retry changed original request or expiry")
			}
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"error":{"code":"token_rotation_completed","message":"completed"}}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/ai-gateway/token-rotations/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"rotationRequestId": requestID, "tokenKind": "personal_access_token", "previousTokenId": "target-test", "replacementTokenId": "replacement-test", "status": "completed", "rotatedAt": time.Now(), "newValueRetrievable": false}})
		default:
			t.Error("unexpected rotation path")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cfg := Config{CurrentProfile: "local", Profiles: map[string]ProfileConfig{"local": {ServerURL: server.URL, AccessToken: "target-test-key"}, "manager": {ServerURL: server.URL, AccessToken: "independent-test-session"}}}
	if err := saveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOHA_TOKEN", "ambient-test-key")
	t.Setenv("SOHA_SERVER", server.URL)
	var out, diagnostics bytes.Buffer
	rt := Runtime{Out: &out, Err: &diagnostics, ConfigPath: path}
	args := []string{"token", "rotate", "--id", "target-test", "--profile", "local", "--auth-profile", "manager"}
	if Run(context.Background(), args, rt) != 1 || requestID == "" {
		t.Fatal("lost response did not retain recoverable request")
	}
	out.Reset()
	diagnostics.Reset()
	if Run(context.Background(), append(args, "--request-id", requestID), rt) != 1 || !strings.Contains(diagnostics.String(), "token_rotation_completed") {
		t.Fatal("retry did not expose completed outcome")
	}
	out.Reset()
	diagnostics.Reset()
	if Run(context.Background(), []string{"token", "rotation-status", "--request-id", requestID, "--profile", "local", "--auth-profile", "manager"}, rt) != 0 {
		t.Fatal("independent rotation recovery failed")
	}
	if !strings.Contains(out.String(), "replacement-test") || strings.Contains(out.String(), "target-test-key") || strings.Contains(out.String(), "independent-test-session") {
		t.Fatal("recovery metadata missing or secret leaked")
	}
}
func TestKeyExpiryRejectsPastAndSelfRotationBeforeWrite(t *testing.T) {
	if _, err := keyExpiry(time.Now().Add(-time.Minute).Format(time.RFC3339)); err == nil {
		t.Fatal("expired key accepted")
	}
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/context" {
			writes.Add(1)
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"credentialKind": "service_account_token", "subjectType": "service_account", "subjectId": "sa-test", "displayName": "CI", "tokenId": "same-key", "permissionKeys": []string{}, "scopes": []string{}}})
	}))
	defer server.Close()
	t.Setenv("SOHA_TOKEN", "test-key")
	t.Setenv("SOHA_SERVER", server.URL)
	var out, diagnostics bytes.Buffer
	rt := Runtime{Out: &out, Err: &diagnostics, ConfigPath: authV2ConfigPath(t)}
	if Run(context.Background(), []string{"service-account", "token-rotate", "--id", "same-key", "--profile", "local"}, rt) != 1 || writes.Load() != 0 || !strings.Contains(diagnostics.String(), "independent_auth_required") {
		t.Fatal("self-rotation must be rejected before server mutation")
	}
}
