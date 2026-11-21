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

func TestRepositoryBranchSelectionPagesAndPreservesConfirmation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/plugins/repository/refs" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			RepositoryURL string `json:"repositoryUrl"`
			Kind          string `json:"kind"`
			Page          int    `json:"page"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.RepositoryURL != "https://github.com/example/plugin" || body.Kind != "branch" {
			t.Errorf("unexpected body: %+v", body)
		}
		if body.Page == 1 {
			_, _ = w.Write([]byte(`{"data":{"refs":[{"name":"main"}],"hasMore":true}}`))
			return
		}
		if body.Page != 2 {
			t.Errorf("unexpected page %d", body.Page)
		}
		_, _ = w.Write([]byte(`{"data":{"refs":[{"name":"release/stable"}],"hasMore":false}}`))
	}))
	defer server.Close()
	var output bytes.Buffer
	rt := Runtime{In: strings.NewReader("n\n1\ny\n"), Err: &output}
	ref, err := selectRepositoryBranch(context.Background(), rt, APIClient{ServerURL: server.URL, Client: server.Client()}, "https://github.com/example/plugin", nil)
	if err != nil || ref != "release/stable" {
		t.Fatalf("selection = %q, %v", ref, err)
	}
	if !strings.Contains(output.String(), "main") || !strings.Contains(output.String(), "release/stable") {
		t.Fatalf("branches missing: %s", &output)
	}
	ok, err := confirmAction(rt, "confirm")
	if err != nil || !ok {
		t.Fatalf("confirmation lost: %v %v", ok, err)
	}
}

func TestRepositoryBranchRequiredNoninteractive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"plugin_repository_branch_required","message":"branch required"}}`))
	}))
	defer server.Close()
	_, err := previewRepositoryPlugin(context.Background(), Runtime{In: strings.NewReader("")}, APIClient{ServerURL: server.URL, Client: server.Client()}, PluginRepositoryPreviewRequest{RepositoryURL: "https://github.com/example/plugin"}, nil)
	if err == nil || !strings.Contains(err.Error(), "--ref") {
		t.Fatalf("expected actionable error, got %v", err)
	}
}

func TestRepositoryMarketplaceUpgradeDiscoversRelease(t *testing.T) {
	for _, ref := range []string{"", "refs/heads/stable"} {
		t.Run(ref, func(t *testing.T) {
			previews, upgrades := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/plugins/example.plugin":
					installed := installedPluginFixture("installed")
					installed["sourceInfo"] = map[string]any{"kind": "marketplace", "canonicalSource": "https://marketplace.example", "sourceId": "official", "repositoryUrl": "https://github.com/example/plugin", "pinnedRef": "v1.0.0"}
					writeJSON(t, w, map[string]any{"data": installed})
				case "/api/v1/plugins/repository/preview":
					var input PluginRepositoryPreviewRequest
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Fatal(err)
					}
					if input.RepositoryURL != "https://github.com/example/plugin" || input.MarketplaceSourceID != "official" || input.PluginID != "example.plugin" || input.Ref != ref {
						t.Fatalf("preview input: %+v", input)
					}
					preview := pluginRepositoryPreviewFixture("preview-token")
					preview["manifest"].(map[string]any)["id"] = "example.plugin"
					writeJSON(t, w, map[string]any{"data": preview})
					previews++
				case "/api/v1/plugins/example.plugin/repository/upgrade":
					writeJSON(t, w, map[string]any{"data": installedPluginFixture("installed")})
					upgrades++
				default:
					t.Fatalf("unexpected route %s", r.URL.Path)
				}
			}))
			defer server.Close()
			var out, errOut bytes.Buffer
			args := []string{"plugin", "upgrade", "--profile", "dev", "--repository", "--yes"}
			if ref != "" {
				args = append(args, "--ref", ref)
			}
			args = append(args, "example.plugin")
			code := Run(context.Background(), args, Runtime{Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 0 || previews != 1 || upgrades != 1 {
				t.Fatalf("code=%d previews=%d upgrades=%d stderr=%s", code, previews, upgrades, &errOut)
			}
		})
	}
}
