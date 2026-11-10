package copilot

import (
	"encoding/json"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
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
	if !json.Valid(raw) || nativeCheckpointIsEnvelope(raw) {
		return nil, protectedpayload.ErrUnavailable
	}
	if r.storageKeys.Active().ID() == "" {
		if run.ProviderKind == "dsh" || nativeCheckpointRequiresSealing(raw) {
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
	if nativeCheckpointIsEnvelope(raw) {
		return protectedpayload.Open(r.storageKeys, nativeCheckpointBinding(run), raw)
	}
	// Older Native checkpoints could not admit these capabilities. Never trust
	// an unsealed protected call just because this process has an active key.
	if (run.ProviderKind == "dsh" && len(raw) > 2) || nativeCheckpointRequiresSealing(raw) {
		return nil, protectedpayload.ErrUnavailable
	}
	return raw, nil
}

func nativeCheckpointIsEnvelope(raw json.RawMessage) bool {
	// Reserve the envelope fields even when the version cannot be opened.
	var header struct {
		Format     json.RawMessage `json:"format"`
		Ciphertext json.RawMessage `json:"ciphertext"`
	}
	return json.Unmarshal(raw, &header) == nil && (header.Format != nil || header.Ciphertext != nil)
}

// The application keeps a model-facing name as well as the resolved tool name.
// Project only those call slots from the persisted Native checkpoint and journal.
type checkpointModelCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func nativeCheckpointRequiresSealing(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var checkpoint struct {
		Messages []struct {
			ToolCalls []checkpointModelCall `json:"ToolCalls"`
		} `json:"messages"`
		Pending []struct {
			Call     checkpointModelCall `json:"call"`
			ToolName string              `json:"toolName"`
		} `json:"pending"`
		Tasks []struct {
			Input domainai.CapabilityTaskInput `json:"input"`
		} `json:"tasks"`
	}
	if protectedpayload.Decode(raw, &checkpoint) != nil {
		return true
	}
	for _, message := range checkpoint.Messages {
		for _, call := range message.ToolCalls {
			if checkpointCallRequiresSealing("", call) {
				return true
			}
		}
	}
	for _, pending := range checkpoint.Pending {
		if checkpointCallRequiresSealing(pending.ToolName, pending.Call) {
			return true
		}
	}
	for _, task := range checkpoint.Tasks {
		if domainai.RequiresProtectedPlanInput(task.Input.Plan) {
			return true
		}
	}
	return false
}

func checkpointCallRequiresSealing(tool string, call checkpointModelCall) bool {
	if checkpointProtectedTool(tool) || checkpointProtectedTool(call.Name) {
		return true
	}
	// Only the governed task tool interprets its arguments as a capability plan.
	if tool != "harness.request_task" && call.Name != "harness_request_task" && call.Name != "harness.request_task" {
		return false
	}
	if len(call.Arguments) == 0 {
		return false
	}
	var input struct {
		Plan domainai.CapabilityPlan `json:"plan"`
	}
	return protectedpayload.Decode(call.Arguments, &input) != nil || domainai.RequiresProtectedPlanInput(input.Plan)
}

func checkpointProtectedTool(tool string) bool {
	switch tool {
	case "k8s_resources_conditional_update":
		tool = domainai.ProtectedResourceUpdateTool
	case "k8s_pods_exec":
		tool = domainai.ProtectedPodExecTool
	}
	return domainai.RequiresProtectedToolInput(tool)
}
