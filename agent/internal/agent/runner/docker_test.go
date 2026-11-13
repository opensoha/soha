package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"go.uber.org/zap"
)

func TestDockerOperationPayloadRequiresTypedComposeFields(t *testing.T) {
	tests := []DockerOperation{
		{OperationKind: "project_deploy", Payload: map[string]any{"action": 7, "composeContent": "services: {}"}},
		{OperationKind: "project_deploy", Payload: map[string]any{"action": "exec", "composeContent": "services: {}"}},
		{OperationKind: "service_action", Payload: map[string]any{"action": "restart", "composeContent": "services: {}"}},
	}
	for index, operation := range tests {
		if err := validateDockerOperationPayload(operation); err == nil {
			t.Fatalf("case %d: validateDockerOperationPayload() error = nil, want rejection", index)
		}
	}
	valid := DockerOperation{OperationKind: "project_deploy", Payload: map[string]any{"action": "deploy", "composeContent": "services: {}"}}
	if err := validateDockerOperationPayload(valid); err != nil {
		t.Fatalf("validateDockerOperationPayload(valid) error = %v", err)
	}
}

func TestDockerOperationStopsWhenInitialCallbackReturnsTerminalState(t *testing.T) {
	operation := DockerOperation{ID: "operation-completed", OperationKind: "project_deploy", Status: "queued", Payload: map[string]any{"action": "deploy", "composeContent": "services: {}"}}
	callbackCount := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/docker/operation-callbacks" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var req dockerCallbackRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode callback: %v", err)
		}
		callbackCount++
		terminal := operation
		terminal.Status = "completed"
		return jsonResponse(t, http.StatusAccepted, map[string]any{"data": terminal}), nil
	})
	runner := New(cfgpkg.ControlPlaneConfig{
		BaseURL: "http://control-plane", BearerToken: "runner-token",
		CallbackRetry: cfgpkg.CallbackRetryConfig{MaxAttempts: 1, Backoff: time.Millisecond},
		Docker:        cfgpkg.DockerRunnerConfig{OperationKinds: []string{"project_deploy"}, ComposeRoot: t.TempDir()},
	}, zap.NewNop())
	runner.httpClient = &http.Client{Transport: transport}
	runner.executeDockerOperation(context.Background(), operation)
	if callbackCount != 1 {
		t.Fatalf("callback count = %d, want one terminal-state check", callbackCount)
	}
}

func TestDockerCommandDoesNotStartWithoutControlPlaneConfirmation(t *testing.T) {
	for _, scenario := range []string{"unreachable", "denied", "different-operation", "missing-status"} {
		t.Run(scenario, func(t *testing.T) {
			bin := t.TempDir()
			marker := filepath.Join(t.TempDir(), "command-started")
			//nolint:gosec // isolated fake command; no Docker daemon is contacted.
			if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\ncase \"$*\" in\n  *\" up \"*) touch \"$SOHA_TEST_COMMAND_MARKER\" ;;\nesac\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("SOHA_TEST_COMMAND_MARKER", marker)
			operation := DockerOperation{ID: "original", OperationKind: "project_deploy", Payload: map[string]any{"action": "deploy", "composeContent": "services: {}"}}
			runner := New(cfgpkg.ControlPlaneConfig{BaseURL: "http://control-plane", BearerToken: "test", CallbackRetry: cfgpkg.CallbackRetryConfig{MaxAttempts: 1, Backoff: time.Millisecond}, Docker: cfgpkg.DockerRunnerConfig{OperationKinds: []string{"project_deploy"}, ComposeRoot: t.TempDir()}}, zap.NewNop())
			runner.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				if scenario == "unreachable" {
					return nil, context.DeadlineExceeded
				}
				if scenario == "denied" {
					return jsonResponse(t, http.StatusForbidden, nil), nil
				}
				remote := operation
				if scenario == "different-operation" {
					remote.ID, remote.Status = "other", "running"
				}
				return jsonResponse(t, http.StatusAccepted, map[string]any{"data": remote}), nil
			})}
			runner.executeDockerOperation(context.Background(), operation)
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("command ran without authorization: %v", err)
			}
		})
	}
}

