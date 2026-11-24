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

func TestSignedRepositoryPreviewBlocksAndTrust(t *testing.T) {
	for _, tc := range []struct {
		name, eligibility, trust   string
		wantPreviews, wantInstalls int
	}{
		{"unsigned", "signed-release-required", "", 1, 0},
		{"untrusted", "publisher-trust-required", "", 1, 0},
		{"mismatch", "publisher-trust-required", "wrong", 1, 0},
		{"acknowledged", "publisher-trust-required", "sha256:abc", 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previews, installs := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/plugins/repository/preview":
					previews++
					var req map[string]any
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Fatal(err)
					}
					p := pluginRepositoryPreviewFixture("preview-only")
					p["installEligibility"] = tc.eligibility
					p["publisherKeyFingerprint"] = "sha256:abc"
					if previews == 2 {
						if req["trustPublisherFingerprint"] != "sha256:abc" {
							t.Fatalf("missing acknowledgement: %#v", req)
						}
						p["installEligibility"] = "confirmable"
						p["previewToken"] = "token"
					}
					writeJSON(t, w, map[string]any{"data": p})
				case "/api/v1/plugins/repository/install":
					installs++
					writeJSON(t, w, map[string]any{"data": installedPluginFixture("installed")})
				default:
					t.Fatalf("unexpected request %s", r.URL.Path)
				}
			}))
			defer server.Close()
			args := []string{"plugin", "install", "--profile", "dev", "--repository", "https://github.com/example/plugin", "--yes"}
			if tc.trust != "" {
				args = append(args, "--trust-publisher", tc.trust)
			}
			var out, errOut bytes.Buffer
			code := Run(context.Background(), args, Runtime{Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			if previews != tc.wantPreviews || installs != tc.wantInstalls || (code == 0) != (tc.wantInstalls == 1) || !strings.Contains(out.String(), "sha256:abc") {
				t.Fatalf("code=%d previews=%d installs=%d out=%s err=%s", code, previews, installs, out.String(), errOut.String())
			}
		})
	}
}

func TestPluginRepositoryRollbackRequest(t *testing.T) {
	for _, tc := range []struct {
		name, installedVersion string
		wantPosts              int
	}{{"matching", "2", 1}, {"stale-version", "3", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			gets, posts := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/api/v1/plugins/example.plugin" {
					gets++
					item := installedPluginFixture("installed")
					item["version"] = tc.installedVersion
					item["updatedAt"] = "2026-11-23T12:34:56.123456Z"
					writeJSON(t, w, map[string]any{"data": item})
					return
				}
				posts++
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/plugins/example.plugin/repository/rollback" {
					t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body["expectedVersion"] != "2" || body["expectedUpdatedAt"] != "2026-11-23T12:34:56.123456Z" || body["version"] != "1" || body["confirmPermissionIncrease"] != true {
					t.Fatalf("body %#v", body)
				}
				writeJSON(t, w, map[string]any{"data": installedPluginFixture("installed")})
			}))
			defer server.Close()
			var out, errOut bytes.Buffer
			code := Run(context.Background(), []string{"plugin", "rollback", "--profile", "dev", "--expected-version", "2", "--version", "1", "--confirm-permission-increase", "--yes", "example.plugin"}, Runtime{Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			if gets != 1 || posts != tc.wantPosts || (code == 0) != (tc.wantPosts == 1) {
				t.Fatalf("code=%d gets=%d posts=%d stderr=%s", code, gets, posts, errOut.String())
			}
			if tc.wantPosts == 1 && !strings.Contains(errOut.String(), "2026-11-23T12:34:56.123456Z") {
				t.Fatalf("missing reviewed timestamp: %s", errOut.String())
			}
		})
	}
}

func TestSignedRepositoryInstallRequiresSeparateEnable(t *testing.T) {
	installs := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/plugins/repository/install" {
			installs++
			return
		}
		p := pluginRepositoryPreviewFixture("token")
		p["installEligibility"] = "confirmable"
		p["releaseAttestation"] = map[string]any{"components": map[string]any{"backend": map[string]any{}}}
		writeJSON(t, w, map[string]any{"data": p})
	}))
	defer server.Close()
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"plugin", "install", "--profile", "dev", "--repository", "https://github.com/example/plugin", "--enable", "--yes"}, Runtime{Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
	if code == 0 || installs != 0 || !strings.Contains(errOut.String(), "enable them separately") {
		t.Fatalf("code=%d installs=%d stderr=%s", code, installs, errOut.String())
	}
}
