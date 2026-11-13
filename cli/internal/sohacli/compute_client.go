package sohacli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func (c APIClient) GetComputeCapabilities(ctx context.Context) (sohaapi.ComputeCapabilityManifest, error) {
	var out sohaapi.ComputeCapabilityManifestEnvelope
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/compute/capabilities", c.Token, nil, nil, &out); err != nil {
		return sohaapi.ComputeCapabilityManifest{}, err
	}
	return out.Data, nil
}

func (c APIClient) GetComputeOverview(ctx context.Context) (sohaapi.ComputeOverview, error) {
	var out sohaapi.ComputeOverviewEnvelope
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/compute/overview", c.Token, nil, nil, &out); err != nil {
		return sohaapi.ComputeOverview{}, err
	}
	return out.Data, nil
}

func (c APIClient) ListComputeAccessSources(ctx context.Context, params sohaapi.ListComputeAccessSourcesParams) (sohaapi.ComputeAccessSourceListEnvelope, error) {
	var out sohaapi.ComputeAccessSourceListEnvelope
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/compute/access-sources"+computeAccessSourceQuery(params), c.Token, nil, nil, &out); err != nil {
		return sohaapi.ComputeAccessSourceListEnvelope{}, err
	}
	return out, nil
}

func (c APIClient) ListComputeProviders(ctx context.Context, params sohaapi.ListComputeProvidersParams) (sohaapi.ComputeProviderListEnvelope, error) {
	var out sohaapi.ComputeProviderListEnvelope
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/compute/providers"+computeProviderQuery(params), c.Token, nil, nil, &out); err != nil {
		return sohaapi.ComputeProviderListEnvelope{}, err
	}
	return out, nil
}

func (c APIClient) ListComputeProviderInstances(ctx context.Context, params sohaapi.ListComputeProviderInstancesParams) (sohaapi.ComputeProviderInstanceListEnvelope, error) {
	var out sohaapi.ComputeProviderInstanceListEnvelope
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/compute/provider-instances"+computeProviderInstanceQuery(params), c.Token, nil, nil, &out); err != nil {
		return sohaapi.ComputeProviderInstanceListEnvelope{}, err
	}
	return out, nil
}

func (c APIClient) GetComputeProviderInstance(ctx context.Context, domain sohaapi.ComputeProviderDomain, providerKey, instanceRef string) (sohaapi.ComputeProviderInstance, error) {
	var out sohaapi.ComputeProviderInstanceEnvelope
	if err := c.doJSON(ctx, http.MethodGet, computeProviderInstancePath(domain, providerKey, instanceRef), c.Token, nil, nil, &out); err != nil {
		return sohaapi.ComputeProviderInstance{}, err
	}
	return out.Data, nil
}

func (c APIClient) CheckComputeProviderInstanceHealth(ctx context.Context, domain sohaapi.ComputeProviderDomain, providerKey, instanceRef, idempotencyKey string, input sohaapi.ComputeProviderReadRequest) (sohaapi.ConnectionCheckResult, error) {
	var out sohaapi.ConnectionCheckResultEnvelope
	path := computeProviderInstancePath(domain, providerKey, instanceRef) + "/health-checks"
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, idempotencyHeaders(idempotencyKey), input, &out); err != nil {
		return sohaapi.ConnectionCheckResult{}, err
	}
	if out.Data.CheckedAt.IsZero() || strings.TrimSpace(out.Data.Status) == "" {
		return sohaapi.ConnectionCheckResult{}, fmt.Errorf("connection checks require a server upgrade to return synchronous results")
	}
	return out.Data, nil
}

func (c APIClient) DiscoverComputeProviderInstance(ctx context.Context, domain sohaapi.ComputeProviderDomain, providerKey, instanceRef, idempotencyKey string, input sohaapi.ComputeProviderDiscoverRequest) (ComputeTaskView, error) {
	return c.mutateComputeProviderInstance(ctx, domain, providerKey, instanceRef, "discoveries", idempotencyKey, input)
}

