package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func buildpacksTestRuntime(t *testing.T, mode string) (*Runner, ExecutionTask, string) {
	t.Helper()
	bin, state := t.TempDir(), t.TempDir()
	digest := "sha256:" + strings.Repeat("a", 64)
	image := "registry.example/app:build-1"
	files := map[string]string{
		"git": "#!/bin/sh\nif [ \"$1\" = rev-parse ]; then echo " + strings.Repeat("b", 40) + "; fi\n",
		"docker": fmt.Sprintf(`#!/bin/sh
state=%q
case "$1 $2" in
  'info --format') echo '{"ID":"daemon-1","OSType":"linux","Architecture":"aarch64"}';;
  'image inspect') echo linux/arm64;;
  'container ls') if [ -f "$state/containers" ]; then cat "$state/containers"; fi;;
  'container rm') touch "$state/cleanup"; if [ -f "$state/block-cleanup" ]; then while [ ! -f "$state/release" ]; do sleep 0.02; done; fi; rm -f "$state/containers";;
  'manifest inspect') echo '{"Descriptor":{"digest":%q,"platform":{"os":"linux","architecture":"arm64"}}}';;
  *) exit 2;;
esac
`, state, digest),
		"pack": fmt.Sprintf(`#!/bin/sh
state=%q
if [ "$1" = version ]; then [ -n "$HOME" ] && [ -d "$HOME" ] || exit 1; echo 0.40.9+git-fixture; exit 0; fi
[ "$TMPDIR" = "$HOME" ] && [ -d "$TMPDIR" ] || exit 1
mkdir "$TMPDIR/imgutil.local.image.fixture"
echo 'temporary image layer' > "$TMPDIR/imgutil.local.image.fixture/layer.tar"
shift
image="$1"
shift
while [ "$#" -gt 0 ]; do
 case "$1" in
 --report-output-dir) report="$2"; shift 2;;
 --sbom-output-dir) sbom="$2"; shift 2;;
 --publish) shift;;
 *) shift 2;;
 esac
done
echo %s > "$state/containers"
touch "$state/started"
if [ -f "$state/block" ]; then while :; do sleep 1; done; fi
printf '[image]\ntags = ["%%s"]\ndigest = "%s"\n' "$image" > "$report/report.toml"
echo '{"bomFormat":"CycloneDX"}' > "$sbom/sbom.cdx.json"
rm -f "$state/containers"
echo 'Build finished'
`, state, strings.Repeat("c", 64), digest),
	}
	for name, body := range files {
		// #nosec G306 -- Owner-only executable fixtures stand in for the installed toolchain.
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if mode != "success" {
		for _, name := range []string{"block", "block-cleanup"} {
			if err := os.WriteFile(filepath.Join(state, name), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	bp := cfgpkg.BuildpacksConfig{Enabled: true, BuilderImage: "registry.example/builder@" + digest, RunImage: "registry.example/run@" + digest, LifecycleImage: "registry.example/lifecycle@" + digest, Platform: "linux/arm64", DockerHost: "unix:///dedicated/docker.sock", DaemonID: "daemon-1", AllowedApplicationIDs: []string{"app-1"}}
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/system/df" || request.URL.Query().Get("type") != "volume" {
			t.Error("unexpected daemon request")
		}
		_, _ = w.Write([]byte(`{"Volumes":[]}`))
	}))
	t.Cleanup(daemon.Close)
	bp.DockerHost = strings.Replace(daemon.URL, "http://", "tcp://", 1)
	r := New(cfgpkg.ControlPlaneConfig{AgentID: "runner-1", BaseURL: "http://control-plane", WorkspaceRoot: t.TempDir(), Buildpacks: bp, DefaultTimeout: 10 * time.Second, CallbackRetry: cfgpkg.CallbackRetryConfig{MaxAttempts: 1}}, nil)
	spec := sohaapi.BuildpacksExecutionSpec{Configuration: sohaapi.BuildpacksConfiguration{BuilderImage: bp.BuilderImage, RunImage: bp.RunImage, Platform: sohaapi.BuildpacksConfigurationPlatform(bp.Platform)}, LifecycleImage: bp.LifecycleImage, PackVersion: cfgpkg.BuildpacksPackVersion, ContextDir: ".", Environment: map[string]string{"BP_TEST": "true"}}
	task := ExecutionTask{ID: "task-1", ApplicationID: "app-1", ProviderKind: "buildpacks_runner.runner-1", TaskKind: "build", CallbackToken: "attempt-1", Payload: map[string]any{"image": image, "buildpacks": spec, "workspace": map[string]any{"checkouts": []map[string]any{{"repositoryURL": "https://git.example/app.git", "refType": "commit", "refName": strings.Repeat("b", 40)}}}}}
	return r, task, state
}

