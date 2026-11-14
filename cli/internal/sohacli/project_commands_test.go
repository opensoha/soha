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
)

func TestValidateAndOrderProjectManifest(t *testing.T) {
	manifest := projectManifest{APIVersion: "opensoha.io/v1alpha1", Kind: "Project"}
	manifest.Metadata.Name = "demo"
	manifest.Spec.Steps = []projectStep{
		{ID: "app", Tool: "docker.projects.deploy.trigger", Input: map[string]any{}, DependsOn: []string{"vm"}},
		{ID: "vm", Tool: "virtualization.vms.create.trigger", Input: map[string]any{}},
	}

	ordered, err := validateAndOrderProjectManifest(manifest)
	if err != nil {
		t.Fatalf("validate manifest: %v", err)
	}
	if ordered[0].ID != "vm" || ordered[1].ID != "app" {
		t.Fatalf("unexpected step order: %#v", ordered)
	}

	manifest.Spec.Steps[1].Input = map[string]any{"nested": map[string]any{"password": "unsafe"}}
	if _, err := validateAndOrderProjectManifest(manifest); err == nil || !strings.Contains(err.Error(), "forbidden credential") {
		t.Fatalf("expected inline credential rejection, got %v", err)
	}

	manifest.Spec.Steps[1].Input = map[string]any{}
	manifest.Spec.Steps[1].SecretRefs = map[string]string{"TOKEN": "soha://secrets/demo-token"}
	if _, err := validateAndOrderProjectManifest(manifest); err != nil {
		t.Fatalf("valid secretRefs were rejected: %v", err)
	}
}

func TestRunProjectApplyPlansInOrderAndPausesForApproval(t *testing.T) {
	var invoked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/ai-gateway/capabilities":
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"name": "soha AI Gateway", "version": "v1alpha1",
				"tools": []map[string]any{
					{"name": "virtualization.vms.create.plan", "riskLevel": "analyze"},
					{"name": "virtualization.vms.create.trigger", "riskLevel": "execute", "requiresApproval": true},
					{"name": "docker.projects.deploy.plan", "riskLevel": "analyze"},
					{"name": "docker.projects.deploy.trigger", "riskLevel": "execute"},
				},
			}})
		case "/api/v1/ai-gateway/tools/virtualization.vms.create.plan/invoke",
			"/api/v1/ai-gateway/tools/docker.projects.deploy.plan/invoke":
			invoked = append(invoked, r.URL.Path)
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode plan invocation: %v", err)
			}
			if strings.Contains(r.URL.Path, "virtualization") {
				assertProjectSecretRefs(t, request)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"toolName": strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/ai-gateway/tools/"), "/invoke"),
				"result":   "success", "output": map[string]any{"ready": true},
			}})
		case "/api/v1/ai-gateway/tools/virtualization.vms.create.trigger/invoke":
			invoked = append(invoked, r.URL.Path)
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode invocation: %v", err)
			}
			input, _ := request["input"].(map[string]any)
			if request["requestId"] == "" || request["requestId"] != input["idempotencyKey"] {
				t.Fatalf("request id and idempotency key differ: %#v", request)
			}
			assertProjectSecretRefs(t, request)
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"toolName": "virtualization.vms.create.trigger", "result": "pending_approval",
				"requiresApproval": true, "output": map[string]any{"operationId": "operation-1"},
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	manifestPath := filepath.Join(t.TempDir(), "project.yaml")
	manifest := `apiVersion: opensoha.io/v1alpha1
kind: Project
metadata:
  name: demo
spec:
  steps:
    - id: app
      tool: docker.projects.deploy.trigger
      dependsOn: [vm]
      wait: false
      input:
        projectId: project-1
    - id: vm
      tool: virtualization.vms.create.trigger
      wait: false
      secretRefs:
        REGISTRY_TOKEN: soha://secrets/registry-token
      input:
        connectionId: connection-1
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	var out bytes.Buffer
	code := Run(context.Background(), []string{
		"project", "apply", manifestPath, "--profile", "dev", "--yes",
	}, Runtime{Out: &out, Err: &bytes.Buffer{}, ConfigPath: writeTestConfig(t, server.URL)})
	if code != 1 {
		t.Fatalf("project apply returned %d, output: %s", code, out.String())
	}
	if len(invoked) != 3 || !strings.Contains(invoked[0], "virtualization.vms.create.plan") ||
		!strings.Contains(invoked[1], "docker.projects.deploy.plan") ||
		!strings.Contains(invoked[2], "virtualization.vms.create.trigger") {
		t.Fatalf("unexpected invocation order: %#v", invoked)
	}
	if !strings.Contains(out.String(), `"status": "pending_approval"`) || strings.Contains(out.String(), "docker.projects.deploy.trigger") {
		t.Fatalf("approval did not pause downstream steps: %s", out.String())
	}
}

func assertProjectSecretRefs(t *testing.T, request map[string]any) {
	t.Helper()
	refs, _ := request["secretRefs"].(map[string]any)
	if refs["REGISTRY_TOKEN"] != "soha://secrets/registry-token" {
		t.Fatalf("secretRefs were not sent at the invocation top level: %#v", request)
	}
	input, _ := request["input"].(map[string]any)
	if _, nested := input["secretRefs"]; nested {
		t.Fatalf("secretRefs leaked into tool input: %#v", request)
	}
}
