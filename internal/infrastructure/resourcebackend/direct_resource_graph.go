package resourcebackend

import (
	"context"
	"errors"
	"fmt"

	contractruntime "github.com/opensoha/soha/contracts/resource/runtime"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
)

func (d *Direct) GetResourceGraph(ctx context.Context, clusterID, namespace, kind, name string) (domainresource.ResourceGraph, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceGraph{}, err
	}
	metadataClient, err := contractruntime.NewResourceGraphMetadataClient(bundle.RESTConfig)
	if err != nil {
		return domainresource.ResourceGraph{}, fmt.Errorf("build resource graph metadata client: %w", err)
	}
	return directNetworkResourceGraph(ctx, bundle.Typed, metadataClient, bundle.Typed.CoreV1().RESTClient(), clusterID, namespace, kind, name)
}

func directResourceGraph(ctx context.Context, client kubernetes.Interface, metadataClient metadata.Interface, clusterID, namespace, kind, name string) (domainresource.ResourceGraph, error) {
	return directNetworkResourceGraph(ctx, client, metadataClient, nil, clusterID, namespace, kind, name)
}

func directNetworkResourceGraph(ctx context.Context, client kubernetes.Interface, metadataClient metadata.Interface, reader rest.Interface, clusterID, namespace, kind, name string) (domainresource.ResourceGraph, error) {
	graph, err := contractruntime.BuildKubernetesResourceGraphWithNetworkSources(ctx, client, metadataClient, reader, domainresource.ResourceRef{ClusterID: clusterID, Namespace: namespace, Kind: kind, Name: name})
	if err == nil {
		return graph, nil
	}
	var code error
	switch {
	case errors.Is(err, contractruntime.ErrInvalidResourceGraphRoot), apierrors.IsBadRequest(err), apierrors.IsInvalid(err):
		code = apperrors.ErrInvalidArgument
	case errors.Is(err, contractruntime.ErrUnsupportedResourceGraphRoot):
		code = apperrors.ErrUnsupportedOperation
	case apierrors.IsNotFound(err):
		code = apperrors.ErrNotFound
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		code = apperrors.ErrAccessDenied
	default:
		return domainresource.ResourceGraph{}, err
	}
	return domainresource.ResourceGraph{}, fmt.Errorf("%w: %w", code, err)
}
