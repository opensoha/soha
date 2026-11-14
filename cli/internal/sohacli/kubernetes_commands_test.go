package sohacli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKubernetesCommandsUseNativeTypedRoutesAndExplicitIdentity(t *testing.T) {
	for _, action := range []string{"list", "identity", "get", "yaml", "apply", "delete"} {
		t.Run(action, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("Authorization") == "" {
					t.Error("native request did not authenticate")
				}
				if r.URL.Query().Get("namespace") != "team-a" {
					t.Errorf("namespace changed: %s", r.URL.RawQuery)
				}
				base := "/api/v1/clusters/cluster-a/"
				switch action {
				case "list":
					if r.Method != "GET" || r.URL.Path != base+"workloads/deployments" {
						t.Errorf("list: %s %s", r.Method, r.URL.Path)
					}
					_, _ = fmt.Fprint(w, `{"items":[]}`)
				case "get", "yaml":
					suffix := "detail"
					if action == "yaml" {
						suffix = "yaml"
					}
					if r.URL.Path != base+"workloads/deployments/app/"+suffix || r.Method != "GET" {
						t.Errorf("read: %s %s", r.Method, r.URL.Path)
					}
					_, _ = fmt.Fprint(w, `{"data":{}}`)
				case "identity":
					if r.URL.Path != base+"resources/Deployment/app/identity" {
						t.Errorf("identity: %s", r.URL.Path)
					}
					_, _ = fmt.Fprint(w, `{"data":{"uid":"uid","resourceVersion":"rv"}}`)
				case "apply":
					var body map[string]any
					if json.NewDecoder(r.Body).Decode(&body) != nil {
						t.Fatal("missing request")
					}
					guard, _ := body["preconditions"].(map[string]any)
					if guard["expectedUid"] != "uid" || guard["expectedResourceVersion"] != "opaque/rv+1" {
						t.Errorf("guard changed: %#v", guard)
					}
					if calls == 1 {
						if r.Method != "POST" || r.URL.Path != base+"resources/Deployment/app/conditional/yaml/preflight" {
							t.Errorf("preview: %s %s", r.Method, r.URL.Path)
						}
						_, _ = fmt.Fprint(w, `{"data":{"ready":true,"requiresApproval":false}}`)
					} else {
						if r.Method != "PUT" || r.URL.Path != base+"resources/Deployment/app/conditional/yaml" {
							t.Errorf("apply fallback: %s %s", r.Method, r.URL.Path)
						}
						_, _ = fmt.Fprint(w, `{"data":{"content":"SENSITIVE_MANIFEST"}}`)
					}
				case "delete":
					if r.Method != "DELETE" || r.URL.Path != base+"resources/Deployment/app/conditional" || r.URL.Query().Get("expectedUid") != "uid" || r.URL.Query().Get("expectedResourceVersion") != "opaque/rv+1" {
						t.Errorf("delete guard: %s %s", r.Method, r.URL.String())
					}
					w.WriteHeader(202)
					_, _ = fmt.Fprint(w, `{"data":{"accepted":true,"state":"deletion_requested","kind":"Deployment","name":"app","namespace":"team-a","preconditions":{"expectedUid":"uid","expectedResourceVersion":"opaque/rv+1"}}}`)
				}
			}))
			defer server.Close()
			args := []string{"k8s", "resources", action, "Deployment"}
			if action != "list" {
				args = append(args, "app")
			}
			args = append(args, "--cluster-id", "cluster-a", "--namespace", "team-a", "--profile", "dev")
			if action == "apply" || action == "delete" {
				args = append(args, "--expected-uid", "uid", "--expected-resource-version", "opaque/rv+1", "--yes")
			}
			if action == "apply" {
				args = append(args, "--file", "-")
			}
			var out, errOut bytes.Buffer
			code := Run(t.Context(), args, Runtime{In: strings.NewReader("kind: Deployment\nmetadata:\n  name: app\n"), Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 0 {
				t.Fatalf("code=%d stderr=%s", code, errOut.String())
			}
			if strings.Contains(out.String(), "SENSITIVE_MANIFEST") {
				t.Fatal("write receipt echoed manifest")
			}
			want := 1
			if action == "apply" {
				want = 2
			}
			if calls != want {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}

func TestKubernetesRejectsUnsafeFlagsAndNeverFallsBack(t *testing.T) {
	for _, test := range []struct {
		name          string
		args          []string
		status, calls int
	}{
		{"missing guard", []string{"apply", "Deployment", "app", "--file", "-", "--yes"}, 0, 0},
		{"all namespace delete", []string{"delete", "Deployment", "app", "--all-namespaces"}, 0, 0},
		{"secret implicit YAML", []string{"yaml", "Secret", "app"}, 0, 0},
		{"path target", []string{"get", "Deployment", "../app"}, 0, 0},
		{"old server", []string{"delete", "Deployment", "app", "--expected-uid", "uid", "--expected-resource-version", "rv", "--yes"}, 404, 1},
		{"forbidden", []string{"delete", "Deployment", "app", "--expected-uid", "uid", "--expected-resource-version", "rv", "--yes"}, 403, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = fmt.Fprint(w, `{"error":{"code":"denied","message":"denied"}}`)
			}))
			defer server.Close()
			args := append([]string{"k8s", "resources"}, test.args...)
			args = append(args, "--cluster-id", "cluster-a", "--profile", "dev")
			var out, errOut bytes.Buffer
			if code := Run(t.Context(), args, Runtime{In: strings.NewReader("kind: Deployment"), Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)}); code == 0 {
				t.Fatal("unsafe command succeeded")
			}
			if calls != test.calls {
				t.Fatalf("calls=%d want %d", calls, test.calls)
			}
		})
	}
}

