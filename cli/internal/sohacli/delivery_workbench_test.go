package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const deliveryTaskFixture = `{"id":"task-1","applicationId":"app-1","applicationEnvironmentId":"binding-1","taskKind":"release","providerKind":"ci_agent_runner","status":"queued","payload":{},"callbackToken":""}`

func runDeliveryTest(t *testing.T, ctx context.Context, server, input string, args ...string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	args = append(append([]string{"delivery"}, args...), "--profile", "dev")
	code := Run(ctx, args, Runtime{In: strings.NewReader(input), Out: &out, Err: &errs, ConfigPath: writeTestConfig(t, server)})
	return code, out.String(), errs.String()
}

func TestDeliveryWorkbenchHTTPAndTypedInputs(t *testing.T) {
	app := `{"id":"app-1","name":"API","key":"api","enabled":true}`
	service := `{"id":"service-1","name":"API","key":"api","serviceKind":"kubernetes_workload","enabled":true}`
	environment := `{"id":"binding-1","applicationId":"app-1","environmentId":"dev"}`
	for _, test := range []struct {
		args                     []string
		method, path, input, out string
	}{
		{[]string{"applications", "list", "--search", "api x", "--limit", "10"}, "GET", "/api/v1/applications?limit=10&search=api+x", "", `{"items":[{"id":"app-1"}]}`},
		{[]string{"applications", "get", "app-1"}, "GET", "/api/v1/applications/app-1", "", `{"data":{"id":"app-1"}}`},
		{[]string{"applications", "detail", "app-1"}, "GET", "/api/v1/applications/app-1/detail", "", `{"data":{"application":{"id":"app-1"}}}`},
		{[]string{"applications", "runtime", "app-1"}, "GET", "/api/v1/applications/app-1/runtime", "", `{"data":{"application":{"id":"app-1"}}}`},
		{[]string{"applications", "create", "--input", "-", "--yes"}, "POST", "/api/v1/applications", app, `{"data":{"id":"app-1"}}`},
		{[]string{"applications", "update", "app-1", "--input", "-", "--yes"}, "PUT", "/api/v1/applications/app-1", strings.TrimSuffix(app, "}") + `,"expectedVersion":2}`, `{"data":{"id":"app-1"}}`},
		{[]string{"applications", "delete", "app-1", "--yes"}, "DELETE", "/api/v1/applications/app-1", "", `{"status":"ok"}`},
		{[]string{"services", "list", "--application-id", "app-1"}, "GET", "/api/v1/applications/app-1/services", "", `{"data":[]}`},
		{[]string{"services", "get", "service-1", "--application-id", "app-1"}, "GET", "/api/v1/applications/app-1/services/service-1", "", `{"data":{"id":"service-1"}}`},
		{[]string{"services", "create", "--application-id", "app-1", "--input", "-", "--yes"}, "POST", "/api/v1/applications/app-1/services", service, `{"data":{"id":"service-1"}}`},
		{[]string{"services", "update", "service-1", "--application-id", "app-1", "--input", "-", "--yes"}, "PUT", "/api/v1/applications/app-1/services/service-1", strings.TrimSuffix(service, "}") + `,"expectedVersion":2}`, `{"data":{"id":"service-1"}}`},
		{[]string{"services", "delete", "service-1", "--application-id", "app-1", "--yes"}, "DELETE", "/api/v1/applications/app-1/services/service-1", "", `{"status":"ok"}`},
		{[]string{"environments", "list", "--application-id", "app-1", "--environment-id", "dev"}, "GET", "/api/v1/application-environments?applicationId=app-1&environmentId=dev", "", `{"items":[]}`},
		{[]string{"environments", "get", "binding-1"}, "GET", "/api/v1/application-environments/binding-1", "", `{"data":{"id":"binding-1"}}`},
		{[]string{"environments", "detail", "binding-1"}, "GET", "/api/v1/application-environments/binding-1/detail", "", `{"data":{"binding":{"id":"binding-1"}}}`},
		{[]string{"environments", "create", "--input", "-", "--yes"}, "POST", "/api/v1/application-environments", environment, `{"data":{"id":"binding-1"}}`},
		{[]string{"environments", "update", "binding-1", "--input", "-", "--yes"}, "PUT", "/api/v1/application-environments/binding-1", strings.TrimSuffix(environment, "}") + `,"expectedUpdatedAt":"2026-10-25T00:00:00Z"}`, `{"data":{"id":"binding-1"}}`},
		{[]string{"environments", "delete", "binding-1", "--yes"}, "DELETE", "/api/v1/application-environments/binding-1", "", ""},
		{[]string{"environment-catalog", "list"}, "GET", "/api/v1/delivery/environments", "", `{"items":[]}`},
		{[]string{"plans", "create", "--input", "-", "--yes"}, "POST", "/api/v1/delivery/plans", `{"id":"plan-1","applicationId":"app-1","applicationEnvironmentId":"binding-1","action":"deploy"}`, `{"data":{"id":"plan-1","status":"waiting_approval"}}`},
		{[]string{"plans", "confirm", "plan-1", "--yes"}, "POST", "/api/v1/delivery/plans/plan-1/confirm", "", `{"data":{"plan":{"id":"plan-1","status":"waiting_approval"},"result":{}}}`},
		{[]string{"tasks", "list", "--status", "running", "--limit", "20"}, "GET", "/api/v1/delivery/execution-tasks?limit=20&status=running", "", `{"items":[` + deliveryTaskFixture + `]}`},
		{[]string{"tasks", "get", "task-1"}, "GET", "/api/v1/delivery/execution-tasks/task-1", "", `{"data":` + deliveryTaskFixture + `}`},
		{[]string{"tasks", "logs", "task-1", "--limit", "1000"}, "GET", "/api/v1/delivery/execution-tasks/task-1/logs?limit=1000", "", `{"items":[{"id":"log-1","executionTaskId":"task-1","logLevel":"info","message":"fixture"}]}`},
		{[]string{"tasks", "artifacts", "task-1"}, "GET", "/api/v1/delivery/execution-tasks/task-1/artifacts", "", `{"items":[]}`},
		{[]string{"tasks", "rollout", "task-1"}, "GET", "/api/v1/delivery/execution-tasks/task-1/rollout", "", `{"data":{"uid":"rollout-uid","resourceVersion":"42","operationId":"operation-1"}}`},
		{[]string{"tasks", "retry", "task-1", "--reason", "审核", "--yes"}, "POST", "/api/v1/delivery/execution-tasks/task-1/control", `{"action":"retry","reason":"审核"}`, `{"data":` + deliveryTaskFixture + `}`},
		{[]string{"tasks", "cancel", "task-1", "--yes"}, "POST", "/api/v1/delivery/execution-tasks/task-1/control", `{"action":"cancel"}`, `{"data":` + strings.Replace(deliveryTaskFixture, `"queued"`, `"canceled"`, 1) + `}`},
		{[]string{"blueprints", "list"}, "GET", "/api/v1/delivery/blueprints", "", `{"items":[]}`},
		{[]string{"blueprints", "usage", "blueprint-1"}, "GET", "/api/v1/delivery/blueprints/blueprint-1/usage", "", `{"data":{"templateKind":"blueprint","templateId":"blueprint-1"}}`},
		{[]string{"blueprints", "render", "blueprint-1", "--yes"}, "POST", "/api/v1/delivery/blueprints/blueprint-1/render-spec", "", `{"data":{}}`},
		{[]string{"manifests", "list", "--application-id", "app-1"}, "GET", "/api/v1/delivery/manifest-packages?applicationId=app-1&page=1&pageSize=20", "", `{"data":{"items":[],"page":1,"pageSize":20,"total":0}}`},
		{[]string{"manifests", "get", "package-1"}, "GET", "/api/v1/delivery/manifest-packages/package-1", "", `{"data":{"id":"package-1"}}`},
		{[]string{"manifests", "revisions", "package-1"}, "GET", "/api/v1/delivery/manifest-packages/package-1/revisions", "", `{"items":[{"id":"revision-1","packageId":"package-1","version":1,"files":[],"bindings":[],"createdAt":"2026-10-25T00:00:00Z"}]}`},
		{[]string{"manifests", "bindings", "package-1"}, "GET", "/api/v1/delivery/manifest-packages/package-1/bindings", "", `{"items":[{"id":"binding-1","packageId":"package-1"}]}`},
		{[]string{"manifest-deployments", "list", "--phase", "converged"}, "GET", "/api/v1/delivery/manifest-deployments?page=1&pageSize=20&phase=converged", "", `{"data":{"items":[],"page":1,"pageSize":20,"total":0}}`},
	} {
		t.Run(strings.Join(test.args[:2], "/"), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != test.method || r.URL.RequestURI() != test.path || r.Header.Get("Authorization") != "Bearer profile-token" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
				}
				if test.input != "" {
					actual, _ := io.ReadAll(r.Body)
					var got, want any
					if json.Unmarshal(actual, &got) != nil || json.Unmarshal([]byte(test.input), &want) != nil {
						t.Error("invalid typed request")
					}
					a, _ := json.Marshal(got)
					b, _ := json.Marshal(want)
					if string(a) != string(b) {
						t.Errorf("typed request mismatch")
					}
				}
				if test.out == "" {
					w.WriteHeader(204)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, test.out)
			}))
			defer server.Close()
			code, out, errs := runDeliveryTest(t, t.Context(), server.URL, test.input, test.args...)
			if code != 0 || requests.Load() != 1 {
				t.Fatalf("exit=%d requests=%d: %s", code, requests.Load(), errs)
			}
			if test.out != "" && !json.Valid([]byte(out)) {
				t.Fatal("invalid structured output")
			}
			if strings.Contains(out+errs, "profile-token") {
				t.Fatal("known profile credential leaked")
			}
		})
	}
}

