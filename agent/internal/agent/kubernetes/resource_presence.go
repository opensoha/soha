package kubernetes

import (
	"context"

	contractresource "github.com/opensoha/soha/contracts/resource"
	contractruntime "github.com/opensoha/soha/contracts/resource/runtime"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/metadata"
)

func (c *Client) ObserveResourcePresence(ctx context.Context, namespace, kind, name string) (contractresource.ResourcePresence, error) {
	definition, ok := contractresource.LookupKubernetesResourceKind(kind)
	if !ok || !definition.GenericYAML || definition.Namespaced != (namespace != "") {
		return contractresource.ResourcePresence{}, apierrors.NewBadRequest("unsupported metadata presence target")
	}
	client, err := contractruntime.NewPresenceMetadataClient(c.restConfig)
	if err != nil {
		return contractresource.ResourcePresence{}, apierrors.NewServiceUnavailable("metadata presence transport is unavailable")
	}
	resource := client.Resource(schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource})
	var reader metadata.ResourceInterface = resource
	if definition.Namespaced {
		reader = resource.Namespace(namespace)
	}
	return contractruntime.ObserveResourcePresence(ctx, reader, definition.Kind, namespace, name)
}
