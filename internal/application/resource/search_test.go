package resource

import (
	"context"
	"errors"
	"testing"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestResourceSearchFiltersRanksAndTruncates(t *testing.T) {
	t.Parallel()
	search := &ResourceSearch{kinds: []resourceSearchKind{
		{
			kind: "Pod",
			load: func(context.Context, domainidentity.Principal, string, string) ([]domainresource.ResourceSearchItem, error) {
				return []domainresource.ResourceSearchItem{
					searchItem("cluster-a", "v1", "Pod", "api-worker", "team-a", "Running"),
					searchItem("cluster-a", "v1", "Pod", "api", "team-a", "Running"),
				}, nil
			},
		},
		{
			kind: "Service",
			load: func(context.Context, domainidentity.Principal, string, string) ([]domainresource.ResourceSearchItem, error) {
				return []domainresource.ResourceSearchItem{
					searchItem("cluster-a", "v1", "Service", "payments-api", "team-b", "ClusterIP"),
				}, nil
			},
		},
	}}

	result, err := search.SearchResources(context.Background(), domainidentity.Principal{}, "cluster-a", domainresource.ResourceSearchInput{
		Query: "api", Limit: 2,
	})
	if err != nil {
		t.Fatalf("SearchResources() error = %v", err)
	}
	if !result.Truncated || len(result.Items) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Items[0].Resource.Name != "api" || result.Items[1].Resource.Name != "api-worker" {
		t.Fatalf("ranked items = %#v", result.Items)
	}
}

func TestResourceSearchSkipsDeniedKindsAndValidatesInput(t *testing.T) {
	t.Parallel()
	search := &ResourceSearch{kinds: []resourceSearchKind{
		{
			kind: "Pod",
			load: func(context.Context, domainidentity.Principal, string, string) ([]domainresource.ResourceSearchItem, error) {
				return nil, apperrors.ErrAccessDenied
			},
		},
		{
			kind: "Node",
			load: func(context.Context, domainidentity.Principal, string, string) ([]domainresource.ResourceSearchItem, error) {
				return []domainresource.ResourceSearchItem{
					searchItem("cluster-a", "v1", "Node", "worker-api", "", "Ready"),
				}, nil
			},
		},
	}}

	result, err := search.SearchResources(context.Background(), domainidentity.Principal{}, "cluster-a", domainresource.ResourceSearchInput{
		Query: "worker", Kinds: []string{"node"}, Limit: 20,
	})
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	for _, input := range []domainresource.ResourceSearchInput{
		{Query: " "},
		{Query: "api", Kinds: []string{"unknown"}},
	} {
		_, err := search.SearchResources(context.Background(), domainidentity.Principal{}, "cluster-a", input)
		if !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("SearchResources(%#v) error = %v", input, err)
		}
	}
}

func searchItem(clusterID, apiVersion, kind, name, namespace, status string) domainresource.ResourceSearchItem {
	scopeMode := domainresource.ResourceScopeModeNamespace
	if namespace == "" {
		scopeMode = domainresource.ResourceScopeModeCluster
	}
	return domainresource.ResourceSearchItem{
		Resource: domainresource.ResourceRef{
			ClusterID: clusterID, APIVersion: apiVersion, Kind: kind, Name: name,
			Namespace: namespace, ScopeMode: scopeMode,
		},
		Status: status,
	}
}
