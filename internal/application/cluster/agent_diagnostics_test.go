package cluster

import (
	"context"
	"testing"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
)

type actionCheckAgentStub struct {
	agentSummaryStub
	checks []contractresource.AgentActionCheck
}

func (s actionCheckAgentStub) GetActionChecks(context.Context) ([]contractresource.AgentActionCheck, error) {
	return s.checks, nil
}

func TestConnectedAgentDiagnosticsNeverInferConfigurationFromInstallation(t *testing.T) {
	for _, old := range []bool{false, true} {
		service := newTestService(t, &stubRepository{})
		service.agents = func(domaincluster.Connection) (AgentSummaryClient, error) {
			if old {
				return agentSummaryStub{}, nil
			}
			return actionCheckAgentStub{checks: []contractresource.AgentActionCheck{{Action: "platform.pods.exec", Implementation: "supported", Configured: "denied", KubernetesRBAC: "allowed", CheckedAt: time.Now().UTC().Format(time.RFC3339)}}}, nil
		}
		detail := domaincluster.Detail{CapabilityMatrix: domaincluster.DefaultCapabilityMatrix()}
		service.describeAgentActions(context.Background(), domaincluster.Connection{Metadata: map[string]any{"agent_action_profile": "workbench"}}, &detail)
		for _, check := range detail.Diagnostics.ActionChecks {
			if check.Action != "platform.pods.exec" {
				continue
			}
			want := "denied"
			if old {
				want = "unknown"
			}
			if check.Configured != want {
				t.Fatalf("installation preset became a runtime policy claim: %+v", check)
			}
		}
		for _, entry := range detail.CapabilityMatrix {
			if entry.Key == "pod.exec" && entry.Agent.Status != domaincluster.CapabilityStatusPartial {
				t.Fatalf("static capability hides unverified/denied runtime: %+v", entry)
			}
		}
	}
}

func TestActionDiagnosticsRejectStaleAndInvalidClaims(t *testing.T) {
	for _, check := range []contractresource.AgentActionCheck{
		{Action: "platform.nodes.drain", Implementation: "supported", Configured: "allowed", KubernetesRBAC: "allowed", CheckedAt: time.Now().Add(-3 * time.Minute).Format(time.RFC3339)},
		{Action: "platform.nodes.drain", Implementation: "supported", Configured: "maybe", KubernetesRBAC: "allowed", CheckedAt: time.Now().Format(time.RFC3339)},
	} {
		checks := normalizeAgentActionChecks([]contractresource.AgentActionCheck{check})
		for _, got := range checks {
			if got.Action == check.Action && (got.Configured != "unknown" || got.KubernetesRBAC != "unknown") {
				t.Fatalf("accepted stale/invalid evidence: %+v", got)
			}
		}
	}
}
