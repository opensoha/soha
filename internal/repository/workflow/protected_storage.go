package workflow

import (
	"database/sql"
	"encoding/json"
	"fmt"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/protectedpayload"
)

const privateWorkflowMetadata = "privateCapabilityState"

func (r *Repository) marshalWorkflowMetadata(run domainworkflow.Run) ([]byte, error) {
	metadata := persistWorkflowMetadata(run.Metadata, run.NodeRuns, run.GatewayAuthorization)
	if run.Scope != domainworkflow.ScopeCapabilityTask || r.storageKeys.Active().ID() == "" {
		raw, err := json.Marshal(metadata)
		if run.Scope == domainworkflow.ScopeCapabilityTask && protectedpayload.ContainsNamedCall(raw, "k8s.resources.conditional.update", "k8s.pods.exec") {
			return nil, protectedpayload.ErrUnavailable
		}
		return raw, err
	}
	intent, err := domainworkflow.CapabilityIntentFrom(run)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(metadata)
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
		if encodeErr != nil || protectedpayload.ContainsNamedCall(raw, "k8s.resources.conditional.update", "k8s.pods.exec") {
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

func (r *Repository) scanWorkflow(rows *sql.Rows) (domainworkflow.Run, error) {
	run, err := scanWorkflow(rows)
	return r.restoreWorkflowStorage(run, err)
}
func (r *Repository) scanWorkflowRow(row *sql.Row) (domainworkflow.Run, error) {
	run, err := scanWorkflowRow(row)
	return r.restoreWorkflowStorage(run, err)
}
