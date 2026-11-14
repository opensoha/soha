package sohacli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func runDockerTest(t *testing.T, server, input string, args ...string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	argv := append(append([]string{"docker"}, args...), "--profile", "dev")
	code := Run(t.Context(), argv, Runtime{In: strings.NewReader(input), Out: &out, Err: &errs, ConfigPath: writeTestConfig(t, server)})
	return code, out.String(), errs.String()
}

func TestDockerWorkbenchRequestsAndSafeResults(t *testing.T) {
	const private = "private-runtime-and-config-value"
	for _, test := range []struct {
		name, path, method, input, response string
		args                                []string
	}{
		{"host-plan", "/api/v1/docker/hosts/quick-create/plan", "POST", `{"name":"host-1"}`, `{"data":{"ready":true}}`, []string{"hosts", "plan", "--input", "-"}},
		{"project-create", "/api/v1/docker/projects", "POST", `{"hostId":"host-1","name":"app","composeContent":"services: {}"}`, `{"data":{"id":"project-1","composeContent":"` + private + `","envContent":"` + private + `","config":{"value":"` + private + `"}}}`, []string{"projects", "create", "--input", "-", "--idempotency-key", "create-01", "--yes"}},
		{"project-get", "/api/v1/docker/projects/project-1", "GET", "", `{"data":{"id":"project-1","envContent":"` + private + `","composeContent":"` + private + `"}}`, []string{"projects", "get", "project-1"}},
		{"project-plan", "/api/v1/docker/projects/project-1/deploy/plan", "POST", "", `{"data":{"ready":true}}`, []string{"projects", "plan", "project-1", "--action", "restart"}},
		{"service-get", "/api/v1/compute/resources/container_runtime/service/service-1", "GET", "", `{"data":{"id":"service-1"}}`, []string{"services", "get", "service-1"}},
		{"host-get", "/api/v1/compute/resources/container_runtime/runtime_host/host-1", "GET", "", `{"data":{"id":"host-1"}}`, []string{"hosts", "get", "host-1"}},
		{"tasks-list", "/api/v1/compute/tasks", "GET", "", `{"items":[]}`, []string{"tasks", "list"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != test.method || r.URL.Path != test.path || r.Header.Get("Authorization") != "Bearer profile-token" {
					t.Errorf("unexpected Docker request %s %s", r.Method, r.URL.Path)
				}
				if test.name == "tasks-list" && r.URL.Query().Get("domain") != "container_runtime" {
					t.Error("Docker task list lost its fixed domain")
				}
				if test.name == "project-create" {
					var body map[string]any
					if json.NewDecoder(r.Body).Decode(&body) != nil || body["idempotencyKey"] != "create-01" || body["composeContent"] != "services: {}" {
						t.Error("project creation lost the exact input/key")
					}
				}
				if test.name == "project-plan" {
					var body map[string]any
					if json.NewDecoder(r.Body).Decode(&body) != nil || body["action"] != "restart" {
						t.Error("project plan lost its runtime action")
					}
				}
				_, _ = fmt.Fprint(w, test.response)
			}))
			defer server.Close()
			code, out, stderr := runDockerTest(t, server.URL, test.input, test.args...)
			if code != 0 || calls != 1 || strings.Contains(out+stderr, private) {
				t.Fatalf("code=%d calls=%d stderr=%s; response metadata projection failed", code, calls, stderr)
			}
		})
	}
}

