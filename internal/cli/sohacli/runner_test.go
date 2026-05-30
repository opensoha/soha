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

func TestRunCapabilitiesJSONFlag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ai-gateway/capabilities" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"name":           "soha AI Gateway",
				"version":        "v1alpha1",
				"permissionKeys": []string{"ai.gateway.view"},
				"tools":          []map[string]any{{"name": "k8s.pods.list", "riskLevel": "read"}},
			},
		})
	}))
	defer server.Close()

	var out bytes.Buffer
	code := Run(context.Background(), []string{"capabilities", "--profile", "dev", "--json"}, Runtime{
		Out:        &out,
		Err:        &bytes.Buffer{},
		ConfigPath: writeTestConfig(t, server.URL),
	})
	if code != 0 {
		t.Fatalf("capabilities --json returned %d", code)
	}
	var manifest Manifest
	if err := json.Unmarshal(out.Bytes(), &manifest); err != nil {
		t.Fatalf("expected JSON manifest: %v\n%s", err, out.String())
	}
	if len(manifest.Tools) != 1 || manifest.Tools[0].Name != "k8s.pods.list" {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
}

func TestRunCapabilitiesInputsOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ai-gateway/capabilities" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"name":           "soha AI Gateway",
				"version":        "v1alpha1",
				"permissionKeys": []string{"ai.gateway.view"},
				"tools": []map[string]any{
					{
						"name": "k8s.pods.logs",
						"inputSchema": map[string]any{
							"type":     "object",
							"required": []any{"podName", "clusterId", "namespace"},
							"properties": map[string]any{
								"tailLines": map[string]any{"type": "integer"},
								"clusterId": map[string]any{"type": "string"},
								"namespace": map[string]any{"type": "string"},
								"podName":   map[string]any{"type": "string"},
								"previous":  map[string]any{"type": "boolean"},
							},
						},
						"outputSchema": map[string]any{
							"type":     "object",
							"required": []any{"lines"},
							"properties": map[string]any{
								"lines":   map[string]any{"type": "array"},
								"podName": map[string]any{"type": "string"},
							},
						},
					},
					{
						"name": "delivery.applications.list",
					},
				},
			},
		})
	}))
	defer server.Close()

	var out bytes.Buffer
	code := Run(context.Background(), []string{"capabilities", "--profile", "dev", "--output", "inputs"}, Runtime{
		Out:        &out,
		Err:        &bytes.Buffer{},
		ConfigPath: writeTestConfig(t, server.URL),
	})
	if code != 0 {
		t.Fatalf("capabilities --output inputs returned %d", code)
	}
	text := out.String()
	for _, want := range []string{
		"profile: dev",
		"tool\tk8s.pods.logs\trequired=clusterId,namespace,podName\tfields=clusterId,namespace,podName,previous,tailLines\toutputRequired=lines\toutputFields=lines,podName",
		"tool\tdelivery.applications.list\trequired=\tfields=\toutputRequired=\toutputFields=",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("capabilities inputs output missing %q in %q", want, text)
		}
	}
}

func TestRunToolCallReadsInputFileAndRedactsOutput(t *testing.T) {
	inputFile := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(inputFile, []byte(`{"clusterId":"cluster-a","password":"secret"}`), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	var invoked bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ai-gateway/tools/k8s.pods.list/invoke" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		invoked = true
		var req map[string]map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["input"]["clusterId"] != "cluster-a" {
			t.Fatalf("unexpected tool input %#v", req)
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{
			"toolName": "k8s.pods.list",
			"result":   "success",
			"output":   map[string]any{"message": "password=supersecret"},
		}})
	}))
	defer server.Close()

	var out bytes.Buffer
	code := Run(context.Background(), []string{"tool", "call", "k8s.pods.list", "--profile", "dev", "--input", inputFile}, Runtime{
		Out:        &out,
		Err:        &bytes.Buffer{},
		ConfigPath: writeTestConfig(t, server.URL),
	})
	if code != 0 {
		t.Fatalf("tool call returned %d", code)
	}
	if !invoked {
		t.Fatalf("tool invoke endpoint was not called")
	}
	if strings.Contains(out.String(), "supersecret") {
		t.Fatalf("tool output leaked sensitive value: %s", out.String())
	}
}