func TestDockerClaimNegotiatesAndCallbackReturnsToken(t *testing.T) {
	const token = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	requestCount := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestCount++
		switch r.URL.Path {
		case "/api/v1/docker/operations/claim":
			var req dockerClaimRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode claim: %v", err)
			}
			if !req.CallbackTokenSupported {
				t.Fatal("claim did not advertise callback token support")
			}
			return jsonResponse(t, http.StatusAccepted, map[string]any{"data": DockerOperation{ID: "operation-1", CallbackToken: token}}), nil
		case "/api/v1/docker/operation-callbacks":
			var req dockerCallbackRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode callback: %v", err)
			}
			if req.CallbackToken != token {
				t.Fatalf("callback token = %q, want claim token", req.CallbackToken)
			}
			return jsonResponse(t, http.StatusAccepted, map[string]any{"data": DockerOperation{ID: "operation-1", Status: "completed"}}), nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})
	runner := New(cfgpkg.ControlPlaneConfig{
		BaseURL: "http://control-plane", BearerToken: "runner-token", AgentID: "agent-1",
		CallbackRetry: cfgpkg.CallbackRetryConfig{MaxAttempts: 1, Backoff: time.Millisecond},
		Docker:        cfgpkg.DockerRunnerConfig{OperationKinds: []string{"host_sync"}},
	}, zap.NewNop())
	runner.httpClient = &http.Client{Transport: transport}

	operation, ok := runner.claimDockerOperation(context.Background())
	if !ok || operation.CallbackToken != token {
		t.Fatalf("claimed operation = %#v ok=%v, want callback token", operation, ok)
	}
	if _, ok := runner.dockerCallback(context.Background(), operation, "completed", nil, nil); !ok {
		t.Fatal("docker callback failed")
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2", requestCount)
	}
}

func TestPrepareComposeWorkspaceRemovesStaleEnvFileWhenEnvContentIsCleared(t *testing.T) {
	root := t.TempDir()
	runner := New(cfgpkg.ControlPlaneConfig{
		Docker: cfgpkg.DockerRunnerConfig{ComposeRoot: root},
	}, zap.NewNop())
	operation := DockerOperation{
		ID:        "operation-1",
		ProjectID: "project-1",
		Payload: map[string]any{
			"projectSlug":    "preview-api",
			"composeContent": "services:\n  api:\n    image: nginx:alpine\n",
			"envContent":     "APP_ENV=test",
		},
	}

	dir, _, err := runner.prepareComposeWorkspace(operation)
	if err != nil {
		t.Fatalf("prepareComposeWorkspace() error = %v", err)
	}
	envPath := filepath.Join(dir, ".env")
	if content, err := os.ReadFile(envPath); err != nil || !strings.Contains(string(content), "APP_ENV=test") {
		t.Fatalf(".env content = %q err=%v, want APP_ENV=test", content, err)
	}

	operation.Payload["envContent"] = ""
	if _, _, err := runner.prepareComposeWorkspace(operation); err != nil {
		t.Fatalf("prepareComposeWorkspace() clearing env error = %v", err)
	}
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf(".env stat err = %v, want not exist", err)
	}
}