func TestDockerExecutionUsesPlanOriginalKeyAndNeverReplaysAutomatically(t *testing.T) {
	for _, resource := range []string{"hosts", "projects"} {
		for _, scenario := range []string{"ready", "blocked", "retry-original", "uncertain", "redirect"} {
			t.Run(resource+"/"+scenario, func(t *testing.T) {
				calls, executions := 0, 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if strings.HasSuffix(r.URL.Path, "/plan") {
						if scenario == "retry-original" {
							t.Error("retry repeated client preflight")
						}
						plan := dockerDeployTestPlan()
						plan.Ready = scenario != "blocked"
						_ = json.NewEncoder(w).Encode(sohaapi.OperationalPlanEnvelope{Data: plan})
						return
					}
					if resource == "projects" && scenario == "retry-original" {
						if r.Method != "GET" || r.URL.Path != "/api/v1/docker/projects/project-1/deploy-requests/original-1" || r.URL.Query().Get("action") != "deploy" {
							t.Errorf("original recovery was not read-only: %s %s", r.Method, r.URL)
						}
					} else {
						executions++
						if r.Header.Get("Idempotency-Key") != "original-1" {
							t.Error("execution lost its original key")
						}
						if resource == "projects" {
							var submitted sohaapi.DockerProjectDeployInput
							if json.NewDecoder(r.Body).Decode(&submitted) != nil || submitted.Preconditions == nil || *submitted.Preconditions != *dockerDeployTestPlan().DockerProjectDeploy {
								t.Error("execution lost its reviewed configuration binding")
							}
						}
					}
					if scenario == "uncertain" {
						w.WriteHeader(503)
						_, _ = fmt.Fprint(w, `{"error":{"code":"service_unavailable","message":"unavailable"}}`)
						return
					}
					if scenario == "redirect" {
						http.Redirect(w, r, "/weaker-endpoint", http.StatusTemporaryRedirect)
						return
					}
					_, _ = fmt.Fprint(w, `{"data":{"id":"task-1","operationKind":"project_deploy","status":"queued","callbackToken":"private-callback","payload":{"envContent":"private-payload"}}}`)
				}))
				defer server.Close()
				args := []string{resource, "create", "--input", "-", "--idempotency-key", "original-1", "--yes"}
				if resource == "projects" {
					args = []string{resource, "deploy", "project-1", "--idempotency-key", "original-1", "--yes"}
				}
				if scenario == "retry-original" {
					args = append(args, "--retry-original")
				}
				code, out, stderr := runDockerTest(t, server.URL, `{"name":"host-1"}`, args...)
				wantCalls, wantExec := 2, 1
				if scenario == "retry-original" {
					wantCalls = 1
					if resource == "projects" {
						wantExec = 0
					}
				}
				if scenario == "blocked" {
					wantCalls, wantExec = 1, 0
				}
				if calls != wantCalls || executions != wantExec || (code == 0) != (scenario == "ready" || scenario == "retry-original") || strings.Contains(out+stderr, "private-") {
					t.Fatalf("code=%d calls=%d executions=%d stderr=%s", code, calls, executions, stderr)
				}
			})
		}
	}
}

func TestDockerAliasesPreserveTaskActionAndLogRoutes(t *testing.T) {
	for _, test := range []struct {
		args         []string
		method, path string
	}{
		{[]string{"services", "action", "service-1", "restart", "--idempotency-key", "restart-01", "--yes"}, "POST", "/api/v1/compute/resources/container_runtime/service/service-1/actions/restart"},
		{[]string{"tasks", "get", "task-1"}, "GET", "/api/v1/compute/tasks/container_runtime/task-1"},
		{[]string{"tasks", "wait", "task-1", "--wait-timeout", "1s"}, "GET", "/api/v1/compute/tasks/container_runtime/task-1"},
		{[]string{"tasks", "logs", "task-1"}, "GET", "/api/v1/compute/tasks/container_runtime/task-1/logs"},
		{[]string{"tasks", "cancel", "task-1", "--reason", "reviewed", "--yes"}, "POST", "/api/v1/compute/tasks/container_runtime/task-1/cancel"},
		{[]string{"tasks", "retry", "task-1", "--reason", "reviewed", "--idempotency-key", "retry-01", "--yes"}, "POST", "/api/v1/compute/tasks/container_runtime/task-1/retry"},
		{[]string{"logs", "query", "--project-id", "project-1", "--output", "ndjson"}, "POST", "/api/v1/docker/projects/project-1/logs/query"},
	} {
		t.Run(strings.Join(test.args[:2], "/"), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != test.method || r.URL.Path != test.path {
					t.Errorf("alias changed route: %s %s", r.Method, r.URL.Path)
				}
				if strings.HasSuffix(test.path, "/cancel") || strings.HasSuffix(test.path, "/retry") || test.args[0] == "services" {
					if r.Header.Get("Idempotency-Key") == "" {
						t.Error("action has no stable request key")
					}
				}
				switch {
				case test.args[0] == "logs":
					_, _ = fmt.Fprint(w, `{"data":{"entries":[{"timestamp":"2026-10-25T00:00:00Z","message":"password=private-log"}]}}`)
				case strings.HasSuffix(test.path, "/logs"):
					_, _ = fmt.Fprint(w, `{"items":[]}`)
				default:
					_, _ = fmt.Fprint(w, `{"data":{"id":"task-1","normalizedStatus":"succeeded"}}`)
				}
			}))
			defer server.Close()
			code, out, stderr := runDockerTest(t, server.URL, "", test.args...)
			if code != 0 || calls != 1 || strings.Contains(out+stderr, "private-log") {
				t.Fatalf("alias failed: code=%d calls=%d stderr=%s", code, calls, stderr)
			}
		})
	}
}