func TestRunResourceReadAndPromptGetProxyGatewayAPI(t *testing.T) {
	var resourceCalled bool
	var promptCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer profile-token" {
			t.Fatalf("unexpected Authorization header %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Soha-Source") != "profile-source" {
			t.Fatalf("unexpected source header %q", r.Header.Get("X-Soha-Source"))
		}
		switch r.URL.Path {
		case "/api/v1/ai-gateway/resources/read":
			resourceCalled = true
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode resource request: %v", err)
			}
			if req["uri"] != "soha://delivery/applications" {
				t.Fatalf("unexpected resource uri %#v", req)
			}
			contextValues, _ := req["context"].(map[string]any)
			if contextValues["applicationId"] != "app-1" {
				t.Fatalf("unexpected resource context %#v", req)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"name":     "soha://delivery/applications",
				"uri":      "soha://delivery/applications",
				"mimeType": "application/json",
				"text":     `{"summary":"token=secret"}`,
			}})
		case "/api/v1/ai-gateway/prompts/get":
			promptCalled = true
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode prompt request: %v", err)
			}
			if req["name"] != "soha.delivery.plan_release" {
				t.Fatalf("unexpected prompt name %#v", req)
			}
			arguments, _ := req["arguments"].(map[string]any)
			contextValues, _ := req["context"].(map[string]any)
			if arguments["applicationId"] != "app-1" || contextValues["environmentId"] != "dev" {
				t.Fatalf("unexpected prompt payload %#v", req)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"name":        "soha.delivery.plan_release",
				"description": "Plan release",
				"messages": []map[string]any{{
					"role":    "user",
					"content": "plan release token=secret",
				}},
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	configPath := writeTestConfig(t, server.URL)
	var resourceOut bytes.Buffer
	code := Run(context.Background(), []string{"resource", "read", "soha://delivery/applications", "--profile", "dev", "--context-json", `{"applicationId":"app-1"}`}, Runtime{
		Out:        &resourceOut,
		Err:        &bytes.Buffer{},
		ConfigPath: configPath,
	})
	if code != 0 {
		t.Fatalf("resource read returned %d", code)
	}
	if !resourceCalled {
		t.Fatalf("resource read endpoint was not called")
	}
	if strings.Contains(resourceOut.String(), "secret") {
		t.Fatalf("resource output leaked sensitive value: %s", resourceOut.String())
	}

	var promptOut bytes.Buffer
	code = Run(context.Background(), []string{"prompt", "get", "soha.delivery.plan_release", "--profile", "dev", "--arguments-json", `{"applicationId":"app-1"}`, "--context-json", `{"environmentId":"dev"}`}, Runtime{
		Out:        &promptOut,
		Err:        &bytes.Buffer{},
		ConfigPath: configPath,
	})
	if code != 0 {
		t.Fatalf("prompt get returned %d", code)
	}
	if !promptCalled {
		t.Fatalf("prompt get endpoint was not called")
	}
	if strings.Contains(promptOut.String(), "secret") {
		t.Fatalf("prompt output leaked sensitive value: %s", promptOut.String())
	}
	if !strings.Contains(promptOut.String(), "soha.delivery.plan_release") {
		t.Fatalf("prompt output missing prompt name: %s", promptOut.String())
	}
}

