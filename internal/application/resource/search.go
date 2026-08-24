package resource

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	defaultResourceSearchLimit = 20
	maxResourceSearchLimit     = 100
)

type resourceSearchLoader func(context.Context, domainidentity.Principal, string, string) ([]domainresource.ResourceSearchItem, error)

type resourceSearchKind struct {
	kind string
	load resourceSearchLoader
}

// ResourceSearch aggregates existing permission-aware inventory capabilities.
type ResourceSearch struct {
	kinds []resourceSearchKind
}

func newResourceSearch(workloads *Workloads, configuration *Configuration, network *Network, inventory *Inventory) *ResourceSearch {
	return &ResourceSearch{kinds: []resourceSearchKind{
		{kind: "Pod", load: func(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ResourceSearchItem, error) {
			items, err := workloads.ListPods(ctx, principal, clusterID, namespace)
			return mapSearchItems(clusterID, "v1", "Pod", domainresource.ResourceScopeModeNamespace, items, func(item domainresource.PodView) (string, string, string) {
				return item.Name, item.Namespace, item.Phase
			}), err
		}},
		{kind: "Deployment", load: func(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ResourceSearchItem, error) {
			items, err := workloads.ListDeployments(ctx, principal, clusterID, namespace)
			return mapSearchItems(clusterID, "apps/v1", "Deployment", domainresource.ResourceScopeModeNamespace, items, func(item domainresource.DeploymentView) (string, string, string) {
				return item.Name, item.Namespace, ""
			}), err
		}},
		{kind: "Service", load: func(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ResourceSearchItem, error) {
			items, err := network.ListServices(ctx, principal, clusterID, namespace)
			return mapSearchItems(clusterID, "v1", "Service", domainresource.ResourceScopeModeNamespace, items, func(item domainresource.ServiceView) (string, string, string) {
				return item.Name, item.Namespace, item.Type
			}), err
		}},
		{kind: "ConfigMap", load: func(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ResourceSearchItem, error) {
			items, err := configuration.ListConfigMaps(ctx, principal, clusterID, namespace)
			return mapSearchItems(clusterID, "v1", "ConfigMap", domainresource.ResourceScopeModeNamespace, items, func(item domainresource.ConfigMapView) (string, string, string) {
				return item.Name, item.Namespace, ""
			}), err
		}},
		{kind: "Namespace", load: func(ctx context.Context, principal domainidentity.Principal, clusterID, _ string) ([]domainresource.ResourceSearchItem, error) {
			items, err := inventory.ListNamespaces(ctx, principal, clusterID)
			return mapSearchItems(clusterID, "v1", "Namespace", domainresource.ResourceScopeModeCluster, items, func(item domainresource.NamespaceView) (string, string, string) {
				return item.Name, "", item.Status
			}), err
		}},
		{kind: "Node", load: func(ctx context.Context, principal domainidentity.Principal, clusterID, _ string) ([]domainresource.ResourceSearchItem, error) {
			items, err := inventory.ListNodes(ctx, principal, clusterID)
			return mapSearchItems(clusterID, "v1", "Node", domainresource.ResourceScopeModeCluster, items, func(item domainresource.NodeView) (string, string, string) {
				return item.Name, "", item.Status
			}), err
		}},
		{kind: "HorizontalPodAutoscaler", load: func(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ResourceSearchItem, error) {
			items, err := workloads.ListHorizontalPodAutoscalers(ctx, principal, clusterID, namespace)
			return mapSearchItems(clusterID, "autoscaling/v2", "HorizontalPodAutoscaler", domainresource.ResourceScopeModeNamespace, items, func(item domainresource.HorizontalPodAutoscalerView) (string, string, string) {
				return item.Name, item.Namespace, item.TargetRef
			}), err
		}},
		{kind: "NetworkPolicy", load: func(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ResourceSearchItem, error) {
			items, err := network.ListNetworkPolicies(ctx, principal, clusterID, namespace)
			return mapSearchItems(clusterID, "networking.k8s.io/v1", "NetworkPolicy", domainresource.ResourceScopeModeNamespace, items, func(item domainresource.NetworkPolicyView) (string, string, string) {
				return item.Name, item.Namespace, strings.Join(item.PolicyTypes, ",")
			}), err
		}},
	}}
}

