package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	gateway "github.com/opensoha/soha/internal/application/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type dshAssemblyProfiles struct {
	RuntimeProfileStore
	allowed bool
}

type dshAuditReplayRuntime struct {
	DSHRuntime
	page  domain.DSHEventPage
	id    domain.DSHIdentity
	after int64
}

func (r *dshAuditReplayRuntime) Events(_ context.Context, id domain.DSHIdentity, after int64) (domain.DSHEventPage, error) {
	r.id, r.after = id, after
	return r.page, nil
}

func TestDSHHookRecoveryReadsLateObservationsWithoutChangingTerminalResult(t *testing.T) {
	service, store, cp := dshFixture(t)
	cp.RuntimeSettled = true
	cp.RuntimeSequence = 9
	cp.AssemblyDecision = &domain.DSHAssemblyDecision{DSHAssemblyReport: domain.DSHAssemblyReport{
		Assets: []domain.RuntimeAssetStatus{{Kind: "hook", ID: "tool-audit", Status: "loaded"}},
	}}
	cp.ToolNames = map[string]string{"observed": "tool_first", "late": "tool_second"}
	cp.HookObservations = []map[string]any{{"callId": "observed", "toolName": "tool_first", "outcome": "completed"}}
	store.lease.Checkpoint, _ = json.Marshal(cp)
	store.lease.Run.Status = "completed"
	store.lease.Run.Output = map[string]any{"runtimeHookPending": true, "answer": "original result"}
	id := cp.Start.DSHIdentity
	runtime := &dshAuditReplayRuntime{page: domain.DSHEventPage{Items: []domain.DSHEvent{
		{DSHIdentity: id, Sequence: 10, Type: "hook_observed", CallID: "observed", Outcome: "completed"},
		{DSHIdentity: id, Sequence: 11, Type: "hook_observed", CallID: "late", Outcome: "failed"},
	}}}
	service.dshRuntime = runtime
	run := service.recoverDSHHookObservations(t.Context(), store.lease.Run)
	observations, _ := run.Output["runtimeHooks"].([]map[string]any)
	if runtime.id != id || runtime.after != 9 || len(observations) != 2 || observations[1]["toolName"] != "tool_second" ||
		run.Status != "completed" || run.Output["answer"] != "original result" ||
		run.Output["runtimeHookPending"] != nil || store.lease.Run.Output["runtimeHookPending"] != true {
		t.Fatalf("recovered audit: %+v, %+v", run.Output, runtime)
	}
	runtime.page.Items[1].Fence++
	run = service.recoverDSHHookObservations(t.Context(), store.lease.Run)
	if run.Output["runtimeHooks"] != nil || run.Output["runtimeHookPending"] != true {
		t.Fatalf("wrong-fence audit was returned: %+v", run.Output)
	}
}

func (r *dshAssemblyProfiles) RuntimeProfileRevisionAuthorized(context.Context, string, int64, string) (bool, error) {
	return r.allowed, nil
}

