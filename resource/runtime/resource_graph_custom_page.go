package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	cr "github.com/opensoha/soha-contracts/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
)

// ReadCustomResourceGraphObject performs one metadata-only GET on an authorized GVR.
func ReadCustomResourceGraphObject(ctx context.Context, config *rest.Config, request cr.CustomResourceGraphObjectRequest) (json.RawMessage, error) {
	definition, namespace, name := request.Definition, request.Namespace, request.Name
	if !validCustomGraphPageScope(definition, namespace) || definition.Namespaced != (namespace != "") || len(validation.IsDNS1123Subdomain(name)) != 0 {
		return nil, apierrors.NewBadRequest("invalid named custom relationship source")
	}
	client, err := NewResourceGraphMetadataClient(config)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, ResourceGraphTimeout)
	defer cancel()
	item, err := customGraphMetadataResource(client, definition, namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if item.Name != name || item.Namespace != namespace || item.UID == "" {
		return nil, fmt.Errorf("named custom graph metadata has invalid identity")
	}
	projected, err := projectCustomGraphMetadata(definition, *item)
	if err != nil {
		return nil, err
	}
	if len(projected) > ResourceGraphMetadataJSONLimit {
		return nil, fmt.Errorf("named custom graph metadata exceeds its size limit")
	}
	return projected, ctx.Err()
}

// ReadCustomResourceGraphPage observes a single metadata page for an authorized
// GVR. It never follows continuation tokens or negotiates full objects.
func ReadCustomResourceGraphPage(ctx context.Context, config *rest.Config, definition cr.CRDResourceDefinition, namespace string) (cr.CustomResourceGraphPage, error) {
	page := cr.CustomResourceGraphPage{Items: []json.RawMessage{}}
	if !validCustomGraphPageScope(definition, namespace) {
		return page, apierrors.NewBadRequest("invalid custom relationship source")
	}
	if config == nil {
		return page, fmt.Errorf("custom graph metadata REST configuration unavailable")
	}
	strict := rest.CopyConfig(config)
	strict.Wrap(func(base http.RoundTripper) http.RoundTripper {
		return customGraphPageTransport{RoundTripper: base}
	})
	client, err := metadata.NewForConfig(strict)
	if err != nil {
		return page, err
	}
	ctx, cancel := context.WithTimeout(ctx, ResourceGraphTimeout)
	defer cancel()
	limit := int64(ResourceGraphMetadataEnrichmentLimit)
	list, err := customGraphMetadataResource(client, definition, namespace).List(ctx, metav1.ListOptions{Limit: limit})
	if err != nil {
		return page, err
	}
	page.Truncated = list.Continue != "" || len(list.Items) > int(limit) || (list.RemainingItemCount != nil && *list.RemainingItemCount > 0)
	for _, item := range list.Items[:min(len(list.Items), int(limit))] {
		if item.UID == "" || item.Name == "" || definition.Namespaced != (item.Namespace != "") || (namespace != "" && item.Namespace != namespace) {
			return cr.CustomResourceGraphPage{}, fmt.Errorf("custom graph metadata has invalid identity")
		}
		projected, err := projectCustomGraphMetadata(definition, item)
		if err != nil {
			return cr.CustomResourceGraphPage{}, err
		}
		page.Items = append(page.Items, projected)
	}
	return page, ctx.Err()
}

func customGraphMetadataResource(client metadata.Interface, definition cr.CRDResourceDefinition, namespace string) metadata.ResourceInterface {
	resource := client.Resource(schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource})
	if namespace != "" {
		return resource.Namespace(namespace)
	}
	return resource
}

func projectCustomGraphMetadata(definition cr.CRDResourceDefinition, item metav1.PartialObjectMetadata) ([]byte, error) {
	return json.Marshal(struct {
		APIVersion string            `json:"apiVersion"`
		Kind       string            `json:"kind"`
		Metadata   metav1.ObjectMeta `json:"metadata"`
	}{
		APIVersion: definition.Group + "/" + definition.Version, Kind: definition.Kind,
		Metadata: metav1.ObjectMeta{Name: item.Name, Namespace: item.Namespace, UID: item.UID,
			ResourceVersion: item.ResourceVersion, OwnerReferences: item.OwnerReferences},
	})
}

func validCustomGraphPageScope(definition cr.CRDResourceDefinition, namespace string) bool {
	return definition.Kind != "" && definition.Group != "" && definition.CRDName == definition.Resource+"."+definition.Group &&
		len(validation.IsDNS1123Subdomain(definition.Group)) == 0 &&
		len(validation.IsDNS1035Label(definition.Version)) == 0 &&
		len(validation.IsDNS1035Label(definition.Resource)) == 0 &&
		(namespace == "" || definition.Namespaced && len(validation.IsDNS1123Label(namespace)) == 0)
}

type customGraphPageTransport struct{ http.RoundTripper }

func (t customGraphPageTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Accept", "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1")
	response, err := t.RoundTripper.RoundTrip(request)
	if err != nil {
		return response, err
	}
	body := response.Body
	defer func() { _ = body.Close() }()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return nil, fmt.Errorf("custom graph metadata endpoint redirected")
	}
	raw, err := io.ReadAll(io.LimitReader(body, resourceGraphMetadataResponseLimit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > resourceGraphMetadataResponseLimit ||
		(response.StatusCode >= 200 && response.StatusCode < 300 && !validCustomGraphPageEnvelope(raw)) {
		return nil, fmt.Errorf("invalid or oversized custom graph metadata page")
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	response.ContentLength = int64(len(raw))
	return response, nil
}

func validCustomGraphPageEnvelope(raw []byte) bool {
	var list metav1.PartialObjectMetadataList
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var extra any
	if decoder.Decode(&list) != nil || decoder.Decode(&extra) != io.EOF ||
		list.APIVersion != "meta.k8s.io/v1" || list.Kind != "PartialObjectMetadataList" || list.Items == nil {
		return false
	}
	for _, item := range list.Items {
		if (item.Kind != "" && item.Kind != "PartialObjectMetadata") ||
			(item.APIVersion != "" && item.APIVersion != "meta.k8s.io/v1") {
			return false
		}
	}
	return true
}