func TestBuildpacksPublishesVerifiedImageAndRejectsUntrustedInputs(t *testing.T) {
	r, task, _ := buildpacksTestRuntime(t, "success")
	r.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		next := task
		next.Status = "running"
		return jsonResponse(t, 200, map[string]any{"data": next}), nil
	})}
	result, err := r.runBuildpacks(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	sbom, ok := result["sbom"].([]map[string]any)
	if !ok || result["imageDigest"] != "sha256:"+strings.Repeat("a", 64) || len(sbom) != 1 {
		t.Fatalf("invalid artifacts: %#v", result)
	}
	root, ok := result["workspacePath"].(string)
	if !ok {
		t.Fatalf("missing workspace path: %#v", result)
	}
	for _, name := range []string{"credentials", "source"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("private workspace retained: %s", name)
		}
	}
	if cap := r.BuildpacksCapability(context.Background(), "other-app"); cap.Ready {
		t.Fatal("cross-application access allowed")
	}
	if cap := r.BuildpacksCapability(context.Background(), task.ApplicationID); !cap.Ready || cap.TimeoutSeconds != 10 {
		t.Fatalf("runner execution budget not advertised: %+v", cap)
	}
	task.Payload["commands"] = []string{"echo override"}
	if _, _, err := r.validateBuildpacksTask(context.Background(), task); err == nil {
		t.Fatal("command override accepted")
	}
	if err := writeBuildpacksRegistryConfig(t.TempDir(), `{"auths":{},"credsStore":"arbitrary-command"}`); err == nil {
		t.Fatal("credential helper accepted")
	}
	if err := r.verifyBuildpacksImage(context.Background(), "registry.example/app:build-1", "sha256:"+strings.Repeat("d", 64), t.TempDir()); err == nil {
		t.Fatal("mismatched registry digest accepted")
	}
}

func TestBuildpacksPlatformEmulationRequiresExplicitOptIn(t *testing.T) {
	r, _, _ := buildpacksTestRuntime(t, "success")
	r.cfg.Buildpacks.Platform = "linux/amd64"
	if err := r.checkBuildpacksDaemon(t.Context()); err == nil {
		t.Fatal("platform mismatch accepted by default")
	}
	r.cfg.Buildpacks.AllowPlatformEmulation = true
	if err := r.checkBuildpacksDaemon(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.cfg.Buildpacks.DaemonID = "another-daemon"
	if err := r.checkBuildpacksDaemon(t.Context()); err == nil {
		t.Fatal("emulation bypassed dedicated daemon identity")
	}
}

func TestBuildpacksCancellationWaitsForContainerCleanup(t *testing.T) {
	for _, source := range []string{"runtime", "control-plane", "timeout"} {
		t.Run(source, func(t *testing.T) {
			r, task, state := buildpacksTestRuntime(t, source)
			if source == "timeout" {
				task.TimeoutSeconds = 5
			}
			var remoteCancel atomic.Bool
			var mu sync.Mutex
			statuses := []string{}
			r.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				next := task
				next.Status = "running"
				if req.Method == http.MethodPost {
					var callback callbackRequest
					if err := json.NewDecoder(req.Body).Decode(&callback); err != nil {
						return nil, err
					}
					mu.Lock()
					statuses = append(statuses, callback.Status)
					if callback.Status != "running" {
						t.Logf("terminal callback: %s %#v", callback.Status, callback.Payload)
					}
					mu.Unlock()
					next.Status = callback.Status
				}
				if remoteCancel.Load() {
					next.Status = "canceling"
				}
				return jsonResponse(t, 200, map[string]any{"data": next}), nil
			})}
			done := make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			go func() { defer close(done); r.executeClaimedTask(ctx, task) }()
			t.Cleanup(func() {
				cancel()
				_ = os.WriteFile(filepath.Join(state, "release"), nil, 0o600)
				select {
				case <-done:
				case <-time.After(3 * time.Second):
				}
			})
			waitBuildpacksMarker(t, filepath.Join(state, "started"))
			if source == "runtime" && !r.CancelActiveTask(task.ID, "test cancellation") {
				t.Fatal("task not active")
			}
			if source == "control-plane" {
				remoteCancel.Store(true)
			}
			waitBuildpacksMarker(t, filepath.Join(state, "cleanup"))
			mu.Lock()
			early := containsStatus(statuses, "completed") || containsStatus(statuses, "canceled") || containsStatus(statuses, "callback_timeout")
			mu.Unlock()
			if early {
				t.Fatal("terminal callback preceded container cleanup")
			}
			if err := os.WriteFile(filepath.Join(state, "release"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("cleanup did not finish")
			}
			want := "canceled"
			if source == "timeout" {
				want = "callback_timeout"
			}
			mu.Lock()
			defer mu.Unlock()
			if !containsStatus(statuses, want) || containsStatus(statuses, "completed") {
				t.Fatalf("callbacks=%v, want %s", statuses, want)
			}
			if _, err := os.Stat(filepath.Join(state, "containers")); !os.IsNotExist(err) {
				t.Fatal("container remained")
			}
		})
	}
}

func waitBuildpacksMarker(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("marker was not created: %s", path)
}

func TestBuildpacksWorkspaceCleanupFailureBlocksRunner(t *testing.T) {
	r, _, _ := buildpacksTestRuntime(t, "success")
	root := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(root, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.cleanupBuildpacksWorkspace(root); err == nil {
		t.Fatal("invalid workspace cleanup succeeded")
	}
	if capability := r.BuildpacksCapability(context.Background(), "app-1"); capability.Ready || capability.Reason != "buildpacks_cleanup_unconfirmed" {
		t.Fatalf("runner accepted work after cleanup failed: %#v", capability)
	}
}
