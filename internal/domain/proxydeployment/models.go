// Package proxydeployment owns the lifecycle of installer-approved proxy slots.
package proxydeployment

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"regexp"
	"time"
)

const TaskKind = "proxy_deployment_control"

// Slot is installation policy, never a user-supplied workload specification.
type Slot struct {
	WorkloadKind         string   `json:"workloadKind,omitempty"`
	Members              []Member `json:"members"`
	ID                   string   `json:"id"`
	ProxyID              string   `json:"proxyId"`
	ExecutorID           string   `json:"executorId"`
	Namespace            string   `json:"namespace"`
	Name                 string   `json:"name"`
	UID                  string   `json:"uid"`
	TemplateDigest       string   `json:"templateDigest"`
	MaxReplicas          int32    `json:"maxReplicas"`
	AllowAdoption        bool     `json:"allowAdoption"`
	PreviousOwnerStopped bool     `json:"previousOwnerStopped"`
}

func (s Slot) ProviderKind() string { return "proxy_deployment." + s.ExecutorID }

type State struct {
	StopConfirmUntil        *time.Time `json:"stopConfirmUntil,omitempty"`
	DrainUntil              *time.Time `json:"drainUntil,omitempty"`
	DrainLeasePinned        bool       `json:"drainLeasePinned"`
	ObservedDesiredReplicas *int32     `json:"observedDesiredReplicas,omitempty"`
	ControlRevision         int64      `json:"controlRevision"`
	MemberSetHash           string     `json:"memberSetHash"`
	Members                 []Member   `json:"members"`
	Slot                    *Slot      `json:"approvedSlot,omitempty"`
	DrainHeld               bool       `json:"drainHeld,omitempty"`
	ProxyID                 string     `json:"proxyId"`
	SlotID                  string     `json:"slotId,omitempty"`
	Owner                   string     `json:"owner"`
	OwnerEpoch              int64      `json:"ownerEpoch"`
	UID                     string     `json:"workloadUid,omitempty"`
	Generation              int64      `json:"generation"`
	ObservedGeneration      int64      `json:"observedGeneration"`
	DesiredReplicas         int32      `json:"desiredReplicas"`
	ActualReplicas          *int32     `json:"actualReplicas,omitempty"`
	ReadyReplicas           *int32     `json:"readyReplicas,omitempty"`
	Phase                   string     `json:"phase"`
	Reason                  string     `json:"reason,omitempty"`
	LatestTaskID            string     `json:"latestTaskId,omitempty"`
	ObservedAt              *time.Time `json:"observedAt,omitempty"`
}

type BindingInput struct {
	ExpectedOwnerEpoch int64  `json:"expectedOwnerEpoch"`
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
	DrainUntil          time.Time `json:"drainUntil"`
	ProxyID             string    `json:"proxyId"`
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

type Member struct {
	InstanceID string `json:"instanceId"`
	RuntimeID  string `json:"runtimeId"`
	Ordinal    int32  `json:"ordinal"`
}
type ApprovedPool struct {
	ObservedReplicas             *int32
	DrainHeld                    bool
	Phase                        string
	Owner                        string
	ServingUntil                 *time.Time
	MasterID                     string
	OwnerEpoch                   int64
	MemberSetHash                string
	Members                      []Member
	MaxReplicas, DesiredReplicas int32
	WorkloadKind, UID            string
}
type InstanceControl = sohaapi.NetworkProxyDeploymentRuntimeControl
type InstanceACK = sohaapi.NetworkProxyDeploymentRuntimeAck
type BindingRequest = sohaapi.NetworkProxyDeploymentInput
type ControlRequest = sohaapi.NetworkProxyDeploymentControlInput
type DrainStatus struct {
	StopRequired      bool       `json:"stopRequired"`
	ConfirmUntil      *time.Time `json:"confirmUntil,omitempty"`
	Instances         int        `json:"instances"`
	ActiveChecks      int64      `json:"activeChecks"`
	AcceptingRequests bool       `json:"acceptingRequests"`
	CountKnown        bool       `json:"countKnown"`
	Ready             bool       `json:"ready"`
}

func (s Slot) MemberSetHash() string {
	data, _ := json.Marshal(s.Members)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data))
}
func (s State) RequiresDrain() bool {
	return s.Owner != "soha-managed" || s.DrainHeld || s.DesiredReplicas == 0 || s.Phase == "unknown"
}

// RetiringRange identifies only the ordinals removed by a trusted scale-down.
// Callers remain responsible for validating ownership, policy and observations.
type ReplicaRange struct{ First, End int32 }

func RetiringRange(target, actual, observedDesired, maximum int32) (ReplicaRange, bool) {
	if target < 0 || actual < 0 || observedDesired < 0 || maximum < 1 || target > maximum || actual > maximum || observedDesired > maximum {
		return ReplicaRange{}, false
	}
	old := max(actual, observedDesired)
	return ReplicaRange{First: min(target, old), End: old}, true
}

func (r ReplicaRange) Contains(ordinal int32) bool { return ordinal >= r.First && ordinal < r.End }
func (r ReplicaRange) Count() int32                { return r.End - r.First }
func PublicPhase(s State) string {
	switch s.Phase {
	case "converged":
		if s.DesiredReplicas == 0 {
			return "stopped"
		}
		return "ready"
	case "resuming":
		return "starting"
	case "reconciling":
		if s.DrainHeld {
			return "draining"
		}
		return "starting"
	case "queued", "unobserved":
		return "pending"
	case "unmanaged":
		return "unbound"
	case "degraded":
		return "failed"
	case "conflict":
		return "conflict"
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
	Resume      bool         `json:"resume,omitempty"`
}

func ValidatePolicy(slot Slot) error {
	if slot.ID == "" || slot.ProxyID == "" || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(slot.ExecutorID) || slot.UID == "" || slot.Name == "" || slot.Namespace == "" || slot.TemplateDigest == "" || slot.MaxReplicas < 1 || slot.MaxReplicas > 128 || len(slot.Members) != int(slot.MaxReplicas) {
		return fmt.Errorf("proxy slot policy is incomplete")
	}
	if slot.WorkloadKind != "" && slot.WorkloadKind != "Deployment" && slot.WorkloadKind != "StatefulSet" {
		return fmt.Errorf("invalid proxy workload kind")
	}
	if slot.WorkloadKind != "StatefulSet" && slot.MaxReplicas != 1 {
		return fmt.Errorf("deployment requires one independent runtime")
	}
	return validateMembers(slot)
}
func validateMembers(slot Slot) error {
	seenRuntime, seenInstance := map[string]bool{}, map[string]bool{}
	for i, member := range slot.Members {
		if member.Ordinal != int32(i) || member.InstanceID == "" || member.RuntimeID == "" || seenRuntime[member.RuntimeID] || seenInstance[member.InstanceID] {
			return fmt.Errorf("invalid independent proxy members")
		}
		if slot.WorkloadKind == "StatefulSet" && member.InstanceID != fmt.Sprintf("%s-%d", slot.Name, i) {
			return fmt.Errorf("member does not match StatefulSet ordinal")
		}
		seenRuntime[member.RuntimeID] = true
		seenInstance[member.InstanceID] = true
	}
	return nil
}
