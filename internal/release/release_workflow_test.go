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
		"WEB_REF: ${{ inputs.web_ref || 'v0.1.8' }}",
		"CONTRACTS_REF: ${{ inputs.contracts_ref }}",
		"WEB_SHA256: ${{ inputs.web_sha256 }}",
		"go list -m -f '{{.Version}}' github.com/opensoha/soha-contracts",
		"contracts input differs from the reviewed go.mod dependency",
		"GOWORK=off go mod verify",
		"go list -m github.com/opensoha/soha-contracts",
		"go list -m -f '{{.Dir}}' github.com/opensoha/soha-contracts",
		"GOWORK=off go test ./...",
		"GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build",
		"contracts-build-context",
		"contracts=./contracts-build-context",
		"target: network-gateway-runtime",
		"ghcr.io/opensoha/soha-network-gateway:${{ github.ref_name }}",
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
		"go get",
		"go mod tidy",
		"inputs.contracts_ref ||",
		"go env -w GOPRIVATE",
		"repository: opensoha/soha-contracts",
		"path: soha-contracts",
		"../soha-contracts/",
		"go.work <<'EOF'",
		"replace github.com/opensoha/soha-contracts v0.0.0 =>",
		"go mod edit -dropreplace=github.com/opensoha/soha-contracts",
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
