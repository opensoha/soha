package release_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseWorkflowUsesPinnedReleaseInputs(t *testing.T) {
	workflow := readReleaseWorkflow(t)

	required := []string{
		"contracts_ref:",
		"CONTRACTS_REF: ${{ inputs.contracts_ref || github.ref_name }}",
		"WEB_SHA256: ${{ inputs.web_sha256 }}",
		"go mod edit -dropreplace=github.com/opensoha/soha-contracts@v0.1.2",
		"go get \"github.com/opensoha/soha-contracts@${CONTRACTS_REF}\"",
		"go list -m github.com/opensoha/soha-contracts",
		"go list -m -f '{{.Dir}}' github.com/opensoha/soha-contracts",
		"contracts-build-context",
		"contracts=./contracts-build-context",
		"soha-web-dist-${WEB_REF}.tar.gz.sha256",
		"soha-web-dist.sha256",
		"sha256sum -c -",
		"soha-linux-amd64.sha256",
		"soha-release-manifest.json",
		"gh release download \"${GITHUB_REF_NAME}\"",
		"downloaded-release/soha-linux-amd64.sha256",
		"sha256sum -c soha-linux-amd64.sha256",
		"contractsModule",
		"webSha256",
		"binary_sha256",
	}
	for _, want := range required {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow is missing %q", want)
		}
	}
}

func TestReleaseWorkflowDoesNotUseSiblingContractsCheckout(t *testing.T) {
	workflow := readReleaseWorkflow(t)

	disallowed := []string{
		"repository: opensoha/soha-contracts",
		"path: soha-contracts",
		"../soha-contracts/",
		"go.work <<'EOF'",
		"replace github.com/opensoha/soha-contracts v0.0.0 =>",
	}
	for _, value := range disallowed {
		if strings.Contains(workflow, value) {
			t.Fatalf("release workflow still contains local contracts wiring %q", value)
		}
	}
}

func readReleaseWorkflow(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	content, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	return string(content)
}