func TestExecuteComposeActionBuildsGitDockerfileBeforeRecreatingContainer(t *testing.T) {
	binDir := t.TempDir()
	gitPath := filepath.Join(binDir, "git")
	gitScript := `#!/bin/sh
set -eu
case "$1" in
  init)
    mkdir -p .git
    ;;
  remote)
    if [ "$2" = "get-url" ]; then
      [ -f .origin ] || exit 1
      echo "https://github.com/opensoha/example.git"
    else
      touch .origin
    fi
    ;;
  fetch)
    ;;
  checkout)
    mkdir -p deploy
    printf 'FROM scratch\n' > deploy/Dockerfile
    ;;
  clean)
    ;;
  rev-parse)
    echo "0123456789abcdef0123456789abcdef01234567"
    ;;
  *)
    exit 1
    ;;
esac
`
	//nolint:gosec // test creates temporary fake executables
	if err := os.WriteFile(gitPath, []byte(gitScript), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	dockerCalls := filepath.Join(t.TempDir(), "docker-calls")
	dockerPath := filepath.Join(binDir, "docker")
	dockerScript := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$DOCKER_CALLS"
`
	//nolint:gosec // test creates temporary fake executables
	if err := os.WriteFile(dockerPath, []byte(dockerScript), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_CALLS", dockerCalls)
	root := t.TempDir()
	runner := New(cfgpkg.ControlPlaneConfig{Docker: cfgpkg.DockerRunnerConfig{ComposeRoot: root}}, zap.NewNop())
	operation := DockerOperation{
		ID:        "operation-1",
		ProjectID: "project-1",
		Payload: map[string]any{
			"action":         "redeploy",
			"sourceKind":     "git_dockerfile",
			"projectSlug":    "preview-api",
			"composeContent": "services:\n  api:\n    image: preview-api:git-main\n",
			"image":          "preview-api:git-main",
			"platform":       "linux/amd64",
			"gitBuild": map[string]any{
				"repositoryUrl":  "https://github.com/opensoha/example.git",
				"ref":            "main",
				"dockerfilePath": "deploy/Dockerfile",
				"contextDir":     ".",
				"pull":           true,
			},
		},
	}

	logs, err := runner.executeComposeAction(context.Background(), operation)
	if err != nil {
		t.Fatalf("executeComposeAction() error = %v logs=%v", err, logs)
	}
	callBytes, err := os.ReadFile(dockerCalls)
	if err != nil {
		t.Fatalf("read docker calls: %v", err)
	}
	calls := string(callBytes)
	buildIndex := strings.Index(calls, "build --file")
	downIndex := strings.Index(calls, "compose -f compose.yaml down --remove-orphans")
	upIndex := strings.Index(calls, "compose -f compose.yaml up -d --force-recreate")
	if buildIndex < 0 || downIndex <= buildIndex || upIndex <= downIndex || !strings.Contains(calls, "--tag preview-api:git-main") || !strings.Contains(calls, "--pull") {
		t.Fatalf("docker calls = %q, want Git build followed by compose down and force-recreate", calls)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "0123456789abcdef0123456789abcdef01234567") {
		t.Fatalf("logs = %v, want resolved commit", logs)
	}
	if _, err := os.Stat(filepath.Join(root, "preview-api", ".soha-build", operation.ID)); !os.IsNotExist(err) {
		t.Fatalf("git build workspace stat err = %v, want cleaned after successful start", err)
	}
}

func TestExecuteComposeActionPullsImageBeforeRecreatingContainer(t *testing.T) {
	binDir := t.TempDir()
	dockerCalls := filepath.Join(t.TempDir(), "docker-calls")
	dockerPath := filepath.Join(binDir, "docker")
	dockerScript := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$DOCKER_CALLS"
`
	//nolint:gosec // test creates a temporary fake executable
	if err := os.WriteFile(dockerPath, []byte(dockerScript), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_CALLS", dockerCalls)
	runner := New(cfgpkg.ControlPlaneConfig{Docker: cfgpkg.DockerRunnerConfig{ComposeRoot: t.TempDir()}}, zap.NewNop())
	operation := DockerOperation{
		ID: "operation-image-redeploy", ProjectID: "project-1",
		Payload: map[string]any{
			"action": "redeploy", "sourceKind": "single_container", "projectSlug": "preview-api",
			"composeContent": "services:\n  api:\n    image: nginx:alpine\n",
		},
	}

	logs, err := runner.executeComposeAction(context.Background(), operation)
	if err != nil {
		t.Fatalf("executeComposeAction() error = %v logs=%v", err, logs)
	}
	callBytes, err := os.ReadFile(dockerCalls)
	if err != nil {
		t.Fatalf("read docker calls: %v", err)
	}
	calls := string(callBytes)
	pullIndex := strings.Index(calls, "compose -f compose.yaml pull")
	downIndex := strings.Index(calls, "compose -f compose.yaml down --remove-orphans")
	upIndex := strings.Index(calls, "compose -f compose.yaml up -d --force-recreate")
	if pullIndex < 0 || downIndex <= pullIndex || upIndex <= downIndex {
		t.Fatalf("docker calls = %q, want pull followed by down and force-recreate", calls)
	}
}

func TestExecuteComposeActionDoesNotDestroyContainerWhenImagePullFails(t *testing.T) {
	binDir := t.TempDir()
	dockerCalls := filepath.Join(t.TempDir(), "docker-calls")
	dockerPath := filepath.Join(binDir, "docker")
	dockerScript := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$DOCKER_CALLS"
case "$*" in
  *" compose -f compose.yaml pull"|"compose -f compose.yaml pull") exit 1 ;;
esac
`
	//nolint:gosec // test creates a temporary fake executable
	if err := os.WriteFile(dockerPath, []byte(dockerScript), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_CALLS", dockerCalls)
	runner := New(cfgpkg.ControlPlaneConfig{Docker: cfgpkg.DockerRunnerConfig{ComposeRoot: t.TempDir()}}, zap.NewNop())
	operation := DockerOperation{
		ID: "operation-image-pull-failed", ProjectID: "project-1",
		Payload: map[string]any{
			"action": "redeploy", "sourceKind": "single_container", "projectSlug": "preview-api",
			"composeContent": "services:\n  api:\n    image: nginx:alpine\n",
		},
	}

	if _, err := runner.executeComposeAction(context.Background(), operation); err == nil {
		t.Fatal("executeComposeAction() error = nil, want pull failure")
	}
	callBytes, err := os.ReadFile(dockerCalls)
	if err != nil {
		t.Fatalf("read docker calls: %v", err)
	}
	calls := string(callBytes)
	if strings.Contains(calls, " down ") || strings.Contains(calls, " up ") {
		t.Fatalf("docker calls = %q, current container must remain running after pull failure", calls)
	}
}

func TestExecuteComposeActionRemovesFailedGitCheckoutButRetainsResolvedCommit(t *testing.T) {
	binDir := t.TempDir()
	gitPath := filepath.Join(binDir, "git")
	gitScript := `#!/bin/sh
set -eu
case "$1" in
  init) mkdir -p .git ;;
  remote)
    if [ "$2" = "get-url" ]; then
      [ -f .origin ] || exit 1
    else
      touch .origin
    fi
    ;;
  fetch) ;;
  checkout) printf 'FROM scratch\n' > Dockerfile ;;
  clean) ;;
  rev-parse) echo "0123456789abcdef0123456789abcdef01234567" ;;
  *) exit 1 ;;
