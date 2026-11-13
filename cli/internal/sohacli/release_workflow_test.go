package sohacli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseWorkflowRunsBinarySmokeBeforeChecksums(t *testing.T) {
	workflow := readCLIReleaseWorkflow(t)

	required := []string{
		"release-smoke:",
		"name: Release binary smoke",
		"name: release-linux-amd64",
		"./scripts/release-smoke.sh",
		"- release-smoke",
		"soha_*_linux_amd64.tar.gz",
	}
	for _, want := range required {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow is missing %q", want)
		}
	}
}

func readCLIReleaseWorkflow(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	content, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if os.IsNotExist(err) {
		// A standalone, trimpath-built native test package uses the original
		// package working directory and attested repository fixture files.
		content, err = os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	}
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	return string(content)
}
