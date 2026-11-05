package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"

	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
)

const resourceGraphMetadataAccept = "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1"
const resourceGraphMetadataResponseLimit = 1 << 20

// NewResourceGraphMetadataClient creates a metadata-only Graph client. It rejects
// full-object fallback and validates bounded metadata envelopes before SDK decoding.
// The caller's REST configuration and transport wrappers remain intact.
func NewResourceGraphMetadataClient(config *rest.Config) (metadata.Interface, error) {
	if config == nil {
		return nil, fmt.Errorf("resource graph metadata REST configuration is unavailable")
	}
	strict := rest.CopyConfig(config)
	strict.Wrap(func(base http.RoundTripper) http.RoundTripper {
		return resourceGraphMetadataTransport{RoundTripper: base}
	})
	return metadata.NewForConfig(strict)
}

type resourceGraphMetadataTransport struct{ http.RoundTripper }

func (t resourceGraphMetadataTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Accept", resourceGraphMetadataAccept)
	response, err := t.RoundTripper.RoundTrip(request)
	if err != nil {
		return response, err
	}
	defer response.Body.Close()
	// A redirected request would pass through the Kubernetes authentication
	// transport again, which can attach credentials to the new endpoint.
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return nil, fmt.Errorf("resource graph metadata endpoint redirected")
	}
	var payload []byte
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		mediaType, _, contentTypeErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if contentTypeErr != nil || mediaType != "application/json" {
			return nil, fmt.Errorf("resource graph metadata response is not JSON metadata")
		}
		payload, err = readResourceGraphMetadataResponse(response.Body)
	} else {
		payload, err = io.ReadAll(io.LimitReader(response.Body, resourceGraphMetadataResponseLimit+1))
		if len(payload) > resourceGraphMetadataResponseLimit {
			return nil, fmt.Errorf("resource graph metadata response exceeds its size limit")
		}
	}
	if err != nil {
		if canceled := request.Context().Err(); canceled != nil {
			return nil, canceled
		}
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(payload))
	response.ContentLength = int64(len(payload))
	return response, nil
}

// Reject ordinary objects before the SDK's permissive metadata conversion runs.
func readResourceGraphMetadataResponse(reader io.Reader) ([]byte, error) {
	var payload bytes.Buffer
	decoder := json.NewDecoder(io.TeeReader(io.LimitReader(reader, resourceGraphMetadataResponseLimit+1), &payload))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("invalid resource graph metadata envelope")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return nil, fmt.Errorf("invalid resource graph metadata field")
		}
		seen[key] = true
		if err = readResourceGraphMetadataField(decoder, key); err != nil {
			return nil, err
		}
	}
	if _, err = decoder.Token(); err != nil {
		return nil, fmt.Errorf("invalid resource graph metadata envelope")
	}
	if !seen["kind"] || !seen["apiVersion"] || !seen["metadata"] {
		return nil, fmt.Errorf("resource graph response is not PartialObjectMetadata")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || payload.Len() > resourceGraphMetadataResponseLimit {
		return nil, fmt.Errorf("resource graph metadata response exceeds its envelope or size limit")
	}
	return payload.Bytes(), nil
}

func readResourceGraphMetadataField(decoder *json.Decoder, key string) error {
	switch key {
	case "kind", "apiVersion":
		want := "PartialObjectMetadata"
		if key == "apiVersion" {
			want = "meta.k8s.io/v1"
		}
		var value string
		if err := decoder.Decode(&value); err != nil || value != want {
			return fmt.Errorf("resource graph response is not PartialObjectMetadata")
		}
	case "metadata":
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || len(value) == 0 || value[0] != '{' {
			return fmt.Errorf("invalid resource graph object metadata")
		}
	default:
		return fmt.Errorf("resource graph response contains a non-metadata field")
	}
	return nil
}
