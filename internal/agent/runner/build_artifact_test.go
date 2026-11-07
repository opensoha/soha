package runner

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteRefreshesWorkspaceArtifactsAfterCommands(t *testing.T) {
	want := "sha256:" + strings.Repeat("b", 64)
	for _, tc := range []struct {
		name, initial, command, status, digest string
	}{
		{name: "first build", command: "printf '%s' '" + want + "' > .soha-image-digest", status: "completed", digest: want},
		{name: "overwrite previous artifact", initial: "old", command: "printf '%s' '" + want + "' > .soha-image-digest", status: "completed", digest: want},
		{name: "removed artifact", initial: want, command: "rm .soha-image-digest", status: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newClaimTestRunner()
			r.cfg.WorkspaceRoot = t.TempDir()
			workspace := filepath.Join(r.cfg.WorkspaceRoot, "build")
			if err := os.Mkdir(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			if tc.initial != "" {
				if err := os.WriteFile(filepath.Join(workspace, ".soha-image-digest"), []byte(tc.initial), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			task := ExecutionTask{ID: "build", ApplicationID: "app", TaskKind: "build", ProviderKind: "ci_agent_runner", Payload: map[string]any{
				"image": "registry.example/app:v1", "commands": []any{tc.command},
				"workspace": map[string]any{"path": "build", "commandDir": ".", "artifactFiles": []any{".soha-image-digest", "../outside"}},
			}}
			var final callbackRequest
			r.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				next := task
				next.Status = "running"
				if req.Method == http.MethodPost {
					var callback callbackRequest
					if err := json.NewDecoder(req.Body).Decode(&callback); err != nil {
						return nil, err
					}
					if callback.Status == "completed" || callback.Status == "failed" {
						final = callback
					}
					next.Status = callback.Status
				}
				return jsonResponse(t, http.StatusAccepted, map[string]any{"data": next}), nil
			})}
			r.execute(t.Context(), task)
			if final.Status != "completed" {
				t.Fatalf("final callback = %#v", final)
			}
			if got := stringMapValue(final.Payload, "imageDigest"); got != tc.digest {
				t.Fatalf("digest = %q, want %q", got, tc.digest)
			}
			artifacts := valueAsMapSlice(final.Payload["workspaceArtifacts"])
			if len(artifacts) != 2 || artifacts[0]["status"] != tc.status || artifacts[1]["status"] != "invalid" {
				t.Fatalf("artifacts = %#v", artifacts)
			}
			if tc.digest != "" && artifacts[0]["sizeBytes"] != float64(len(tc.digest)) {
				t.Fatalf("stale artifact size: %#v", artifacts[0])
			}
			artifact := final.Payload["artifact"].(map[string]any)
			if got := stringMapValue(artifact, "digest"); got != tc.digest {
				t.Fatalf("artifact digest = %q, want %q", got, tc.digest)
			}
		})
	}
}

