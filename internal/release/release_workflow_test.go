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
		"RELEASE_VERSION: ${{ inputs.version || github.ref_name }}",
		"- name: Verify release version",
		"if: startsWith(github.ref, 'refs/tags/v')",
		`"$RELEASE_VERSION" != "$GITHUB_REF_NAME"`,
		"Core version must match the exact root release tag.",
		"CONTRACTS_REF: ${{ inputs.contracts_ref }}",
		"npm run build:web",
		"python3 scripts/source-workspace.py check-go",
		".tmp/embedded-web.json",
		"same-source SDK differs from published npm package",
		"go list -m -f '{{.Version}}' github.com/opensoha/soha-contracts",
		"contracts input differs from the reviewed go.mod dependency",
		`[[ "${reviewed_ref}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]`,
		"GOWORK=off go mod download",
		"GOWORK=off go mod verify",
		"published contracts module must not use a replacement",
		"published contracts module requires an unreplaced checksum",
		"published contracts version/checksum mismatch",
		"Qualify Core against the published Go SDK",
		"export GOWORK=off GOFLAGS=-mod=readonly",
		"${RUNNER_TEMP:?}/soha-published-sdk.XXXXXX",
		`CGO_ENABLED=0 go build -tags embedassets -trimpath -o "$qualification_dir/soha-server" ./cmd/server`,
		"go list -m github.com/opensoha/soha-contracts",
		"run: go test ./...",
		"CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build",
		"target: network-gateway-runtime",
		"target: proxy-runtime-mihomo",
		"ghcr.io/opensoha/soha-proxy-mihomo:${{ github.ref_name }}",
		"target: proxy-runtime-sing-box",
		"ghcr.io/opensoha/soha-proxy-sing-box:${{ github.ref_name }}",
		"target: proxy-runtime-v2ray",
		"ghcr.io/opensoha/soha-proxy-v2ray:${{ github.ref_name }}",
		"target: radius-runtime",
		"ghcr.io/opensoha/soha-radius:${{ github.ref_name }}",
		"ghcr.io/opensoha/soha-network-gateway:${{ github.ref_name }}",
		"soha-web-dist.tar.gz",
		"soha-web-dist.sha256",
		"sha256sum ../soha-web-dist.tar.gz",
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
	if strings.Contains(workflow, "inputs.web_ref") || strings.Contains(workflow, "--repo opensoha/soha-web") || strings.Contains(workflow, "git describe --tags") {
		t.Fatal("release workflow must build Web from its own commit and extract the exact Core version")
	}
}

func TestReleaseWorkflowDoesNotUseSiblingContractsCheckout(t *testing.T) {
	workflow := readReleaseWorkflow(t)

	disallowed := []string{
		// Release images must resolve the checksummed module themselves rather
		// than replace it with staged or sibling source.
		"go list -m -f '{{.Dir}}' github.com/opensoha/soha-contracts",
		"contracts-build-context",
		"contracts=./",
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

func TestPublicGoQualificationPrecedesPublication(t *testing.T) {
	workflow := readReleaseWorkflow(t)
	start := strings.Index(workflow, "      - name: Qualify Core against the published Go SDK\n")
	if start < 0 {
		t.Fatal("required independent published Go SDK qualification is missing")
	}
	end := strings.Index(workflow[start:], "\n      - ")
	if end < 0 {
		t.Fatal("release publication steps must remain after public qualification")
	}
	qualification := workflow[start : start+end]
	for _, required := range []string{"export GOWORK=off GOFLAGS=-mod=readonly", "go test ./...", "go build -tags embedassets", "./cmd/server"} {
		if !strings.Contains(qualification, required) {
			t.Fatalf("public qualification is missing %q", required)
		}
	}
	if strings.Contains(qualification, "if:") || strings.Contains(qualification, "continue-on-error:") {
		t.Fatal("public Go qualification must be an unconditional blocking release step")
	}
	if web := strings.Index(workflow, "Build Web and SDK from the same release commit"); web < 0 || web > start {
		t.Fatal("same-source Web must be staged before published SDK embed compilation")
	}
	for _, publication := range []string{"Login to GHCR", "Publish Soha image to GHCR", "Publish GitHub release asset"} {
		if index := strings.Index(workflow, publication); index < start {
			t.Fatalf("%s must follow successful public qualification", publication)
		}
	}
	if strings.Index(workflow, "SDK_IDENTITY") > strings.Index(workflow, "GOWORK=off go mod download") {
		t.Fatal("module replacement must be rejected before download/public qualification")
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
