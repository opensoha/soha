package workflow

import (
	"database/sql"
	"encoding/json"
	"fmt"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/protectedpayload"
)

const privateWorkflowMetadata = "privateCapabilityState"

func (r *Repository) marshalWorkflowMetadata(run domainworkflow.Run) ([]byte, error) {
	metadata := persistWorkflowMetadata(run.Metadata, run.NodeRuns, run.GatewayAuthorization)
	raw, err := json.Marshal(metadata)
	if err != nil || run.Scope != domainworkflow.ScopeCapabilityTask {
		return raw, err
	}
	if r.storageKeys.Active().ID() == "" {
		if workflowRequiresSealing(raw) {
			return nil, protectedpayload.ErrUnavailable
		}
		return raw, nil
	}
	intent, err := domainworkflow.CapabilityIntentFrom(run)
	if err != nil {
		return nil, err
	}
	sealed, err := protectedpayload.Seal(r.storageKeys, workflowStorageBinding(run.ID, intent.ActorID), raw)
	if err != nil {
		return nil, fmt.Errorf("%w: private capability state requires credential encryption", apperrors.ErrServiceUnavailable)
	}
	// Only existing SQL admission/poll indexes stay visible. The intent, prepared
	// calls, archived revisions and their copies are sealed together.
	visible := map[string]any{privateWorkflowMetadata: sealed}
	for _, key := range []string{"capabilityActorId", "capabilityIdempotencyKey", "capabilityInitialDigest", "capabilityNextPollAt"} {
		if value, ok := metadata[key]; ok {
			visible[key] = value
		}
	}
	return json.Marshal(visible)
}

func workflowStorageBinding(id, actor string) string {
	raw, _ := json.Marshal([]string{"workflow-capability-state/v1", id, actor})
	return string(raw)
}

func (r *Repository) restoreWorkflowStorage(run domainworkflow.Run, err error) (domainworkflow.Run, error) {
	if err != nil || run.Scope != domainworkflow.ScopeCapabilityTask {
		return run, err
	}
	value, sealed := run.Metadata[privateWorkflowMetadata]
	if !sealed {
		raw, encodeErr := json.Marshal(run.Metadata)
		if encodeErr != nil || workflowRequiresSealing(raw) {
			return domainworkflow.Run{}, protectedpayload.ErrUnavailable
		}
		return run, nil
	} // Earlier admitted plans contain no protected calls.
	actor, _ := run.Metadata["capabilityActorId"].(string)
	raw, encodeErr := json.Marshal(value)
	if encodeErr != nil || actor == "" {
		return domainworkflow.Run{}, protectedpayload.ErrUnavailable
	}
	opened, openErr := protectedpayload.Open(r.storageKeys, workflowStorageBinding(run.ID, actor), raw)
	if openErr != nil {
		return domainworkflow.Run{}, protectedpayload.ErrUnavailable
	}
	var metadata map[string]any
	if protectedpayload.Decode(opened, &metadata) != nil {
		return domainworkflow.Run{}, protectedpayload.ErrUnavailable
	}
	for _, key := range []string{"capabilityActorId", "capabilityIdempotencyKey", "capabilityInitialDigest"} {
		if metadata[key] != run.Metadata[key] {
			return domainworkflow.Run{}, protectedpayload.ErrUnavailable
		}
	}
	// Poll scheduling is a public SQL index updated by the owning state machine.
	if poll, ok := run.Metadata["capabilityNextPollAt"]; ok {
		metadata["capabilityNextPollAt"] = poll
	}
	run.Metadata = metadata
	run.GatewayAuthorization, _ = metadata["gatewayAuthorizationCredential"].(string)
	delete(run.Metadata, "gatewayAuthorizationCredential")
	run.NodeRuns = extractWorkflowNodeRuns(metadata)
	intent, decodeErr := domainworkflow.CapabilityIntentFrom(run)
	if decodeErr != nil || intent.ActorID != actor {
		return domainworkflow.Run{}, protectedpayload.ErrUnavailable
	}
	return run, nil
}

// Classify the same call slots whether they come from a fresh run or a legacy row.
func workflowRequiresSealing(raw json.RawMessage) bool {
	var state struct {
		Intent    domainworkflow.CapabilityIntent     `json:"capabilityIntent"`
		Revisions []domainworkflow.CapabilityRevision `json:"capabilityRevisions"`
		Nodes     []domainworkflow.NodeRun            `json:"nodeRuns"`
	}
	if protectedpayload.Decode(raw, &state) != nil {
		return true
	}
	if domainai.RequiresProtectedPlanInput(state.Intent.Input.Plan) || workflowNodesRequireSealing(state.Nodes) {
		return true
	}
	for _, revision := range state.Revisions {
		if domainai.RequiresProtectedPlanInput(revision.Intent.Input.Plan) || workflowNodesRequireSealing(revision.Nodes) {
			return true
		}
	}
	return false
}

func workflowNodesRequireSealing(nodes []domainworkflow.NodeRun) bool {
	for _, node := range nodes {
		for _, call := range []*domainai.ToolInvocationRequest{node.PreparedCall, node.ControlCall} {
			if call != nil && domainai.RequiresProtectedToolInput(call.ToolName) {
				return true
			}
		}
		if workflowInvocationRequiresSealing(node.Invocation) {
			return true
		}
	}
	return false
}

func workflowInvocationRequiresSealing(invocation *domainai.ToolInvocationResult) bool {
	if invocation == nil {
		return false
	}
	if domainai.RequiresProtectedToolInput(invocation.ToolName) {
		return true
	}
	if task := invocation.Task; task != nil &&
		(domainai.RequiresProtectedToolInput(task.StatusCall.ToolName) ||
			task.CancelCall != nil && domainai.RequiresProtectedToolInput(task.CancelCall.ToolName)) {
		return true
	}
	if assessment := invocation.Assessment; assessment != nil {
		for _, evidence := range assessment.Evidence {
			if evidence.Reference != nil && domainai.RequiresProtectedToolInput(evidence.Reference.ToolName) {
				return true
			}
		}
	}
	return false
}

func (r *Repository) scanWorkflow(rows *sql.Rows) (domainworkflow.Run, error) {
	run, err := scanWorkflow(rows)
	return r.restoreWorkflowStorage(run, err)
}
func (r *Repository) scanWorkflowRow(row *sql.Row) (domainworkflow.Run, error) {
	run, err := scanWorkflowRow(row)
	return r.restoreWorkflowStorage(run, err)
}
