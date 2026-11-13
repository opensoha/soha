package resource

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	cr "github.com/opensoha/soha/contracts/resource"
	shared "github.com/opensoha/soha/contracts/resource/runtime"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func graphNetworkRoot(root domainresource.ResourceRef) bool {
	return resourceGroupForKind(root.Kind) == "network" && root.Kind != "PortForward"
}

func graphNetworkRootDefinition(root domainresource.ResourceRef) (cr.NetworkTopologySourceDefinition, bool) {
	for _, definition := range cr.NetworkTopologySources() {
		if definition.Kind == root.Kind && !definition.RequiresCustomResourceAccess() {
			if root.APIVersion == "" {
				return definition, true
			}
			for _, version := range cr.NetworkTopologySourceVersions(definition) {
				if root.APIVersion == strings.TrimPrefix(definition.Group+"/"+version, "/") {
					return definition, true
				}
			}

		}
	}
	return cr.NetworkTopologySourceDefinition{}, false
}

func (g *GenericResources) getNetworkResourceGraph(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, root domainresource.ResourceRef) (domainresource.ResourceGraph, map[string]bool, error) {
	definition, ok := graphNetworkRootDefinition(root)
	if !ok || connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent && g.topologyAgent == nil || connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent && g.topologyDirect == nil {
		return domainresource.ResourceGraph{}, nil, fmt.Errorf("%w: named network graph snapshot unavailable", apperrors.ErrUnsupportedOperation)
	}
	// Reuse the topology source policy planner, without requiring the topology
	// page permission. Every source is authorized before either transport reads it.
	network := &Network{resourceAccess: g.resourceAccess, topologyAgent: g.topologyAgent, topologyDirect: g.topologyDirect, customResources: g.customResources}
	named, observed, err := readGraphNetworkRoot(ctx, network, connection, root, definition)
	if err != nil {
		return domainresource.ResourceGraph{}, nil, err
	}
	snapshot, err := readGraphNetworkSources(ctx, network, principal, connection, root.ClusterID)
	if err != nil {
		return domainresource.ResourceGraph{}, nil, err
	}
	// Named observation wins over a truncated or stale list. Its separate coverage
	// never claims that the rest of the root kind was completely listed.
	snapshot.Sources = append(snapshot.Sources, named.Sources...)
	graph, err := shared.BuildNetworkResourceGraph(ctx, observed, snapshot)
	if err != nil {
		return domainresource.ResourceGraph{}, nil, err
	}
	graph.Warnings = graphNetworkSafeWarnings(graph.Warnings)
	return graph, graphNetworkObservedIdentities(snapshot), nil
}

func graphNetworkNamedResult(snapshot cr.NetworkTopologySnapshot, query cr.NetworkTopologySourceQuery) error {
	for _, source := range snapshot.Sources {
		if source.Coverage.SourceID != query.SourceID || source.Coverage.Namespace != query.Namespace || source.Coverage.Name != query.Name {
			continue
		}
		switch source.Coverage.State {
		case "available":
			return nil
		case "denied":
			return apperrors.ErrAccessDenied
		case "not_installed":
			if source.Coverage.ReasonCode == "named_not_found" {
				return apperrors.ErrNotFound
			}
			return apperrors.ErrUnsupportedOperation
		default:
			return fmt.Errorf("%w: named network graph root observation unavailable", apperrors.ErrClusterUnready)
		}
	}
	return apperrors.ErrUnsupportedOperation
}

func graphNetworkSnapshotError(err error) error {
	if errors.Is(err, cr.ErrNetworkTopologySnapshotUnsupported) {
		return fmt.Errorf("%w: %w", apperrors.ErrUnsupportedOperation, err)
	}
	return err
}

func graphNetworkSafeWarnings(warnings []string) []string {
	result := []string{}
	sourcePartial := false
	for _, warning := range warnings {
		if strings.HasPrefix(warning, "network source ") {
			sourcePartial = true
			continue
		}
		result = append(result, warning)
	}
	if sourcePartial {
		result = append(result, "Some network relationship sources were unavailable, unauthorized or truncated.")
	}
	return result
}

// The selected authenticated connection owns cluster identity, just as in the
// ordinary Graph path; Agent-local configured IDs are not public cluster IDs.
func graphNetworkClusterIdentity(snapshot cr.NetworkTopologySnapshot, clusterID string) cr.NetworkTopologySnapshot {
	for i := range snapshot.Sources {
		for j := range snapshot.Sources[i].Objects {
			snapshot.Sources[i].Objects[j].Resource.ClusterID = clusterID
		}
	}
	return snapshot
}

