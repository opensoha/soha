package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
)

// NewPresenceMetadataClient removes client-go's full-object JSON fallback for
// this protocol only. Other callers retain their existing metadata clients.
func NewPresenceMetadataClient(config *rest.Config) (metadata.Interface, error) {
	if config == nil {
		return nil, fmt.Errorf("resource presence metadata transport is unavailable")
	}
	copy := rest.CopyConfig(config)
	copy.Wrap(func(base http.RoundTripper) http.RoundTripper { return presenceMetadataTransport{base: base} })
	return metadata.NewForConfig(copy)
}

type presenceMetadataTransport struct{ base http.RoundTripper }

func (t presenceMetadataTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.Header = request.Header.Clone()
	copy.Header.Set("Accept", "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1")
	response, err := t.base.RoundTrip(copy)
	if err != nil {
		return nil, err
	}
	const maximum = 2 << 20
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > maximum {
		return nil, fmt.Errorf("bounded resource presence response is unavailable")
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 && !presenceMetadataResponse(raw) {
		return nil, fmt.Errorf("resource presence requires a metadata-only collection")
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return response, nil
}

func presenceMetadataResponse(raw []byte) bool {
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil || top == nil {
		return false
	}
	for key := range top {
		if key != "apiVersion" && key != "kind" && key != "metadata" && key != "items" {
			return false
		}
	}
	var value struct {
		APIVersion string            `json:"apiVersion"`
		Kind       string            `json:"kind"`
		Items      []json.RawMessage `json:"items"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Kind != "PartialObjectMetadataList" || value.APIVersion != "meta.k8s.io/v1" || !presenceListItemsExplicit(top, value.Items) {
		return false
	}
	for _, rawItem := range value.Items {
		var item map[string]json.RawMessage
		if json.Unmarshal(rawItem, &item) != nil || item == nil {
			return false
		}
		for key := range item {
			if key != "apiVersion" && key != "kind" && key != "metadata" {
				return false
			}
		}
	}
	return true
}

func presenceListItemsExplicit(top map[string]json.RawMessage, items []json.RawMessage) bool {
	raw, exists := top["items"]
	// Kubernetes PartialObjectMetadataList encodes an empty collection as null.
	// A missing items member is not an explicit empty collection.
	return exists && (items != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")))
}
