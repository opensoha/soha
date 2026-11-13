package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

// Two queued tasks exercise the actual claim loop against a real CNB runtime.
// The first is canceled while a lifecycle container exists; the second times out.
func TestBuildpacksStopIntegration(t *testing.T) {
	fixture := readBuildpacksIntegrationConfig(t)
	var err error
	fixture.WorkspaceRoot, err = os.MkdirTemp(fixture.WorkspaceRoot, "stop-integration-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fixture.WorkspaceRoot) })
	r := New(cfgpkg.ControlPlaneConfig{AgentID: "buildpacks-integration", BaseURL: "http://fixture.invalid", WorkspaceRoot: fixture.WorkspaceRoot, Buildpacks: fixture.Runtime, DefaultTimeout: 10 * time.Minute, MaxConcurrency: 1, PollInterval: 50 * time.Millisecond, CallbackRetry: cfgpkg.CallbackRetryConfig{MaxAttempts: 1}}, nil)
	files := buildpacksLanguageFiles()
	// The real buildpack runs this bounded fixture script until cancellation.
	files["node/package.json"] = `{"name":"stop-fixture","version":"1.0.0","scripts":{"start":"node index.js","build":"node -e 'setTimeout(() => {}, 1800000)'"}}`
	repository, commit := buildpacksGitFixture(t, files, "")
	base, secrets, _ := buildpacksSSHFixture(t, map[string]string{"/app.git": repository})
	secrets["REGISTRY_AUTH"] = fixture.RegistryAuth
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), secretValuesContextKey{}, secrets), 15*time.Minute)
	defer cancel()
	capability := r.BuildpacksCapability(ctx, "buildpacks-integration-go")
	if !capability.Ready {
		t.Fatal(capability.Reason)
	}
	tasks := []ExecutionTask{}
	for _, name := range []string{"cancel", "timeout"} {
		tasks = append(tasks, ExecutionTask{ID: "stop-" + name, TaskKind: "build", Status: "running", CallbackToken: name, ProviderKind: capability.ProviderKind, ApplicationID: "buildpacks-integration-go", Payload: map[string]any{
			"image":      fixture.ImagePrefix + "/stop:" + name + fmt.Sprint(time.Now().UnixNano()),
			"buildpacks": sohaapi.BuildpacksExecutionSpec{Configuration: *capability.Configuration, PackVersion: capability.PackVersion, Runtime: sohaapi.BuildpacksExecutionSpecRuntime(capability.Runtime), RuntimeVersion: capability.RuntimeVersion, LifecycleImage: capability.LifecycleImage, ContextDir: "node", Environment: map[string]string{"BP_NODE_RUN_SCRIPTS": "build"}},
			"workspace":  map[string]any{"checkouts": []buildpacksCheckout{{RepositoryURL: base + "/app.git", RefType: "commit", RefName: commit}}},
		}})
	}
	tasks[1].TimeoutSeconds = 420
	var claims atomic.Int64
	terminal := make(chan callbackRequest, 2)
	r.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "/claim") {
			index := claims.Add(1) - 1
			if index < int64(len(tasks)) {
				return jsonResponse(t, http.StatusAccepted, map[string]any{"data": tasks[index]}), nil
			}
			return jsonResponse(t, http.StatusAccepted, map[string]any{"data": ExecutionTask{}}), nil
		}
		if request.Method == http.MethodPost {
			var callback callbackRequest
			if err := json.NewDecoder(request.Body).Decode(&callback); err != nil {
				return nil, err
			}
			if callback.Status != "running" {
				ids, err := r.buildpacksContainers(context.WithoutCancel(ctx))
				if err != nil || len(ids) != 0 {
					t.Error("terminal callback preceded real container cleanup", ids, err)
				}
				private, _ := filepath.Glob(filepath.Join(fixture.WorkspaceRoot, "buildpacks-*", "credentials"))
				source, _ := filepath.Glob(filepath.Join(fixture.WorkspaceRoot, "buildpacks-*", "source"))
				if len(private)+len(source) != 0 {
					t.Error("terminal callback retained private workspace")
				}
				terminal <- callback
			}
		}
		return jsonResponse(t, http.StatusOK, map[string]any{"data": ExecutionTask{ID: tasks[min(int(claims.Load())-1, len(tasks)-1)].ID, Status: "running"}}), nil
	})}
	loopDone := make(chan struct{})
	go func() { defer close(loopDone); r.loop(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-loopDone
		deadline := time.Now().Add(45 * time.Second)
		for len(r.executionSlots) > 0 && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
	})
	for index, want := range []string{"canceled", "callback_timeout"} {
		started := time.Now()
		for {
			select {
			case early := <-terminal:
				t.Fatalf("task ended before a real lifecycle container ran: %s: %v", early.Status, redactResolvedSecretValues(ctx, early.Payload))
			default:
			}
			var output string
			var err error
			args := []string{"container", "ls", "--quiet", "--filter", "status=running"}
			if fixture.Runtime.Runtime == "podman" {
				output, err = r.buildpacksPodman(ctx, "", args...)
			} else {
				output, err = buildpacksCommand(ctx, "", r.buildpacksEnvironment(""), "docker", args...)
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(output) != "" && claims.Load() == int64(index+1) {
				break
			}
			if time.Since(started) > 5*time.Minute {
				t.Fatal("real lifecycle container did not start")
			}
			time.Sleep(time.Second)
		}
		// Allow multiple poll ticks while work is active: the next task must wait.
		time.Sleep(250 * time.Millisecond)
		if claims.Load() != int64(index+1) {
			t.Fatal("dedicated runner claimed another task while build was active")
		}
		if index == 0 && !r.CancelActiveTask(tasks[0].ID, "integration cancellation") {
			t.Fatal("active Buildpacks task could not be canceled")
		}
		select {
		case result := <-terminal:
			if result.Status != want {
				t.Fatalf("stop status %s, want %s: %v", result.Status, want, redactResolvedSecretValues(ctx, result.Payload))
			}
			evidence := map[string]any{"status": result.Status, "realLifecycleObserved": true, "containersAtTerminal": 0, "privateWorkspaceRemoved": true, "singleSlotClaims": index + 1, "elapsedSeconds": time.Since(started).Seconds(), "platform": fixture.Runtime.Platform, "platformEmulationAllowed": fixture.Runtime.AllowPlatformEmulation}
			data, err := json.MarshalIndent(evidence, "", "  ")
			if err == nil {
				err = os.WriteFile(filepath.Join(fixture.EvidenceDirectory, "stop-"+want+".json"), data, 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s confirmed after real container and private workspace cleanup", want)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
