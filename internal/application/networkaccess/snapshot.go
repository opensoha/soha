package networkaccess

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
)

func newPolicySnapshot(policies []domainnetworkaccess.Policy, protectedResourceIDs []string, publishedAt time.Time) (domainnetworkaccess.PolicySnapshot, error) {
	policies = slices.Clone(policies)
	protectedResourceIDs = slices.Clone(protectedResourceIDs)
	for index := range policies {
		policies[index].Subjects.Users = sortedStrings(policies[index].Subjects.Users)
		policies[index].Subjects.Teams = sortedStrings(policies[index].Subjects.Teams)
		policies[index].Subjects.Tags = sortedStrings(policies[index].Subjects.Tags)
		policies[index].SiteIDs = sortedStrings(policies[index].SiteIDs)
		policies[index].ResourceIDs = sortedStrings(policies[index].ResourceIDs)
		policies[index].Modes = sortedStrings(policies[index].Modes)
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
		if err := validatePolicyInput(domainnetworkaccess.PolicyInput{Name: policy.Name, Enabled: policy.Enabled, Priority: policy.Priority, Effect: policy.Effect, Subjects: policy.Subjects, SiteIDs: policy.SiteIDs, ResourceIDs: policy.ResourceIDs, Modes: policy.Modes, DeviceStatuses: policy.DeviceStatuses, PostureStatuses: policy.PostureStatuses, AccessProfile: policy.AccessProfile}); err != nil {
			return domainnetworkaccess.PolicySnapshot{}, fmt.Errorf("compile network policy %s: %w", policy.ID, err)
		}
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

func sortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	values = slices.Clone(values)
	sort.Strings(values)
	return values
}
