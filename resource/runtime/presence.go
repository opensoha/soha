package runtime

import (
	"context"
	"fmt"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

type ResourceMetadataLister interface {
	List(context.Context, metav1.ListOptions) (*metav1.PartialObjectMetadataList, error)
}

// ObserveResourcePresence uses a successful complete metadata-only collection,
// never GET/404, as the object-presence oracle. A missing API, RBAC denial, stale
// transport or unsupported partial metadata produces an error, not absence.
func ObserveResourcePresence(ctx context.Context, client ResourceMetadataLister, kind, namespace, name string) (contractresource.ResourcePresence, error) {
	definition, ok := contractresource.LookupKubernetesResourceKind(kind)
	if !ok || !definition.GenericYAML || client == nil {
		return contractresource.ResourcePresence{}, fmt.Errorf("unsupported resource presence target")
	}
	apiVersion := definition.Version
	if definition.Group != "" {
		apiVersion = definition.Group + "/" + definition.Version
	}
	result := contractresource.ResourcePresence{Protocol: contractresource.ResourcePresenceProtocol, APIVersion: apiVersion,
		Resource: definition.Resource, Kind: kind, Namespace: namespace, Name: name, CollectionResourceVersion: "pending", State: "absent"}
	if err := result.ValidateFor(kind, namespace, name); err != nil {
		return contractresource.ResourcePresence{}, err
	}
	query, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Empty resourceVersion requests the current collection; "0" would permit
	// an older cached snapshot. Never follow pagination or expand the selector.
	items, err := client.List(query, metav1.ListOptions{FieldSelector: fields.OneTermEqualSelector("metadata.name", name).String(), Limit: 2})
	if err != nil {
		return contractresource.ResourcePresence{}, err
	}
	if items == nil || items.ResourceVersion == "" || items.Continue != "" || len(items.Items) > 1 || items.RemainingItemCount != nil && *items.RemainingItemCount != 0 {
		return contractresource.ResourcePresence{}, fmt.Errorf("resource presence collection is incomplete")
	}
	result.CollectionResourceVersion = items.ResourceVersion
	if len(items.Items) == 1 {
		item := items.Items[0]
		if item.Name != name || item.Namespace != namespace {
			return contractresource.ResourcePresence{}, fmt.Errorf("resource presence collection target changed")
		}
		result.State, result.UID, result.ResourceVersion = "present", string(item.UID), item.ResourceVersion
		if item.DeletionTimestamp != nil {
			stamp := item.DeletionTimestamp.Time
			result.DeletionTimestamp = &stamp
		}
	}
	return result, result.ValidateFor(kind, namespace, name)
}
