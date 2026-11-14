package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
)

func fixtureMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	// #nosec G703 -- Callers inspect fixed paths under their private t.TempDir fixtures.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestWorkspacePermissionsProtectNewDirectoriesAndKeepExistingState(t *testing.T) {
	root := t.TempDir()
	r := &Runner{cfg: cfgpkg.ControlPlaneConfig{WorkspaceRoot: root}}
	workspace, _, _, _, err := r.prepareWorkspace(t.Context(), ExecutionTask{ID: "new", Payload: map[string]any{"workspace": map[string]any{"path": "nested/new"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{workspace, filepath.Dir(workspace)} {
		if got := fixtureMode(t, path); got != 0o700 {
			t.Fatalf("new workspace mode=%04o, want 0700", got)
		}
	}
	existing := filepath.Join(root, "existing")
	// #nosec G301 -- Deliberately broad legacy fixture verifies there is no implicit chmod/migration.
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(existing, "state")
	if err := os.WriteFile(marker, []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := r.prepareWorkspace(t.Context(), ExecutionTask{ID: "existing"}); err != nil {
		t.Fatal(err)
	}
	// #nosec G304 -- Fixed sentinel under this test's private temporary workspace.
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "retained" || fixtureMode(t, existing) != 0o755 {
		t.Fatalf("existing workspace changed: data=%q err=%v", data, err)
	}
}

func TestAgentRuntimeCreatesPrivateWorkspace(t *testing.T) {
	root := t.TempDir()
	r := &Runner{cfg: cfgpkg.ControlPlaneConfig{AgentRuntime: cfgpkg.AgentRuntimeConfig{WorkspaceRoot: root}}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, _ = r.executeCLIAgentRun(ctx, AgentRun{ID: "new-run"}, agentProviderCommandSpec{Command: "/bin/sh"})
	if got := fixtureMode(t, filepath.Join(root, "new-run")); got != 0o700 {
		t.Fatalf("agent runtime workspace mode=%04o", got)
	}
}

func TestGitBuildKeepsReadableRepositoryInsidePrivateOperationRoot(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := &Runner{}
	op := DockerOperation{ID: "operation", Payload: map[string]any{"image": "fixture:latest", "gitBuild": map[string]any{"repositoryUrl": "https://example.invalid/fixture.git"}}}
	_, root, err := r.executeGitDockerfileBuild(ctx, t.TempDir(), op)
	if err == nil || root == "" {
		t.Fatalf("cancelled build must stop before any Git operation: root=%q err=%v", root, err)
	}
	if got := fixtureMode(t, root); got != 0o700 {
		t.Fatalf("operation root mode=%04o, want 0700", got)
	}
	if got := fixtureMode(t, filepath.Join(root, "repository")); got != 0o755 {
		t.Fatalf("repository mode=%04o, want 0755", got)
	}
}

func TestComposeControlFilesRemainPrivate(t *testing.T) {
	r := &Runner{cfg: cfgpkg.ControlPlaneConfig{Docker: cfgpkg.DockerRunnerConfig{ComposeRoot: t.TempDir()}}}
	dir, _, err := r.prepareComposeWorkspace(DockerOperation{ID: "operation", ProjectID: "project", Payload: map[string]any{"composeContent": "services: {}", "envContent": "FIXTURE=value"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"compose.yaml", ".env"} {
		if got := fixtureMode(t, filepath.Join(dir, name)); got != 0o600 {
			t.Fatalf("control file %s mode=%04o, want 0600", name, got)
		}
	}
}
