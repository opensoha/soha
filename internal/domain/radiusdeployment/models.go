// Package radiusdeployment owns managed, installer-approved FreeRADIUS pools.
package radiusdeployment

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const TaskKind = "radius_deployment_control"
const PoolPrefix = "radius-pool:"
const MemberPrefix = "radius-member:"
const Capability = "radius-managed-runtime-v1"

type Member struct {
	MemberID      string `json:"memberId"`
	RuntimeID     string `json:"runtimeId"`
	Ordinal       int32  `json:"ordinal"`
	Endpoint      string `json:"endpoint"`
	CredentialRef string `json:"credentialRef"`
}
type Configuration struct {
	Material       api.NetworkRADIUSConfiguration `json:"material"`
	EAPServerTrust api.NetworkEAPServerTrust      `json:"eapServerTrust"`
}
type Slot struct {
	NASID          string          `json:"nasId"`
	NASName        string          `json:"nasName"`
	AccessMedium   string          `json:"accessMedium"`
	SSID           string          `json:"ssid,omitempty"`
	SiteID         string          `json:"siteId"`
	NASBindingID   string          `json:"nasBindingId"`
	ID             string          `json:"id"`
	ExecutorID     string          `json:"executorId"`
	Namespace      string          `json:"namespace"`
	Name           string          `json:"name"`
	UID            string          `json:"uid"`
	WorkloadKind   string          `json:"workloadKind"`
	TemplateDigest string          `json:"templateDigest"`
	ServiceAccount string          `json:"serviceAccount"`
	MaxReplicas    int32           `json:"maxReplicas"`
	Members        []Member        `json:"members"`
	Configurations []Configuration `json:"configurations"`
}

func (s Slot) ProviderKind() string { return "radius_deployment." + s.ExecutorID }
func (s Slot) Hash() string {
	encoded, _ := json.Marshal(s)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(encoded))
}
func (s Slot) Configuration(reference string) (Configuration, bool) {
	for _, c := range s.Configurations {
		if c.Material.Reference == reference {
			return c, true
		}
	}
	return Configuration{}, false
}

type State struct {
	DrainProven             bool                  `json:"drainProven"`
	Pool                    api.NetworkRADIUSPool `json:"pool"`
	Slot                    Slot                  `json:"slot"`
	SlotHash                string                `json:"slotHash"`
	Configuration           Configuration         `json:"configuration"`
	ControlRevision         int64                 `json:"controlRevision"`
	DrainUntil              *time.Time            `json:"drainUntil,omitempty"`
	StopConfirmUntil        *time.Time            `json:"stopConfirmUntil,omitempty"`
	RetiringFrom            int32                 `json:"retiringFrom"`
	RetiringEnd             int32                 `json:"retiringEnd"`
	ObservedDesiredReplicas *int32                `json:"observedDesiredReplicas,omitempty"`
	ObservedAt              *time.Time            `json:"observedAt,omitempty"`
}

func (s State) Retiring(ordinal int32) bool {
	return ordinal >= s.RetiringFrom && ordinal < s.RetiringEnd
}
func (s State) ConfigurationHash() string {
	encoded, _ := json.Marshal(struct {
		Pool, Binding, Site string
		Configuration       Configuration
	}{s.Pool.PoolID, s.Pool.NASBindingID, s.Pool.SiteID, s.Configuration})
	return fmt.Sprintf("sha256:%x", sha256.Sum256(encoded))
}

type MemberRecord struct {
	Member         Member                           `json:"member"`
	Control        *api.NetworkRADIUSRuntimeControl `json:"control,omitempty"`
	ACK            *api.NetworkRADIUSRuntimeACK     `json:"ack,omitempty"`
	ReceivedAt     *time.Time                       `json:"receivedAt,omitempty"`
	NASUsedAt      *time.Time                       `json:"nasUsedAt,omitempty"`
	UnknownLatched bool                             `json:"unknownLatched"`
}

func (r MemberRecord) Fresh(state State, now time.Time) bool {
	c, a := r.Control, r.ACK
	return !r.UnknownLatched && c != nil && a != nil && r.ReceivedAt != nil && r.ReceivedAt.After(now.Add(-15*time.Second)) && c.ControlLeaseUntil.After(now) && c.ConfigurationLeaseUntil.After(now) && int64(a.ControlRevision) == state.ControlRevision && a.OwnerEpoch == state.Pool.OwnerEpoch && a.ConfigurationHash == state.Pool.ConfigurationHash && a.ConfigurationRevision == state.Pool.ConfigurationRevision && a.PoolID == state.Pool.PoolID && a.MemberID == r.Member.MemberID && a.RuntimeID == r.Member.RuntimeID && a.CredentialID == c.CredentialID && a.ProcessEpoch == c.ProcessEpoch
}
func (r MemberRecord) KnownZero(state State, now time.Time) bool {
	return r.Fresh(state, now) && r.ACK.StateCountKnown && r.ACK.ActiveStates != nil && *r.ACK.ActiveStates == 0 && !r.ACK.AcceptingNewAuthentications
}
func (r MemberRecord) Ready(state State, now time.Time) bool {
	return r.Fresh(state, now) && r.Control.RunProcess && !r.Control.StopRequired && r.Control.ServeExistingUntil.After(now) && r.ACK.Ready && r.ACK.ConfigurationApplied && r.ACK.HooksReady && r.ACK.StateCountKnown && r.ACK.ActiveStates != nil && !r.ACK.ProcessStopped
}

