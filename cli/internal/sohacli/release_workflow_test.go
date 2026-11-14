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
		"./cli/scripts/release-smoke.sh",
		"- release-smoke",
		"soha_*_linux_amd64.tar.gz",
		`"cli/v*"`,
		"environment: release-cli",
		"release-control.py verify --component cli",
		"--mode declared --consumer cli",
		"REQUESTED_CONTRACTS_REF: v0.2.0",
		"id-token: write",
		"npm@12.2.2",
		"verify-public-npm-release.mjs verify",
		"--images-unused",
		`npm publish "$package_file"`,
		`--pack-destination "$RUNNER_TEMP/cli-package" --json`,
		"publish-release-assets.py --component cli",
		"expected 13 release assets",
		"Verify anonymous release downloads and SHA-256",
	}
	for _, want := range required {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow is missing %q", want)
		}
	}
	for _, forbidden := range []string{"NPM_TOKEN", "NODE_AUTH_TOKEN:", "repository: opensoha/soha-skills", "github.com/opensoha/soha-cli/internal/", `- "v*"`, "gh release upload", "softprops/action-gh-release@"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("Root CLI release retains forbidden route or credential %q", forbidden)
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
	content, err := os.ReadFile(filepath.Join(root, "..", ".github", "workflows", "cli-release.yml"))
	if os.IsNotExist(err) {
		// A standalone, trimpath-built native test package uses the original
		// package working directory and attested repository fixture files.
		content, err = os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "cli-release.yml"))
	}
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	return string(content)
}
