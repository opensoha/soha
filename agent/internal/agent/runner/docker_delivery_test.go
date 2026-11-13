package runner

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDeliveryComposePreflightDoesNotTouchLiveWorkspace(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	commandLog := filepath.Join(root, "commands")
	t.Setenv("SOHA_TEST_COMMAND_LOG", commandLog)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// #nosec G306 -- executable fixture in t.TempDir; no Docker daemon is invoked.
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nprintf '%s:%s\\n' \"$PWD\" \"$*\" >> \"$SOHA_TEST_COMMAND_LOG\"\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(root, "live")
	if err := os.Mkdir(live, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "compose.yaml"), []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Runner{}
	r.cfg.Docker.ComposeRoot = live
	_, err := r.executeComposeAction(context.Background(), DockerOperation{ID: "preflight", ProjectID: "project", OperationKind: "project_deploy", Payload: map[string]any{"action": "validate", "projectSlug": "project", "composeContent": "name: project\nservices:\n  api:\n    image: registry.example/api@sha256:" + strings.Repeat("a", 64), "envContent": "SECRET=value"}})
	if err != nil {
		t.Fatal(err)
	}
	// #nosec G304 -- fixed filename under this test's private temporary directory.
	unchanged, _ := os.ReadFile(filepath.Join(live, "compose.yaml"))
	if string(unchanged) != "unchanged" {
		t.Fatal("preflight changed live configuration")
	}
	// #nosec G304 -- command log path is created inside this test's private directory.
	logs, err := os.ReadFile(commandLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(logs)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], ":info --format") || !strings.HasSuffix(lines[1], ":compose -f compose.yaml config --quiet") || strings.Contains(string(logs), "SECRET") {
		t.Fatalf("preflight commands: %s", logs)
	}
	workspace, _, _ := strings.Cut(lines[0], ":")
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("private preflight workspace remains: %v", err)
	}
	if args := composeArgsForAction("delivery_deploy"); !reflect.DeepEqual(args, []string{"compose", "-f", "compose.yaml", "up", "-d", "--no-build", "--pull", "always", "--wait"}) {
		t.Fatalf("delivery command lost artifact/wait controls: %v", args)
	}
}
