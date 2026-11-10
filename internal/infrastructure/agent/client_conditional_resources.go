package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const conditionalResourcePath = "/api/v1/platform/preconditions-v1/resources"

func (c *Client) GetResourceIdentity(ctx context.Context, namespace, kind, name string) (contractresource.ResourceIdentity, error) {
	var payload struct {
		Data contractresource.ResourceIdentity `json:"data"`
	}
	query := url.Values{"namespace": {namespace}, "kind": {kind}, "name": {name}}
	err := c.request(ctx, http.MethodGet, conditionalResourcePath+"/identity?"+query.Encode(), nil, &payload)
	return payload.Data, err
}

func conditionalYAMLRequest(namespace, kind, name, content string, expected contractresource.ResourceMutationPreconditions) contractresource.ConditionalResourceYAMLRequest {
	return contractresource.ConditionalResourceYAMLRequest{Namespace: namespace, Kind: kind, Name: name, ConditionalResourceYAMLInput: contractresource.ConditionalResourceYAMLInput{Content: content, Preconditions: expected}}
}

func (c *Client) ApplyResourceYAMLConditional(ctx context.Context, namespace, kind, name, content string, expected contractresource.ResourceMutationPreconditions) (domainresource.ResourceYAMLView, error) {
	var payload struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	err := c.request(ctx, http.MethodPut, conditionalResourcePath+"/yaml", conditionalYAMLRequest(namespace, kind, name, content, expected), &payload)
	return payload.Data, err
}

func (c *Client) DryRunResourceYAMLConditional(ctx context.Context, namespace, kind, name, content string, expected contractresource.ResourceMutationPreconditions) (domainresource.ResourceUpdateAnalysis, error) {
	var payload struct {
		Data struct {
			Analysis domainresource.ResourceUpdateAnalysis `json:"analysis"`
		} `json:"data"`
	}
	err := c.request(ctx, http.MethodPost, conditionalResourcePath+"/yaml/preflight", conditionalYAMLRequest(namespace, kind, name, content, expected), &payload)
	return payload.Data.Analysis, err
}

func (c *Client) DeleteResourceConditional(ctx context.Context, namespace, kind, name string, expected contractresource.ResourceMutationPreconditions) error {
	var payload struct {
		Data contractresource.ResourceDeletionReceipt `json:"data"`
	}
	req := contractresource.ConditionalResourceMutationRequest{Namespace: namespace, Kind: kind, Name: name, Preconditions: expected}
	if err := c.request(ctx, http.MethodDelete, conditionalResourcePath, req, &payload); err != nil {
		return err
	}
	if !payload.Data.Accepted || payload.Data.State != "deletion_requested" || payload.Data.Preconditions != expected || payload.Data.Name != name || payload.Data.Kind != kind || payload.Data.Namespace != namespace {
		return fmt.Errorf("%w: Agent returned an invalid conditional deletion receipt", apperrors.ErrClusterUnready)
	}
	return nil
}
