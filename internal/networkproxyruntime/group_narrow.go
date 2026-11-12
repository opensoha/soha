package networkproxyruntime

import (
	"errors"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"slices"
)

func installedGroup(groups []api.NetworkProxyGroup, id string) (api.NetworkProxyGroup, bool) {
	for _, group := range groups {
		if group.GroupID == id {
			return group, true
		}
	}
	return api.NetworkProxyGroup{}, false
}

// Native membership is checked against the configuration actually installed.
// A source revocation narrows only the logical view and gates, so a surviving
// stream need not restart merely to remove an unreachable native group member.
func narrowedGroupReadback(current, installed api.NetworkProxyGroup, selected string, native []string) (api.NetworkProxyGroup, error) {
	if current.GroupID != installed.GroupID || current.Type != installed.Type || !slices.Equal(native, installed.NodeIDs) || !slices.Contains(installed.NodeIDs, selected) || len(current.NodeIDs) == 0 {
		return api.NetworkProxyGroup{}, errors.New("group membership or selection drift")
	}
	seen := map[string]bool{}
	for _, node := range current.NodeIDs {
		if seen[node] || !slices.Contains(installed.NodeIDs, node) {
			return api.NetworkProxyGroup{}, errors.New("group scope widened")
		}
		seen[node] = true
	}
	current.NodeIDs = slices.Clone(current.NodeIDs)
	current.Label, current.SelectedNodeID = current.GroupID, ""
	if slices.Contains(current.NodeIDs, selected) {
		current.SelectedNodeID = selected
	}
	return current, nil
}
