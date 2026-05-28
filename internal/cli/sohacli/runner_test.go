package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunLoginStoresProfileAndRedactsShow(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "config.json")
	accessToken := "access-token-value-12345"
	refreshToken := "refresh-token-value-9999"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/login" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode login request: %v", err)
		}
		if req["login"] != "ada" || req["password"] != "secret" {
			t.Fatalf("unexpected login payload %#v", req)
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"user": map[string]any{
					"userId":   "u-1",
					"userName": "Ada",
					"roles":    []string{"developer"},
				},
				"tokens": map[string]any{
					"accessToken":  accessToken,
					"refreshToken": refreshToken,
					"expiresAt":    time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC),
				},
			},
		})
	}))
	defer server.Close()

	var out bytes.Buffer
	code := Run(ctx, []string{
		"login",
		"--server", server.URL + "/",
		"--login", "ada",
		"--password", "secret",
		"--profile", "dev",
		"--ai-client-id", "codex-local",
		"--ai-client", "Codex",
	}, Runtime{Out: &out, Err: &bytes.Buffer{}, ConfigPath: configPath})
	if code != 0 {
		t.Fatalf("login returned %d, output %q", code, out.String())
	}
	if strings.Contains(out.String(), accessToken) || strings.Contains(out.String(), refreshToken) || strings.Contains(out.String(), "secret") {
		t.Fatalf("login output leaked sensitive material: %q", out.String())
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	profile := cfg.Profiles["dev"]
	if cfg.CurrentProfile != "dev" || profile.ServerURL != server.URL {
		t.Fatalf("unexpected saved profile: %#v", cfg)
	}
	if profile.AccessToken != accessToken || profile.RefreshToken != refreshToken {
		t.Fatalf("token was not stored in local profile")
	}
	if profile.AIClientID != "codex-local" || profile.AIClientName != "Codex" {
		t.Fatalf("AI client context was not stored: %#v", profile)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o, want 0600", info.Mode().Perm())
	}

	var showOut bytes.Buffer
	code = Run(ctx, []string{"profile", "show", "dev"}, Runtime{Out: &showOut, Err: &bytes.Buffer{}, ConfigPath: configPath})
	if code != 0 {
		t.Fatalf("profile show returned %d", code)
	}
	if strings.Contains(showOut.String(), accessToken) || strings.Contains(showOut.String(), refreshToken) {
		t.Fatalf("profile show leaked full token: %q", showOut.String())
	}
	if !strings.Contains(showOut.String(), "access-t...2345") {
		t.Fatalf("profile show did not include redacted access token: %q", showOut.String())
	}
}

func TestRunCapabilitiesNamesSendsGatewayHeaders(t *testing.T) {
	ctx := context.Background()
	var sawCapabilities bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ai-gateway/capabilities" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		sawCapabilities = true
		if r.Header.Get("Authorization") != "Bearer profile-token" {
			t.Fatalf("unexpected Authorization header %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Soha-AI-Client-ID") != "override-client" {
			t.Fatalf("unexpected AI client id header %q", r.Header.Get("X-Soha-AI-Client-ID"))
		}
		if r.Header.Get("X-Soha-AI-Client") != "Codex" {
			t.Fatalf("unexpected AI client header %q", r.Header.Get("X-Soha-AI-Client"))
		}
		if r.Header.Get("X-Soha-Skill-ID") != "skill-override" {
			t.Fatalf("unexpected skill header %q", r.Header.Get("X-Soha-Skill-ID"))
		}
		if r.Header.Get("X-Soha-Source") != "profile-source" {
			t.Fatalf("unexpected source header %q", r.Header.Get("X-Soha-Source"))
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"name":           "soha AI Gateway",
				"version":        "v1alpha1",
				"permissionKeys": []string{"ai.gateway.view"},
				"tools": []map[string]any{{
					"name":             "delivery.applications.list",
					"description":      "List delivery applications",
					"riskLevel":        "read",
					"requiresApproval": false,
				}},
				"resources": []map[string]any{{"name": "delivery.applications"}},
				"prompts":   []map[string]any{{"name": "delivery.release.diagnose"}},
				"skills":    []map[string]any{{"id": "delivery-developer", "name": "Developer Delivery", "category": "delivery"}},
			},
		})
	}))
	defer server.Close()

	configPath := writeTestConfig(t, server.URL)
	var out bytes.Buffer
	code := Run(ctx, []string{
		"capabilities",
		"--profile", "dev",
		"--output", "names",
		"--ai-client-id", "override-client",
		"--skill-id", "skill-override",
	}, Runtime{Out: &out, Err: &bytes.Buffer{}, ConfigPath: configPath})
	if code != 0 {
		t.Fatalf("capabilities returned %d", code)
	}
	if !sawCapabilities {
		t.Fatalf("capabilities endpoint was not called")
	}
	text := out.String()
	for _, want := range []string{
		"profile: dev",
		"tool\tdelivery.applications.list\tread\tdirect",
		"resource\tdelivery.applications",
		"prompt\tdelivery.release.diagnose",
		"skill\tdelivery-developer\tDeveloper Delivery",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("capabilities output missing %q in %q", want, text)
		}
	}
}