func TestRunTokenAndServiceAccountCommands(t *testing.T) {
	paths := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths[r.Method+" "+r.URL.Path]++
		switch r.URL.Path {
		case "/api/v1/ai-gateway/personal-access-tokens":
			switch r.Method {
			case http.MethodGet:
				writeJSON(t, w, map[string]any{"items": []map[string]any{{"id": "pat-1", "name": "local", "tokenPrefix": "soha_pat_abc"}}})
			case http.MethodPost:
				var req map[string]any
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatalf("decode token create: %v", err)
				}
				if req["name"] != "local" {
					t.Fatalf("unexpected token create payload %#v", req)
				}
				writeJSON(t, w, map[string]any{"data": map[string]any{
					"token": map[string]any{"id": "pat-2", "name": "local", "tokenPrefix": "soha_pat_def"},
					"value": "soha_pat_plain_once",
				}})
			default:
				t.Fatalf("unexpected token method %s", r.Method)
			}
		case "/api/v1/ai-gateway/personal-access-tokens/pat-1/revoke":
			writeJSON(t, w, map[string]any{"status": "ok"})
		case "/api/v1/ai-gateway/service-accounts":
			switch r.Method {
			case http.MethodGet:
				writeJSON(t, w, map[string]any{"items": []map[string]any{{"id": "sa-1", "name": "ci", "status": "active"}}})
			case http.MethodPost:
				writeJSON(t, w, map[string]any{"data": map[string]any{"id": "sa-2", "name": "ci", "status": "active"}})
			default:
				t.Fatalf("unexpected service account method %s", r.Method)
			}
		case "/api/v1/ai-gateway/service-account-tokens":
			writeJSON(t, w, map[string]any{"items": []map[string]any{{
				"id":               "sat-1",
				"serviceAccountId": "sa-2",
				"name":             "runner",
				"tokenPrefix":      "soha_sat_abc",
				"permissionKeys":   []string{"ai.gateway.invoke"},
			}}})
		case "/api/v1/ai-gateway/service-accounts/sa-2/tokens":
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"token": map[string]any{"id": "sat-1", "serviceAccountId": "sa-2", "name": "runner", "tokenPrefix": "soha_sat_abc"},
				"value": "soha_sat_plain_once",
			}})
		case "/api/v1/ai-gateway/service-account-tokens/sat-1/revoke":
			writeJSON(t, w, map[string]any{"status": "ok"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	configPath := writeTestConfig(t, server.URL)
	commands := [][]string{
		{"token", "list", "--profile", "dev"},
		{"token", "create", "--profile", "dev", "--name", "local", "--permission-keys", "ai.gateway.view"},
		{"token", "revoke", "--profile", "dev", "pat-1"},
		{"service-account", "list", "--profile", "dev"},
		{"service-account", "create", "--profile", "dev", "--name", "ci", "--role-ids", "operator"},
		{"service-account", "token-list", "--profile", "dev"},
		{"service-account", "token-create", "--profile", "dev", "--service-account-id", "sa-2", "--name", "runner"},
		{"service-account", "token-revoke", "--profile", "dev", "sat-1"},
	}
	for _, args := range commands {
		var out bytes.Buffer
		code := Run(context.Background(), args, Runtime{Out: &out, Err: &bytes.Buffer{}, ConfigPath: configPath})
		if code != 0 {
			t.Fatalf("%v returned %d", args, code)
		}
	}
	if paths["GET /api/v1/ai-gateway/personal-access-tokens"] != 1 ||
		paths["POST /api/v1/ai-gateway/personal-access-tokens"] != 1 ||
		paths["POST /api/v1/ai-gateway/personal-access-tokens/pat-1/revoke"] != 1 ||
		paths["GET /api/v1/ai-gateway/service-accounts"] != 1 ||
		paths["POST /api/v1/ai-gateway/service-accounts"] != 1 ||
		paths["GET /api/v1/ai-gateway/service-account-tokens"] != 1 ||
		paths["POST /api/v1/ai-gateway/service-accounts/sa-2/tokens"] != 1 ||
		paths["POST /api/v1/ai-gateway/service-account-tokens/sat-1/revoke"] != 1 {
		t.Fatalf("unexpected calls: %#v", paths)
	}
}

func TestRunAuditListAndDiagnoseHints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/ai-gateway/audit-logs":
			if r.URL.Query().Get("toolName") != "k8s.pods.logs" || r.URL.Query().Get("limit") != "10" {
				t.Fatalf("unexpected audit query %s", r.URL.RawQuery)
			}
			writeJSON(t, w, map[string]any{"items": []map[string]any{{
				"id":       "audit-1",
				"toolName": "k8s.pods.logs",
				"result":   "success",
				"summary":  "ok token=secret",
			}}})
		case "/api/v1/ai-gateway/capabilities":
			if r.Header.Get("X-Soha-AI-Client-ID") != "diag-client" {
				t.Fatalf("unexpected AI client id header %q", r.Header.Get("X-Soha-AI-Client-ID"))
			}
			if r.Header.Get("X-Soha-AI-Client") != "Diagnose Client" {
				t.Fatalf("unexpected AI client header %q", r.Header.Get("X-Soha-AI-Client"))
			}
			if r.Header.Get("X-Soha-Skill-ID") != "k8s-sre" {
				t.Fatalf("unexpected skill id header %q", r.Header.Get("X-Soha-Skill-ID"))
			}
			if r.Header.Get("X-Soha-Source") != "codex-mcp" {
				t.Fatalf("unexpected source header %q", r.Header.Get("X-Soha-Source"))
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"name":           "soha AI Gateway",
				"permissionKeys": []string{"ai.gateway.invoke"},
				"tools": []map[string]any{{
					"name":             "k8s.pods.logs",
					"domain":           "k8s",
					"action":           "logs",
					"riskLevel":        "read",
					"mcpAdapterId":     "k8s.v1",
					"mcpToolName":      "k8s.pods.logs",
					"requiresApproval": false,
					"permissionKeys":   []string{"ai.gateway.invoke", "platform.workloads.view"},
					"requiredScopes":   []string{"cluster", "namespace", "pod"},
					"inputSchema": map[string]any{
						"type":     "object",
						"required": []string{"clusterId", "namespace", "podName"},
						"properties": map[string]any{
							"clusterId":    map[string]any{"type": "string"},
							"namespace":    map[string]any{"type": "string"},
							"podName":      map[string]any{"type": "string"},
							"container":    map[string]any{"type": "string"},
							"tailLines":    map[string]any{"type": "integer"},
							"sinceSeconds": map[string]any{"type": "integer"},
							"previous":     map[string]any{"type": "boolean"},
						},
					},
					"outputSchema": map[string]any{
						"type":     "object",
						"required": []string{"lines"},
						"properties": map[string]any{
							"lines":   map[string]any{"type": "array"},
							"podName": map[string]any{"type": "string"},
						},
					},
				}},
				"resources": []map[string]any{{
					"name":           "soha://k8s/runtime",
					"description":    "Kubernetes runtime manifest",
					"permissionKeys": []string{"ai.gateway.invoke", "workspace.resource.view"},
					"requiredScopes": []string{"cluster", "namespace"},
					"contextSchema": map[string]any{
						"type":     "object",
						"required": []string{"clusterId"},
						"properties": map[string]any{
							"clusterId": map[string]any{"type": "string"},
							"namespace": map[string]any{"type": "string"},
							"podName":   map[string]any{"type": "string"},
						},
					},
				}},
				"prompts": []map[string]any{{
					"name":           "soha.k8s.diagnose_workload",
					"description":    "Diagnose workload",
					"permissionKeys": []string{"ai.gateway.invoke", "workspace.resource.view"},
					"requiredScopes": []string{"cluster", "namespace", "workload"},
					"argumentSchema": map[string]any{
						"type":     "object",
						"required": []string{"clusterId"},
						"properties": map[string]any{
							"clusterId": map[string]any{"type": "string"},
							"namespace": map[string]any{"type": "string"},
							"symptom":   map[string]any{"type": "string"},
						},
					},
					"contextSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"clusterId": map[string]any{"type": "string"},
							"namespace": map[string]any{"type": "string"},
						},
					},
				}},
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	configPath := writeTestConfig(t, server.URL)

	var auditOut bytes.Buffer
	code := Run(context.Background(), []string{"audit", "list", "--profile", "dev", "--tool-name", "k8s.pods.logs", "--limit", "10"}, Runtime{
		Out:        &auditOut,
		Err:        &bytes.Buffer{},
		ConfigPath: configPath,
	})
	if code != 0 {
		t.Fatalf("audit list returned %d", code)
	}
	if strings.Contains(auditOut.String(), "secret") {
		t.Fatalf("audit output leaked sensitive text: %s", auditOut.String())
	}

	var diagOut bytes.Buffer
	code = Run(context.Background(), []string{"diagnose", "--profile", "dev", "--tool", "k8s.pods.logs", "--ai-client-id", "diag-client", "--ai-client", "Diagnose Client", "--skill-id", "k8s-sre", "--source", "codex-mcp"}, Runtime{
		Out:        &diagOut,
		Err:        &bytes.Buffer{},
		ConfigPath: configPath,
	})
	if code != 0 {
		t.Fatalf("diagnose returned %d", code)
	}
	for _, want := range []string{"permissionKeys: 1", "aiClientId: diag-client", "aiClient: Diagnose Client", "skillId: k8s-sre", "source: codex-mcp", "domain: k8s", "action: logs", "mcpAdapterId: k8s.v1", "mcpToolName: k8s.pods.logs", "requiredPermissionKeys:", "inputRequired: clusterId,namespace,podName", "inputFields: clusterId,container,namespace,podName,previous,sinceSeconds,tailLines", "outputRequired: lines", "outputFields: lines,podName", "MCP tool grants", "skill bindings"} {
		if !strings.Contains(diagOut.String(), want) {
			t.Fatalf("diagnose output missing %q in %s", want, diagOut.String())
		}
	}

	var resourceDiagOut bytes.Buffer
	code = Run(context.Background(), []string{"diagnose", "--profile", "dev", "--resource", "soha://k8s/runtime", "--ai-client-id", "diag-client", "--ai-client", "Diagnose Client", "--skill-id", "k8s-sre", "--source", "codex-mcp"}, Runtime{
		Out:        &resourceDiagOut,
		Err:        &bytes.Buffer{},
		ConfigPath: configPath,
	})
	if code != 0 {
		t.Fatalf("diagnose resource returned %d", code)
	}
	for _, want := range []string{"resource: soha://k8s/runtime", "requiredPermissionKeys:", "requiredScopes: cluster,namespace", "contextRequired: clusterId", "contextFields: clusterId,namespace,podName", "resources/read", "context scope fields"} {
		if !strings.Contains(resourceDiagOut.String(), want) {
			t.Fatalf("resource diagnose output missing %q in %s", want, resourceDiagOut.String())
		}
	}

	var promptDiagOut bytes.Buffer
	code = Run(context.Background(), []string{"diagnose", "--profile", "dev", "--prompt", "soha.k8s.diagnose_workload", "--ai-client-id", "diag-client", "--ai-client", "Diagnose Client", "--skill-id", "k8s-sre", "--source", "codex-mcp"}, Runtime{
		Out:        &promptDiagOut,
		Err:        &bytes.Buffer{},
		ConfigPath: configPath,
	})
	if code != 0 {
		t.Fatalf("diagnose prompt returned %d", code)
	}
	for _, want := range []string{"prompt: soha.k8s.diagnose_workload", "requiredPermissionKeys:", "requiredScopes: cluster,namespace,workload", "argumentRequired: clusterId", "argumentFields: clusterId,namespace,symptom", "contextFields: clusterId,namespace", "prompts/get", "arguments/context"} {
		if !strings.Contains(promptDiagOut.String(), want) {
			t.Fatalf("prompt diagnose output missing %q in %s", want, promptDiagOut.String())
		}
	}
}

func TestRunGovernanceStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ai-gateway/governance/status" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("windowHours") != "48" {
			t.Fatalf("unexpected governance query %s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer profile-token" {
			t.Fatalf("unexpected Authorization header %q", r.Header.Get("Authorization"))
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{
			"generatedAt": "2026-05-29T00:00:00Z",
			"windowHours": 48,
			"health": map[string]any{
				"status":  "degraded",
				"message": "review token=secret",
				"checks": []map[string]any{{
					"name":    "high_risk_guardrails",
					"status":  "degraded",
					"message": "high risk token=secret",
					"count":   1,
				}},
			},
			"metrics": map[string]any{"totalCalls": 3, "successCount": 1, "denyCount": 1, "failureCount": 1, "pendingApprovalCount": 0, "dryRunCount": 0, "riskCounts": map[string]any{"mutate": 1}},
			"tokens": map[string]any{
				"personalAccessTokens":  map[string]any{"active": 1},
				"serviceAccountTokens":  map[string]any{"active": 2},
				"expiringSoon":          []map[string]any{{"id": "pat-1", "tokenPrefix": "soha_pat_abc", "daysUntilDue": 3}},
				"lastUsedTrackingState": "enabled",
			},
			"clients":         map[string]any{"total": 1, "active": 1, "pendingApproval": 1, "pendingApprovalClientIds": []string{"codex-local"}, "registrationApproval": "configured"},
			"approvals":       map[string]any{"pending": 2, "dueSoon": 1, "stalePending": 1, "overdue": 0, "oldestPendingHours": 25, "oldestPendingRequestId": "approval-stale", "nextDueAt": "2026-05-29T00:30:00Z", "nextDueRequestId": "approval-due", "dueSoonRequestIds": []string{"approval-due"}, "stalePendingRequestIds": []string{"approval-stale"}},
			"policyCoverage":  map[string]any{"accessPolicies": 2, "activeAccessPolicies": 1, "toolGrants": 2, "activeToolGrants": 1, "skillBindings": 2, "activeSkillBindings": 1, "budgetState": "configured", "rateLimitState": "configured", "redactionPolicyState": "configured", "resourceScopeState": "configured", "resourceScopedAccessPolicies": 1, "resourceScopedToolGrants": 1},
			"redaction":       map[string]any{"totalMatches": 5, "auditsWithRedaction": 2, "inputAudits": 1, "outputAudits": 1, "fieldMatches": 1, "sensitiveKeyMatches": 1, "sensitiveTextMatches": 0, "valuePatternMatches": 1, "secretClassifierMatches": 1, "structuredSecretMatches": 1, "topTargets": []map[string]any{{"key": "input", "count": 1}, {"key": "output", "count": 1}}, "topMatchTypes": []map[string]any{{"key": "secret_classifier", "count": 1}}, "topClassifiers": []map[string]any{{"key": "openai", "count": 1}}, "topFieldPaths": []map[string]any{{"key": "metadata.apiToken", "count": 1}}, "topPolicies": []map[string]any{{"key": "policy-redaction", "count": 2}}, "topTools": []map[string]any{{"key": "k8s.pods.logs", "count": 1}}},
			"anomalies":       []map[string]any{{"type": "high_risk_allow_without_approval", "severity": "warning", "summary": "failed token=secret", "count": 1, "subjectType": "role", "subjectId": "developer", "aiClientId": "codex-local", "policyId": "policy-risk-open", "riskLevel": "mutate"}, {"type": "approval_sla_due_soon", "severity": "warning", "summary": "approval waits", "count": 1, "approvalRequestId": "approval-due"}},
			"recommendations": []string{"rotate token=secret"},
			"recommendationActions": []map[string]any{{
				"type":       "high_risk_guardrails",
				"severity":   "warning",
				"summary":    "fix token=secret",
				"action":     "create_high_risk_approval_guardrail",
				"targetKind": "access_policies",
				"refs":       []string{"policy-risk-open"},
				"count":      1,
				"metadata":   map[string]any{"policyTemplate": "approval_guardrail"},
			}},
		}})
	}))
	defer server.Close()

	var out bytes.Buffer
	code := Run(context.Background(), []string{"governance", "status", "--profile", "dev", "--window-hours", "48"}, Runtime{
		Out:        &out,
		Err:        &bytes.Buffer{},
		ConfigPath: writeTestConfig(t, server.URL),
	})
	if code != 0 {
		t.Fatalf("governance status returned %d", code)
	}
	text := out.String()
	for _, want := range []string{"health: degraded", "healthCheck: degraded\thigh_risk_guardrails\tcount=1\thigh risk token=[REDACTED]", "calls: total=3", "approvals: pending=2 dueSoon=1 stale=1 overdue=0 oldestPendingHours=25 nextDue=2026-05-29T00:30:00Z", "policyCoverage:", "access=2 activeAccess=1", "grants=2 activeGrants=1", "skills=2 activeSkills=1", "resourceScopes=configured", "scopedAccess=1", "scopedGrants=1", "redaction: matches=5 audits=2 inputAudits=1 outputAudits=1", "redactionClassifiers: openai=1", "redactionPolicies: policy-redaction=2", "redactionTools: k8s.pods.logs=1", "finding: warning", "policy=policy-risk-open", "approval=approval-due", "subject=role:developer", "client=codex-local", "risk=mutate", "recommendationAction: warning", "action=create_high_risk_approval_guardrail", "target=access_policies", "refs=policy-risk-open"} {
		if !strings.Contains(text, want) {
			t.Fatalf("governance output missing %q in %s", want, text)
		}
	}
	if strings.Contains(text, "token=secret") {
		t.Fatalf("governance output leaked sensitive value: %s", text)
	}

	var jsonOut bytes.Buffer
	code = Run(context.Background(), []string{"governance", "status", "--profile", "dev", "--window-hours", "48", "--json"}, Runtime{
		Out:        &jsonOut,
		Err:        &bytes.Buffer{},
		ConfigPath: writeTestConfig(t, server.URL),
	})
	if code != 0 {
		t.Fatalf("governance status --json returned %d", code)
	}
	jsonText := jsonOut.String()
	for _, want := range []string{"\"riskCounts\"", "\"pendingApprovalClientIds\"", "\"daysUntilDue\"", "\"policyId\"", "\"riskLevel\"", "\"approvalRequestId\"", "\"approvals\"", "\"dueSoonRequestIds\"", "\"activeAccessPolicies\"", "\"activeToolGrants\"", "\"resourceScopeState\"", "\"resourceScopedAccessPolicies\"", "\"redaction\"", "\"auditsWithRedaction\"", "\"topClassifiers\"", "\"recommendationActions\"", "\"create_high_risk_approval_guardrail\"", "\"policyTemplate\""} {
		if !strings.Contains(jsonText, want) {
			t.Fatalf("governance JSON output missing %q in %s", want, jsonText)
		}
	}
	if strings.Contains(jsonText, "token=secret") {
		t.Fatalf("governance JSON output leaked sensitive value: %s", jsonText)
	}

	var invalidOut bytes.Buffer
	var invalidErr bytes.Buffer
	code = Run(context.Background(), []string{"governance", "status", "--profile", "dev", "--window-hours", "169"}, Runtime{
		Out:        &invalidOut,
		Err:        &invalidErr,
		ConfigPath: writeTestConfig(t, server.URL),
	})
	if code == 0 {
		t.Fatalf("governance status accepted invalid window")
	}
	if !strings.Contains(invalidErr.String(), "window-hours must be between 1 and 168") {
		t.Fatalf("governance status invalid window error mismatch: %q", invalidErr.String())
	}
}