func TestKubernetesCreateRetriesOriginalDurableRequestWithoutNewPreflight(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprint(retry), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/preflight") {
					if retry {
						t.Error("receipt recovery was blocked by a second preflight")
					}
					_, _ = fmt.Fprint(w, `{"data":{"ready":true}}`)
					return
				}
				if r.URL.Path != "/api/v1/clusters/cluster-a/resource-creation/execute" || r.Header.Get("Idempotency-Key") != "original-key" {
					t.Errorf("changed original request: %s", r.URL.String())
				}
				_, _ = fmt.Fprintf(w, `{"data":{"status":"succeeded","operationId":"operation-original","contentHash":"%s","items":[]}}`, strings.Repeat("a", 64))
			}))
			defer server.Close()
			args := []string{"k8s", "create", "execute", "--cluster-id", "cluster-a", "--file", "-", "--idempotency-key", "original-key", "--yes", "--profile", "dev"}
			if retry {
				args = append(args, "--retry-original")
			}
			var out, errOut bytes.Buffer
			if code := Run(t.Context(), args, Runtime{In: strings.NewReader("kind: ConfigMap\nmetadata:\n  name: app\n"), Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)}); code != 0 {
				t.Fatalf("code=%d stderr=%s", code, errOut.String())
			}
			want := 2
			if retry {
				want = 1
			}
			if calls != want {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}

func TestKubernetesExplicitYAMLReadPreservesTheAuthorizedManifest(t *testing.T) {
	content := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: app\n  namespace: default\nstringData:\n  password: fixture-resource-value\n"
	for _, format := range []string{"json", "yaml", "raw"} {
		t.Run(format, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/clusters/cluster-a/configuration/secrets/app/yaml" {
					t.Errorf("secret read path=%s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"kind": "Secret", "name": "app", "namespace": "default", "content": content}})
			}))
			defer server.Close()
			var out, errOut bytes.Buffer
			code := Run(t.Context(), []string{"k8s", "resources", "yaml", "Secret", "app", "--cluster-id", "cluster-a", "--show-secret-data", "--output", format, "--profile", "dev"}, Runtime{Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 0 {
				t.Fatalf("code=%d stderr=%s", code, errOut.String())
			}
			if format == "raw" {
				if out.String() != content {
					t.Fatal("raw manifest changed")
				}
			} else if !strings.Contains(out.String(), "fixture-resource-value") {
				t.Fatal("authorized YAML was silently redacted before export")
			}
		})
	}
}
