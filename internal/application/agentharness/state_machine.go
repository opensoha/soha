package agentharness

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrTerminalRun       = errors.New("agent run is terminal")
	ErrInvalidTransition = errors.New("invalid agent run transition")
	ErrBudgetExceeded    = errors.New("agent run budget exceeded")
)

type RunState string

const (
	RunQueued             RunState = "queued"
	RunPreparing          RunState = "preparing"
	RunRunning            RunState = "running"
	RunWaitingApproval    RunState = "waiting_approval"
	RunWaitingEnvironment RunState = "waiting_environment"
	RunPaused             RunState = "paused"
	RunRejected           RunState = "rejected"
	RunCompleted          RunState = "completed"
	RunFailed             RunState = "failed"
	RunCancelled          RunState = "cancelled"
	RunBudgetExceeded     RunState = "budget_exceeded"
	RunTimedOut           RunState = "timed_out"
)

type StopReason string

const (
	StopAnswerAccepted StopReason = "answer_accepted"
	StopMaxSteps       StopReason = "max_steps"
	StopMaxTokens      StopReason = "max_tokens"
	StopMaxToolCalls   StopReason = "max_tool_calls"
	StopMaxCost        StopReason = "max_cost"
	StopDeadline       StopReason = "deadline"
	StopNoProgress     StopReason = "no_progress"
	StopUserCancelled  StopReason = "user_cancelled"
	StopPolicyDenied   StopReason = "policy_denied"
	StopProvider       StopReason = "provider_stop"
	StopEnvironment    StopReason = "environment_failure"
)

type Budget struct {
	MaxSteps     int       `json:"maxSteps"`
	MaxTokens    int64     `json:"maxTokens"`
	MaxToolCalls int       `json:"maxToolCalls"`
	MaxCost      float64   `json:"maxCost"`
	Deadline     time.Time `json:"deadline"`
}

type Usage struct {
	Steps     int     `json:"steps"`
	Tokens    int64   `json:"tokens"`
	ToolCalls int     `json:"toolCalls"`
	Cost      float64 `json:"cost"`
}

type ProgressTracker struct {
	MaxIdenticalActions int `json:"maxIdenticalActions"`
	lastFingerprint     string
	repeated            int
}

func (t *ProgressTracker) Observe(actionFingerprint string) (StopReason, error) {
	actionFingerprint = strings.TrimSpace(actionFingerprint)
	if actionFingerprint == "" {
		return "", fmt.Errorf("action fingerprint is required")
	}
	if t.MaxIdenticalActions <= 0 {
		return "", fmt.Errorf("max identical actions must be bounded")
	}
	if actionFingerprint == t.lastFingerprint {
		t.repeated++
	} else {
		t.lastFingerprint = actionFingerprint
		t.repeated = 1
	}
	if t.repeated >= t.MaxIdenticalActions {
		return StopNoProgress, nil
	}
	return "", nil
}

type Run struct {
	ID                     string     `json:"id"`
	Attempt                int        `json:"attempt"`
	State                  RunState   `json:"state"`
	StopReason             StopReason `json:"stopReason,omitempty"`
	ProviderID             string     `json:"providerId"`
	ProviderVersion        string     `json:"providerVersion"`
	PluginVersion          string     `json:"pluginVersion,omitempty"`
	AdapterProtocolVersion string     `json:"adapterProtocolVersion"`
	CatalogRevision        uint64     `json:"catalogRevision"`
	Budget                 Budget     `json:"budget"`
	Usage                  Usage      `json:"usage"`
	CheckpointRef          string     `json:"checkpointRef,omitempty"`
	UpdatedAt              time.Time  `json:"updatedAt"`
}

type Step struct {
	SchemaVersion   string     `json:"schemaVersion"`
	ID              string     `json:"id"`
	RunID           string     `json:"runId"`
	Sequence        int        `json:"sequence"`
	Attempt         int        `json:"attempt"`
	State           RunState   `json:"state"`
	ContextRef      string     `json:"contextRef"`
	ModelCallRef    string     `json:"modelCallRef,omitempty"`
	ToolCallRefs    []string   `json:"toolCallRefs,omitempty"`
	ObservationRefs []string   `json:"observationRefs,omitempty"`
	CheckpointRef   string     `json:"checkpointRef,omitempty"`
	StopReason      StopReason `json:"stopReason,omitempty"`
	StartedAt       time.Time  `json:"startedAt"`
	CompletedAt     time.Time  `json:"completedAt,omitempty"`
}

type Observation struct {
	SchemaVersion string    `json:"schemaVersion"`
	ID            string    `json:"id"`
	RunID         string    `json:"runId"`
	StepID        string    `json:"stepId"`
	Kind          string    `json:"kind"`
	Status        string    `json:"status"`
	Summary       string    `json:"summary"`
	ArtifactRefs  []string  `json:"artifactRefs,omitempty"`
	Redacted      bool      `json:"redacted"`
	ObservedAt    time.Time `json:"observedAt"`
}

