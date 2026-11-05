package agent

import (
	"context"
	"net/http"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

const observedResourceYAMLPath = "/api/v1/platform/ownership-v2/resources/yaml/observed"

func (c *Client) ApplyObservedResourceYAML(ctx context.Context, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	var response struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	err := c.request(ctx, http.MethodPut, observedResourceYAMLPath, resourceYAMLRequest{Namespace: namespace, Kind: kind, Name: name, Content: content}, &response)
	return response.Data, err
}

func (c *Client) DryRunObservedResourceYAML(ctx context.Context, namespace, kind, name, content string) (domainresource.ResourceUpdateAnalysis, error) {
	var response struct {
		Data struct {
			Analysis domainresource.ResourceUpdateAnalysis `json:"analysis"`
		} `json:"data"`
	}
	err := c.request(ctx, http.MethodPost, observedResourceYAMLPath+"/preflight", resourceYAMLRequest{Namespace: namespace, Kind: kind, Name: name, Content: content}, &response)
	return response.Data.Analysis, err
}
