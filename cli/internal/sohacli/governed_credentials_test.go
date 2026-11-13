package sohacli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func TestGovernedCredentialMintingFailsClosedAcrossServerVersions(t *testing.T) {
	for _, serviceAccount := range []bool{false, true} {
		for _, rotate := range []bool{false, true} {
			for _, scenario := range []string{"supported", "missing-capability", "capability-error", "old-instance", "downgraded-prefix", "changed-binding", "ordinary"} {
				name := scenario
				if serviceAccount {
					name += "/service"
				} else {
					name += "/personal"
				}
				if rotate {
					name += "/rotate"
				} else {
					name += "/create"
				}
				t.Run(name, func(t *testing.T) {
					metadata := map[string]any{"governedAI": map[string]any{"mode": "gateway_only", "aiClientId": "codex", "skillId": "k8s-sre"}}
					if scenario == "ordinary" {
						metadata = nil
					}
					var ordinaryWrites, governedWrites, optionsCalls, revocations atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/auth/cli/options") {
							optionsCalls.Add(1)
							if scenario == "capability-error" {
								w.WriteHeader(503)
								return
							}
							_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"supportsGovernedAI": scenario != "missing-capability"}})
							return
						}
						if r.Method == http.MethodGet {
							_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"id": "previous", "metadata": metadata}}})
							return
						}
						if strings.HasSuffix(r.URL.Path, "/revoke") {
							revocations.Add(1)
							_, _ = w.Write([]byte(`{"status":"ok"}`))
							return
						}
						if strings.Contains(r.URL.Path, "/governed-credentials/") {
							governedWrites.Add(1)
						} else {
							ordinaryWrites.Add(1)
						}
						if scenario == "old-instance" {
							w.WriteHeader(404)
							return
						}
						prefix := "soha_gpat_test"
						if serviceAccount {
							prefix = "soha_gsat_test"
						}
						if scenario == "downgraded-prefix" || scenario == "ordinary" {
							prefix = "soha_pat_test"
						}
						resultMetadata := metadata
						if scenario == "changed-binding" {
							resultMetadata = map[string]any{"governedAI": map[string]any{"mode": "gateway_only", "aiClientId": "spoofed", "skillId": "k8s-sre"}}
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"token": map[string]any{"id": "new", "tokenPrefix": prefix, "metadata": resultMetadata}, "value": prefix + "secret-never-print"}})
					}))
					defer server.Close()
					client := APIClient{ServerURL: server.URL, Client: server.Client(), Token: "management-test-key"}
					payload := map[string]any{"name": "test", "metadata": metadata}
					var err error
					if rotate {
						kind := "personal_access_token"
						if serviceAccount {
							kind = "service_account_token"
						}
						_, err = client.RotateGatewayCredential(t.Context(), kind, "previous", sohaapi.TokenRotationInput{})
					} else if serviceAccount {
						_, err = client.CreateServiceAccountToken(t.Context(), "account", payload, nil)
					} else {
						_, err = client.CreatePersonalAccessToken(t.Context(), payload, nil)
					}
					wantSuccess := scenario == "supported" || scenario == "ordinary"
					if (err == nil) != wantSuccess {
						t.Fatalf("result for %s: %v", name, err)
					}
					if err != nil && strings.Contains(err.Error(), "secret-never-print") {
						t.Fatal("one-time secret leaked in an error")
					}
					if scenario == "ordinary" {
						if ordinaryWrites.Load() != 1 || optionsCalls.Load() != 0 || governedWrites.Load() != 0 {
							t.Fatal("ordinary credential behavior changed")
						}
					} else {
						if ordinaryWrites.Load() != 0 || optionsCalls.Load() != 1 {
							t.Fatal("bound operation fell back to an ordinary route")
						}
						blocked := scenario == "missing-capability" || scenario == "capability-error"
						if (governedWrites.Load() == 0) != blocked {
							t.Fatal("capability failure did not stop minting")
						}
					}
					wantRevoke := scenario == "downgraded-prefix" || scenario == "changed-binding"
					if (revocations.Load() == 1) != wantRevoke {
						t.Fatal("unexpected credential was not revoked")
					}
				})
			}
		}
	}
}

func TestGovernedKeyImportUsesServerBoundIdentity(t *testing.T) {
	for _, spoof := range []bool{false, true} {
		t.Run(map[bool]string{false: "bound", true: "spoofed"}[spoof], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":{"credentialKind":"personal_access_token","subjectType":"user","subjectId":"u","displayName":"test","governedAI":{"mode":"gateway_only","aiClientId":"codex","skillId":"k8s-sre"}}}`))
			}))
			defer server.Close()
			path := authV2ConfigPath(t)
			var out, stderr bytes.Buffer
			args := []string{"login", "--server", server.URL, "--profile", "local", "--with-token", "--credential-store", "file"}
			if spoof {
				args = append(args, "--ai-client-id", "other")
			}
			code := Run(t.Context(), args, Runtime{ConfigPath: path, In: strings.NewReader("soha_gpat_synthetic-test\n"), Out: &out, Err: &stderr})
			if spoof {
				if code == 0 || !strings.Contains(stderr.String(), "governed_ai_identity_mismatch") {
					t.Fatal("spoofed client binding was accepted")
				}
				return
			}
			if code != 0 {
				t.Fatalf("import failed: %s", stderr.String())
			}
			cfg, err := loadConfig(path)
			if err != nil || cfg.Profiles["local"].AIClientID != "codex" || cfg.Profiles["local"].SkillID != "k8s-sre" {
				t.Fatal("server binding was not used in the saved profile", err)
			}
			if strings.Contains(out.String(), "synthetic-test") || strings.Contains(stderr.String(), "synthetic-test") {
				t.Fatal("Key was printed")
			}
		})
	}
}