esac
`
	//nolint:gosec // test creates temporary fake executables
	if err := os.WriteFile(gitPath, []byte(gitScript), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	dockerPath := filepath.Join(binDir, "docker")
	dockerScript := `#!/bin/sh
exit 1
`
	//nolint:gosec // test creates temporary fake executables
	if err := os.WriteFile(dockerPath, []byte(dockerScript), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	runner := New(cfgpkg.ControlPlaneConfig{Docker: cfgpkg.DockerRunnerConfig{ComposeRoot: root}}, zap.NewNop())
	operation := DockerOperation{
		ID: "operation-failed-build", ProjectID: "project-1",
		Payload: map[string]any{
			"action": "deploy", "sourceKind": "git_dockerfile", "projectSlug": "preview-api",
			"composeContent": "services:\n  api:\n    image: preview-api:git-main\n", "image": "preview-api:git-main",
			"gitBuild": map[string]any{"repositoryUrl": "https://github.com/opensoha/example.git", "ref": "main"},
		},
	}

	if _, err := runner.executeComposeAction(context.Background(), operation); err == nil {
		t.Fatal("executeComposeAction() error = nil, want failed Docker build")
	}
	buildRoot := filepath.Join(root, "preview-api", ".soha-build", operation.ID)
	if _, err := os.Stat(filepath.Join(buildRoot, "repository")); !os.IsNotExist(err) {
		t.Fatalf("repository checkout stat err = %v, want removed", err)
	}
	commit, err := os.ReadFile(filepath.Join(buildRoot, "resolved-commit"))
	if err != nil {
		t.Fatalf("read retained resolved commit: %v", err)
	}
	if strings.TrimSpace(string(commit)) != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("resolved commit = %q, want pinned commit", commit)
	}
}

func TestGitDockerfileBuildSpecRejectsCredentialsAndPathEscape(t *testing.T) {
	tests := []DockerOperation{
		{Payload: map[string]any{"image": "preview:git", "gitBuild": map[string]any{"repositoryUrl": "https://token@github.com/opensoha/example.git"}}},
		{Payload: map[string]any{"image": "preview:git", "gitBuild": map[string]any{"repositoryUrl": "https://github.com/opensoha/example.git", "dockerfilePath": "../Dockerfile"}}},
		{Payload: map[string]any{"image": "preview:git", "gitBuild": map[string]any{"repositoryUrl": "https://github.com/opensoha/example.git", "ref": "--upload-pack=evil"}}},
	}
	for index, operation := range tests {
		if _, err := gitDockerfileBuildSpecFromOperation(operation); err == nil {
			t.Fatalf("case %d: gitDockerfileBuildSpecFromOperation() error = nil, want rejection", index)
		}
	}
}

func TestExecuteGitDockerfileBuildRejectsBroadWorkspaceOperationID(t *testing.T) {
	runner := New(cfgpkg.ControlPlaneConfig{}, zap.NewNop())
	operation := DockerOperation{
		ID: ".",
		Payload: map[string]any{
			"image": "preview:git",
			"gitBuild": map[string]any{
				"repositoryUrl": "https://github.com/opensoha/example.git",
			},
		},
	}
	if _, _, err := runner.executeGitDockerfileBuild(context.Background(), t.TempDir(), operation); err == nil {
		t.Fatalf("executeGitDockerfileBuild() error = nil, want invalid operation id rejection")
	}
}

func TestValidateDockerHostProvisionRequiresDockerRuntime(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	runner := New(cfgpkg.ControlPlaneConfig{}, zap.NewNop())

	logs, err := runner.validateDockerHostProvision(context.Background())
	if err == nil {
		t.Fatalf("validateDockerHostProvision() err = nil, want docker runtime error; logs=%v", logs)
	}
	if !strings.Contains(err.Error(), "docker runtime unavailable") {
		t.Fatalf("validateDockerHostProvision() err = %v, want docker runtime unavailable", err)
	}
}

func TestValidateDockerHostProvisionChecksDockerAndCompose(t *testing.T) {
	binDir := t.TempDir()
	dockerPath := filepath.Join(binDir, "docker")
	script := `#!/bin/sh