type Command struct {
	PoolID     string    `json:"poolId"`
	Slot       Slot      `json:"slot"`
	SlotHash   string    `json:"slotHash"`
	Revision   int64     `json:"revision"`
	OwnerEpoch int64     `json:"ownerEpoch"`
	Action     string    `json:"action"`
	Replicas   int32     `json:"replicas"`
	Deadline   time.Time `json:"deadline"`
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
type Receipt struct {
	Drained     bool         `json:"drained"`
	Revision    int64        `json:"revision"`
	OwnerEpoch  int64        `json:"ownerEpoch"`
	Observation *Observation `json:"observation,omitempty"`
	Uncertain   bool         `json:"uncertain"`
	Reason      string       `json:"reason,omitempty"`
}
type DrainStatus struct {
	DrainProven      bool       `json:"drainProven"`
	ExpectedMembers  int        `json:"expectedMembers"`
	ConfirmedMembers int        `json:"confirmedMembers"`
	CountKnown       bool       `json:"countKnown"`
	ActiveStates     *int64     `json:"activeStates,omitempty"`
	AcceptingNew     bool       `json:"acceptingNew"`
	Ready            bool       `json:"ready"`
	Deadline         *time.Time `json:"deadline,omitempty"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var hash = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var secretRef = regexp.MustCompile(`^secretref:[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)

func ValidateSlot(slot Slot) error {
	if !validSlotIdentity(slot) {
		return apperrors.ErrInvalidArgument
	}
	if slot.WorkloadKind != "StatefulSet" && (slot.WorkloadKind != "Deployment" || slot.MaxReplicas != 1) {
		return apperrors.ErrInvalidArgument
	}
	if !validSlotMedium(slot) {
		return apperrors.ErrInvalidArgument
	}
	if err := validateMembers(slot); err != nil {
		return err
	}
	if err := validateConfigurations(slot.Configurations); err != nil {
		return err
	}
	encoded, _ := json.Marshal(slot)
	if len(encoded) > 256<<10 {
		return apperrors.ErrInvalidArgument
	}
	return nil
}
func ValidIdentity(value string) bool { return identifier.MatchString(value) }
func validSlotIdentity(slot Slot) bool {
	for _, value := range []string{slot.ID, slot.SiteID, slot.NASBindingID, slot.NASID, slot.UID} {
		if !identifier.MatchString(value) {
			return false
		}
	}
	for _, value := range []string{slot.Namespace, slot.Name, slot.ServiceAccount} {
		if value == "" || len(value) > 253 || !regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*[a-z0-9]$|^[a-z0-9]$`).MatchString(value) {
			return false
		}
	}
	return slot.NASName != "" && regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(slot.ExecutorID) && hash.MatchString(slot.TemplateDigest) && slot.MaxReplicas > 0 && slot.MaxReplicas <= 128 && len(slot.Members) == int(slot.MaxReplicas)
}
func validateMembers(slot Slot) error {
	seen := map[string]bool{}
	for index, member := range slot.Members {
		host, port, err := net.SplitHostPort(member.Endpoint)
		numericPort, portErr := strconv.Atoi(port)
		if err != nil || host == "" || portErr != nil || numericPort < 1 || numericPort > 65535 || member.Ordinal != int32(index) || !identifier.MatchString(member.MemberID) || !strings.HasPrefix(member.RuntimeID, MemberPrefix) || !identifier.MatchString(member.RuntimeID) || seen[member.RuntimeID] || seen[member.MemberID] || seen["endpoint:"+member.Endpoint] || seen["credential:"+member.CredentialRef] || !secretRef.MatchString(member.CredentialRef) {
			return apperrors.ErrInvalidArgument
		}
		if slot.WorkloadKind == "StatefulSet" && member.MemberID != fmt.Sprintf("%s-%d", slot.Name, index) {
			return apperrors.ErrInvalidArgument
		}
		seen[member.RuntimeID], seen[member.MemberID] = true, true
		seen["endpoint:"+member.Endpoint], seen["credential:"+member.CredentialRef] = true, true
	}
	return nil
}
func validateConfigurations(configurations []Configuration) error {
	if len(configurations) < 1 || len(configurations) > 16 {
		return apperrors.ErrInvalidArgument
	}
	seen := map[string]bool{}
	for _, c := range configurations {
		m := c.Material
		if !identifier.MatchString(m.Reference) || seen[m.Reference] || !hash.MatchString(m.TreeHash) || !m.HooksAttested {
			return apperrors.ErrInvalidArgument
		}
		seen[m.Reference] = true
		for _, ref := range []string{m.NasSharedSecretRef, m.EapServerCertificateRef, m.EapServerKeyRef, m.EapCARef} {
			if !secretRef.MatchString(ref) {
				return apperrors.ErrInvalidArgument
			}
		}
	}
	return nil
}

// SSID limits use original UTF-8 bytes, matching the persisted NAS constraint.
// Wired absence is represented as NULL by the repository, never a fake SSID.
func validSlotMedium(slot Slot) bool {
	switch slot.AccessMedium {
	case "wired":
		return slot.SSID == ""
	case "wifi":
		return len(slot.SSID) >= 1 && len(slot.SSID) <= 32
	default:
		return false
	}
}
