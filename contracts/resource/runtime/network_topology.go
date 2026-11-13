package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	resource "github.com/opensoha/soha/contracts/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

// ReadNetworkTopologySnapshot performs only fixed, discovery-verified, bounded reads.
// The caller authenticates and authorizes each query before invoking this reader.
func ReadNetworkTopologySnapshot(ctx context.Context, client rest.Interface, clusterID string, input resource.NetworkTopologySnapshotRequest) (resource.NetworkTopologySnapshot, error) {
	if err := resource.ValidateNetworkTopologySnapshotRequest(input); err != nil {
		return resource.NetworkTopologySnapshot{}, err
	}
	if client == nil {
		return resource.NetworkTopologySnapshot{}, fmt.Errorf("kubernetes REST client unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output := resource.NetworkTopologySnapshot{Version: resource.NetworkTopologySnapshotFeature, Sources: make([]resource.NetworkTopologySourceResult, len(input.Queries))}
	for _, query := range input.Queries {
		if query.Name != "" {
			output.Version = resource.NetworkTopologyNamedRootFeature
			break
		}
	}
	budget := &topologyReadBudget{remaining: resource.NetworkTopologyMaxTotalBytes}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for index, query := range input.Queries {
		wg.Add(1)
		go func(index int, query resource.NetworkTopologySourceQuery) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				output.Sources[index] = topologySourceFailure(query, "error", "timeout")
				return
			}
			output.Sources[index] = readTopologySource(ctx, client, clusterID, query, budget)
		}(index, query)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return resource.NetworkTopologySnapshot{}, err
	}
	total := 0
	for i := range output.Sources {
		source := &output.Sources[i]
		kept := source.Objects[:0]
		for _, object := range source.Objects {
			size := len(object.Content)
			if total+size > resource.NetworkTopologyMaxTotalBytes {
				source.Coverage.State = "partial"
				source.Coverage.Truncated = true
				source.Coverage.ReasonCode = "total_budget"
				continue
			}
			total += size
			kept = append(kept, object)
		}
		source.Objects = kept
	}
	return output, nil
}

func topologySourceFailure(query resource.NetworkTopologySourceQuery, state, reason string) resource.NetworkTopologySourceResult {
	definition, _ := resource.LookupNetworkTopologySource(query.SourceID)
	return resource.NetworkTopologySourceResult{Coverage: resource.NetworkTopologyCoverage{Provider: definition.Provider, SourceID: query.SourceID, Name: query.Name, Namespace: query.Namespace, State: state, ReasonCode: reason, CheckedAt: time.Now().UTC().Format(time.RFC3339), SupportedKinds: []string{definition.Kind}}, Objects: []resource.NetworkTopologySourceObject{}}
}

func readTopologySource(ctx context.Context, client rest.Interface, clusterID string, query resource.NetworkTopologySourceQuery, budget *topologyReadBudget) resource.NetworkTopologySourceResult {
	if query.Name != "" {
		return readNetworkGraphNamedSource(ctx, client, clusterID, query, budget)
	}
	definition, _ := resource.LookupNetworkTopologySource(query.SourceID)
	result := topologySourceFailure(query, "available", "")
	version, err := discoverTopologySource(ctx, client, definition, budget)
	if err != nil {
		result.Coverage.State, result.Coverage.ReasonCode = topologyReadError(err)
		return result
	}
	base := topologyAPIBase(definition.Group, version)
	if query.Namespace != "" {
		base += "/namespaces/" + query.Namespace
	}
	raw, err := boundedTopologyGET(ctx, client, base+"/"+definition.Resource, true, budget)
	if err != nil {
		result.Coverage.State, result.Coverage.ReasonCode = topologyReadError(err)
		return result
	}
	var list struct {
		Metadata struct {
			Continue string `json:"continue"`
		} `json:"metadata"`
		Items []map[string]any `json:"items"`
	}
	if json.Unmarshal(raw, &list) != nil {
		result.Coverage.State = "error"
		result.Coverage.ReasonCode = "invalid_response"
		return result
	}
	result.Coverage.ObservedAt = time.Now().UTC().Format(time.RFC3339)
	if len(list.Items) > resource.NetworkTopologyMaxSourceItems || list.Metadata.Continue != "" {
		result.Coverage.Truncated = true
		result.Coverage.State = "partial"
		result.Coverage.ReasonCode = "item_budget"
	}
	for index, object := range list.Items {
		if index >= resource.NetworkTopologyMaxSourceItems {
			break
		}
		projected, err := resource.ProjectNetworkTopologyObject(query.SourceID, object)
		if err != nil {
			result.Coverage.Truncated = true
			result.Coverage.State = "partial"
			result.Coverage.ReasonCode = "object_budget"
			continue
		}
		metadata, _ := projected["metadata"].(map[string]any)
		name, _ := metadata["name"].(string)
		namespace, _ := metadata["namespace"].(string)
		if name == "" || (query.Namespace != "" && namespace != query.Namespace) {
			result.Coverage.State = "partial"
			result.Coverage.ReasonCode = "invalid_identity"
			continue
		}
		uid, _ := metadata["uid"].(string)
		rv, _ := metadata["resourceVersion"].(string)
		generation, _ := metadata["generation"].(float64)
		content, _ := json.Marshal(projected)
		result.Objects = append(result.Objects, resource.NetworkTopologySourceObject{Resource: resource.ResourceRef{ClusterID: clusterID, APIVersion: strings.TrimPrefix(definition.Group+"/"+version, "/"), ScopeMode: topologyScope(definition.Namespaced), Kind: definition.Kind, Namespace: namespace, Name: name, UID: uid, ResourceVersion: rv}, Generation: int64(generation), Content: string(content)})
	}
	return result
}

