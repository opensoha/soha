package copilot

import (
	"encoding/json"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/protectedpayload"
)

func nativeCheckpointBinding(run domaincopilot.AgentRun) string {
	raw, _ := json.Marshal([]string{"native-checkpoint/v1", run.ID, run.CreatedBy, run.SessionID})
	return string(raw)
}

func (r *Repository) encodeNativeCheckpoint(run domaincopilot.AgentRun, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	if !json.Valid(raw) || protectedpayload.IsSealed(raw) {
		return nil, protectedpayload.ErrUnavailable
	}
	if r.storageKeys.Active().ID() == "" {
		if hasPrivateCheckpointInput(raw) {
			return nil, protectedpayload.ErrUnavailable
		}
		return raw, nil
	}
	if run.ID == "" || run.CreatedBy == "" || run.SessionID == "" {
		return nil, protectedpayload.ErrUnavailable
	}
	return protectedpayload.Seal(r.storageKeys, nativeCheckpointBinding(run), raw)
}

func (r *Repository) decodeNativeCheckpoint(run domaincopilot.AgentRun, raw json.RawMessage) (json.RawMessage, error) {
	if protectedpayload.IsSealed(raw) {
		return protectedpayload.Open(r.storageKeys, nativeCheckpointBinding(run), raw)
	}
	// Older Native checkpoints could not admit these capabilities. Never trust
	// an unsealed protected call just because this process has an active key.
	if hasPrivateCheckpointInput(raw) {
		return nil, protectedpayload.ErrUnavailable
	}
	return raw, nil
}

func hasPrivateCheckpointInput(raw json.RawMessage) bool {
	return protectedpayload.ContainsNamedCall(raw, "k8s.resources.conditional.update", "k8s.pods.exec", "k8s_resources_conditional_update", "k8s_pods_exec")
}