func (c APIClient) mutateComputeProviderInstance(ctx context.Context, domain sohaapi.ComputeProviderDomain, providerKey, instanceRef, action, idempotencyKey string, input any) (ComputeTaskView, error) {
	var out itemResponse[ComputeTaskView]
	path := computeProviderInstancePath(domain, providerKey, instanceRef) + "/" + action
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, idempotencyHeaders(idempotencyKey), input, &out); err != nil {
		return ComputeTaskView{}, err
	}
	return out.Data, nil
}

func (c APIClient) GetComputeResource(ctx context.Context, domain sohaapi.ComputeDomain, kind sohaapi.ComputeResourceKind, id string) (any, error) {
	var out sohaapi.GenericDataEnvelope
	if err := c.doJSON(ctx, http.MethodGet, computeResourcePath(domain, kind, id), c.Token, nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c APIClient) ListComputeResourceRelations(ctx context.Context, domain sohaapi.ComputeDomain, kind sohaapi.ComputeResourceKind, id string, params sohaapi.ListComputeResourceRelationsParams) (sohaapi.ComputeResourceRelations, error) {
	var out sohaapi.ComputeResourceRelationListEnvelope
	path := computeResourcePath(domain, kind, id) + "/relations" + computeRelationQuery(params)
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, nil, nil, &out); err != nil {
		return sohaapi.ComputeResourceRelations{}, err
	}
	return out.Data, nil
}

func (c APIClient) ExecuteComputeResourceAction(ctx context.Context, domain sohaapi.ComputeDomain, kind sohaapi.ComputeResourceKind, id, action, idempotencyKey string, input sohaapi.ComputeResourceActionRequest) (ComputeTaskView, error) {
	var out itemResponse[ComputeTaskView]
	path := computeResourcePath(domain, kind, id) + "/actions/" + url.PathEscape(strings.TrimSpace(action))
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, idempotencyHeaders(idempotencyKey), input, &out); err != nil {
		return ComputeTaskView{}, err
	}
	return out.Data, nil
}

func (c APIClient) ListComputeTasks(ctx context.Context, params sohaapi.ListComputeTasksParams) (sohaapi.ComputeTaskListEnvelope, error) {
	var out sohaapi.ComputeTaskListEnvelope
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/compute/tasks"+computeTaskQuery(params), c.Token, nil, nil, &out); err != nil {
		return sohaapi.ComputeTaskListEnvelope{}, err
	}
	return out, nil
}

func (c APIClient) ListComputeTaskLogs(ctx context.Context, domain ComputeTaskDomain, taskID string) (sohaapi.ComputeTaskLogListEnvelope, error) {
	var out sohaapi.ComputeTaskLogListEnvelope
	if err := c.doJSON(ctx, http.MethodGet, computeTaskPath(domain, taskID)+"/logs", c.Token, nil, nil, &out); err != nil {
		return sohaapi.ComputeTaskLogListEnvelope{}, err
	}
	return out, nil
}

func (c APIClient) CancelComputeTaskWithKey(ctx context.Context, domain ComputeTaskDomain, taskID, idempotencyKey string, input ComputeTaskMutationRequest) (ComputeTaskView, error) {
	return c.mutateComputeTask(ctx, domain, taskID, "cancel", idempotencyKey, input)
}

func (c APIClient) RetryComputeTask(ctx context.Context, domain ComputeTaskDomain, taskID string, input ComputeTaskMutationRequest) (ComputeTaskView, error) {
	return c.RetryComputeTaskWithKey(ctx, domain, taskID, computeIdempotencyKey(string(domain), taskID, "retry", input), input)
}

func (c APIClient) RetryComputeTaskWithKey(ctx context.Context, domain ComputeTaskDomain, taskID, idempotencyKey string, input ComputeTaskMutationRequest) (ComputeTaskView, error) {
	return c.mutateComputeTask(ctx, domain, taskID, "retry", idempotencyKey, input)
}

func (c APIClient) mutateComputeTask(ctx context.Context, domain ComputeTaskDomain, taskID, action, idempotencyKey string, input ComputeTaskMutationRequest) (ComputeTaskView, error) {
	var out itemResponse[ComputeTaskView]
	path := computeTaskPath(domain, taskID) + "/" + action
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, idempotencyHeaders(idempotencyKey), input, &out); err != nil {
		return ComputeTaskView{}, err
	}
	return out.Data, nil
}

