package resource

import (
	"errors"
	"os"
	"strings"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestPodRoutesPreserveRuntimeErrorSemantics(t *testing.T) {
	t.Parallel()

	cause := errors.New("backend unavailable")
	agentErr := (agentPodRoute{}).RuntimeError(cause)
	if !errors.Is(agentErr, apperrors.ErrClusterUnready) {
		t.Fatalf("agent RuntimeError() = %v, want cluster-unready classification", agentErr)
	}
	if errors.Is(agentErr, cause) {
		t.Fatal("agent RuntimeError() unexpectedly wraps the remote cause")
	}

	directErr := (directPodRoute{}).RuntimeError(cause)
	if directErr != cause {
		t.Fatalf("direct RuntimeError() = %v, want original error", directErr)
	}
}

func TestAgentPodDeleteDoesNotRequestFailureAudit(t *testing.T) {
	t.Parallel()

	auditFailure, err := (agentPodRoute{}).DeletePod(t.Context(), "platform", "api-0")
	if err == nil {
		t.Fatal("DeletePod() error = nil, want unsupported operation")
	}
	if auditFailure {
		t.Fatal("DeletePod() requested a failure audit for the unchanged agent unsupported path")
	}
}

func TestAgentPodDeleteRouteDoesNotResolveClient(t *testing.T) {
	t.Parallel()

	route, err := (&Workloads{}).routePodDeletion(domaincluster.Connection{
		Summary: domaincluster.Summary{ID: "agent-cluster", ConnectionMode: domaincluster.ConnectionModeAgent},
	}, "agent-cluster")
	if err != nil {
		t.Fatalf("routePodDeletion() error = %v, want nil without an agent client factory", err)
	}
	if route.Source() != "agent" {
		t.Fatalf("route source = %q, want agent", route.Source())
	}
}

func TestPodConnectionModeRoutingStaysCentralized(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"pods.go", "pods_helpers.go"} {
		//nolint:gosec // paths are fixed test fixtures in the current package
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(content), "ConnectionModeAgent") {
			t.Errorf("%s contains a connection-mode branch; keep pod routing in pods_routing.go", path)
		}
	}
}