func readGraphNetworkRoot(ctx context.Context, network *Network, connection domaincluster.Connection, root cr.ResourceRef, definition cr.NetworkTopologySourceDefinition) (cr.NetworkTopologySnapshot, cr.ResourceRef, error) {
	query := cr.NetworkTopologySourceQuery{SourceID: definition.ID, Namespace: root.Namespace, Name: root.Name}
	namedRequest := cr.NetworkTopologySnapshotRequest{Queries: []cr.NetworkTopologySourceQuery{query}}
	named, err := network.readTopologySnapshot(ctx, connection, namedRequest)
	if err != nil {
		return cr.NetworkTopologySnapshot{}, cr.ResourceRef{}, graphNetworkSnapshotError(err)
	}
	if named.Version != cr.NetworkTopologyNamedRootFeature {
		return cr.NetworkTopologySnapshot{}, cr.ResourceRef{}, fmt.Errorf("%w: named network graph snapshot unsupported", apperrors.ErrUnsupportedOperation)
	}
	if err := graphNetworkNamedResult(named, query); err != nil {
		return cr.NetworkTopologySnapshot{}, cr.ResourceRef{}, err
	}
	named = validateTopologySnapshot(graphNetworkClusterIdentity(named, root.ClusterID), namedRequest, root.ClusterID)
	if len(named.Sources) != 1 || len(named.Sources[0].Objects) != 1 {
		return cr.NetworkTopologySnapshot{}, cr.ResourceRef{}, fmt.Errorf("%w: named network graph identity unavailable", apperrors.ErrUnsupportedOperation)
	}
	observed := named.Sources[0].Objects[0].Resource
	if observed.Name != root.Name || observed.Namespace != root.Namespace || root.UID != "" && root.UID != observed.UID {
		return cr.NetworkTopologySnapshot{}, cr.ResourceRef{}, apperrors.ErrInvalidArgument
	}
	return named, observed, nil
}

func readGraphNetworkSources(ctx context.Context, network *Network, principal domainidentity.Principal, connection domaincluster.Connection, clusterID string) (cr.NetworkTopologySnapshot, error) {
	deadline, _ := ctx.Deadline()
	authorizationCtx, finishAuthorization := context.WithTimeout(ctx, time.Until(deadline)/2)
	request, coverage := network.topologySourceQueries(authorizationCtx, principal, connection, "")
	finishAuthorization()
	if len(request.Queries) >= cr.NetworkTopologyMaxQueries {
		omitted := request.Queries[cr.NetworkTopologyMaxQueries-1:]
		request.Queries = request.Queries[:cr.NetworkTopologyMaxQueries-1]
		for _, query := range omitted {
			definition, _ := cr.LookupNetworkTopologySource(query.SourceID)
			coverage = append(coverage, topologyCoverage(definition, query.Namespace, "partial", "query_budget"))
		}
	}
	snapshot := cr.NetworkTopologySnapshot{Version: cr.NetworkTopologyNamedRootFeature}
	var err error
	if len(request.Queries) > 0 {
		snapshot, err = network.readTopologySnapshot(ctx, connection, request)
		if err != nil {
			if ctx.Err() != nil {
				return cr.NetworkTopologySnapshot{}, ctx.Err()
			}
			for _, query := range request.Queries {
				definition, _ := cr.LookupNetworkTopologySource(query.SourceID)
				coverage = append(coverage, topologyCoverage(definition, query.Namespace, "error", "source_unavailable"))
			}
			snapshot.Sources = nil
		} else {
			snapshot = validateTopologySnapshot(graphNetworkClusterIdentity(snapshot, clusterID), request, clusterID)
		}
	}
	for _, item := range coverage {
		snapshot.Sources = append(snapshot.Sources, cr.NetworkTopologySourceResult{Coverage: item})
	}
	return snapshot, nil
}

// These objects already passed source policy, namespace planning and snapshot
// validation. Unobserved declarations never acquire these credentials.
func graphNetworkObservedIdentities(snapshot cr.NetworkTopologySnapshot) map[string]bool {
	allowed := map[string]bool{}
	for _, source := range snapshot.Sources {
		definition, known := cr.LookupNetworkTopologySource(source.Coverage.SourceID)
		if !known || !definition.RequiresCustomResourceAccess() {
			continue
		}
		if source.Coverage.State != "available" && source.Coverage.State != "partial" {
			continue
		}
		for _, object := range source.Objects {
			if object.Resource.UID != "" && graphResourceScopeValid(object.Resource, !definition.Namespaced) {
				allowed[graphAuthorizedIdentity(object.Resource)] = true
			}
		}
	}
	return allowed
}
