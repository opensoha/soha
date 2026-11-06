package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func (c *Client) listControllerRolloutHistory(ctx context.Context, resource, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	var payload struct {
		Items []domainresource.RolloutHistoryView `json:"items"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/%s/%s/rollouts?namespace=%s", resource, url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}
func (c *Client) getControllerRevisionYAML(ctx context.Context, resource, namespace, name, revisionName string) (domainresource.ResourceYAMLView, error) {
	var payload struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/%s/%s/rollouts/%s/yaml?namespace=%s", resource, url.PathEscape(name), url.PathEscape(revisionName), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return payload.Data, nil
}

func controllerHistoryRouteUnavailable(path string, resp *http.Response) bool {
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		return false
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		return false
	}
	route, _, _ := strings.Cut(path, "?")
	const prefix = "/api/v1/platform/workloads/"
	if !strings.HasPrefix(route, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(route, prefix), "/")
	return len(parts) >= 3 && (parts[0] == "statefulsets" || parts[0] == "daemonsets") && parts[2] == "rollouts" &&
		(len(parts) == 3 || (len(parts) == 5 && parts[4] == "yaml"))
}