func TestDeliveryWorkbenchRejectsUnsafeInputsAndIgnoredFlags(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	for _, args := range [][]string{
		{"applications", "create", "--input-json", `{"name":"API","key":"api","enabled":true}`, "--yes"},
		{"applications", "update", "app-1", "--input-json", `{"name":"API","key":"api","enabled":true}`, "--yes"},
		{"applications", "get", "app-1", "--yes"},
		{"applications", "list", "--application-id", "app-1"},
		{"services", "list"},
		{"services", "get", "../service", "--application-id", "app-1"},
		{"environments", "update", "binding-1", "--input-json", `{"applicationId":"app-1","environmentId":"dev"}`, "--yes"},
		{"plans", "create", "--input-json", `{"id":"plan-1","applicationId":"app-1","applicationEnvironmentId":"binding-1","action":"unknown"}`, "--yes"},
		{"tasks", "logs", "task-1", "--limit", "1001"},
		{"tasks", "retry", "task-1", "--input-json", `{"action":"cancel"}`, "--yes"},
		{"tasks", "get", "task-1", "--status", "completed"},
		{"tasks", "wait", "task-1", "--wait-timeout", "0s"},
		{"manifest-bindings", "update", "binding-1", "--input-json", `{}`, "--yes"},
		{"manifest-deployments", "rollback", "deployment-1", "--input-json", `{"expectedGeneration":1,"targetRevision":2,"useLastKnownGood":true}`, "--yes"},
	} {
		code, out, _ := runDeliveryTest(t, t.Context(), server.URL, "", args...)
		if code == 0 || out != "" {
			t.Fatalf("unsafe input accepted: %v", args)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("invalid command reached HTTP")
	}
}

func TestDeliveryManifestControlsPreserveReviewedTargetsAndVersions(t *testing.T) {
	binding := `{"applicationEnvironmentId":"env-1","clusterId":"cluster-1","namespace":"app","deletionPolicy":"orphan","driftPolicy":"report","enabled":true}`
	action := `{"data":{"deployment":{"id":"deployment-1","bindingId":"binding-1"},"task":{"id":"task-1","status":"queued","callbackToken":"synthetic-runner-secret"}}}`
	for _, test := range []struct {
		args                      []string
		method, path, body, reply string
	}{
		{[]string{"manifest-bindings", "create", "--package-id", "package-1"}, "POST", "/api/v1/delivery/manifest-packages/package-1/bindings", binding, `{"data":{"id":"binding-1","packageId":"package-1"}}`},
		{[]string{"manifest-bindings", "update", "binding-1"}, "PUT", "/api/v1/delivery/manifest-bindings/binding-1", strings.TrimSuffix(binding, "}") + `,"expectedVersion":2}`, `{"data":{"id":"binding-1","packageId":"package-1"}}`},
		{[]string{"manifest-bindings", "set-revision", "binding-1"}, "POST", "/api/v1/delivery/manifest-bindings/binding-1/desired-revision", `{"desiredRevision":2,"expectedGeneration":1,"reconcilePolicy":"manual"}`, action},
		{[]string{"manifest-deployments", "reconcile", "deployment-1"}, "POST", "/api/v1/delivery/manifest-deployments/deployment-1/reconcile", `{"expectedGeneration":1}`, action},
		{[]string{"manifest-deployments", "repair", "deployment-1"}, "POST", "/api/v1/delivery/manifest-deployments/deployment-1/repair", `{"expectedGeneration":1}`, action},
		{[]string{"manifest-deployments", "adopt", "deployment-1"}, "POST", "/api/v1/delivery/manifest-deployments/deployment-1/adopt", `{"expectedGeneration":1}`, action},
		{[]string{"manifest-deployments", "rollback", "deployment-1"}, "POST", "/api/v1/delivery/manifest-deployments/deployment-1/rollback", `{"expectedGeneration":1,"targetRevision":2}`, action},
		{[]string{"tasks", "rollout-control", "task-1"}, "POST", "/api/v1/delivery/execution-tasks/task-1/rollout", `{"action":"promote","uid":"native-uid","resourceVersion":"42"}`, `{"data":{"uid":"native-uid","resourceVersion":"43","operationId":"operation-1"}}`},
	} {
		t.Run(strings.Join(test.args[:2], "/"), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != test.method || r.URL.RequestURI() != test.path {
					t.Errorf("unexpected manifest target %s %s", r.Method, r.URL.RequestURI())
				}
				var actual, expected any
				if json.NewDecoder(r.Body).Decode(&actual) != nil || json.Unmarshal([]byte(test.body), &expected) != nil {
					t.Error("invalid manifest input")
				}
				a, _ := json.Marshal(actual)
				b, _ := json.Marshal(expected)
				if string(a) != string(b) {
					t.Error("reviewed manifest input changed")
				}
				_, _ = io.WriteString(w, test.reply)
			}))
			defer server.Close()
			args := append(test.args, "--input", "-", "--yes")
			code, out, errs := runDeliveryTest(t, t.Context(), server.URL, test.body, args...)
			if code != 0 || calls.Load() != 1 || !json.Valid([]byte(out)) || strings.Contains(out+errs, "synthetic-runner-secret") {
				t.Fatalf("manifest control failed %d: %s %s", code, out, errs)
			}
		})
	}
}