if [ "$1" = "info" ]; then
  echo "24.0.0 x86_64"
  exit 0
fi
if [ "$1" = "compose" ] && [ "$2" = "version" ]; then
  echo "Docker Compose version v2.27.0"
  exit 0
fi
exit 1
`
	//nolint:gosec // test creates a temporary executable fake docker binary
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", binDir)
	runner := New(cfgpkg.ControlPlaneConfig{}, zap.NewNop())

	logs, err := runner.validateDockerHostProvision(context.Background())
	if err != nil {
		t.Fatalf("validateDockerHostProvision() err = %v, logs=%v", err, logs)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "24.0.0 x86_64") || !strings.Contains(joined, "Docker Compose version") || !strings.Contains(joined, "docker runtime validated") {
		t.Fatalf("validateDockerHostProvision() logs = %v", logs)
	}
}

func TestDockerOperationKindAllowedRequiresExplicitAllowlist(t *testing.T) {
	cases := []struct {
		name      string
		allowed   []string
		kind      string
		wantAllow bool
	}{
		{name: "empty list denies", kind: "project_deploy", wantAllow: false},
		{name: "exact match allows", allowed: []string{"project_deploy"}, kind: "project_deploy", wantAllow: true},
		{name: "host provision allows quick-created docker hosts", allowed: []string{"host_provision"}, kind: "host_provision", wantAllow: true},
		{name: "different kind denies", allowed: []string{"host_sync"}, kind: "project_deploy", wantAllow: false},
		{name: "wildcard allows", allowed: []string{"*"}, kind: "service_action", wantAllow: true},
		{name: "empty kind denies", allowed: []string{"*"}, kind: "", wantAllow: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dockerOperationKindAllowed(tc.allowed, tc.kind); got != tc.wantAllow {
				t.Fatalf("dockerOperationKindAllowed(%v, %q) = %t, want %t", tc.allowed, tc.kind, got, tc.wantAllow)
			}
		})
	}
}

func TestDockerCancellationAcknowledgmentFollowsStoppedCommand(t *testing.T) {
	binDir := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "command-pid")
	//nolint:gosec // isolated fake command only; it never invokes a Docker daemon.
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(`#!/bin/sh
case "$*" in
  "compose -f compose.yaml up -d")
    echo $$ > "$SOHA_TEST_COMMAND_PID"
    exec sleep 120
    ;;
  *) echo "test-runtime" ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SOHA_TEST_COMMAND_PID", pidFile)
	operation := DockerOperation{ID: "cancel-in-flight", OperationKind: "project_deploy", ProjectID: "project-1", CallbackToken: strings.Repeat("x", 40), Payload: map[string]any{"action": "deploy", "projectSlug": "cancel-test", "composeContent": "services: {}"}}
	acknowledged, requested := false, false
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/v1/docker/operation-callbacks" {
			t.Errorf("unexpected request %s", request.URL.Path)
			return jsonResponse(t, 500, nil), nil
		}
		var callback dockerCallbackRequest
		if err := json.NewDecoder(request.Body).Decode(&callback); err != nil {
			t.Error(err)
		}
		remote := operation
		remote.Status = "running"
		// #nosec G304 -- PID file belongs to the isolated fake command in t.TempDir.
		pidData, started := os.ReadFile(pidFile)
		if started == nil {
			remote.Status, requested = "canceling", true
		}
		if callback.Status == "canceled" {
			if !requested || !callback.CancellationAcknowledged || callback.CallbackToken != operation.CallbackToken {
				t.Error("cancellation acknowledgment is missing its request or attempt")
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
			if err != nil {
				t.Error(err)
			} else if err := syscall.Kill(pid, 0); err == nil {
				t.Error("runner acknowledged before command exited")
			}
			remote.Status, acknowledged = "canceled", true
		}
		return jsonResponse(t, http.StatusAccepted, map[string]any{"data": remote}), nil
	})
	r := New(cfgpkg.ControlPlaneConfig{BaseURL: "http://control-plane", BearerToken: "test-runner-token", CallbackRetry: cfgpkg.CallbackRetryConfig{MaxAttempts: 1, Backoff: time.Millisecond}, Docker: cfgpkg.DockerRunnerConfig{OperationKinds: []string{"project_deploy"}, ComposeRoot: t.TempDir()}}, zap.NewNop())
	r.httpClient = &http.Client{Transport: transport}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	r.executeDockerOperation(ctx, operation)
	if !acknowledged {
		t.Fatal("runner did not acknowledge stopped commands")
	}
}
