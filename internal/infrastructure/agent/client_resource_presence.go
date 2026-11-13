package agent

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	contractresource "github.com/opensoha/soha/contracts/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const resourcePresencePath = "/api/v1/platform/resource-presence-v1/resources"

func (c *Client) ObserveResourcePresence(ctx context.Context, namespace, kind, name string) (contractresource.ResourcePresence, error) {
	var payload struct {
		Data contractresource.ResourcePresence `json:"data"`
	}
	query := url.Values{"namespace": {namespace}, "kind": {kind}, "name": {name}}
	err := c.request(ctx, http.MethodGet, resourcePresencePath+"?"+query.Encode(), nil, &payload)
	// This protocol represents an absent object with a successful collection.
	// 404 can only mean unsupported/unconfirmed transport, never object absence.
	if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrUnsupportedOperation) {
		return contractresource.ResourcePresence{}, apperrors.ErrUnsupportedOperation
	}
	if err != nil {
		return contractresource.ResourcePresence{}, err
	}
	if payload.Data.ValidateFor(kind, namespace, name) != nil {
		return contractresource.ResourcePresence{}, apperrors.ErrClusterUnready
	}
	return payload.Data, nil
}