func TestRunApprovalListAndApprove(t *testing.T) {
	var approved bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer profile-token" {
			t.Fatalf("unexpected Authorization header %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/v1/ai-gateway/approval-requests":
			if r.Method != http.MethodGet {
				t.Fatalf("unexpected approval list method %s", r.Method)
			}
			if r.URL.Query().Get("status") != "pending" || r.URL.Query().Get("toolName") != "delivery.actions.trigger" || r.URL.Query().Get("limit") != "5" {
				t.Fatalf("unexpected approval query %s", r.URL.RawQuery)
			}
			writeJSON(t, w, map[string]any{"items": []map[string]any{{
				"id":        "approval-1",
				"status":    "pending",
				"strategy":  "require_approval",
				"toolName":  "delivery.actions.trigger",
				"riskLevel": "execute",
				"summary":   "waiting token=secret",
				"toolInput": map[string]any{"token": "secret", "applicationId": "app-1"},
				"output":    map[string]any{"password": "secret"},
				"createdAt": "2026-05-29T00:00:00Z",
				"updatedAt": "2026-05-29T00:00:00Z",
			}}})
		case "/api/v1/ai-gateway/approval-requests/approval-1/approve":
			approved = true
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected approval decision method %s", r.Method)
			}
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode approval decision request: %v", err)
			}
			if req["comment"] != "ship it" {
				t.Fatalf("unexpected approval decision payload %#v", req)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"request": map[string]any{
					"id":              "approval-1",
					"status":          "executed",
					"strategy":        "require_approval",
					"toolName":        "delivery.actions.trigger",
					"riskLevel":       "execute",
					"summary":         "executed token=secret",
					"decisionComment": "ship token=secret",
					"output":          map[string]any{"authorization": "Bearer secret"},
					"createdAt":       "2026-05-29T00:00:00Z",
					"updatedAt":       "2026-05-29T00:01:00Z",
				},
				"invocation": map[string]any{
					"toolName":  "delivery.actions.trigger",
					"riskLevel": "execute",
					"result":    "success",
					"output":    map[string]any{"token": "secret", "releaseBundleId": "bundle-1"},
				},
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	configPath := writeTestConfig(t, server.URL)

	var listOut bytes.Buffer
	code := Run(context.Background(), []string{"approval", "list", "--profile", "dev", "--status", "pending", "--tool-name", "delivery.actions.trigger", "--limit", "5"}, Runtime{
		Out:        &listOut,
		Err:        &bytes.Buffer{},
		ConfigPath: configPath,
	})
	if code != 0 {
		t.Fatalf("approval list returned %d", code)
	}
	if !strings.Contains(listOut.String(), "approval-1") || strings.Contains(listOut.String(), "secret") {
		t.Fatalf("unexpected approval list output: %s", listOut.String())
	}

	var approveOut bytes.Buffer
	code = Run(context.Background(), []string{"approval", "approve", "approval-1", "--profile", "dev", "--comment", "ship it"}, Runtime{
		Out:        &approveOut,
		Err:        &bytes.Buffer{},
		ConfigPath: configPath,
	})
	if code != 0 {
		t.Fatalf("approval approve returned %d", code)
	}
	if !approved {
		t.Fatalf("approval decision endpoint was not called")
	}
	if !strings.Contains(approveOut.String(), "\"status\": \"executed\"") || !strings.Contains(approveOut.String(), "bundle-1") || strings.Contains(approveOut.String(), "secret") {
		t.Fatalf("unexpected approval approve output: %s", approveOut.String())
	}
}

func TestRunApprovalTimelineAndAuditFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer profile-token" {
			t.Fatalf("unexpected Authorization header %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/v1/ai-gateway/approval-requests/approval-1/timeline":
			if r.Method != http.MethodGet {
				t.Fatalf("unexpected approval timeline method %s", r.Method)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"request": map[string]any{
					"id":        "approval-1",
					"status":    "pending",
					"strategy":  "require_approval",
					"toolName":  "delivery.actions.trigger",
					"riskLevel": "execute",
					"summary":   "waiting",
					"createdAt": "2026-05-29T00:00:00Z",
					"updatedAt": "2026-05-29T00:01:00Z",
				},
				"trace": map[string]any{
					"approvalMode":        "all",
					"currentStageIndex":   1,
					"currentStageName":    "security",
					"workflowRunId":       "workflow-1",
					"pendingRequirements": []any{"stage:1:approvals:0/1"},
					"decisions": []any{map[string]any{
						"userId":  "release-1",
						"comment": "ship token=secret",
					}},
				},
				"events": []map[string]any{
					{
						"id":        "event-1",
						"kind":      "audit",
						"action":    "ai_gateway.approval.vote",
						"result":    "pending",
						"summary":   "vote token=secret",
						"metadata":  map[string]any{"token": "secret"},
						"createdAt": "2026-05-29T00:01:00Z",
					},
				},
			}})
		case "/api/v1/ai-gateway/audit-logs":
			if r.Method != http.MethodGet {
				t.Fatalf("unexpected audit list method %s", r.Method)
			}
			if r.URL.Query().Get("approvalRequestId") != "approval-1" {
				t.Fatalf("expected approvalRequestId query, got %s", r.URL.RawQuery)
			}
			writeJSON(t, w, map[string]any{"items": []map[string]any{{
				"id":        "audit-1",
				"action":    "ai_gateway.approval.vote",
				"result":    "pending",
				"summary":   "vote",
				"createdAt": "2026-05-29T00:01:00Z",
			}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	configPath := writeTestConfig(t, server.URL)

	var timelineOut bytes.Buffer
	code := Run(context.Background(), []string{"approval", "timeline", "approval-1", "--profile", "dev"}, Runtime{
		Out:        &timelineOut,
		Err:        &bytes.Buffer{},
		ConfigPath: configPath,
	})
	if code != 0 {
		t.Fatalf("approval timeline returned %d", code)
	}
	if !strings.Contains(timelineOut.String(), "workflow-1") || strings.Contains(timelineOut.String(), "secret") {
		t.Fatalf("unexpected approval timeline output: %s", timelineOut.String())
	}

	var auditOut bytes.Buffer
	code = Run(context.Background(), []string{"audit", "list", "--profile", "dev", "--approval-request-id", "approval-1"}, Runtime{
		Out:        &auditOut,
		Err:        &bytes.Buffer{},
		ConfigPath: configPath,
	})
	if code != 0 {
		t.Fatalf("audit list returned %d", code)
	}
	if !strings.Contains(auditOut.String(), "audit-1") {
		t.Fatalf("unexpected audit list output: %s", auditOut.String())
	}
}

func TestRunCompletionPrintsScript(t *testing.T) {
	var out bytes.Buffer
	code := Run(context.Background(), []string{"completion", "bash"}, Runtime{Out: &out, Err: &bytes.Buffer{}})
	if code != 0 {
		t.Fatalf("completion returned %d", code)
	}
	if !strings.Contains(out.String(), "complete -F _soha_cli_completion soha-cli") {
		t.Fatalf("unexpected completion output: %s", out.String())
	}
	if !strings.Contains(out.String(), "approval") {
		t.Fatalf("completion output missing approval command: %s", out.String())
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
						"title":            "List Applications",
						"name":             "delivery.applications.list",
						"description":      "List delivery applications",
						"domain":           "delivery",
						"action":           "list",
						"riskLevel":        "read",
						"mcpAdapterId":     "delivery.v1",
						"mcpToolName":      "delivery.applications.list",
						"requiresApproval": false,
						"permissionKeys":   []string{"ai.gateway.invoke", "delivery.applications.view"},
						"requiredScopes":   []string{"toolBusinessLine", "toolApplication"},
						"inputSchema": map[string]any{
							"type":       "object",
							"required":   []string{"applicationId"},
							"properties": map[string]any{"applicationId": map[string]any{"type": "string"}},
						},
						"outputSchema": map[string]any{
							"type":       "object",
							"properties": map[string]any{"items": map[string]any{"type": "array"}},
						},
					}},
					"resources": []map[string]any{{
						"name":           "soha://delivery/applications",
						"description":    "Delivery application manifest",
						"permissionKeys": []string{"ai.gateway.invoke", "delivery.applications.view"},
						"requiredScopes": []string{"businessLine", "application"},
						"contextSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"applicationId": map[string]any{"type": "string"},
								"environmentId": map[string]any{"type": "string"},
							},
						},
					}},
					"prompts": []map[string]any{{
						"name":           "soha.delivery.plan_release",
						"description":    "Plan release",
						"permissionKeys": []string{"ai.gateway.invoke", "delivery.applications.view"},
						"requiredScopes": []string{"application", "environment"},
						"argumentSchema": map[string]any{
							"type":     "object",
							"required": []string{"applicationId"},
							"properties": map[string]any{
								"applicationId": map[string]any{"type": "string", "description": "Delivery application id."},
								"environmentId": map[string]any{"type": "string", "description": "Target environment id."},
							},
						},
						"contextSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"applicationId": map[string]any{"type": "string"},
								"environmentId": map[string]any{"type": "string"},
							},
						},
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
		case "/api/v1/ai-gateway/resources/read":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected resource read method %s", r.Method)
			}
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode resource read request: %v", err)
			}
			if req["uri"] != "soha://delivery/applications" {
				t.Fatalf("unexpected resource read payload %#v", req)
			}
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"name":     "soha://delivery/applications",
					"uri":      "soha://delivery/applications",
					"mimeType": "application/json",
					"text":     `{"uri":"soha://delivery/applications","relatedTools":["delivery.applications.list"]}`,
				},
			})
		case "/api/v1/ai-gateway/prompts/get":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected prompt get method %s", r.Method)
			}
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode prompt get request: %v", err)
			}
			if req["name"] != "soha.delivery.plan_release" {
				t.Fatalf("unexpected prompt get payload %#v", req)
			}
			arguments, _ := req["arguments"].(map[string]any)
			contextValues, _ := req["context"].(map[string]any)
			if arguments["applicationId"] != "app-1" || contextValues["environmentId"] != "prod" {
				t.Fatalf("unexpected prompt get payload %#v", req)
			}
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"name":        "soha.delivery.plan_release",
					"description": "Plan release",
					"messages": []map[string]any{{
						"role":    "user",
						"content": "plan release with application app-1",
					}},
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":0,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delivery.applications.list","arguments":{"search":"api"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"soha://delivery/applications"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"prompts/list"}`,
		`{"jsonrpc":"2.0","id":6,"method":"prompts/get","params":{"name":"soha.delivery.plan_release","arguments":{"applicationId":"app-1"},"context":{"environmentId":"prod"}}}`,
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
	if !strings.Contains(text, `"serverInfo":{"name":"soha","version":"0.1.0"}`) || !strings.Contains(text, `"instructions":"soha MCP is a Gateway proxy.`) || !strings.Contains(text, `permission checks`) || !strings.Contains(text, `audit`) {
		t.Fatalf("MCP initialize response missing server info or instructions: %q", text)
	}
	if !strings.Contains(text, `"tools"`) || !strings.Contains(text, "delivery.applications.list") {
		t.Fatalf("MCP tools/list response missing tool: %q", text)
	}
	if !strings.Contains(text, `"inputSchema":{"properties":{"applicationId":{"type":"string"}},"required":["applicationId"],"type":"object"}`) {
		t.Fatalf("MCP tools/list response missing structured input schema: %q", text)
	}
	if !strings.Contains(text, `"outputSchema"`) || !strings.Contains(text, `"items":{"type":"array"}`) {
		t.Fatalf("MCP tools/list response missing structured output schema: %q", text)
	}
	if !strings.Contains(text, `"annotations":{"destructiveHint":false,"idempotentHint":true,"openWorldHint":true,"readOnlyHint":true,"title":"List Applications"}`) {
		t.Fatalf("MCP tools/list response missing tool annotations: %q", text)
	}
	for _, want := range []string{
		`"requiredScopes":["toolBusinessLine","toolApplication"]`,
		`"requiresApproval":false`,
		`"riskLevel":"read"`,
		`"domain":"delivery"`,
		`"action":"list"`,
		`"mcpAdapterId":"delivery.v1"`,
		`"mcpToolName":"delivery.applications.list"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("MCP tools/list response missing soha metadata %q: %q", want, text)
		}
	}
	if !strings.Contains(text, `"isError":false`) || !strings.Contains(text, "app-1") {
		t.Fatalf("MCP tools/call response missing successful result: %q", text)
	}
	if !strings.Contains(text, `"uri":"soha://delivery/applications"`) || !strings.Contains(text, `"contents"`) {
		t.Fatalf("MCP resources/read response missing resource content: %q", text)
	}
	if !strings.Contains(text, `"contextSchema"`) || !strings.Contains(text, `"environmentId":{"type":"string"}`) {
		t.Fatalf("MCP list response missing resource/prompt context schema: %q", text)
	}
	if !strings.Contains(text, `"permissionKeys":["ai.gateway.invoke","delivery.applications.view"]`) || !strings.Contains(text, `"requiredScopes":["businessLine","application"]`) {
		t.Fatalf("MCP resources/list response missing soha metadata: %q", text)
	}
	if !strings.Contains(text, `"requiredScopes":["application","environment"]`) || !strings.Contains(text, `"soha.delivery.plan_release"`) {
		t.Fatalf("MCP prompts/list response missing soha metadata: %q", text)
	}
	if !strings.Contains(text, `"arguments":[{"description":"Delivery application id.","name":"applicationId","required":true}`) || !strings.Contains(text, `"argumentSchema"`) {
		t.Fatalf("MCP prompts/list response missing prompt argument schema: %q", text)
	}
	if !strings.Contains(text, `"messages"`) || !strings.Contains(text, "plan release with application app-1") {
		t.Fatalf("MCP prompts/get response missing prompt message: %q", text)
	}
}

func TestRunMCPStartRejectsEmptyCapabilityNames(t *testing.T) {
	var backendCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalls++
		t.Fatalf("empty MCP params should not call backend path %s", r.URL.Path)
	}))
	defer server.Close()

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"arguments":{"search":"api"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"context":{"clusterId":"c1"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"prompts/get","params":{"arguments":{"applicationId":"app-1"}}}`,
		"",
	}, "\n")
	var out bytes.Buffer
	code := Run(context.Background(), []string{"mcp", "start", "--profile", "dev"}, Runtime{
		In:         strings.NewReader(input),
		Out:        &out,
		Err:        &bytes.Buffer{},
		ConfigPath: writeTestConfig(t, server.URL),
	})
	if code != 0 {
		t.Fatalf("mcp start returned %d", code)
	}
	if backendCalls != 0 {
		t.Fatalf("backend was called %d times", backendCalls)
	}
	text := out.String()
	for _, want := range []string{
		`"code":-32602`,
		`tools/call requires name`,
		`resources/read requires uri or name`,
		`prompts/get requires name`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("MCP validation output missing %q in %q", want, text)
		}
	}
}

