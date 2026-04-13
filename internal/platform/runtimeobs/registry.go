package runtimeobs

import (
	"sync"
	"time"
)

const (
	ComponentClusterSync       = "cluster_sync"
	ComponentWorkflowRunner    = "workflow_runner"
	ComponentCopilotInspection = "copilot_inspection"
)

const (
	OutcomeSucceeded = "succeeded"
	OutcomeFailed    = "failed"
	OutcomeCanceled  = "canceled"
)

type ComponentSnapshot struct {
	Started         int64  `json:"started"`
	Succeeded       int64  `json:"succeeded"`
	Failed          int64  `json:"failed"`
	Canceled        int64  `json:"canceled"`
	LastStartedAt   string `json:"lastStartedAt,omitempty"`
	LastFinishedAt  string `json:"lastFinishedAt,omitempty"`
	LastDurationMS  int64  `json:"lastDurationMs,omitempty"`
	LastOperationID string `json:"lastOperationId,omitempty"`
	LastError       string `json:"lastError,omitempty"`
	QueueDepth      int    `json:"queueDepth,omitempty"`
	LastItems       int    `json:"lastItems,omitempty"`
}

type Snapshot struct {
	GeneratedAt       string            `json:"generatedAt"`
	ClusterSync       ComponentSnapshot `json:"clusterSync"`
	WorkflowRunner    ComponentSnapshot `json:"workflowRunner"`
	CopilotInspection ComponentSnapshot `json:"copilotInspection"`
}

type Registry struct {
	mu       sync.RWMutex
	snapshot Snapshot
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snapshot := r.snapshot
	snapshot.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	return snapshot
}

func (r *Registry) RecordStart(component, operationID string, queueDepth, items int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	current := r.component(component)
	current.Started++
	current.LastStartedAt = time.Now().UTC().Format(time.RFC3339)
	current.LastOperationID = operationID
	current.QueueDepth = queueDepth
	current.LastItems = items
}

func (r *Registry) SetQueueDepth(component string, queueDepth int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.component(component).QueueDepth = queueDepth
}

func (r *Registry) RecordFinish(component, operationID string, duration time.Duration, queueDepth, items int, outcome string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	current := r.component(component)
	current.LastFinishedAt = time.Now().UTC().Format(time.RFC3339)
	current.LastDurationMS = duration.Milliseconds()
	current.LastOperationID = operationID
	current.QueueDepth = queueDepth
	current.LastItems = items
	if err != nil {
		current.LastError = err.Error()
	} else {
		current.LastError = ""
	}

	switch outcome {
	case OutcomeCanceled:
		current.Canceled++
	case OutcomeFailed:
		current.Failed++
	default:
		current.Succeeded++
	}
}

func (r *Registry) component(name string) *ComponentSnapshot {
	switch name {
	case ComponentClusterSync:
		return &r.snapshot.ClusterSync
	case ComponentCopilotInspection:
		return &r.snapshot.CopilotInspection
	default:
		return &r.snapshot.WorkflowRunner
	}
}
