package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dockerruntime "github.com/opensoha/soha-contracts/docker/runtime"
)

func TestBoundComposeWorkspaceAndEveryActionUseSameNamespace(t *testing.T) {
	binding, _ := dockerruntime.NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	r := &Runner{}
	r.cfg.Docker.ComposeRoot = t.TempDir()
	op := DockerOperation{ID: "operation", ProjectID: "project", Payload: map[string]any{"projectSlug": "collision", "runtimeBinding": binding, "composeContent": "name: user-name\nservices:\n  web:\n    image: alpine:3.21\n", "envContent": "COMPOSE_PROJECT_NAME=collision"}}
	dir, _, err := r.prepareComposeWorkspace(op)
	if err != nil || filepath.Base(dir) != binding.Namespace {
		t.Fatal(dir, err)
	}
	// #nosec G304 -- fixed file within the workspace produced under this test's private t.TempDir Compose root; no external path input.
	content, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if err != nil || !strings.Contains(string(content), binding.Namespace) || !strings.Contains(string(content), binding.ProjectUID) {
		t.Fatal(string(content), err)
	}
	for _, action := range []string{"validate", "delivery_deploy", "deploy", "redeploy", "start", "restart", "stop", "down", "destroy", "pull", "build"} {
		args, err := boundComposeArgs(op, composeArgsForAction(action))
		if err != nil || len(args) < 4 || args[1] != "-p" || args[2] != binding.Namespace {
			t.Fatal(action, args, err)
		}
	}
	for _, action := range []string{"start", "stop", "restart", "logs"} {
		args, err := boundComposeArgs(op, composeServiceArgsForAction(action, "web"))
		if err != nil || args[2] != binding.Namespace {
			t.Fatal(action, args, err)
		}
	}
	other := binding
	other.Namespace = "collision"
	op.Payload["runtimeBinding"] = other
	if _, _, err := r.prepareComposeWorkspace(op); err == nil {
		t.Fatal("bad namespace accepted")
	}
	if _, err := boundComposeArgs(op, composeArgsForAction("down")); err == nil {
		t.Fatal("bad binding dispatched")
	}
	if _, err := os.Stat(filepath.Join(r.cfg.Docker.ComposeRoot, "collision")); !os.IsNotExist(err) {
		t.Fatal("untrusted slug workspace touched", err)
	}
}

func TestBoundProjectRejectsMissingOriginalContainerSetBeforeDocker(t *testing.T) {
	binding, _ := dockerruntime.NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	op := DockerOperation{Payload: map[string]any{"runtimeBinding": binding}}
	if err := validateBoundProjectNamespace(context.Background(), op); err == nil {
		t.Fatal("missing original inventory admitted a project command")
	}
	op.Payload["runtimeContainers"] = nil
	if err := validateBoundProjectNamespace(context.Background(), op); err == nil {
		t.Fatal("null original inventory admitted a project command")
	}
}

func TestBoundServiceCannotFallBackToComposeName(t *testing.T) {
	b, _ := dockerruntime.NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	target := dockerruntime.ContainerBinding{ProjectBinding: b, ServiceName: "web", ContainerID: strings.Repeat("a", 64)}
	op := DockerOperation{OperationKind: "service_action", Payload: map[string]any{"runtimeBinding": b, "serviceName": "web", "runtimeContainer": target}}
	got, bound, err := boundServiceTarget(op)
	if err != nil || !bound || got != target {
		t.Fatal(got, bound, err)
	}
	delete(op.Payload, "runtimeContainer")
	if _, bound, err := boundServiceTarget(op); err == nil || !bound {
		t.Fatal("missing CID fell back to legacy Compose")
	}
	op.Payload["runtimeContainer"] = target
	op.Payload["serviceName"] = "other"
	if _, _, err := boundServiceTarget(op); err == nil {
		t.Fatal("other service accepted")
	}
	delete(op.Payload, "runtimeBinding")
	if _, bound, err := boundServiceTarget(op); bound || err != nil {
		t.Fatal("legacy behavior changed")
	}
}