func TestRunMCPStartProxiesToolsToGateway(t *testing.T) {
	ctx := context.Background()
	var invoked bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/ai-gateway/capabilities":
			if r.Header.Get("X-Soha-Source") != "soha-mcp" {
				t.Fatalf("unexpected MCP source header %q", r.Header.Get("X-Soha-Source"))
			}
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"name":    "soha AI Gateway",
					"version": "v1alpha1",
					"tools": []map[string]any{{
						"name":             "delivery.applications.list",
						"description":      "List delivery applications",
						"riskLevel":        "read",
						"requiresApproval": false,
						"inputSchema":      map[string]any{"type": "object"},
					}},
				},
			})
		case "/api/v1/ai-gateway/tools/delivery.applications.list/invoke":
			invoked = true
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected invoke method %s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer profile-token" {
				t.Fatalf("unexpected Authorization header %q", r.Header.Get("Authorization"))
			}
			var req map[string]map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode invoke request: %v", err)
			}
			if req["input"]["search"] != "api" {
				t.Fatalf("unexpected invoke payload %#v", req)
			}
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"toolName":  "delivery.applications.list",
					"riskLevel": "read",
					"result":    "success",
					"output":    []map[string]any{{"id": "app-1", "name": "api"}},
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delivery.applications.list","arguments":{"search":"api"}}}`,
		"",
	}, "\n")
	var out bytes.Buffer
	code := Run(ctx, []string{"mcp", "start", "--profile", "dev"}, Runtime{
		In:         strings.NewReader(input),
		Out:        &out,
		Err:        &bytes.Buffer{},
		ConfigPath: writeTestConfig(t, server.URL),
	})
	if code != 0 {
		t.Fatalf("mcp start returned %d", code)
	}
	if !invoked {
		t.Fatalf("tool invocation endpoint was not called")
	}
	text := out.String()
	if !strings.Contains(text, `"tools"`) || !strings.Contains(text, "delivery.applications.list") {
		t.Fatalf("MCP tools/list response missing tool: %q", text)
	}
	if !strings.Contains(text, `"isError":false`) || !strings.Contains(text, "app-1") {
		t.Fatalf("MCP tools/call response missing successful result: %q", text)
	}
}

func TestRunMCPInstallUsesCurrentProfile(t *testing.T) {
	var out bytes.Buffer
	code := Run(context.Background(), []string{"mcp", "install", "--command", "/usr/local/bin/soha-cli"}, Runtime{
		Out:        &out,
		Err:        &bytes.Buffer{},
		ConfigPath: writeTestConfig(t, "https://soha.example"),
	})
	if code != 0 {
		t.Fatalf("mcp install returned %d", code)
	}
	text := out.String()
	for _, want := range []string{`"/usr/local/bin/soha-cli"`, `"--profile"`, `"dev"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("mcp install output missing %q in %q", want, text)
		}
	}
}

func TestRunSkillListAndInstall(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	dest := t.TempDir()
	writeSkill(t, source, "delivery-developer", "# Delivery Developer\n")
	writeSkill(t, source, "k8s-sre", "# K8s SRE\n")

	var listOut bytes.Buffer
	code := Run(ctx, []string{"skill", "list", "--source", source}, Runtime{Out: &listOut, Err: &bytes.Buffer{}})
	if code != 0 {
		t.Fatalf("skill list returned %d", code)
	}
	if listOut.String() != "delivery-developer\nk8s-sre\n" {
		t.Fatalf("unexpected skill list output %q", listOut.String())
	}

	var installOut bytes.Buffer
	code = Run(ctx, []string{"skill", "install", "--source", source, "--dest", dest, "delivery-developer"}, Runtime{
		Out: &installOut,
		Err: &bytes.Buffer{},
	})
	if code != 0 {
		t.Fatalf("skill install returned %d", code)
	}
	raw, err := os.ReadFile(filepath.Join(dest, "delivery-developer", "SKILL.md"))
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if string(raw) != "# Delivery Developer\n" {
		t.Fatalf("unexpected installed skill content %q", string(raw))
	}

	var errOut bytes.Buffer
	code = Run(ctx, []string{"skill", "install", "--source", source, "--dest", dest, "delivery-developer"}, Runtime{
		Out: &bytes.Buffer{},
		Err: &errOut,
	})
	if code == 0 {
		t.Fatalf("expected duplicate skill install to fail")
	}
	if !strings.Contains(errOut.String(), "--overwrite") {
		t.Fatalf("expected overwrite guidance, got %q", errOut.String())
	}
}

func writeTestConfig(t *testing.T, serverURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveConfig(path, Config{
		CurrentProfile: "dev",
		Profiles: map[string]ProfileConfig{
			"dev": {
				ServerURL:    serverURL,
				AccessToken:  "profile-token",
				UserID:       "u-1",
				UserName:     "Ada",
				AIClientID:   "profile-client",
				AIClientName: "Codex",
				SkillID:      "profile-skill",
				Source:       "profile-source",
			},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return path
}

func writeSkill(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("write JSON: %v", err)
	}
}