func TestDeliveryListsRejectMalformedAliasesAndFilterOldServers(t *testing.T) {
	for _, raw := range []string{`{}`, `{"items":null}`, `{"items":{}}`, `{"data":[],"items":[{"id":"other"}]}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, raw) }))
		code, out, _ := runDeliveryTest(t, t.Context(), server.URL, "", "applications", "list")
		server.Close()
		if code == 0 || out != "" {
			t.Fatal("malformed list silently became empty")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"items":[{"id":"wanted","applicationId":"app-1","environmentId":"dev"},{"id":"other-app","applicationId":"app-2","environmentId":"dev"},{"id":"other-env","applicationId":"app-1","environmentId":"prod"}]}`)
	}))
	defer server.Close()
	code, out, errs := runDeliveryTest(t, t.Context(), server.URL, "", "environments", "list", "--application-id", "app-1", "--environment-id", "dev")
	if code != 0 || !strings.Contains(out, "wanted") || strings.Contains(out, "other-") {
		t.Fatalf("old server filter widened: %s %s", out, errs)
	}
}

func TestDeliveryTaskControlsNeverFallbackRedirectOrReplay(t *testing.T) {
	for _, status := range []int{401, 403, 404, 409, 500, 501, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if !strings.HasSuffix(r.URL.Path, "/control") {
					t.Error("legacy fallback or redirect reached")
				}
				w.Header().Set("Location", "/api/v1/delivery/execution-tasks/task-1/retry")
				w.WriteHeader(status)
			}))
			defer server.Close()
			code, out, errs := runDeliveryTest(t, t.Context(), server.URL, "", "tasks", "retry", "task-1", "--yes")
			if code == 0 || requests.Load() != 1 || out != "" {
				t.Fatalf("unsafe replay: %d %d %s", code, requests.Load(), errs)
			}
		})
	}
}

