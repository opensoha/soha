// Package dshruntime implements the trusted Core-to-dsh service protocol.
// These transport types follow soha-contracts/agent/dsh-runtime.schema.json;
// they are not public user API or application/domain contracts.
package dshruntime

import (
	"encoding/json"

	domain "github.com/opensoha/soha/internal/domain/copilot"
)

const SchemaVersion = "opensoha.dev/dsh-runtime/v1"
const maxSafeInteger int64 = 9007199254740991
const maxBody = 1 << 20

type Identity struct {
	RunID   string `json:"runId"`
	Attempt int64  `json:"attempt"`
	Fence   int64  `json:"fence"`
}

// Submission is the frozen receipt supplied by the consumer for reconciliation.
type Submission struct {
	Identity
	InputHash    string `json:"inputHash"`
	SubmissionID string `json:"submissionId"`
}

type ContextMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	ReadOnly    bool            `json:"readOnly"`
}

type StartRequest struct {
	Identity
	OwnerKey            string              `json:"ownerKey"`
	SessionID           string              `json:"sessionId"`
	RequestID           string              `json:"requestId"`
	InputHash           string              `json:"inputHash"`
	ContextRevision     string              `json:"contextRevision"`
	Profile             string              `json:"profile"`
	Model               string              `json:"model"`
	Prompt              string              `json:"prompt"`
	Context             []ContextMessage    `json:"context,omitempty"`
	ToolCatalogRevision string              `json:"toolCatalogRevision"`
	Tools               []Tool              `json:"tools,omitempty"`
	ToolGrant           string              `json:"toolGrant,omitempty"`
	Deadline            string              `json:"deadline"`
	MaxOutputTokens     int64               `json:"maxOutputTokens"`
	Assembly            *domain.DSHAssembly `json:"assembly,omitempty"`
}

type startWire struct {
	SchemaVersion string `json:"schemaVersion"`
	Kind          string `json:"kind"`
	StartRequest
}

type Receipt struct {
	SchemaVersion string `json:"schemaVersion"`
	Kind          string `json:"kind"`
	Submission
	Status   string `json:"status"`
	Sequence int64  `json:"sequence"`
}

type Usage struct {
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	TotalTokens  int64 `json:"totalTokens"`
}

type Status struct {
	SchemaVersion string `json:"schemaVersion"`
	Kind          string `json:"kind"`
	Submission
	Status        string `json:"status"`
	Sequence      int64  `json:"sequence"`
	StopReason    string `json:"stopReason,omitempty"`
	UsageKnown    bool   `json:"usageKnown"`
	Usage         *Usage `json:"usage,omitempty"`
	AssistantText string `json:"assistantText,omitempty"`
}

// Terminal describes service facts only; idle and unknown do not prove completion.
func (s Status) Terminal() bool {
	return s.Status == "completed" || s.Status == "failed" || s.Status == "canceled"
}

type EventData struct {
	Delta         string `json:"delta,omitempty"`
	AssistantText string `json:"assistantText,omitempty"`
	CallID        string `json:"callId,omitempty"`
	ToolName      string `json:"toolName,omitempty"`
	Status        string `json:"status,omitempty"`
	StopReason    string `json:"stopReason,omitempty"`
	ErrorCode     string `json:"errorCode,omitempty"`
	Outcome       string `json:"outcome,omitempty"`
	UsageKnown    *bool  `json:"usageKnown,omitempty"`
	Usage         *Usage `json:"usage,omitempty"`
}

type Event struct {
	SchemaVersion string `json:"schemaVersion"`
	Kind          string `json:"kind"`
	Identity
	Sequence int64     `json:"sequence"`
	Type     string    `json:"type"`
	Data     EventData `json:"data"`
}

type EventPage struct {
	Items        []Event `json:"items"`
	NextSequence int64   `json:"nextSequence"`
	HasMore      bool    `json:"hasMore"`
}

type Health struct {
	SchemaVersion   string   `json:"schemaVersion"`
	Kind            string   `json:"kind"`
	Ready           bool     `json:"ready"`
	ProtocolVersion string   `json:"protocolVersion"`
	RuntimeVersion  string   `json:"runtimeVersion"`
	Capabilities    []string `json:"capabilities"`
}

type controlWire struct {
	SchemaVersion string `json:"schemaVersion"`
	Kind          string `json:"kind"`
	Identity
	Reason string `json:"reason,omitempty"`
}
