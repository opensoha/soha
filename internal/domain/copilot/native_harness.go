package copilot

import (
	"encoding/json"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

const NativeWorkToolCallLimit = 8
const NativeCleanupToolCallLimit = 8

// Cleanup has a separate, bounded reserve so exhausting work cannot prevent
// cancellation. Both counters are durable and shared by the parent and children.
type NativeToolUsage struct {
	WorkCalls    int
	CleanupCalls int
	WorkLimit    int
	CleanupLimit int
}

// Native uses the existing AgentRun identity; this lease is private worker state.
type NativeLease struct {
	Run             AgentRun
	Version         int64
	Fence           int64
	Phase           string
	Sequence        int64
	Checkpoint      json.RawMessage
	RequestHash     string
	CancelRequested bool
	LeaseUntil      *time.Time
	ToolUsage       NativeToolUsage
}

type NativeAdvance struct {
	FinalMessage *Message
	Run          AgentRun
	Phase        string
	StopReason   string
	Checkpoint   json.RawMessage
	Events       []WorkbenchStreamEvent
	Release      bool
}

// NativeTaskParent binds an existing Workflow task to its original Native run.
// A worker takeover changes the lease fence, but never this request identity.
type NativeTaskParent struct {
	RunID       string    `json:"runId"`
	OwnerID     string    `json:"ownerId"`
	SessionID   string    `json:"sessionId"`
	RequestHash string    `json:"requestHash"`
	Deadline    time.Time `json:"deadline"`
}

type TaskIntent = sohaapi.WorkbenchTaskIntent

type NativeEventPage struct {
	Items          []WorkbenchStreamEvent `json:"items"`
	NextSequence   int64                  `json:"nextSequence"`
	HasMore        bool                   `json:"hasMore"`
	ResyncRequired bool                   `json:"resyncRequired"`
}

func NativeRunProjection(run AgentRun) AgentRun {
	run.ExecutionOwner = "external"
	if run.ProviderKind != "native" {
		return run
	}
	run.ExecutionOwner = "native"
	run.Phase, _ = run.Output["phase"].(string)
	run.StopReason, _ = run.Output["stopReason"].(string)
	run.UsageKnown, _ = run.Output["usageKnown"].(bool)
	if n, ok := run.Output["eventSequence"].(float64); ok {
		run.EventSequence = int64(n)
	}
	return run
}
