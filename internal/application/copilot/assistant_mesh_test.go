package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	gateway "github.com/opensoha/soha/internal/domain/aigateway"
	mesh "github.com/opensoha/soha/internal/domain/assistantmesh"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type meshSkillReaderFixture struct {
	asset mesh.AssetInput
	allow bool
}

type meshProjectGateway struct {
	allowed   string
	seen      []string
	projected map[string]string
}

func (m *meshProjectGateway) InvokeTool(_ context.Context, p identity.Principal, call gateway.ToolInvocationRequest) (gateway.ToolInvocationResult, error) {
	project, _ := call.Input["projectId"].(string)
	m.seen = append(m.seen, project)
	if p.UserID != "caller" || call.RequestID != "agent:mesh" || call.ToolName != "docker.projects.get" ||
		project != m.allowed || len(call.SecretRefs) != 0 {
		return gateway.ToolInvocationResult{}, apperrors.ErrAccessDenied
	}
	if m.projected != nil {
		return gateway.ToolInvocationResult{Output: m.projected}, nil
	}
	return gateway.ToolInvocationResult{Output: map[string]string{"id": project}}, nil
}

func TestAssistantMeshMCPRechecksProjectBeforeReturningRunReceipt(t *testing.T) {
	const projectA = "8a9cca3d-9c7e-437f-ba02-3d9ee4ea323d"
	const projectB = "640fc390-73df-4979-8d97-b8e2bf9a5a79"
	tools := &meshProjectGateway{allowed: projectA}
	s := &Service{}
	s.runtime.Tools = tools
	run := domain.AgentRun{ID: "agent:mesh", Input: map[string]any{"_sohaAssistantMeshMCP": domain.AssistantMeshMCP{AssetID: "proposal"}}}
	principal := identity.Principal{UserID: "caller"}
	for _, item := range []struct {
		project, allowed string
		denied           bool
	}{{projectA, projectA, false}, {projectB, projectA, true}, {projectA, "", true}} {
		tools.allowed = item.allowed
		raw, err := json.Marshal(map[string]any{"projectId": item.project})
		if err != nil {
			t.Fatal(err)
		}
		in := domain.DSHToolCall{ToolName: "docker_projects_get", CallID: "same-call", InputHash: "hash", Input: raw}
		checkpoint := dshCheckpoint{ToolResults: map[string]domain.DSHToolResult{
			"same-call": {InputHash: "hash", Output: json.RawMessage(`{"content":"{\"id\":\"` + projectA + `\"}"}`)},
		}}
		result, err := s.settleDSHTool(t.Context(), domain.NativeLease{Run: run}, checkpoint, principal, in)
		if item.denied {
			if !errors.Is(err, apperrors.ErrAccessDenied) || len(result.Output) > 0 {
				t.Fatalf("project %s after access change returned a receipt: %+v, %v", item.project, result, err)
			}
		} else if err != nil || len(result.Output) == 0 {
			t.Fatalf("authorized project lost its receipt: %+v, %v", result, err)
		}
	}
	if strings.Join(tools.seen, ",") != projectA+","+projectB+","+projectA {
		t.Fatalf("unexpected dispatch targets: %v", tools.seen)
	}
	tools.allowed = projectA
	tools.projected = map[string]string{"id": "redacted"}
	run.ToolExecutions = []domain.ToolExecution{{
		ToolName: "docker.projects.get", Status: "success",
		Input:  map[string]any{"input": map[string]string{"projectId": projectA}},
		Output: map[string]any{"content": `{"id":"` + projectA + `"}`},
	}}
	if err := s.recheckAssistantMeshProjectHistory(t.Context(), run, principal); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("a changed output policy leaves old model evidence accessible: %v", err)
	}
}

func (m *meshSkillReaderFixture) ResolveMeshSkill(_ context.Context, p identity.Principal, id, digest string, version int, trial bool) (mesh.AssetInput, error) {
	if !m.allow || p.UserID != "caller" || id != "skill" || digest != m.asset.Digest() || version != 1 || trial {
		return mesh.AssetInput{}, apperrors.ErrAccessDenied
	}
	return m.asset, nil
}

func TestAssistantMeshSkillReauthorizesFrozenPackage(t *testing.T) {
	asset := mesh.AssetInput{Kind: "skill", Title: "Project status", Body: "Use the authorized project status read.", Dependencies: []mesh.Dependency{{ID: "project-read", Version: "1"}}}
	reader := &meshSkillReaderFixture{asset: asset, allow: true}
	service := &Service{assistantMeshSkills: reader}
	run := domain.AgentRun{Input: map[string]any{"_sohaAssistantMeshSkill": domain.AssistantMeshSkill{
		AssetID: "skill", Digest: asset.Digest(), Version: 1,
	}}}
	ctx := context.Background()
	caller := identity.Principal{UserID: "caller"}
	content, err := service.assistantMeshContext(ctx, caller, run)
	if err != nil || !strings.Contains(content, asset.Body) {
		t.Fatalf("skill was not loaded for its caller: %v", err)
	}
	start := &domain.DSHStart{Assembly: &domain.DSHAssembly{Assets: []domain.RuntimeAssetRef{{Kind: "mcp", ID: "project-read", Version: "2"}}}}
	if err := service.requireAssistantMeshDependencies(ctx, caller, run, start); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("different dependency version: %v", err)
	}
	start.Assembly.Assets[0].Version = "1"
	if err := service.requireAssistantMeshDependencies(ctx, caller, run, start); err != nil || !start.Assembly.Assets[0].Required {
		t.Fatalf("required dependency not pinned: %v", err)
	}
	reader.allow = false
	if err := service.reauthorizeAssistantMeshRun(ctx, caller, run); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked run continued: %v", err)
	}
}
