package networkaccess

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func newPolicySnapshot(policies []domainnetworkaccess.Policy, protectedResourceIDs []string, publishedAt time.Time) (domainnetworkaccess.PolicySnapshot, error) {
	policies = slices.Clone(policies)
	protectedResourceIDs = slices.Clone(protectedResourceIDs)
	for index := range policies {
		policies[index].Subjects.Roles = sortedStrings(policies[index].Subjects.Roles)
		policies[index].Subjects.Users = sortedStrings(policies[index].Subjects.Users)
		policies[index].Subjects.Teams = sortedStrings(policies[index].Subjects.Teams)
		policies[index].Subjects.Tags = sortedStrings(policies[index].Subjects.Tags)
		policies[index].SiteIDs = sortedStrings(policies[index].SiteIDs)
		policies[index].ResourceIDs = sortedStrings(policies[index].ResourceIDs)
		policies[index].Modes = sortedStrings(policies[index].Modes)
		policies[index].DeviceManagementStates = sortedStrings(policies[index].DeviceManagementStates)
		policies[index].DeviceStatuses = sortedStrings(policies[index].DeviceStatuses)
		policies[index].PostureStatuses = sortedStrings(policies[index].PostureStatuses)
	}
	sort.Slice(policies, func(left, right int) bool {
		if policies[left].Priority == policies[right].Priority {
			return policies[left].ID < policies[right].ID
		}
		return policies[left].Priority < policies[right].Priority
	})
	sort.Strings(protectedResourceIDs)
	for _, policy := range policies {
		if err := validatePolicyInput(domainnetworkaccess.PolicyInput{Name: policy.Name, Enabled: policy.Enabled, Priority: policy.Priority, Effect: policy.Effect, Subjects: policy.Subjects, SiteIDs: policy.SiteIDs, ResourceIDs: policy.ResourceIDs, Modes: policy.Modes, DeviceManagementStates: policy.DeviceManagementStates, DeviceStatuses: policy.DeviceStatuses, PostureStatuses: policy.PostureStatuses, AccessProfile: policy.AccessProfile}); err != nil {
			return domainnetworkaccess.PolicySnapshot{}, fmt.Errorf("compile network policy %s: %w", policy.ID, err)
		}
	}
	if err := validateAllowPrecedence(policies); err != nil {
		return domainnetworkaccess.PolicySnapshot{}, err
	}
	payload, err := json.Marshal(struct {
		Policies             []domainnetworkaccess.Policy `json:"policies"`
		ProtectedResourceIDs []string                     `json:"protectedResourceIds"`
	}{Policies: policies, ProtectedResourceIDs: protectedResourceIDs})
	if err != nil {
		return domainnetworkaccess.PolicySnapshot{}, err
	}
	digest := sha256.Sum256(payload)
	return domainnetworkaccess.PolicySnapshot{ContentHash: fmt.Sprintf("sha256:%x", digest), PolicyCount: len(policies), ProtectedResourceCount: len(protectedResourceIDs), PublishedAt: publishedAt, Policies: policies, ProtectedResourceIDs: protectedResourceIDs}, nil
}

func validateAllowPrecedence(policies []domainnetworkaccess.Policy) error {
	for index, left := range policies {
		if !left.Enabled || left.Effect != domainnetworkaccess.PolicyEffectAllow {
			continue
		}
		for _, right := range policies[index+1:] {
			if !right.Enabled || right.Effect != domainnetworkaccess.PolicyEffectAllow || left.Priority != right.Priority || left.AccessProfile == right.AccessProfile {
				continue
			}
			// Users are scalar; a user may hold multiple roles, teams and tags.
			if policySelectorsOverlap(left.Subjects.Users, right.Subjects.Users) &&
				policySelectorsOverlap(left.SiteIDs, right.SiteIDs) &&
				policySelectorsOverlap(left.ResourceIDs, right.ResourceIDs) &&
				policySelectorsOverlap(left.Modes, right.Modes) &&
				policySelectorsOverlap(left.DeviceManagementStates, right.DeviceManagementStates) &&
				policySelectorsOverlap(left.DeviceStatuses, right.DeviceStatuses) &&
				policySelectorsOverlap(left.PostureStatuses, right.PostureStatuses) {
				return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "network_policy_ambiguous",
					fmt.Sprintf("Allow policies %s and %s may match at the same priority with different access profiles; assign distinct priorities or disjoint scopes.", left.ID, right.ID),
					fmt.Sprintf("允许策略 %s 与 %s 同优先级且接入等级不同，可能同时匹配；请调整优先级或互斥范围。", left.ID, right.ID))
			}
		}
	}
	return nil
}

func policySelectorsOverlap(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return true
	}
	for _, value := range left {
		if slices.Contains(right, value) {
			return true
		}
	}
	return false
}

func sortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	values = slices.Clone(values)
	sort.Strings(values)
	return values
}