func TestPrepareWorkspaceOptionalCommandDir(t *testing.T) {
	for _, tc := range []struct {
		name      string
		workspace map[string]any
		wantDir   string
		wantError bool
	}{
		{name: "absent", workspace: map[string]any{}},
		{name: "null", workspace: map[string]any{"commandDir": nil}},
		{name: "workingDir alias", workspace: map[string]any{"workingDir": "sub"}, wantDir: "sub"},
		{name: "escape", workspace: map[string]any{"commandDir": "../outside"}, wantError: true},
		{name: "absolute", workspace: map[string]any{"commandDir": "/tmp/outside"}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newClaimTestRunner()
			r.cfg.WorkspaceRoot = t.TempDir()
			workspace := filepath.Join(r.cfg.WorkspaceRoot, "app")
			if err := os.MkdirAll(filepath.Join(workspace, "sub"), 0o700); err != nil {
				t.Fatal(err)
			}
			path, dir, _, _, err := r.prepareWorkspace(t.Context(), ExecutionTask{ID: "task", ApplicationID: "app", Payload: map[string]any{"workspace": tc.workspace}})
			if tc.wantError {
				if err == nil {
					t.Fatal("unsafe commandDir accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if path != workspace || dir != filepath.Join(workspace, tc.wantDir) {
				t.Fatalf("workspace=%q commandDir=%q", path, dir)
			}
		})
	}
}

func TestPrepareWorkspaceCheckoutDefaultsToRepositoryRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git required for checkout regression test")
	}
	repo := filepath.Join(t.TempDir(), "source")
	for _, args := range [][]string{
		{"init", "--initial-branch=main", repo},
		{"-C", repo, "-c", "user.name=Runner Test", "-c", "user.email=runner@example.invalid", "commit", "--allow-empty", "-m", "fixture"},
	} {
		if logs, err := runCommand(t.Context(), "", "git", args...); err != nil {
			t.Fatalf("git fixture: %v: %v", err, logs)
		}
	}
	r := newClaimTestRunner()
	r.cfg.WorkspaceRoot = t.TempDir()
	path, dir, _, _, err := r.prepareWorkspace(t.Context(), ExecutionTask{ID: "task", ApplicationID: "app", Payload: map[string]any{
		"workspace": map[string]any{"checkout": map[string]any{"enabled": true, "repositoryURL": repo}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(r.cfg.WorkspaceRoot, "app") || dir != path || !hasGitRepository(path) {
		t.Fatalf("checkout path=%q commandDir=%q", path, dir)
	}
}

func TestBuildImageArtifactOmitsAbsentDigest(t *testing.T) {
	for _, payload := range []map[string]any{
		{}, {"imageDigest": nil}, {"imageDigest": "pending"},
		{"artifacts": []any{map[string]any{"kind": "image"}}},
	} {
		if _, ok := buildImageArtifact(payload, "app:v1")["digest"]; ok {
			t.Fatal("artifact contains invented digest")
		}
		for _, artifact := range buildArtifactList(payload, "app:v1") {
			if _, ok := artifact["digest"]; ok {
				t.Fatal("artifact list contains invented digest")
			}
		}
	}
	if got := resolveImageFromCommands(nil, []string{"docker build -t app:v1 ."}); got != "app:v1" {
		t.Fatalf("image fallback = %q", got)
	}
	if got := resolveImageFromCommands(nil, []string{"printf done"}); got != "" {
		t.Fatalf("invented image = %q", got)
	}
}

func TestImageDigestFromWorkspaceArtifacts(t *testing.T) {
	workspace := t.TempDir()
	want := "sha256:" + strings.Repeat("a", 64)
	if err := os.WriteFile(filepath.Join(workspace, ".soha-image-digest"), []byte("registry.example/api@"+want+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := imageDigestFromWorkspaceArtifacts(workspace, []map[string]any{{
		"path": ".soha-image-digest", "status": "completed",
	}})
	if got != want {
		t.Fatalf("digest = %q, want %q", got, want)
	}
}

func TestImageDigestFromWorkspaceArtifactsRejectsInvalidEvidence(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, ".soha-image-digest"), []byte("sha256:short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := imageDigestFromWorkspaceArtifacts(workspace, []map[string]any{{"path": ".soha-image-digest", "status": "completed"}}); got != "" {
		t.Fatalf("invalid digest accepted: %q", got)
	}
	if got := imageDigestFromWorkspaceArtifacts(workspace, []map[string]any{{"path": "../.soha-image-digest", "status": "completed"}}); got != "" {
		t.Fatalf("escaped artifact accepted: %q", got)
	}
}

func TestBuildFailureStages(t *testing.T) {
	if got := commandPipelineStage("docker push registry.example/api:v1"); got != "push" {
		t.Fatalf("push stage = %q", got)
	}
	if got := commandPipelineStage("docker image inspect app:v1 > .soha-image-digest"); got != "publish" {
		t.Fatalf("publish stage = %q", got)
	}
	if got := commandPipelineStage("docker build -t app:v1 ."); got != "build" {
		t.Fatalf("build stage = %q", got)
	}
	if got := workspaceFailureStage(os.ErrNotExist); got != "prepare" {
		t.Fatalf("prepare stage = %q", got)
	}
}
