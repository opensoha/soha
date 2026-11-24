package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/opensoha/soha/internal/application/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestDSHReceptionCallbackFenceAndZeroTools(t *testing.T) {
	for _, name := range []string{"confirmed", "unconfirmed", "native_tools", "final_tools", "tool_history", "wrong_grant", "stale_fence", "cancelled", "tool_response"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			start := domain.DSHStart{
				DSHIdentity:     domain.DSHIdentity{RunID: dshReceptionPrefix + "fixture", Attempt: 1, Fence: 1},
				ContextRevision: "context", ToolCatalogRevision: "catalog", Model: dshReceptionModel, ToolGrant: "grant",
				Assembly: &domain.DSHAssembly{ProfileID: dshReceptionProfile, Revision: 1, Assets: []domain.RuntimeAssetRef{}},
			}
			calls := 0
			round := &dshReceptionRound{ctx: ctx, start: start, invoke: func(context.Context) (aigateway.WorkbenchRelayResponse, error) {
				calls++
				answer := aigateway.WorkbenchRelayResponse{Content: "authorized answer content", Usage: map[string]float64{"inputTokens": 42}}
				if name == "tool_response" {
					answer.ToolCalls = []aigateway.WorkbenchToolCall{{Name: "fixture"}}
				}
				return answer, nil
			}}
			s := &Service{dshReception: &dshReceptionRounds{active: map[string]*dshReceptionRound{start.RunID: round}}}
			body := []byte(`{"model":"soha-workbench"}`)
			hash := sha256.Sum256(body)
			call := DSHModelCall{DSHIdentity: start.DSHIdentity, CallID: "model-1", ContextRevision: start.ContextRevision,
				ToolCatalogRevision: start.ToolCatalogRevision, InputHash: hex.EncodeToString(hash[:]), RawBody: body,
				Request: aigateway.WorkbenchRelayRequest{PublicModel: start.Model},
			}
			report := domain.DSHAssemblyReport{SchemaVersion: dshAssemblySchema, Kind: "assembly_report",
				RunID: start.RunID, Attempt: start.Attempt, Fence: start.Fence,
				ProfileID: start.Assembly.ProfileID, Revision: start.Assembly.Revision, Assets: []domain.RuntimeAssetStatus{},
			}
			grant := configureDSHReceptionCallback(name, start.ToolGrant, &report, &call)
			if name != "unconfirmed" {
				_, err := s.ConfirmDSHAssembly(ctx, start.ToolGrant, report)
				if (err != nil) != (name == "native_tools") {
					t.Fatalf("assembly authorization: %v", err)
				}
			}
			if name == "cancelled" {
				cancel()
			}
			reply, err := s.InvokeDSHModel(t.Context(), grant, call)
			if name == "confirmed" {
				if err != nil || calls != 1 || reply.Content != dshReceptionCompleted || reply.Usage != nil {
					t.Fatalf("expected one isolated reply, calls=%d reply=%q error=%v", calls, reply.Content, err)
				}
				if _, err := s.InvokeDSHModel(t.Context(), grant, call); !errors.Is(err, apperrors.ErrConflict) || calls != 1 {
					t.Fatalf("repeated dispatch: calls=%d error=%v", calls, err)
				}
				res, err := round.result(domain.DSHStatus{Status: "completed", AssistantText: dshReceptionCompleted})
				if err != nil || res.Content != "authorized answer content" {
					t.Fatalf("answer did not stay in Core: %q %v", res.Content, err)
				}
			} else {
				expectCalls := 0
				if name == "tool_response" {
					expectCalls = 1
				}
				if !errors.Is(err, apperrors.ErrAccessDenied) || calls != expectCalls || reply.Content != "" {
					t.Fatalf("rejected callback reached provider or DSH: calls=%d reply=%q error=%v", calls, reply.Content, err)
				}
			}
		})
	}
}

func configureDSHReceptionCallback(name, grant string, report *domain.DSHAssemblyReport, call *DSHModelCall) string {
	switch name {
	case "native_tools":
		report.NativeTools = []domain.DSHNativeTool{{Name: "fixture"}}
	case "final_tools":
		call.Request.Tools = []aigateway.WorkbenchTool{{Name: "fixture"}}
	case "tool_history":
		call.Request.Messages = []aigateway.WorkbenchRelayMessage{{Role: "tool"}}
	case "wrong_grant":
		return "wrong"
	case "stale_fence":
		call.Fence++
	}
	return grant
}