func TestDeliveryTaskWaitReadsTerminalAndDetachNeverCancels(t *testing.T) {
	for _, terminal := range []string{"completed", "failed", "canceled", "unknown", ""} {
		t.Run(terminal, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/v1/delivery/execution-tasks/task-1" {
					t.Error("wait issued a mutation")
				}
				status := "running"
				if requests.Add(1) > 1 && terminal != "" {
					status = terminal
				}
				_, _ = io.WriteString(w, `{"data":`+strings.Replace(deliveryTaskFixture, `"queued"`, `"`+status+`"`, 1)+`}`)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 350*time.Millisecond)
			defer cancel()
			code, out, errs := runDeliveryTest(t, ctx, server.URL, "", "tasks", "wait", "task-1", "--poll-interval", "100ms")
			if (code == 0) != (terminal == "completed") || !json.Valid([]byte(out)) || requests.Load() < 2 {
				t.Fatalf("wait terminal=%s exit=%d: %s %s", terminal, code, out, errs)
			}
		})
	}
}

func TestDeliveryWorkbenchPreservesIntegerVersionsThroughInputListAndOutput(t *testing.T) {
	const version = "9007199254740993"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			decoder := json.NewDecoder(r.Body)
			decoder.UseNumber()
			var input map[string]any
			if decoder.Decode(&input) != nil || input["expectedVersion"] != json.Number(version) {
				t.Error("reviewed integer version changed in request")
			}
			_, _ = io.WriteString(w, `{"data":{"id":"app-1","version":`+version+`}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"app-1","version":`+version+`}],"items":[{"id":"app-1","version":`+version+`}]}`)
	}))
	defer server.Close()
	for _, format := range []string{"json", "yaml"} {
		for _, args := range [][]string{
			{"applications", "list", "--output", format},
			{"applications", "update", "app-1", "--input", "-", "--yes", "--output", format},
		} {
			input := `{"key":"api","name":"API","enabled":true,"expectedVersion":` + version + `}`
			code, out, errs := runDeliveryTest(t, t.Context(), server.URL, input, args...)
			if code != 0 || !strings.Contains(out, version) || strings.Contains(out, `"`+version+`"`) {
				t.Fatalf("integer receipt changed in %s: %d %s %s", format, code, out, errs)
			}
		}
	}
}