func mapSearchItems[T any](clusterID, apiVersion, kind string, scopeMode domainresource.ResourceScopeMode, items []T, fields func(T) (string, string, string)) []domainresource.ResourceSearchItem {
	result := make([]domainresource.ResourceSearchItem, 0, len(items))
	for _, item := range items {
		name, namespace, status := fields(item)
		result = append(result, domainresource.ResourceSearchItem{
			Resource: domainresource.ResourceRef{
				ClusterID: clusterID, APIVersion: apiVersion, Kind: kind, Name: name,
				Namespace: namespace, ScopeMode: scopeMode,
			},
			Status: status,
		})
	}
	return result
}

func (s *ResourceSearch) SearchResources(ctx context.Context, principal domainidentity.Principal, clusterID string, input domainresource.ResourceSearchInput) (domainresource.ResourceSearchResult, error) {
	query := strings.ToLower(strings.TrimSpace(input.Query))
	if query == "" || strings.TrimSpace(clusterID) == "" {
		return domainresource.ResourceSearchResult{}, fmt.Errorf("%w: cluster and query are required", apperrors.ErrInvalidArgument)
	}
	selected, err := s.selectedKinds(input.Kinds)
	if err != nil {
		return domainresource.ResourceSearchResult{}, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultResourceSearchLimit
	}
	if limit > maxResourceSearchLimit {
		limit = maxResourceSearchLimit
	}
	matches := make([]domainresource.ResourceSearchItem, 0, limit+1)
	// ponytail: eight bounded inventory reads stay sequential; add bounded errgroup fan-out only if measured latency requires it.
	for _, kind := range selected {
		items, loadErr := kind.load(ctx, principal, clusterID, strings.TrimSpace(input.Namespace))
		if errors.Is(loadErr, apperrors.ErrAccessDenied) {
			continue
		}
		if loadErr != nil {
			return domainresource.ResourceSearchResult{}, loadErr
		}
		for _, item := range items {
			if resourceSearchScore(item, query) >= 0 {
				matches = append(matches, item)
			}
		}
	}
	sort.SliceStable(matches, func(left, right int) bool {
		leftScore := resourceSearchScore(matches[left], query)
		rightScore := resourceSearchScore(matches[right], query)
		if leftScore != rightScore {
			return leftScore < rightScore
		}
		leftRef, rightRef := matches[left].Resource, matches[right].Resource
		if leftRef.Kind != rightRef.Kind {
			return leftRef.Kind < rightRef.Kind
		}
		if leftRef.Namespace != rightRef.Namespace {
			return leftRef.Namespace < rightRef.Namespace
		}
		return leftRef.Name < rightRef.Name
	})
	truncated := len(matches) > limit
	if truncated {
		matches = matches[:limit]
	}
	return domainresource.ResourceSearchResult{Items: matches, Truncated: truncated}, nil
}

func (s *ResourceSearch) selectedKinds(requested []string) ([]resourceSearchKind, error) {
	if len(requested) == 0 {
		return s.kinds, nil
	}
	selected := make([]resourceSearchKind, 0, len(requested))
	seen := map[string]struct{}{}
	for _, requestedKind := range requested {
		matched := false
		for _, kind := range s.kinds {
			if !strings.EqualFold(strings.TrimSpace(requestedKind), kind.kind) {
				continue
			}
			matched = true
			key := strings.ToLower(kind.kind)
			if _, exists := seen[key]; !exists {
				selected = append(selected, kind)
				seen[key] = struct{}{}
			}
			break
		}
		if !matched {
			return nil, fmt.Errorf("%w: unsupported resource search kind %s", apperrors.ErrInvalidArgument, requestedKind)
		}
	}
	return selected, nil
}

func resourceSearchScore(item domainresource.ResourceSearchItem, query string) int {
	name := strings.ToLower(item.Resource.Name)
	if name == query {
		return 0
	}
	if strings.HasPrefix(name, query) {
		return 1
	}
	if strings.Contains(name, query) {
		return 2
	}
	for _, value := range []string{item.Resource.Namespace, item.Resource.Kind, item.Status} {
		if strings.Contains(strings.ToLower(value), query) {
			return 3
		}
	}
	return -1
}
