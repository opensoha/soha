package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	resource "github.com/opensoha/soha-contracts/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// ReadRelationSnapshot reads metadata only. Its caller must authorize every
// collection and namespace; Kubernetes RBAC still applies to the actual reads.
func ReadRelationSnapshot(ctx context.Context, client rest.Interface, clusterID string, input resource.RelationSnapshotRequest) (resource.RelationSnapshot, error) {
	if err := resource.ValidateRelationSnapshotRequest(input); err != nil {
		return resource.RelationSnapshot{}, err
	}
	if client == nil {
		return resource.RelationSnapshot{}, fmt.Errorf("resource metadata client unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out := resource.RelationSnapshot{Version: resource.RelationSnapshotFeature, Sources: []resource.RelationSourceResult{}}
	budget := &topologyReadBudget{remaining: 8 << 20}
	for _, query := range input.Queries {
		out.Sources = append(out.Sources, readRelationSource(ctx, client, clusterID, query, budget))
	}
	return out, nil
}

func readRelationSource(ctx context.Context, client rest.Interface, clusterID string, q resource.RelationQuery, budget *topologyReadBudget) resource.RelationSourceResult {
	out := resource.RelationSourceResult{Coverage: resource.RelationCoverage{QueryID: q.ID, State: "available"}, Objects: []resource.RelationObject{}}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	group, version := relationGroupVersion(q.Source.APIVersion)
	base := topologyAPIBase(group, version)
	data, err := boundedTopologyGET(ctx, client, base, false, budget)
	if err == nil {
		err = validateRelationDiscovery(data, q.Source)
	}
	if err != nil {
		out.Coverage.State, out.Coverage.ReasonCode = relationReadError(err)
		return out
	}
	path := base + "/namespaces/" + q.Source.Namespace + "/" + q.Source.Resource
	if q.Name != "" {
		path += "/" + q.Name
	}
	request := client.Get().AbsPath(path)
	if q.Name == "" {
		request = request.Param("limit", "200").SetHeader("Accept", "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1")
	} else {
		request = request.SetHeader("Accept", "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1")
	}
	if len(q.Selector) > 0 {
		terms := []string{}
		for k, v := range q.Selector {
			terms = append(terms, k+"="+v)
		}
		sort.Strings(terms)
		request = request.Param("labelSelector", strings.Join(terms, ","))
	}
	stream, err := request.Stream(ctx)
	if err != nil {
		out.Coverage.State, out.Coverage.ReasonCode = relationReadError(err)
		return out
	}
	defer func() { _ = stream.Close() }()
	data, err = io.ReadAll(io.LimitReader(&topologyBudgetReader{reader: stream, budget: budget}, (512<<10)+1))
	if err != nil || len(data) > 512<<10 {
		out.Coverage.State = "partial"
		out.Coverage.ReasonCode = "byte_budget"
		out.Coverage.Truncated = true
		return out
	}
	return relationMetadataResult(out, clusterID, q, data)
}

func relationMetadataResult(out resource.RelationSourceResult, clusterID string, q resource.RelationQuery, data []byte) resource.RelationSourceResult {
	var objects []metav1.PartialObjectMetadata
	if q.Name != "" {
		var object metav1.PartialObjectMetadata
		if json.Unmarshal(data, &object) != nil {
			out.Coverage.State = "error"
			out.Coverage.ReasonCode = "invalid_response"
			return out
		}
		objects = append(objects, object)
	} else {
		var list metav1.PartialObjectMetadataList
		if json.Unmarshal(data, &list) != nil {
			out.Coverage.State = "error"
			out.Coverage.ReasonCode = "invalid_response"
			return out
		}
		objects = list.Items
		if list.Continue != "" || len(objects) > resource.RelationMaxItems {
			out.Coverage.State = "partial"
			out.Coverage.ReasonCode = "item_budget"
			out.Coverage.Truncated = true
		}
	}
	for i, object := range objects {
		if i >= resource.RelationMaxItems {
			break
		}
		if object.Name == "" || object.UID == "" || object.Namespace != q.Source.Namespace || (q.Name != "" && q.Name != object.Name) || len(object.OwnerReferences) > 32 {
			out.Coverage.State = "partial"
			out.Coverage.ReasonCode = "invalid_identity"
			continue
		}
		item := resource.RelationObject{Resource: resource.ResourceRef{ClusterID: clusterID, APIVersion: q.Source.APIVersion, Kind: q.Source.Kind, Namespace: object.Namespace, Name: object.Name, UID: string(object.UID), ResourceVersion: object.ResourceVersion, ScopeMode: resource.ResourceScopeModeNamespace}}
		for _, owner := range object.OwnerReferences {
			item.Owners = append(item.Owners, resource.RelationOwner{APIVersion: owner.APIVersion, Kind: owner.Kind, Name: owner.Name, UID: string(owner.UID), Controller: owner.Controller != nil && *owner.Controller})
		}
		out.Objects = append(out.Objects, item)
	}
	return out
}

func relationGroupVersion(apiVersion string) (string, string) {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 1 {
		return "", parts[0]
	}
	return parts[0], parts[1]
}

func validateRelationDiscovery(data []byte, source resource.RelationSource) error {
	var list metav1.APIResourceList
	if json.Unmarshal(data, &list) != nil {
		return fmt.Errorf("invalid discovery response")
	}
	for _, item := range list.APIResources {
		if item.Name == source.Resource && item.Kind == source.Kind && item.Namespaced {
			return nil
		}
	}
	return errRelationSourceNotServed
}

var errRelationSourceNotServed = errors.New("resource definition not served")

func relationReadError(err error) (string, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return "error", "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "error", "cancelled"
	}
	if errors.Is(err, errRelationSourceNotServed) {
		return "not_installed", "source_not_served"
	}
	return topologyReadError(err)
}
