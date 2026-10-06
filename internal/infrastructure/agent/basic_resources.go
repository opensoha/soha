package agent

import (
	"context"
	"net/http"
	"net/url"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

const basicMutationPath = "/api/v1/platform/ownership-v2"

func (c *Client) GetConfigMapDetail(ctx context.Context, namespace, name string) (domainresource.ConfigMapDetailView, error) {
	var payload struct {
		Data domainresource.ConfigMapDetailView `json:"data"`
	}
	err := c.request(ctx, http.MethodGet, basicMutationPath+"/configuration/configmaps/"+url.PathEscape(name)+"/detail?namespace="+url.QueryEscape(namespace), nil, &payload)
	return payload.Data, err
}
func (c *Client) GetSecretDetail(ctx context.Context, namespace, name string) (domainresource.SecretDetailView, error) {
	var payload struct {
		Data domainresource.SecretDetailView `json:"data"`
	}
	err := c.request(ctx, http.MethodGet, basicMutationPath+"/configuration/secrets/"+url.PathEscape(name)+"/detail?namespace="+url.QueryEscape(namespace), nil, &payload)
	return payload.Data, err
}
func (c *Client) UpdateConfigMapData(ctx context.Context, namespace, name string, data, binaryData map[string]string) (domainresource.ConfigMapDetailView, error) {
	var payload struct {
		Data domainresource.ConfigMapDetailView `json:"data"`
	}
	err := c.request(ctx, http.MethodPut, basicMutationPath+"/configuration/configmaps/"+url.PathEscape(name)+"/data?namespace="+url.QueryEscape(namespace), map[string]any{"data": data, "binaryData": binaryData}, &payload)
	return payload.Data, err
}
func (c *Client) UpdateSecretData(ctx context.Context, namespace, name string, data map[string]string) (domainresource.SecretDetailView, error) {
	var payload struct {
		Data domainresource.SecretDetailView `json:"data"`
	}
	err := c.request(ctx, http.MethodPut, basicMutationPath+"/configuration/secrets/"+url.PathEscape(name)+"/data?namespace="+url.QueryEscape(namespace), map[string]any{"data": data}, &payload)
	return payload.Data, err
}
func (c *Client) ListConfigReferences(ctx context.Context, namespace, name string, configMap bool) ([]domainresource.ConfigReferenceView, error) {
	kind := "secrets"
	if configMap {
		kind = "configmaps"
	}
	var payload struct {
		Items []domainresource.ConfigReferenceView `json:"items"`
	}
	err := c.request(ctx, http.MethodGet, basicMutationPath+"/configuration/"+kind+"/"+url.PathEscape(name)+"/references?namespace="+url.QueryEscape(namespace), nil, &payload)
	return payload.Items, err
}
func (c *Client) CreateNamespace(ctx context.Context, input domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error) {
	var payload struct {
		Data domainresource.NamespaceView `json:"data"`
	}
	err := c.request(ctx, http.MethodPost, basicMutationPath+"/namespaces", input, &payload)
	return payload.Data, err
}
func (c *Client) UpdateNamespace(ctx context.Context, name string, input domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error) {
	var payload struct {
		Data domainresource.NamespaceView `json:"data"`
	}
	err := c.request(ctx, http.MethodPut, basicMutationPath+"/namespaces/"+url.PathEscape(name), input, &payload)
	return payload.Data, err
}
func (c *Client) DeleteNamespace(ctx context.Context, name string) error {
	return c.request(ctx, http.MethodDelete, basicMutationPath+"/namespaces/"+url.PathEscape(name), nil, nil)
}
func (c *Client) UpdateNode(ctx context.Context, name string, input domainresource.NodeUpdateInput) (domainresource.NodeDetailView, error) {
	var payload struct {
		Data domainresource.NodeDetailView `json:"data"`
	}
	err := c.request(ctx, http.MethodPut, basicMutationPath+"/infrastructure/nodes/"+url.PathEscape(name), input, &payload)
	return payload.Data, err
}
func (c *Client) SetNodeUnschedulable(ctx context.Context, name string, unschedulable bool) error {
	return c.request(ctx, http.MethodPut, basicMutationPath+"/infrastructure/nodes/"+url.PathEscape(name)+"/schedulability", map[string]bool{"unschedulable": unschedulable}, nil)
}
func (c *Client) DrainNode(ctx context.Context, name string, input domainresource.NodeDrainInput) error {
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 300
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds+5)*time.Second)
	defer cancel()
	requestClient := *c
	httpClient := *c.httpClient
	httpClient.Timeout = time.Duration(input.TimeoutSeconds+5) * time.Second
	requestClient.httpClient = &httpClient
	return requestClient.request(ctx, http.MethodPost, basicMutationPath+"/infrastructure/nodes/"+url.PathEscape(name)+"/drain", input, nil)
}
func (c *Client) DeleteNode(ctx context.Context, name string) error {
	return c.DeleteResource(ctx, "", "Node", name)
}
func (c *Client) DeletePod(ctx context.Context, namespace, name string) error {
	return c.DeleteResource(ctx, namespace, "Pod", name)
}
func (c *Client) GetNodeYAML(ctx context.Context, name string) (domainresource.ResourceYAMLView, error) {
	return c.GetResourceYAML(ctx, "", "Node", name)
}
func (c *Client) SetCronJobSuspend(ctx context.Context, namespace, name string, suspend bool) (domainresource.CronJobDetailView, error) {
	var payload struct {
		Data domainresource.CronJobDetailView `json:"data"`
	}
	err := c.request(ctx, http.MethodPost, basicMutationPath+"/workloads/cronjobs/"+url.PathEscape(name)+"/suspend?namespace="+url.QueryEscape(namespace), map[string]bool{"suspend": suspend}, &payload)
	return payload.Data, err
}
