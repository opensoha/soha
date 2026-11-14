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
		`"agent/v*"`,
		"environment: release-agent",
		"release-control.py verify --component agent",
		"--mode declared --consumer agent",
		"REQUESTED_CONTRACTS_REF: v0.2.0",
		"publish-release-assets.py --component agent",
		"ghcr.io/opensoha/soha-agent:v${{ needs.verify.outputs.version }}",
		"ghcr.io/opensoha/soha-buildpacks-agent:daemonless-v${{ needs.verify.outputs.version }}",
		"github.com/opensoha/soha/agent/internal/agent/buildinfo.Version",
		"name: Smoke test linux amd64 release archive",
		"soha-agent_*_linux_amd64.tar.gz",
		"./smoke/soha-agent version",
		"- release-smoke",
		"name: Download published release assets and verify",
		"gh release download \"${RELEASE_TAG}\"",
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
	for _, forbidden := range []string{"context: soha-agent", "build-contexts:", "github.com/opensoha/soha-agent/internal/", `- "v*"`, "gh release upload", "softprops/action-gh-release@"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("Root Agent release retains forbidden source or tag route %q", forbidden)
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
	content, err := os.ReadFile(filepath.Join(root, "..", ".github", "workflows", "agent-release.yml"))
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	return string(content)
}
