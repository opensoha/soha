package networkaccess

import (
	"slices"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

// VPNGovernanceScope identifies the authorization being saved or revalidated.
type VPNGovernanceScope struct {
	RuntimeID   string
	SubjectID   string
	DeviceID    string
	SiteID      string
	SpaceID     string
	Mode        string
	Profile     string
	ResourceIDs []string
}

type VPNGovernancePath int

const (
	VPNGovernanceLegacy VPNGovernancePath = iota + 1
	VPNGovernanceAdmission
	VPNGovernanceResource
	VPNGovernanceWorkload
)

// VPNGovernanceFacts contains the transaction-local inputs for a network
// admission or a single resource decision. The caller supplies the clock.
type VPNGovernanceFacts struct {
	Scope       VPNGovernanceScope
	Subject     domain.Subject
	Device      domain.Device
	Site        domain.Site
	Space       domain.Space
	Resource    domain.Resource
	Snapshot    domain.PolicySnapshot
	EvaluatedAt time.Time
}

// VPNGovernanceEvaluator lets a transaction adapter collect the facts needed
// by the policy and evaluate them before committing a grant or revocation.
type VPNGovernanceEvaluator interface {
	Path(VPNGovernanceScope, domain.Site, domain.PolicySnapshot) VPNGovernancePath
	HasManagementSelectors(domain.PolicySnapshot) bool
	WorkloadScopeMatches(VPNGovernanceScope, domain.WorkloadBinding, string) bool
	WorkloadResourceMatches(domain.WorkloadBinding, string) bool
	Allows(VPNGovernancePath, VPNGovernanceFacts) bool
}

type VPNGovernancePolicy struct{}

func (VPNGovernancePolicy) Path(scope VPNGovernanceScope, site domain.Site, snapshot domain.PolicySnapshot) VPNGovernancePath {
	if strings.HasPrefix(scope.SubjectID, "service_account:") {
		return VPNGovernanceWorkload
	}
	if site.EffectiveDeviceGovernance() == domain.DeviceGovernanceLegacy && !slices.ContainsFunc(snapshot.Policies, func(policy domain.Policy) bool {
		return hasManagementSelector(policy) && matchNetworkValue(policy.SiteIDs, site.ID) && matchNetworkValue(policy.Modes, scope.Mode)
	}) {
		return VPNGovernanceLegacy
	}
	if scope.Mode == domain.ModeExternalVPN {
		return VPNGovernanceAdmission
	}
	return VPNGovernanceResource
}

func hasManagementSelector(policy domain.Policy) bool {
	return policy.Enabled && len(policy.DeviceManagementStates) != 0
}

func (VPNGovernancePolicy) HasManagementSelectors(snapshot domain.PolicySnapshot) bool {
	return slices.ContainsFunc(snapshot.Policies, hasManagementSelector)
}

func (VPNGovernancePolicy) WorkloadScopeMatches(scope VPNGovernanceScope, binding domain.WorkloadBinding, credentialSubjectID string) bool {
	return credentialSubjectID == scope.SubjectID && binding.DeviceID == scope.DeviceID && binding.SiteID == scope.SiteID &&
		binding.NetworkSpaceID == scope.SpaceID && scope.Mode == domain.ModeExternalDirectZTNA && len(scope.ResourceIDs) != 0
}

func (VPNGovernancePolicy) WorkloadResourceMatches(binding domain.WorkloadBinding, resourceID string) bool {
	return slices.Contains(binding.ResourceIDs, resourceID)
}

func (VPNGovernancePolicy) Allows(path VPNGovernancePath, facts VPNGovernanceFacts) bool {
	scope := facts.Scope
	switch path {
	case VPNGovernanceLegacy:
		return true
	case VPNGovernanceAdmission:
		input := AdmissionInput{SubjectUserID: scope.SubjectID, DeviceID: scope.DeviceID, SiteID: scope.SiteID, Mode: scope.Mode}
		decision := evaluateAdmissionAt(facts.EvaluatedAt, input, facts.Subject, facts.Device, facts.Site, facts.Snapshot)
		return decision.Decision == domain.DecisionAllow && decision.NetworkProfile == scope.Profile
	case VPNGovernanceResource, VPNGovernanceWorkload:
		if facts.Resource.ID == "" {
			return false
		}
		input := PreviewInput{SubjectUserID: scope.SubjectID, DeviceID: scope.DeviceID, SiteID: scope.SiteID, ResourceID: facts.Resource.ID, Mode: scope.Mode}
		if path == VPNGovernanceWorkload {
			profile := evaluateWorkloadPolicyAt(facts.EvaluatedAt, input, facts.Subject, facts.Device, facts.Site, facts.Space, facts.Resource, facts.Snapshot)
			return profile != "" && profile == scope.Profile
		}
		decision := evaluatePolicyAt(facts.EvaluatedAt, input, facts.Subject, facts.Device, facts.Site, facts.Space, facts.Resource, facts.Snapshot)
		return decision.Decision == domain.DecisionAllow && decision.NetworkProfile == scope.Profile
	default:
		return false
	}
}
