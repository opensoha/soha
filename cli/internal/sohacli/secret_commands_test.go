package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecretCreateAndRotateKeepValuesWriteOnly(t *testing.T) {
	const secretValue = "  opaque value  "
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests++
		if req.Header.Get("Authorization") != "Bearer profile-token" {
			t.Fatalf("authorization = %q", req.Header.Get("Authorization"))
		}
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/api/v1/secrets":
			var input SecretCreateRequest
			if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.Value != secretValue || input.Name != "registry-token" || len(input.Bindings) != 1 {
				t.Fatalf("create input = %#v", input)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"id": "secret-1", "name": input.Name, "scopeType": input.ScopeType, "scopeId": input.ScopeID, "status": "active", "currentVersion": 1, "bindings": input.Bindings}})
		case req.Method == http.MethodPost && req.URL.Path == "/api/v1/secrets/secret-1/versions":
			var input SecretRotateRequest
			if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.Value != secretValue {
				t.Fatalf("rotate value = %q", input.Value)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"secretId": "secret-1", "version": 2, "status": "active"}})
		default:
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
		}
	}))
	defer server.Close()

	configPath := writeTestConfig(t, server.URL)
	for _, args := range [][]string{
		{"secret", "create", "--profile", "dev", "--name", "registry-token", "--scope-type", "project", "--scope-id", "demo", "--binding", "capability=docker.projects.deploy.trigger"},
		{"secret", "rotate", "secret-1", "--profile", "dev"},
	} {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), args, Runtime{In: strings.NewReader(secretValue + "\n"), Out: &out, Err: &errOut, ConfigPath: configPath})
		if code != 0 {
			t.Fatalf("%v returned %d: %s", args, code, errOut.String())
		}
		if strings.Contains(out.String()+errOut.String(), secretValue) {
			t.Fatalf("%v leaked plaintext: %s%s", args, out.String(), errOut.String())
		}
	}
	if requests != 2 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestSecretCreateAndRotateAcceptVaultKV2LocatorsWithoutReadingAValue(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests++
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/api/v1/secrets":
			var input SecretCreateRequest
			if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.Value != "" || input.VaultKv2 == nil || *input.VaultKv2 != (SecretVaultKV2Reference{Mount: "secret", Path: "demo/app", Key: " token ", Version: 3}) {
				t.Fatalf("create input = %#v", input)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"id": "secret-1", "name": input.Name, "scopeType": input.ScopeType, "scopeId": input.ScopeID, "status": "active", "currentVersion": 1, "bindings": input.Bindings}})
		case req.Method == http.MethodPost && req.URL.Path == "/api/v1/secrets/secret-1/versions":
			var input SecretRotateRequest
			if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.Value != "" || input.VaultKv2 == nil || *input.VaultKv2 != (SecretVaultKV2Reference{Mount: "secret", Path: "demo/app", Key: " token ", Version: 4}) {
				t.Fatalf("rotate input = %#v", input)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"secretId": "secret-1", "version": 2, "status": "active"}})
		default:
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
		}
	}))
	defer server.Close()

	configPath := writeTestConfig(t, server.URL)
	for _, args := range [][]string{
		{"secret", "create", "--profile", "dev", "--name", "registry-token", "--vault-mount", "secret", "--vault-path", "demo/app", "--vault-key", " token ", "--vault-version", "3"},
		{"secret", "rotate", "--profile", "dev", "secret-1", "--vault-mount", "secret", "--vault-path", "demo/app", "--vault-key", " token ", "--vault-version", "4"},
	} {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), args, Runtime{In: strings.NewReader(""), Out: &out, Err: &errOut, ConfigPath: configPath})
		if code != 0 {
			t.Fatalf("%v returned %d: %s", args, code, errOut.String())
		}
		if strings.Contains(errOut.String(), "Secret value:") {
			t.Fatalf("%v prompted for a local value", args)
		}
	}
	if requests != 2 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestSecretVaultKV2LocatorFlagsFailClosedWhenPartial(t *testing.T) {
	var errOut bytes.Buffer
	code := Run(context.Background(), []string{"secret", "create", "--name", "registry-token", "--vault-mount", "secret"}, Runtime{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &errOut,
	})
	if code != 1 || !strings.Contains(errOut.String(), "must be provided together") {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
	if strings.Contains(errOut.String(), "Secret value:") {
		t.Fatalf("partial locator prompted for a local value: %q", errOut.String())
	}
}

func TestToolCallSendsSecretRefsOutsideInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/v1/ai-gateway/capabilities":
			writeJSON(t, w, map[string]any{"data": map[string]any{"tools": []map[string]any{{"name": "docker.projects.deploy.trigger", "riskLevel": "execute"}}}})
		case "/api/v1/ai-gateway/tools/docker.projects.deploy.trigger/invoke":
			var input map[string]any
			if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			refs, _ := input["secretRefs"].(map[string]any)
			arguments, _ := input["input"].(map[string]any)
			if refs["REGISTRY_TOKEN"] != "soha://secrets/registry-token" {
				t.Fatalf("secretRefs = %#v", refs)
			}
			if _, nested := arguments["secretRefs"]; nested {
				t.Fatalf("secretRefs nested in input: %#v", input)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"toolName": "docker.projects.deploy.trigger", "result": "success"}})
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
	}))
	defer server.Close()

	code := Run(context.Background(), []string{
		"tool", "call", "docker.projects.deploy.trigger", "--profile", "dev", "--yes",
		"--secret-ref", "REGISTRY_TOKEN=soha://secrets/registry-token",
	}, Runtime{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, ConfigPath: writeTestConfig(t, server.URL)})
	if code != 0 {
		t.Fatalf("tool call returned %d", code)
	}
}
