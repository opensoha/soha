package resource

import (
	"errors"
	"fmt"
	"regexp"
)

var topologyNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

var topologyNamespacePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func ValidateNetworkTopologySnapshotRequest(request NetworkTopologySnapshotRequest) error {
	if len(request.Queries) == 0 || len(request.Queries) > NetworkTopologyMaxQueries {
		return fmt.Errorf("topology snapshot requires between 1 and 64 source queries")
	}
	seen := make(map[NetworkTopologySourceQuery]bool, len(request.Queries))
	for _, query := range request.Queries {
		definition, ok := LookupNetworkTopologySource(query.SourceID)
		if !ok || seen[query] {
			return fmt.Errorf("unknown or duplicate topology source query")
		}
		seen[query] = true
		if query.Name != "" && (definition.RequiresCustomResourceAccess() || len(query.Name) > 253 || !topologyNamePattern.MatchString(query.Name) || definition.Namespaced && query.Namespace == "") {
			return fmt.Errorf("invalid named topology query")
		}
		if query.Namespace != "" && (!definition.Namespaced || len(query.Namespace) > 63 || !topologyNamespacePattern.MatchString(query.Namespace)) {
			return fmt.Errorf("invalid topology source namespace")
		}
	}
	return nil
}

var ErrNetworkTopologySnapshotUnsupported = errors.New("agent topology snapshot unsupported")