type Checkpoint struct {
	SchemaVersion string    `json:"schemaVersion"`
	ID            string    `json:"id"`
	RunID         string    `json:"runId"`
	StepID        string    `json:"stepId"`
	Attempt       int       `json:"attempt"`
	Sequence      int       `json:"sequence"`
	StateHash     string    `json:"stateHash"`
	ContextRef    string    `json:"contextRef"`
	CreatedAt     time.Time `json:"createdAt"`
}

func NewRun(run Run, now time.Time) (Run, error) {
	if strings.TrimSpace(run.ID) == "" || strings.TrimSpace(run.ProviderID) == "" || strings.TrimSpace(run.ProviderVersion) == "" {
		return Run{}, fmt.Errorf("run id and pinned provider identity are required")
	}
	if run.CatalogRevision == 0 || strings.TrimSpace(run.AdapterProtocolVersion) == "" {
		return Run{}, fmt.Errorf("catalog revision and adapter protocol version are required")
	}
	if run.Budget.MaxSteps <= 0 || run.Budget.MaxTokens <= 0 || run.Budget.MaxToolCalls <= 0 || run.Budget.MaxCost <= 0 || !run.Budget.Deadline.After(now) {
		return Run{}, fmt.Errorf("agent run budget must be bounded")
	}
	run.State = RunQueued
	run.Attempt = 1
	run.UpdatedAt = now.UTC()
	return run, nil
}

func (r *Run) Transition(next RunState, reason StopReason, now time.Time) error {
	if r.Terminal() {
		return ErrTerminalRun
	}
	if !allowedTransition(r.State, next) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, r.State, next)
	}
	r.State = next
	r.StopReason = reason
	r.UpdatedAt = now.UTC()
	return nil
}

func (r *Run) ApplyUsage(delta Usage, now time.Time) error {
	if r.Terminal() {
		return ErrTerminalRun
	}
	if delta.Steps < 0 || delta.Tokens < 0 || delta.ToolCalls < 0 || delta.Cost < 0 {
		return fmt.Errorf("usage delta cannot be negative")
	}
	r.Usage.Steps += delta.Steps
	r.Usage.Tokens += delta.Tokens
	r.Usage.ToolCalls += delta.ToolCalls
	r.Usage.Cost += delta.Cost
	reason := r.budgetStopReason(now)
	if reason == "" {
		r.UpdatedAt = now.UTC()
		return nil
	}
	r.State = RunBudgetExceeded
	if reason == StopDeadline {
		r.State = RunTimedOut
	}
	r.StopReason = reason
	r.UpdatedAt = now.UTC()
	return ErrBudgetExceeded
}

func (r Run) Terminal() bool {
	switch r.State {
	case RunRejected, RunCompleted, RunFailed, RunCancelled, RunBudgetExceeded, RunTimedOut:
		return true
	default:
		return false
	}
}

func (r Run) budgetStopReason(now time.Time) StopReason {
	if !now.Before(r.Budget.Deadline) {
		return StopDeadline
	}
	if r.Usage.Steps >= r.Budget.MaxSteps {
		return StopMaxSteps
	}
	if r.Usage.Tokens >= r.Budget.MaxTokens {
		return StopMaxTokens
	}
	if r.Usage.ToolCalls >= r.Budget.MaxToolCalls {
		return StopMaxToolCalls
	}
	if r.Usage.Cost >= r.Budget.MaxCost {
		return StopMaxCost
	}
	return ""
}

func allowedTransition(from, to RunState) bool {
	allowed := map[RunState]map[RunState]bool{
		RunQueued:             {RunPreparing: true, RunCancelled: true},
		RunPreparing:          {RunRunning: true, RunFailed: true, RunCancelled: true},
		RunRunning:            {RunWaitingApproval: true, RunWaitingEnvironment: true, RunPaused: true, RunCompleted: true, RunFailed: true, RunCancelled: true, RunBudgetExceeded: true, RunTimedOut: true},
		RunWaitingApproval:    {RunRunning: true, RunRejected: true, RunCancelled: true, RunTimedOut: true},
		RunWaitingEnvironment: {RunRunning: true, RunFailed: true, RunCancelled: true, RunTimedOut: true},
		RunPaused:             {RunRunning: true, RunCancelled: true, RunTimedOut: true},
	}
	return allowed[from][to]
}

func ResumeFromCheckpoint(original Run, checkpoint Checkpoint, newRunID string, now time.Time) (Run, error) {
	if !original.Terminal() && original.State != RunPaused {
		return Run{}, fmt.Errorf("run must be paused or terminal before resume")
	}
	if checkpoint.RunID != original.ID || strings.TrimSpace(checkpoint.StateHash) == "" || strings.TrimSpace(newRunID) == "" {
		return Run{}, fmt.Errorf("checkpoint does not belong to original run")
	}
	resumed := original
	resumed.ID = newRunID
	resumed.Attempt = original.Attempt + 1
	resumed.State = RunQueued
	resumed.StopReason = ""
	resumed.CheckpointRef = checkpoint.ID
	resumed.Usage = Usage{}
	resumed.UpdatedAt = now.UTC()
	return resumed, nil
}
