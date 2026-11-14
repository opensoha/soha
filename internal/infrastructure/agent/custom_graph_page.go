package agent

import (
	"context"
	"encoding/json"
	"net/http"

	cr "github.com/opensoha/soha/contracts/resource"
	shared "github.com/opensoha/soha/contracts/resource/runtime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const customGraphPagePath = "/api/v1/platform/extensions/custom-resources/graph-page"
const customGraphObjectPath = "/api/v1/platform" + cr.CustomResourceGraphObjectPath
const customGraphObjectEnvelopeLimit = shared.ResourceGraphMetadataJSONLimit + len(`{"data":}`)

func (c *Client) ReadCustomResourceGraphPage(ctx context.Context, definition cr.CRDResourceDefinition, namespace string) (cr.CustomResourceGraphPage, error) {
	var payload struct {
		Data cr.CustomResourceGraphPage `json:"data"`
	}
	err := c.request(ctx, http.MethodPost, customGraphPagePath, customResourceListRequest{
		Definition: definition, Namespace: namespace,
	}, &payload)
	if err == nil && payload.Data.Items == nil {
		err = apperrors.ErrUnsupportedOperation
	}
	return payload.Data, err
}

func (c *Client) ReadCustomResourceGraphObject(ctx context.Context, request cr.CustomResourceGraphObjectRequest) (json.RawMessage, error) {
	var payload struct {
		Data json.RawMessage `json:"data"`
	}
	if err := c.request(ctx, http.MethodPost, customGraphObjectPath, request, &payload); err != nil {
		return nil, err
	}
	if len(payload.Data) == 0 || string(payload.Data) == "null" {
		return nil, apperrors.ErrUnsupportedOperation
	}
	return payload.Data, nil
}
