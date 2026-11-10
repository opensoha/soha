package copilot

import "encoding/json"

type DSHIdentity struct {
	RunID   string
	Attempt int64
	Fence   int64
}
type DSHSubmission struct {
	DSHIdentity
	InputHash    string
	SubmissionID string
}
type DSHContextMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type DSHTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	ReadOnly    bool
}
type DSHStart struct {
	DSHIdentity
	OwnerKey, SessionID, RequestID, InputHash, ContextRevision, Profile, Model, Prompt, ToolCatalogRevision, ToolGrant, Deadline string
	Context                                                                                                                      []DSHContextMessage
	Tools                                                                                                                        []DSHTool
	MaxOutputTokens                                                                                                              int64
	Assembly                                                                                                                     *DSHAssembly
}
type DSHAssembly struct {
	ProfileID string            `json:"profileId"`
	Revision  int64             `json:"revision"`
	Assets    []RuntimeAssetRef `json:"assets"`
}

type RuntimeAssetStatus struct {
	Kind         string   `json:"kind"`
	ID           string   `json:"id"`
	Version      string   `json:"version"`
	Status       string   `json:"status"`
	Code         string   `json:"code,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type RuntimeAssembly struct {
	ProfileID string               `json:"profileId"`
	Revision  int64                `json:"revision"`
	Status    string               `json:"status"`
	Assets    []RuntimeAssetStatus `json:"assets"`
}

type DSHAssemblyReport struct {
	SchemaVersion string               `json:"schemaVersion"`
	Kind          string               `json:"kind"`
	RunID         string               `json:"runId"`
	Attempt       int64                `json:"attempt"`
	Fence         int64                `json:"fence"`
	ProfileID     string               `json:"profileId"`
	Revision      int64                `json:"revision"`
	Assets        []RuntimeAssetStatus `json:"assets"`
	NativeTools   []DSHNativeTool      `json:"nativeTools,omitempty"`
}

type DSHNativeTool struct {
	Kind        string          `json:"kind"`
	AssetID     string          `json:"assetId"`
	Version     string          `json:"version"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type DSHAssemblyDecision struct {
	DSHAssemblyReport
	Status string `json:"status"`
}
type DSHReceipt struct {
	DSHSubmission
	Status   string
	Sequence int64
}
type DSHUsage struct{ InputTokens, OutputTokens, TotalTokens int64 }
type DSHStatus struct {
	DSHSubmission
	Status, StopReason, AssistantText string
	Sequence                          int64
	UsageKnown                        bool
	Usage                             *DSHUsage
}

func (s DSHStatus) Terminal() bool {
	return s.Status == "completed" || s.Status == "failed" || s.Status == "canceled"
}

type DSHEvent struct {
	DSHIdentity
	Sequence                                            int64
	Type                                                string
	Delta, AssistantText, Status, StopReason, ErrorCode string
	CallID, ToolName, Outcome                           string
}
type DSHEventPage struct {
	Items        []DSHEvent
	NextSequence int64
	HasMore      bool
}
type DSHToolCall struct {
	SchemaVersion       string          `json:"schemaVersion"`
	Kind                string          `json:"kind"`
	RunID               string          `json:"runId"`
	Attempt             int64           `json:"attempt"`
	Fence               int64           `json:"fence"`
	CallID              string          `json:"callId"`
	ToolName            string          `json:"toolName"`
	ToolCatalogRevision string          `json:"toolCatalogRevision"`
	InputHash           string          `json:"inputHash"`
	Input               json.RawMessage `json:"input"`
}
type DSHToolResult struct {
	SchemaVersion string          `json:"schemaVersion"`
	Kind          string          `json:"kind"`
	RunID         string          `json:"runId"`
	Attempt       int64           `json:"attempt"`
	Fence         int64           `json:"fence"`
	CallID        string          `json:"callId"`
	InputHash     string          `json:"inputHash"`
	Status        string          `json:"status"`
	Output        json.RawMessage `json:"output,omitempty"`
	ErrorCode     string          `json:"errorCode,omitempty"`
}