func topologyAPIBase(group, version string) string {
	if group == "" {
		return "/api/" + version
	}
	return "/apis/" + group + "/" + version
}

func discoverTopologySource(ctx context.Context, client rest.Interface, definition resource.NetworkTopologySourceDefinition, budget *topologyReadBudget) (string, error) {
	return discoverTopologySourceVersions(ctx, client, definition, budget, resource.NetworkTopologySourceVersions(definition))
}

func discoverTopologySourceVersions(ctx context.Context, client rest.Interface, definition resource.NetworkTopologySourceDefinition, budget *topologyReadBudget, versions []string) (string, error) {
	for _, version := range versions {
		raw, err := boundedTopologyGET(ctx, client, topologyAPIBase(definition.Group, version), false, budget)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		var list metav1.APIResourceList
		if err := json.Unmarshal(raw, &list); err != nil {
			return "", fmt.Errorf("invalid discovery")
		}
		for _, item := range list.APIResources {
			if item.Name == definition.Resource && item.Kind == definition.Kind && item.Namespaced == definition.Namespaced {
				return version, nil
			}
		}
	}
	return "", apierrors.NewNotFound(schema.GroupResource{Group: definition.Group, Resource: definition.Resource}, "")
}

func boundedTopologyGET(ctx context.Context, client rest.Interface, path string, list bool, budget *topologyReadBudget) ([]byte, error) {
	request := client.Get().AbsPath(path).SetHeader("Accept", "application/json")
	if list {
		request = request.Param("limit", fmt.Sprint(resource.NetworkTopologyMaxSourceItems))
	}
	stream, err := request.Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	body, err := io.ReadAll(io.LimitReader(&topologyBudgetReader{reader: stream, budget: budget}, resource.NetworkTopologyMaxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > resource.NetworkTopologyMaxSourceBytes {
		return nil, fmt.Errorf("topology byte budget")
	}
	return body, nil
}

func topologyReadError(err error) (string, string) {
	switch {
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return "denied", "kubernetes_rbac"
	case apierrors.IsNotFound(err):
		return "not_installed", "source_not_served"
	case strings.Contains(err.Error(), "budget"):
		return "partial", "byte_budget"
	default:
		return "error", "source_unavailable"
	}
}

func topologyScope(namespaced bool) resource.ResourceScopeMode {
	if namespaced {
		return resource.ResourceScopeModeNamespace
	}
	return resource.ResourceScopeModeCluster
}

type topologyReadBudget struct {
	mu        sync.Mutex
	remaining int
}
type topologyBudgetReader struct {
	reader io.Reader
	budget *topologyReadBudget
}

func (r *topologyBudgetReader) Read(data []byte) (int, error) {
	r.budget.mu.Lock()
	available := min(len(data), r.budget.remaining)
	r.budget.remaining -= available
	r.budget.mu.Unlock()
	if available <= 0 {
		return 0, errors.New("topology total byte budget")
	}
	count, err := r.reader.Read(data[:available])
	r.budget.mu.Lock()
	r.budget.remaining += available - count
	r.budget.mu.Unlock()
	return count, err
}
