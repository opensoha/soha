package dto

type ApprovalPolicyRequest struct {
	ID                string         `json:"id"`
	Key               string         `json:"key"`
	Name              string         `json:"name"`
	Description       string         `json:"description"`
	Mode              string         `json:"mode"`
	RequiredApprovals int            `json:"requiredApprovals"`
	SLAMinutes        int            `json:"slaMinutes"`
	ApproverRoles     []string       `json:"approverRoles"`
	ChangeWindow      map[string]any `json:"changeWindow"`
	Enabled           bool           `json:"enabled"`
	Metadata          map[string]any `json:"metadata"`
}

type ExecutionCallbackRequest struct {
	CallbackToken string         `json:"callbackToken"`
	Status        string         `json:"status"`
	Payload       map[string]any `json:"payload"`
}

type ClaimExecutionTaskRequest struct {
	AgentID         string   `json:"agentId"`
	ProviderKinds   []string `json:"providerKinds"`
	RuntimeEndpoint string   `json:"runtimeEndpoint"`
}

type ExecutionTaskActionRequest struct {
	Reason string `json:"reason"`
}
