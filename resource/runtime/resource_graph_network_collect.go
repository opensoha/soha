package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	cr "github.com/opensoha/soha-contracts/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
)

var errNetworkGraphNamedNotFound = errors.New("named network graph root not found")

func networkGraphRootKind(kind string) bool {
	switch kind {
	case "Service", "Ingress", "IngressClass", "GatewayClass", "Gateway", "HTTPRoute", "GRPCRoute", "BackendTLSPolicy", "ReferenceGrant", "EndpointSlice", "NetworkPolicy":
		return true
	}
	return false
}

func networkGraphRootSource(root cr.ResourceRef) (cr.NetworkTopologySourceDefinition, bool) {
	for _, definition := range cr.NetworkTopologySources() {
		if definition.Kind != root.Kind || !networkGraphRootKind(root.Kind) {
			continue
		}
		if root.APIVersion == "" {
			return definition, true
		}
		for _, version := range cr.NetworkTopologySourceVersions(definition) {
			if root.APIVersion == strings.TrimPrefix(definition.Group+"/"+version, "/") {
				return definition, true
			}
		}

	}
	return cr.NetworkTopologySourceDefinition{}, false
}

// BuildKubernetesResourceGraphWithNetworkSources preserves the ordinary Graph
// collector and reuses the bounded topology reader for known network roots.
func BuildKubernetesResourceGraphWithNetworkSources(ctx context.Context, typed kubernetes.Interface, metadataClient metadata.Interface, client rest.Interface, root cr.ResourceRef) (cr.ResourceGraph, error) {
	root, err := NormalizeResourceGraphRoot(root)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	definition, network := networkGraphRootSource(root)
	if !network {
		return BuildKubernetesResourceGraphWithMetadata(ctx, typed, metadataClient, root)
	}
	if client == nil {
		return cr.ResourceGraph{}, fmt.Errorf("%w: network resource graph reader unavailable", ErrUnsupportedResourceGraphRoot)
	}
	ctx, cancel := context.WithTimeout(ctx, ResourceGraphTimeout)
	defer cancel()
	object, err := getNetworkGraphRoot(ctx, client, definition, root, &topologyReadBudget{remaining: cr.NetworkTopologyMaxTotalBytes})
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	request := cr.NetworkTopologySnapshotRequest{}
	for _, source := range cr.NetworkTopologySources() {
		if source.Provider == "kubernetes" || source.Provider == "gateway" && !source.RequiresCustomResourceAccess() {
			request.Queries = append(request.Queries, cr.NetworkTopologySourceQuery{SourceID: source.ID})
		}
	}
	snapshot, err := ReadNetworkTopologySnapshot(ctx, client, root.ClusterID, request)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	appendNetworkGraphRoot(&snapshot, definition.ID, object)
	graph, err := BuildNetworkResourceGraph(ctx, object.Resource, snapshot)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	return graph, ctx.Err()
}