func TestDockerRejectsInvalidInputAndIrrelevantFlagsBeforeHTTP(t *testing.T) {
	for _, args := range [][]string{
		{"projects", "create", "--input-json", `{"hostId":"h","name":"p","composeContent":"x"}`, "--yes"},
		{"projects", "create", "--input-json", `{"hostId":"h","name":"p","composeContent":"x","id":"caller"}`, "--idempotency-key", "original-1", "--yes"},
		{"projects", "create", "--input-json", `{"hostId":"h","name":"p","composeContent":"x","idempotencyKey":"other-key"}`, "--idempotency-key", "original-1", "--yes"},
		{"hosts", "plan", "--input-json", `{"name":"h"}`, "--yes"},
		{"projects", "get", "project-1", "--action", "destroy"},
		{"projects", "get", "project-1", "--input-json", `{}`},
		{"projects", "plan", "project-1", "--action", "shell"},
		{"projects", "plan", "../outside"},
		{"tasks", "list", "--domain=virtualization"},
		{"services", "action", "service-1", "restart", "--reason", "reviewed", "--yes"},
		{"services", "action", "service-1", "restart", "--input-json", `{"metadata":{"key":"value"}}`, "--yes"},
		{"logs", "query", "--source", "cluster", "--project-id", "p"},
		{"hosts", "plan", "--input-json", `{"name":"h","unknown":"private-value"}`},
		{"hosts", "plan", "--input-json", `{"name":"h"}{"name":"second"}`},
	} {
		t.Run(strings.Join(args[:2], "/"), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer server.Close()
			code, out, stderr := runDockerTest(t, server.URL, "", args...)
			if code == 0 || calls != 0 || strings.Contains(out+stderr, "private-value") {
				t.Fatalf("invalid command reached HTTP: code=%d calls=%d stderr=%s", code, calls, stderr)
			}
		})
	}
}

func TestDockerConfirmationIsRequiredAfterPreflight(t *testing.T) {
	project := dockerWorkbenchConfirmation("projects", "create", "deploy", dockerWorkbenchRequest{body: &sohaapi.DockerProjectInput{Name: "payments", HostID: "host-7"}})
	host := dockerWorkbenchConfirmation("hosts", "create", "deploy", dockerWorkbenchRequest{body: &sohaapi.DockerQuickCreateHostInput{Name: "worker-8"}})
	if !strings.Contains(project, "payments") || !strings.Contains(project, "host-7") || strings.Contains(project, "deploy") || !strings.Contains(host, "worker-8") || !strings.Contains(host, "backing VM") {
		t.Fatal("creation confirmation omits its target or describes a deployment")
	}
	for _, resource := range []string{"hosts", "projects"} {
		t.Run(resource, func(t *testing.T) {
			plans, executions := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/plan") {
					plans++
					_ = json.NewEncoder(w).Encode(sohaapi.OperationalPlanEnvelope{Data: dockerDeployTestPlan()})
					return
				}
				executions++
			}))
			defer server.Close()
			args := []string{resource, "create", "--input", "-", "--idempotency-key", "original-1"}
			if resource == "projects" {
				args = []string{resource, "deploy", "project-1", "--idempotency-key", "original-1"}
			}
			code, _, _ := runDockerTest(t, server.URL, `{"name":"host-1"}`, args...)
			if code == 0 || plans != 1 || executions != 0 {
				t.Fatalf("confirmation bypass: code=%d plans=%d executions=%d", code, plans, executions)
			}
		})
	}
}