func TestMCPToolAnnotationsMapRiskHints(t *testing.T) {
	for _, tc := range []struct {
		name            string
		riskLevel       string
		wantReadOnly    bool
		wantDestructive bool
		wantIdempotent  bool
	}{
		{name: "read", riskLevel: "read", wantReadOnly: true, wantIdempotent: true},
		{name: "analyze", riskLevel: "analyze"},
		{name: "mutate", riskLevel: "mutate", wantDestructive: true},
		{name: "execute", riskLevel: "execute", wantDestructive: true},
		{name: "high", riskLevel: "high", wantDestructive: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			annotations := mcpToolAnnotations(ToolCapability{
				Name:      "tool." + tc.name,
				Title:     "Tool " + tc.name,
				RiskLevel: tc.riskLevel,
			})

			if annotations["title"] != "Tool "+tc.name {
				t.Fatalf("title = %#v", annotations["title"])
			}
			if annotations["readOnlyHint"] != tc.wantReadOnly {
				t.Fatalf("readOnlyHint = %#v, want %v", annotations["readOnlyHint"], tc.wantReadOnly)
			}
			if annotations["destructiveHint"] != tc.wantDestructive {
				t.Fatalf("destructiveHint = %#v, want %v", annotations["destructiveHint"], tc.wantDestructive)
			}
			if annotations["idempotentHint"] != tc.wantIdempotent {
				t.Fatalf("idempotentHint = %#v, want %v", annotations["idempotentHint"], tc.wantIdempotent)
			}
			if annotations["openWorldHint"] != true {
				t.Fatalf("openWorldHint = %#v, want true", annotations["openWorldHint"])
			}
		})
	}
}

func TestRunMCPInstallUsesCurrentProfile(t *testing.T) {
	var out bytes.Buffer
	code := Run(context.Background(), []string{
		"mcp", "install",
		"--command", "/usr/local/bin/soha-cli",
		"--ai-client-id", "codex-local",
		"--ai-client", "Codex",
		"--skill-id", "k8s-sre",
	}, Runtime{
		Out:        &out,
		Err:        &bytes.Buffer{},
		ConfigPath: writeTestConfig(t, "https://soha.example"),
	})
	if code != 0 {
		t.Fatalf("mcp install returned %d", code)
	}
	text := out.String()
	for _, want := range []string{`"/usr/local/bin/soha-cli"`, `"--profile"`, `"dev"`, `"--ai-client-id"`, `"codex-local"`, `"--ai-client"`, `"Codex"`, `"--skill-id"`, `"k8s-sre"`} {
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