func getNetworkGraphRoot(ctx context.Context, client rest.Interface, definition cr.NetworkTopologySourceDefinition, root cr.ResourceRef, budget *topologyReadBudget) (cr.NetworkTopologySourceObject, error) {
	if err := cr.ValidateNetworkTopologySnapshotRequest(cr.NetworkTopologySnapshotRequest{Queries: []cr.NetworkTopologySourceQuery{{SourceID: definition.ID, Namespace: root.Namespace, Name: root.Name}}}); err != nil {
		return cr.NetworkTopologySourceObject{}, invalidGraphRoot("invalid named network graph scope")
	}
	versions := cr.NetworkTopologySourceVersions(definition)
	if root.APIVersion != "" {
		versions = []string{strings.TrimPrefix(root.APIVersion, definition.Group+"/")}
	}
	version, err := discoverTopologySourceVersions(ctx, client, definition, budget, versions)
	if err != nil {
		return cr.NetworkTopologySourceObject{}, err
	}
	path := topologyAPIBase(definition.Group, version)
	if definition.Namespaced {
		path += "/namespaces/" + root.Namespace
	}
	raw, err := boundedTopologyGET(ctx, client, path+"/"+definition.Resource+"/"+root.Name, false, budget)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return cr.NetworkTopologySourceObject{}, fmt.Errorf("%w: %w", errNetworkGraphNamedNotFound, err)
		}
		return cr.NetworkTopologySourceObject{}, err
	}
	var input map[string]any
	api := strings.TrimPrefix(definition.Group+"/"+version, "/")
	if json.Unmarshal(raw, &input) != nil || input["apiVersion"] != api || input["kind"] != root.Kind {
		return cr.NetworkTopologySourceObject{}, invalidGraphRoot("network graph root GVK changed")
	}
	projected, err := cr.ProjectNetworkTopologyObject(definition.ID, input)
	if err != nil {
		return cr.NetworkTopologySourceObject{}, err
	}
	meta, _ := projected["metadata"].(map[string]any)
	namespace, _ := meta["namespace"].(string)
	uid, _ := meta["uid"].(string)
	if meta["name"] != root.Name || namespace != root.Namespace || uid == "" || root.UID != "" && root.UID != uid {
		return cr.NetworkTopologySourceObject{}, invalidGraphRoot("network graph root identity changed")
	}
	root.APIVersion, root.UID, root.ScopeMode = api, uid, topologyScope(definition.Namespaced)
	root.ResourceVersion, _ = meta["resourceVersion"].(string)
	content, _ := json.Marshal(projected)
	generation, _ := meta["generation"].(float64)
	return cr.NetworkTopologySourceObject{Resource: root, Generation: int64(generation), Content: string(content)}, nil
}

func appendNetworkGraphRoot(snapshot *cr.NetworkTopologySnapshot, sourceID string, root cr.NetworkTopologySourceObject) {
	// The named root must survive a truncated page and stale list observations.
	total := len(root.Content)
	for index := range snapshot.Sources {
		source := &snapshot.Sources[index]
		objects := source.Objects[:0]
		for _, object := range source.Objects {
			if object.Resource.Kind == root.Resource.Kind && object.Resource.Namespace == root.Resource.Namespace && object.Resource.Name == root.Resource.Name {
				continue
			}
			if total+len(object.Content) > cr.NetworkTopologyMaxTotalBytes {
				source.Coverage.State, source.Coverage.Truncated, source.Coverage.ReasonCode = "partial", true, "total_budget"
				continue
			}
			total += len(object.Content)
			objects = append(objects, object)
		}
		source.Objects = objects
		if source.Coverage.SourceID == sourceID {
			if len(source.Objects) >= cr.NetworkTopologyMaxSourceItems {
				source.Objects = source.Objects[:cr.NetworkTopologyMaxSourceItems-1]
				source.Coverage.Truncated = true
			}
			source.Objects = append(source.Objects, root)
			if source.Coverage.State != "available" {
				source.Coverage.State = "partial"
			}
		}
	}
}

func readNetworkGraphNamedSource(ctx context.Context, client rest.Interface, clusterID string, query cr.NetworkTopologySourceQuery, budget *topologyReadBudget) cr.NetworkTopologySourceResult {
	definition, _ := cr.LookupNetworkTopologySource(query.SourceID)
	result := topologySourceFailure(query, "available", "named_get")
	// Named reads use the same fixed catalog and projection, never arbitrary GVRs.
	root := cr.ResourceRef{ClusterID: clusterID, Kind: definition.Kind, Namespace: query.Namespace, Name: query.Name}
	object, err := getNetworkGraphRoot(ctx, client, definition, root, budget)
	if err != nil {
		result.Coverage.State, result.Coverage.ReasonCode = topologyReadError(err)
		if errors.Is(err, errNetworkGraphNamedNotFound) {
			result.Coverage.ReasonCode = "named_not_found"
		}
		return result
	}
	result.Objects = []cr.NetworkTopologySourceObject{object}
	return result
}