func TestDSHAssemblyConfirmsAndFencesPluginDispatch(t *testing.T) {
	s, store, cp := dshFixture(t)
	profiles := &dshAssemblyProfiles{allowed: true}
	s.runtimeProfiles = profiles
	sessions := s.sessions.(*inspectionAuthzTestRepository)
	sessions.session.Metadata = map[string]any{"conversationKind": "personal_main"}
	sessions.accessStates = map[string]domain.PersonalAgentAccess{"reader": {Status: "approved"}}
	cp.Start.Profile = "heya"
	cp.Start.ToolCatalogRevision = "catalog"
	cp.Start.Assembly = &domain.DSHAssembly{ProfileID: "heya-default", Revision: 2, Assets: []domain.RuntimeAssetRef{{Kind: "mcp", ID: "evidence", Version: "1", Required: true}}}
	cp.Start.Prompt = "query"
	store.lease.Checkpoint, _ = json.Marshal(cp)
	id := cp.Start.DSHIdentity
	if _, err := s.InvokeDSHModel(t.Context(), "secret", DSHModelCall{DSHIdentity: id}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("unconfirmed model request: %v", err)
	}
	name := "mcp_evidence_probe"
	tool := domain.DSHNativeTool{Kind: "mcp", AssetID: "evidence", Version: "1", Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)}
	report := domain.DSHAssemblyReport{SchemaVersion: dshAssemblySchema, Kind: "assembly_report", RunID: id.RunID, Attempt: id.Attempt, Fence: id.Fence, ProfileID: "heya-default", Revision: 2,
		Assets: []domain.RuntimeAssetStatus{{Kind: "mcp", ID: "evidence", Version: "1", Status: "loaded", Capabilities: []string{name}}}, NativeTools: []domain.DSHNativeTool{tool}}
	wrong := report
	wrong.NativeTools = []domain.DSHNativeTool{{Kind: "mcp", AssetID: "other", Version: "1", Name: "mcp_other_probe", InputSchema: tool.InputSchema}}
	if _, err := s.ConfirmDSHAssembly(t.Context(), "secret", wrong); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("unauthorized asset: %v", err)
	}
	decision, err := s.ConfirmDSHAssembly(t.Context(), "secret", report)
	if err != nil || decision.Status != "confirmed" {
		t.Fatalf("confirmation: %+v %v", decision, err)
	}
	if _, err := s.ConfirmDSHAssembly(t.Context(), "secret", report); err != nil {
		t.Fatalf("exact confirmation retry: %v", err)
	}
	cp, err = readDSHCheckpoint(store.lease)
	if err != nil {
		t.Fatal(err)
	}
	input := json.RawMessage(`{"target":"owned"}`)
	hash := sha256.Sum256(input)
	cp.ToolHashes["plugin-call"] = dshHash(map[string]any{"target": "owned"})
	cp.ToolNames["plugin-call"] = name
	cp.Pending = []nativeCall{{State: "plugin_prepared", ToolName: name, Call: gateway.WorkbenchToolCall{ID: "plugin-call", Name: name, Arguments: input}}}
	store.lease.Checkpoint, _ = json.Marshal(cp)
	call := domain.DSHToolCall{SchemaVersion: dshAssemblySchema, Kind: "tool_call", RunID: id.RunID, Attempt: id.Attempt, Fence: id.Fence, CallID: "plugin-call", ToolName: name, ToolCatalogRevision: "catalog", InputHash: hex.EncodeToString(hash[:]), Input: input}
	if _, err := s.InvokeDSHTool(t.Context(), "secret", call); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("plugin reached platform bridge: %v", err)
	}
	delete(sessions.accessStates, "reader")
	if _, err := s.AuthorizeDSHPluginTool(t.Context(), "secret", call); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked Heya received dispatch permission: %v", err)
	}
	sessions.accessStates["reader"] = domain.PersonalAgentAccess{Status: "approved"}
	if previous, err := s.AuthorizeDSHPluginTool(t.Context(), "secret", call); previous != nil || err != nil {
		t.Fatalf("one dispatch permission: %+v %v", previous, err)
	}
	if _, err := s.AuthorizeDSHPluginTool(t.Context(), "secret", call); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("duplicate dispatch permitted: %v", err)
	}
	result := domain.DSHToolResult{SchemaVersion: dshAssemblySchema, Kind: "tool_result", RunID: id.RunID, Attempt: id.Attempt, Fence: id.Fence, CallID: call.CallID, InputHash: call.InputHash, Status: "completed", Output: json.RawMessage(`{"value":"evidence"}`)}
	delete(sessions.accessStates, "reader")
	if _, err := s.SettleDSHPluginTool(t.Context(), "secret", result); err != nil {
		t.Fatalf("settlement: %v", err)
	}
	sessions.accessStates["reader"] = domain.PersonalAgentAccess{Status: "approved"}
	if previous, err := s.AuthorizeDSHPluginTool(t.Context(), "secret", call); previous == nil || previous.Status != "completed" || err != nil {
		t.Fatalf("completed receipt was not reused: %+v %v", previous, err)
	}
	profiles.allowed = false
	if _, err := s.AuthorizeDSHPluginTool(t.Context(), "secret", call); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked profile resumed: %v", err)
	}
}

