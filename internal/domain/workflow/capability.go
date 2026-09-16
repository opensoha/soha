package workflow

import (
	"encoding/json"
	"fmt"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
)

const ScopeCapabilityTask = "capability_task"

type CapabilityIntent struct {
	Input          domainaigateway.CapabilityTaskInput `json:"input"`
	ActorID        string                              `json:"actorId"`
	ActorTokenID   string                              `json:"actorTokenId,omitempty"`
	ActorSessionID string                              `json:"actorSessionId,omitempty"`
	Digest         string                              `json:"digest"`
	PlanVersion    int                                 `json:"planVersion"`
	Deadline       time.Time                           `json:"deadline"`
}

// Revisions retain the frozen plan and every previous child reference. They do
// not run independently; only the current Run can be claimed by the worker.
type CapabilityRevision struct {
	Intent     CapabilityIntent `json:"intent"`
	Nodes      []NodeRun        `json:"nodes"`
	Status     string           `json:"status"`
	Version    int64            `json:"version"`
	ArchivedAt string           `json:"archivedAt"`
}

func CapabilityRevisionsFrom(run Run) ([]CapabilityRevision, error) {
	var revisions []CapabilityRevision
	raw, err := json.Marshal(run.Metadata["capabilityRevisions"])
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &revisions)
	return revisions, err
}

func CapabilityIntentFrom(run Run) (CapabilityIntent, error) {
	var intent CapabilityIntent
	if run.Scope != ScopeCapabilityTask {
		return intent, fmt.Errorf("workflow is not a capability task")
	}
	raw, err := json.Marshal(run.Metadata["capabilityIntent"])
	if err != nil {
		return intent, err
	}
	if err := json.Unmarshal(raw, &intent); err != nil {
		return intent, err
	}
	if intent.ActorID == "" || intent.Digest == "" || intent.Deadline.IsZero() {
		return intent, fmt.Errorf("capability intent is incomplete")
	}
	return intent, nil
}

func AssessCapabilityRun(run Run, plan domainaigateway.CapabilityPlan) domainaigateway.CapabilityAssessment {
	assessment := domainaigateway.CapabilityAssessment{Verdict: "satisfied", Summary: "all frozen verification steps satisfied", Evidence: []domainaigateway.CapabilityEvidence{}}
	if len(plan.VerificationSteps) == 0 {
		assessment.Verdict = "inconclusive"
	}
	for _, id := range plan.VerificationSteps {
		found := false
		for _, node := range run.NodeRuns {
			if node.NodeID != id || node.Status != "completed" || node.Invocation == nil || node.Invocation.Assessment == nil {
				continue
			}
			found = true
			current := node.Invocation.Assessment
			assessment.Evidence = append(assessment.Evidence, current.Evidence...)
			if current.Verdict == "unsatisfied" {
				assessment.Verdict = "unsatisfied"
			} else if assessment.Verdict != "unsatisfied" && (current.Verdict != "satisfied" || len(current.Evidence) == 0) {
				assessment.Verdict = "inconclusive"
			}
		}
		if !found && assessment.Verdict != "unsatisfied" {
			assessment.Verdict = "inconclusive"
		}
	}
	if assessment.Verdict != "satisfied" {
		assessment.Summary = "goal verification is " + string(assessment.Verdict)
	}
	return assessment
}

func CapabilityNodeDeadline(intent CapabilityIntent, node NodeRun) time.Time {
	deadline := intent.Deadline
	started, err := time.Parse(time.RFC3339, node.StartedAt)
	if err != nil {
		return deadline
	}
	for _, step := range intent.Input.Plan.Steps {
		if step.ID == node.NodeID && step.TimeoutSeconds > 0 {
			stepDeadline := started.Add(time.Duration(step.TimeoutSeconds) * time.Second)
			if stepDeadline.Before(deadline) {
				return stepDeadline
			}
		}
	}
	return deadline
}
