package resourcebackend

import (
	"context"

	contractresource "github.com/opensoha/soha-contracts/resource"
	contractruntime "github.com/opensoha/soha-contracts/resource/runtime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/metadata"
)

func (d *Direct) ObserveResourcePresence(ctx context.Context, clusterID, namespace, kind, name string) (contractresource.ResourcePresence, error) {
	definition, supported := contractresource.LookupKubernetesResourceKind(kind)
	if !supported || !definition.GenericYAML || definition.Namespaced != (namespace != "") {
		return contractresource.ResourcePresence{}, apperrors.ErrInvalidArgument
	}
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return contractresource.ResourcePresence{}, conditionalResourceError(err)
	}
	client, err := contractruntime.NewPresenceMetadataClient(bundle.RESTConfig)
	if err != nil {
		return contractresource.ResourcePresence{}, apperrors.ErrUnsupportedOperation
	}
	resource := client.Resource(schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource})
	var reader metadata.ResourceInterface = resource
	if definition.Namespaced {
		reader = resource.Namespace(namespace)
	}
	result, err := contractruntime.ObserveResourcePresence(ctx, reader, definition.Kind, namespace, name)
	return result, conditionalResourceError(err)
}
