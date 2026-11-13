package release_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseWorkflowVerifiesPublishedAssets(t *testing.T) {
	workflow := readReleaseWorkflow(t)

	required := []string{
		"release-smoke:",
		"name: Smoke test linux amd64 release archive",
		"soha-agent_*_linux_amd64.tar.gz",
		"./smoke/soha-agent version",
		"- release-smoke",
		"name: Download published release assets and verify",
		"gh release download \"${GITHUB_REF_NAME}\"",
		"--pattern \"soha-agent_*.tar.gz\"",
		"--pattern \"soha-agent_*.tar.gz.sha256\"",
		"--pattern \"SHA256SUMS\"",
		"shasum -a 256 -c SHA256SUMS",
		"shasum -a 256 -c \"$(basename \"${checksum}\")\"",
	}
	for _, want := range required {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow is missing %q", want)
		}
	}
}

func readReleaseWorkflow(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	content, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	return string(content)
}