func computeProviderInstancePath(domain sohaapi.ComputeProviderDomain, providerKey, instanceRef string) string {
	return "/api/v1/compute/provider-instances/" + url.PathEscape(string(domain)) + "/" + url.PathEscape(strings.TrimSpace(providerKey)) + "/" + url.PathEscape(strings.TrimSpace(instanceRef))
}

func computeResourcePath(domain sohaapi.ComputeDomain, kind sohaapi.ComputeResourceKind, id string) string {
	return "/api/v1/compute/resources/" + url.PathEscape(string(domain)) + "/" + url.PathEscape(string(kind)) + "/" + url.PathEscape(strings.TrimSpace(id))
}

func computeAccessSourceQuery(params sohaapi.ListComputeAccessSourcesParams) string {
	values := url.Values{}
	setComputeQuery(values, "sourceType", string(params.SourceType))
	setComputeQuery(values, "providerKey", params.ProviderKey)
	setComputeQuery(values, "cursor", string(params.Cursor))
	setComputeInt(values, "limit", int(params.Limit))
	return encodeComputeQuery(values)
}

func computeProviderQuery(params sohaapi.ListComputeProvidersParams) string {
	values := url.Values{}
	setComputeQuery(values, "domain", string(params.Domain))
	setComputeQuery(values, "source", string(params.Source))
	setComputeQuery(values, "cursor", string(params.Cursor))
	setComputeInt(values, "limit", int(params.Limit))
	return encodeComputeQuery(values)
}

func computeProviderInstanceQuery(params sohaapi.ListComputeProviderInstancesParams) string {
	values := url.Values{}
	setComputeQuery(values, "domain", string(params.Domain))
	setComputeQuery(values, "providerKey", params.ProviderKey)
	setComputeQuery(values, "cursor", string(params.Cursor))
	setComputeInt(values, "limit", int(params.Limit))
	return encodeComputeQuery(values)
}

func computeRelationQuery(params sohaapi.ListComputeResourceRelationsParams) string {
	values := url.Values{}
	setComputeQuery(values, "cursor", string(params.Cursor))
	setComputeInt(values, "limit", int(params.Limit))
	return encodeComputeQuery(values)
}

func computeTaskQuery(params sohaapi.ListComputeTasksParams) string {
	values := url.Values{}
	setComputeQuery(values, "domain", string(params.Domain))
	setComputeQuery(values, "providerKey", params.ProviderKey)
	setComputeQuery(values, "status", string(params.Status))
	setComputeQuery(values, "category", string(params.Category))
	setComputeQuery(values, "resourceKind", params.ResourceKind)
	setComputeQuery(values, "resourceId", params.ResourceID)
	setComputeQuery(values, "sortBy", params.SortBy)
	setComputeQuery(values, "sortOrder", string(params.SortOrder))
	setComputeQuery(values, "cursor", string(params.Cursor))
	setComputeInt(values, "limit", int(params.Limit))
	return encodeComputeQuery(values)
}

func idempotencyHeaders(key string) map[string]string {
	return map[string]string{"Idempotency-Key": strings.TrimSpace(key)}
}

func computeIdempotencyKey(domain, id, action string, input any) string {
	raw, _ := json.Marshal(input)
	sum := sha256.Sum256([]byte(strings.TrimSpace(domain) + "\x00" + strings.TrimSpace(id) + "\x00" + strings.TrimSpace(action) + "\x00" + string(raw)))
	return fmt.Sprintf("compute-%x", sum)
}

func setComputeQuery(values url.Values, key, value string) {
	if strings.TrimSpace(value) != "" {
		values.Set(key, strings.TrimSpace(value))
	}
}

func setComputeInt(values url.Values, key string, value int) {
	if value != 0 {
		values.Set(key, strconv.Itoa(value))
	}
}

func encodeComputeQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	return "?" + values.Encode()
}
