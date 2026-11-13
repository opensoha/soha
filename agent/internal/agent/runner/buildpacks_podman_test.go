package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func TestBuildpacksLifecycleCredentialBoundary(t *testing.T) {
	spec := sohaapi.BuildpacksExecutionSpec{Configuration: sohaapi.BuildpacksConfiguration{BuilderImage: "approved-builder", Platform: "linux/amd64"}, LifecycleImage: "trusted-lifecycle"}
	for _, phase := range []string{"analyzer", "detector", "restorer", "builder", "exporter"} {
		trusted := phase != "detector" && phase != "builder"
		args := buildpacksLifecycleArguments("/private/task", "/private/task/source/go", "1001:1001", phase, trusted, spec)
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "/registry/config.json") != trusted || strings.Contains(joined, "DOCKER_CONFIG") != trusted {
			t.Fatalf("%s registry credential boundary: %v", phase, args)
		}
		if strings.Contains(joined, "/private/task/credentials:") || strings.Contains(joined, "docker.sock") || strings.Contains(joined, "--privileged") {
			t.Fatalf("%s exposes host credentials or privilege: %v", phase, args)
		}
		if !trusted && (!strings.Contains(joined, "/cnb/lifecycle:ro") || args[len(args)-1] != spec.Configuration.BuilderImage) {
			t.Fatalf("%s did not use the frozen lifecycle and builder", phase)
		}
		if trusted && args[len(args)-1] != spec.LifecycleImage {
			t.Fatalf("%s gave registry credentials to the builder", phase)
		}
	}
}

func TestBuildpacksLifecycleRejectsChangedRunImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analyzed.toml")
	image := "docker.io/approved/run@sha256:" + strings.Repeat("a", 64)
	for _, test := range []struct {
		contents string
		valid    bool
	}{
		{"[run-image]\nreference = '" + image + "'\n", true},
		{"[run-image]\nreference = 'index." + image + "'\n", true},
		{"[run-image]\nreference = 'untrusted.example/approved/run@sha256:" + strings.Repeat("a", 64) + "'\n", false},
		{"[run-image]\nreference = '" + image + "'\nextend = true\n", false},
		{"[run-image]\nreference = 'other.example/run@sha256:" + strings.Repeat("b", 64) + "'\n", false},
	} {
		if err := os.WriteFile(path, []byte(test.contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := validateBuildpacksAnalyzed(path, image); (err == nil) != test.valid {
			t.Fatalf("run image validation: %v", err)
		}
	}
}
