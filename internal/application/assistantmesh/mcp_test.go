package assistantmesh

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	gateway "github.com/opensoha/soha/internal/domain/aigateway"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	identity "github.com/opensoha/soha/internal/domain/identity"
	knowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type mcpTestStore struct {
	Store
	record domain.Record
}

func (s mcpTestStore) Get(context.Context, string) (domain.Record, error) {
	return s.record, nil
}

type mcpTestUsers struct{}

func (mcpTestUsers) GetByID(_ context.Context, id string) (identity.User, error) {
	return identity.User{ID: id, Status: "active"}, nil
}

type mcpTestGateway struct {
	calls    []gateway.ToolInvocationRequest
	callers  []identity.Principal
	resource string
}

func (m *mcpTestGateway) InvokeTool(_ context.Context, caller identity.Principal, req gateway.ToolInvocationRequest) (gateway.ToolInvocationResult, error) {
	m.calls = append(m.calls, req)
	m.callers = append(m.callers, caller)
	if req.Input["projectId"] != m.resource {
		return gateway.ToolInvocationResult{}, apperrors.ErrAccessDenied
	}
	return gateway.ToolInvocationResult{Output: map[string]any{
		"id": m.resource, "name": "test project", "status": "running", "envContent": "must stay private",
	}}, nil
}

func TestMeshMCPInvokesAsCallerForEachProject(t *testing.T) {
	const projectA = "8a9cca3d-9c7e-437f-ba02-3d9ee4ea323d"
	const projectB = "640fc390-73df-4979-8d97-b8e2bf9a5a79"
	input := domain.AssetInput{Kind: "mcp", Title: "Read project status", Body: "Check the current project status."}
	proposal := mcpProposal{Input: input, Status: "approved", Audience: knowledge.AccessScope{Visibility: "organization"}, Revision: 1}
	raw, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	target := &mcpTestGateway{resource: projectA}
	service := &MCPService{store: mcpTestStore{record: domain.Record{ID: "proposal", OwnerID: "publisher", Kind: "mcp", Payload: raw}}, gateway: target, users: mcpTestUsers{}}
	caller := identity.Principal{UserID: "caller"}
	req := ActionRequest{ResourceID: projectA, ExpectedDigest: proposal.approvalDigest(), RequestID: "same-request-scope"}
	result, err := service.invoke(context.Background(), caller, "proposal", req)
	if err != nil || result["project"] == nil {
		t.Fatalf("allowed project rejected: %v", err)
	}
	req.ResourceID = projectB
	if _, err := service.invoke(context.Background(), caller, "proposal", req); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("second project must use a separate authorization: %v", err)
	}
	if len(target.calls) != 2 || target.calls[1].Input["projectId"] != projectB || target.callers[1].UserID != caller.UserID || len(target.calls[1].SecretRefs) != 0 {
		t.Fatalf("unexpected invocation context: %+v %+v", target.calls, target.callers)
	}
}

func TestAssistantMeshMCPReviewBindsProposalAudienceAndRound(t *testing.T) {
	f := newFixture(t)
	s := NewMCPService(f.store, f.service.config, f.service.permissions, nil, f.audit, f.service.users)
	author := employee("user-a", "team-1")
	ctx := t.Context()
	asset, err := s.Create(ctx, author, domain.AssetInput{Kind: "mcp", Title: "Read project status", Body: "Read current status."})
	if err != nil {
		t.Fatal(err)
	}
	id, draft := asset["id"].(string), asset["draftDigest"].(string)
	submit := func(scope knowledge.AccessScope) string {
		t.Helper()
		_, err := s.Act(ctx, author, id, "publish", ActionRequest{ExpectedDigest: draft, Audience: scope})
		if err != nil {
			t.Fatal(err)
		}
		rec, err := f.store.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		item, err := decodeMCP(rec)
		if err != nil {
			t.Fatal(err)
		}
		return item.candidateDigest()
	}
	old := submit(knowledge.AccessScope{Visibility: "teams", Teams: []string{"team-1"}})
	if _, err := s.Act(ctx, author, id, "withdraw", ActionRequest{ExpectedDigest: old}); err != nil {
		t.Fatal(err)
	}
	current := submit(knowledge.AccessScope{Visibility: "organization"})
	if _, err := s.Act(ctx, admin(), id, "approve", ActionRequest{ExpectedDigest: old}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale team approval applied to organization: %v", err)
	}
	if _, err := s.Act(ctx, admin(), id, "approve", ActionRequest{ExpectedDigest: current}); err != nil {
		t.Fatal(err)
	}
}