func TestDSHAssemblyReportsRequiredAndOptionalFailure(t *testing.T) {
	report := domain.DSHAssemblyReport{ProfileID: "chat-default", Revision: 1,
		Assets: []domain.RuntimeAssetStatus{{Kind: "skill", ID: "observability", Version: "1", Status: "unavailable", Code: "dependency_unavailable"}}}
	assembly := domain.DSHAssembly{ProfileID: report.ProfileID, Revision: 1, Assets: []domain.RuntimeAssetRef{{Kind: "skill", ID: "observability", Version: "1"}}}
	for _, wanted := range []string{"degraded", "rejected"} {
		decision, err := decideDSHAssembly(assembly, report)
		if err != nil || decision.Status != wanted {
			t.Fatalf("expected %s: %+v %v", wanted, decision, err)
		}
		assembly.Assets[0].Required = true
	}
}

type dshAuditPages struct {
	dshTestRuntime
	events []domain.DSHEvent
}

func (f *dshAuditPages) Events(_ context.Context, _ domain.DSHIdentity, after int64) (domain.DSHEventPage, error) {
	if after >= int64(len(f.events)) {
		return domain.DSHEventPage{NextSequence: after}, nil
	}
	return domain.DSHEventPage{Items: f.events[after : after+1], NextSequence: after + 1, HasMore: after+1 < int64(len(f.events))}, nil
}

func TestDSHUnknownCancellationDrainsTerminalAuditPages(t *testing.T) {
	s, store, cp := dshFixture(t)
	id := cp.Start.DSHIdentity
	cp.StopReason = "unknown_outcome"
	cp.Receipt = &domain.DSHReceipt{DSHSubmission: domain.DSHSubmission{DSHIdentity: id, InputHash: cp.Start.InputHash, SubmissionID: "receipt"}}
	cp.AssemblyDecision = &domain.DSHAssemblyDecision{Status: "confirmed", DSHAssemblyReport: domain.DSHAssemblyReport{Assets: []domain.RuntimeAssetStatus{{Kind: "hook", ID: "tool-audit", Version: "1", Status: "loaded"}}}}
	cp.ToolNames = map[string]string{"observed": "mcp_evidence_probe"}
	store.lease.Phase = "cancelling"
	store.lease.CancelRequested = true
	store.lease.Checkpoint, _ = json.Marshal(cp)
	s.dshRuntime = &dshAuditPages{
		dshTestRuntime: dshTestRuntime{status: domain.DSHStatus{DSHSubmission: cp.Receipt.DSHSubmission, Status: "canceled", Sequence: 2}},
		events: []domain.DSHEvent{
			{DSHIdentity: id, Sequence: 1, Type: "message_delta", Delta: "aborted output"},
			{DSHIdentity: id, Sequence: 2, Type: "hook_observed", CallID: "observed", ToolName: "mcp_evidence_probe", Outcome: "completed"},
		},
	}
	done, err := s.reconcileDSHStatus(t.Context(), store.lease, cp)
	if err != nil || !done || store.lease.Run.Status != "canceled" || len(store.final) != 1 || store.final[0].Metadata["runtimeHooks"] == nil {
		t.Fatalf("terminal reconciliation incomplete: %v %v %+v", done, err, store.lease.Run.Output)
	}
	for _, event := range store.events {
		if event.Type == "message.delta" {
			t.Fatal("aborted output was replayed")
		}
	}
}
