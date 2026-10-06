package resource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestPodRoutesPreserveRuntimeErrorSemantics(t *testing.T) {
	t.Parallel()

	typed := fmt.Errorf("%w: terminal is not supported", apperrors.ErrUnsupportedOperation)
	if got := (agentPodRoute{}).RuntimeError(typed); got != typed {
		t.Fatalf("agent RuntimeError() = %v, want typed error preserved", got)
	}

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

func TestAgentPodDeleteRequestsFailureAudit(t *testing.T) {
	t.Parallel()

	auditFailure, err := (agentPodRoute{client: failedPodDeleteAgent{}}).DeletePod(t.Context(), "platform", "api-0")
	if err == nil {
		t.Fatal("DeletePod() error = nil, want denied operation")
	}
	if !auditFailure {
		t.Fatal("DeletePod() did not request a failure audit for the denied attempt")
	}
}

type failedPodDeleteAgent struct{ PodAgent }

func (failedPodDeleteAgent) DeletePod(context.Context, string, string) error {
	return apperrors.ErrAccessDenied
}
func TestAgentPodDeleteRouteResolvesClient(t *testing.T) {
	called := false
	w := &Workloads{agent: func(connection domaincluster.Connection) (WorkloadAgent, error) {
		called = true
		return nil, apperrors.ErrClusterUnready
	}}
	_, err := w.routePodDeletion(domaincluster.Connection{Summary: domaincluster.Summary{ID: "agent-cluster", ConnectionMode: domaincluster.ConnectionModeAgent}}, "agent-cluster")
	if !called || !errors.Is(err, apperrors.ErrClusterUnready) {
		t.Fatalf("client resolved=%v, err=%v", called, err)
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
