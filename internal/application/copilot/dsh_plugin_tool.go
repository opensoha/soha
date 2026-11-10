package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	gateway "github.com/opensoha/soha/internal/application/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// AuthorizeDSHPluginTool issues a single dispatch permission for the frozen
// head call. A lost response does not authorize the runtime to retry.
func (s *Service) AuthorizeDSHPluginTool(ctx context.Context, grant string, call domain.DSHToolCall) (*domain.DSHToolResult, error) {
	lock := s.dshLock(call.RunID)
	lock.Lock()
	defer lock.Unlock()
	id := domain.DSHIdentity{RunID: call.RunID, Attempt: call.Attempt, Fence: call.Fence}
	lease, cp, _, ctx, err := s.authorizeDSHCallback(ctx, grant, id)
	if err != nil {
		return nil, err
	}
	tool, err := frozenDSHPluginCall(cp, call)
	if err != nil {
		return nil, err
	}
	if previous, ok := cp.ToolResults[call.CallID]; ok {
		if previous.InputHash != call.InputHash || previous.Status != "completed" {
			return nil, apperrors.ErrConflict
		}
		return &previous, nil
	}
	if cp.PluginDispatches[call.CallID] != "" {
		return nil, apperrors.ErrConflict
	}
	if output, ok := dshToolObservation(cp, call.CallID); ok {
		skipped := domain.DSHToolResult{SchemaVersion: dshAssemblySchema, Kind: "tool_result", RunID: call.RunID, Attempt: call.Attempt, Fence: call.Fence, CallID: call.CallID, InputHash: call.InputHash, Status: "completed", Output: output}
		cp.ToolResults[call.CallID] = skipped
		_, err := s.saveDSH(ctx, lease, cp, lease.Phase, nil)
		return &skipped, err
	}
	if len(cp.Pending) == 0 || cp.Pending[0].Call.ID != call.CallID || cp.Pending[0].State != "plugin_prepared" {
		return nil, apperrors.ErrConflict
	}
	if err := reserveNativeWorkCall(ctx, s.nativeRuns, lease, &cp.nativeCheckpoint); err != nil {
		return nil, err
	}
	if cp.PluginDispatches == nil {
		cp.PluginDispatches = make(map[string]string)
	}
	cp.PluginDispatches[call.CallID] = call.InputHash
	execution := domain.ToolExecution{ID: lease.Run.ID + ":" + call.CallID, AdapterID: tool.AssetID, ToolName: tool.Name, Status: "running", StartedAt: time.Now().UTC()}
	lease.Run.ToolExecutions = append(slices.Clone(lease.Run.ToolExecutions), execution)
	_, err = s.saveDSH(ctx, lease, cp, "tools", nil)
	return nil, err
}

func frozenDSHPluginCall(cp dshCheckpoint, call domain.DSHToolCall) (domain.DSHNativeTool, error) {
	var input map[string]any
	hash := sha256.Sum256(call.Input)
	if call.SchemaVersion != dshAssemblySchema || call.Kind != "tool_call" || call.ToolCatalogRevision != cp.Start.ToolCatalogRevision ||
		len(call.Input) > 131072 || json.Unmarshal(call.Input, &input) != nil || input == nil || call.InputHash != hex.EncodeToString(hash[:]) ||
		cp.ToolNames[call.CallID] != call.ToolName || cp.ToolHashes[call.CallID] != dshHash(input) {
		return domain.DSHNativeTool{}, apperrors.ErrAccessDenied
	}
	for _, tool := range dshNativeTools(cp) {
		if tool.Name == call.ToolName {
			return tool, nil
		}
	}
	return domain.DSHNativeTool{}, apperrors.ErrAccessDenied
}

func (s *Service) SettleDSHPluginTool(ctx context.Context, grant string, result domain.DSHToolResult) (domain.DSHToolResult, error) {
	if err := validateDSHPluginResult(result); err != nil {
		return domain.DSHToolResult{}, err
	}
	lock := s.dshLock(result.RunID)
	lock.Lock()
	defer lock.Unlock()
	id := domain.DSHIdentity{RunID: result.RunID, Attempt: result.Attempt, Fence: result.Fence}
	// Settlement records an already dispatched action. Live authorization is
	// checked again on every model or tool dispatch, not on this receipt.
	lease, cp, err := s.boundDSHCallback(ctx, grant, id)
	if err != nil {
		return domain.DSHToolResult{}, err
	}
	if cp.PluginDispatches[result.CallID] != result.InputHash {
		return domain.DSHToolResult{}, apperrors.ErrAccessDenied
	}
	if previous, ok := cp.ToolResults[result.CallID]; ok {
		if dshHash(previous) != dshHash(result) {
			return domain.DSHToolResult{}, apperrors.ErrConflict
		}
		return previous, nil
	}
	if len(cp.Pending) == 0 || cp.Pending[0].Call.ID != result.CallID || cp.Pending[0].State != "plugin_prepared" {
		return domain.DSHToolResult{}, apperrors.ErrConflict
	}
	cp.ToolResults[result.CallID] = result
	finishDSHPluginExecution(&lease.Run, result.CallID, result.Status)
	if result.Status == "completed" {
		cp.Messages = append(cp.Messages, gateway.WorkbenchRelayMessage{Role: "tool", ToolCallID: result.CallID, Content: string(result.Output)})
		cp.Pending = cp.Pending[1:]
	}
	if _, err := s.saveDSH(ctx, lease, cp, "running", nil); err != nil {
		return domain.DSHToolResult{}, err
	}
	return result, nil
}

func validateDSHPluginResult(result domain.DSHToolResult) error {
	if result.SchemaVersion != dshAssemblySchema || result.Kind != "tool_result" || len(result.InputHash) != sha256.Size*2 || result.CallID == "" {
		return apperrors.ErrInvalidArgument
	}
	switch result.Status {
	case "completed":
		if result.ErrorCode == "" && len(result.Output) <= 131072 && json.Valid(result.Output) {
			return nil
		}
	case "failed":
		if result.ErrorCode == "plugin_tool_failed" && len(result.Output) == 0 {
			return nil
		}
	case "unknown":
		if result.ErrorCode == "plugin_outcome_unknown" && len(result.Output) == 0 {
			return nil
		}
	}
	return apperrors.ErrInvalidArgument
}

func finishDSHPluginExecution(run *domain.AgentRun, id, status string) {
	run.ToolExecutions = slices.Clone(run.ToolExecutions)
	for i := range run.ToolExecutions {
		if run.ToolExecutions[i].ID == run.ID+":"+id {
			now := time.Now().UTC()
			run.ToolExecutions[i].Status = status
			run.ToolExecutions[i].CompletedAt = &now
			return
		}
	}
}

func markUnsettledDSHPluginExecutions(run *domain.AgentRun, cp dshCheckpoint) {
	for id := range cp.PluginDispatches {
		if _, ok := cp.ToolResults[id]; !ok {
			finishDSHPluginExecution(run, id, "unknown")
		}
	}
}
