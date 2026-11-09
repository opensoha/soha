// Package outpostruntime owns the lifecycle of installer-approved Outpost slots.
package outpostruntime

import "time"

const (
	TaskKind        = "outpost_runtime_control"
	ControlLeaseTTL = 30 * time.Second
)

// Slot is installation policy, never a user-supplied workload specification.
type Slot struct {
	ID                   string `json:"id"`
	OutpostID            string `json:"outpostId"`
	ExecutorID           string `json:"executorId"`
	Namespace            string `json:"namespace"`
	Name                 string `json:"name"`
	UID                  string `json:"uid"`
	TemplateDigest       string `json:"templateDigest"`
	MaxReplicas          int32  `json:"maxReplicas"`
	AllowAdoption        bool   `json:"allowAdoption"`
	PreviousOwnerStopped bool   `json:"previousOwnerStopped"`
}

func (s Slot) ProviderKind() string { return "outpost_runtime." + s.ExecutorID }

type State struct {
	DrainHeld          bool       `json:"drainHeld,omitempty"`
	OutpostID          string     `json:"outpostId"`
	SlotID             string     `json:"slotId,omitempty"`
	Owner              string     `json:"owner"`
	OwnerEpoch         int64      `json:"ownerEpoch"`
	UID                string     `json:"workloadUid,omitempty"`
	Generation         int64      `json:"generation"`
	ObservedGeneration int64      `json:"observedGeneration"`
	DesiredReplicas    int32      `json:"desiredReplicas"`
	ActualReplicas     *int32     `json:"actualReplicas,omitempty"`
	ReadyReplicas      *int32     `json:"readyReplicas,omitempty"`
	Phase              string     `json:"phase"`
	Reason             string     `json:"reason,omitempty"`
	LatestTaskID       string     `json:"latestTaskId,omitempty"`
	ProjectedTaskID    string     `json:"projectedTaskId,omitempty"`
	ObservedAt         *time.Time `json:"observedAt,omitempty"`
}

type BindingInput struct {
	SlotID             string `json:"slotId"`
	ExpectedGeneration int64  `json:"expectedGeneration"`
	RequestID          string `json:"requestId"`
	ExplicitAdoption   bool   `json:"explicitAdoption"`
}

type ControlInput struct {
	Action              string `json:"action"`
	Replicas            *int32 `json:"replicas,omitempty"`
	ExpectedGeneration  int64  `json:"expectedGeneration"`
	RequestID           string `json:"requestId"`
	DrainTimeoutSeconds int    `json:"drainTimeoutSeconds,omitempty"`
}

type Command struct {
	OutpostID           string    `json:"outpostId"`
	Slot                Slot      `json:"slot"`
	Generation          int64     `json:"generation"`
	OwnerEpoch          int64     `json:"ownerEpoch"`
	Action              string    `json:"action"`
	Replicas            int32     `json:"replicas"`
	DrainTimeoutSeconds int       `json:"drainTimeoutSeconds"`
	Deadline            time.Time `json:"deadline"`
}

type Observation struct {
	UID             string    `json:"uid"`
	ResourceVersion string    `json:"resourceVersion"`
	TemplateDigest  string    `json:"templateDigest"`
	Replicas        int32     `json:"replicas"`
	DesiredReplicas int32     `json:"desiredReplicas"`
	ReadyReplicas   int32     `json:"readyReplicas"`
	ObservedAt      time.Time `json:"observedAt"`
}

type InstanceControl struct {
	ProtocolVersion string    `json:"protocolVersion"`
	Generation      int64     `json:"generation"`
	Action          string    `json:"action"`
	IssuedAt        time.Time `json:"issuedAt"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type InstanceObservation struct {
	CredentialHash       string    `json:"-"`
	ConfigurationVersion int64     `json:"-"`
	OutpostID            string    `json:"outpostId"`
	InstanceID           string    `json:"instanceId"`
	ProcessEpoch         string    `json:"processEpoch"`
	Generation           int64     `json:"generation"`
	AcceptingRequests    bool      `json:"acceptingRequests"`
	ActiveChecks         int64     `json:"activeChecks"`
	ObservedAt           time.Time `json:"observedAt"`
	LeaseExpiresAt       time.Time `json:"leaseExpiresAt"`
}

// InstanceCredential is passed only by the authenticated identity runtime.
// It is never a public DTO or part of a task/audit payload.
type InstanceCredential struct {
	TokenHash            string    `json:"-"`
	ConfigurationVersion int64     `json:"-"`
	ExpiresAt            time.Time `json:"-"`
}

func (state State) RequiresDrain() bool {
	if state.Owner != "soha" || state.LatestTaskID == "" {
		return false
	}
	if state.DrainHeld {
		return true
	}
	if state.DesiredReplicas == 0 {
		return true
	}
	shrinking := state.ActualReplicas != nil && state.DesiredReplicas < *state.ActualReplicas
	return shrinking && state.Phase != "resuming" && state.Phase != "converged"
}

func ControlForState(state State, now time.Time) InstanceControl {
	control := InstanceControl{ProtocolVersion: "v1", Generation: state.Generation, Action: "serve", IssuedAt: now, ExpiresAt: now.Add(ControlLeaseTTL)}
	if state.RequiresDrain() {
		control.Action = "drain"
	}
	return control
}

type DrainStatus struct {
	Instances         int   `json:"instances"`
	ActiveChecks      int64 `json:"activeChecks"`
	AcceptingRequests bool  `json:"acceptingRequests"`
}

func PublicPhase(state State) string {
	switch state.Phase {
	case "converged":
		if state.DesiredReplicas == 0 {
			return "stopped"
		}
		return "ready"
	case "queued", "unobserved", "unmanaged":
		return "pending"
	case "reconciling", "resuming":
		if state.DesiredReplicas == 0 {
			return "draining"
		}
		return "applying"
	case "degraded", "conflict":
		return "failed"
	default:
		return "unknown"
	}
}

type Receipt struct {
	Generation  int64        `json:"generation"`
	OwnerEpoch  int64        `json:"ownerEpoch"`
	Observation *Observation `json:"observation,omitempty"`
	Uncertain   bool         `json:"uncertain"`
	Reason      string       `json:"reason,omitempty"`
	Drained     bool         `json:"drained"`
}
