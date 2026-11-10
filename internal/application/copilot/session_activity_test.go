package copilot

import (
	"context"
	"testing"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type activityRuns struct{ *inspectionAuthzTestRepository }

func (activityRuns) ListAgentRuns(context.Context, domaincopilot.AgentRunFilter) ([]domaincopilot.AgentRun, error) {
	return []domaincopilot.AgentRun{{CreatedBy: "owner", SessionID: "queued", Status: "queued", ProviderKind: "dsh"}, {CreatedBy: "owner", SessionID: "legacy", Status: "running", ProviderKind: "native"}, {CreatedBy: "owner", SessionID: "isolated", Status: "running", Output: map[string]any{"executionReadOnly": true}}, {CreatedBy: "owner", SessionID: "running", Status: "running"}, {CreatedBy: "other", SessionID: "foreign", Status: "running"}, {CreatedBy: "owner", SessionID: "child", ParentRunID: "parent", Status: "running"}}, nil
}

type activityApprovals struct{ chatGatewayToolStub }

func (activityApprovals) ListApprovalRequests(context.Context, domainidentity.Principal, domainaigateway.ApprovalRequestFilter) ([]domainaigateway.ApprovalRequest, error) {
	return []domainaigateway.ApprovalRequest{{ActorID: "owner", ActorSessionID: "approval", Status: "pending"}, {ActorID: "owner", ActorSessionID: "legacy", Status: "pending"}, {ActorID: "other", ActorSessionID: "foreign", Status: "pending"}}, nil
}
func TestSessionActivityUsesOwnedRootRunsAndCurrentApprovals(t *testing.T) {
	service := &Service{agentRuns: activityRuns{&inspectionAuthzTestRepository{}}, runtime: RuntimeDependencies{Approvals: &activityApprovals{}}}
	sessions := []domaincopilot.Session{{ID: "queued"}, {ID: "running"}, {ID: "approval"}, {ID: "foreign"}, {ID: "child"}, {ID: "legacy"}, {ID: "isolated"}}
	result := service.sessionActivities(context.Background(), domainidentity.Principal{UserID: "owner"}, sessions)
	for index, want := range []string{"queued", "running", "waiting_approval", "", "", "", ""} {
		if result[index].Activity != want {
			t.Fatalf("activity %s = %q, want %q", result[index].ID, result[index].Activity, want)
		}
	}
}
