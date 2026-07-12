package aigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) writeRelayModels(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, writer http.ResponseWriter) error {
	repo := s.llmRelayRepository()
	if repo == nil {
		return fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	routes, err := repo.ListLLMModelRoutes(ctx, domainaigateway.LLMModelRouteFilter{})
	if err != nil {
		return err
	}
	metadata, err := ParseLLMTokenMetadata(accessCtx.Metadata)
	if err != nil {
		return err
	}
	models := make([]string, 0, len(routes))
	for _, route := range routes {
		if !route.Enabled || route.PublicModel == "" || slices.Contains(models, route.PublicModel) || !relayRouteMatchesRequestProvider(route, req.ProviderKind) {
			continue
		}
		if !relayAllowListAllows(metadata.AllowedModels, route.PublicModel, false) {
			continue
		}
		if !relayMetadataTeamPolicyAllows(principal.Teams, route.Metadata) {
			continue
		}
		if !s.relayRouteUpstreamTeamPolicyAllows(ctx, principal.Teams, route, req.ProviderKind) {
			continue
		}
		models = append(models, route.PublicModel)
	}
	sort.Strings(models)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	if req.ProviderKind == "anthropic" {
		_ = json.NewEncoder(writer).Encode(anthropicModelsResponse(models))
		return nil
	}
	if normalizeRelayProviderKind(req.ProviderKind) == "gemini" {
		_ = json.NewEncoder(writer).Encode(geminiModelsResponse(models))
		return nil
	}
	_ = json.NewEncoder(writer).Encode(openAIModelsResponse(models))
	return nil
}

func openAIModelsResponse(models []string) map[string]any {
	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		data = append(data, map[string]any{
			"id":       model,
			"object":   "model",
			"created":  0,
			"owned_by": "soha",
		})
	}
	return map[string]any{"object": "list", "data": data}
}

func anthropicModelsResponse(models []string) map[string]any {
	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		data = append(data, map[string]any{
			"id":           model,
			"type":         "model",
			"display_name": model,
		})
	}
	return map[string]any{
		"data":     data,
		"has_more": false,
		"first_id": firstModelID(models),
		"last_id":  lastModelID(models),
	}
}

func geminiModelsResponse(models []string) map[string]any {
	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		data = append(data, map[string]any{
			"name":                       "models/" + model,
			"version":                    "",
			"displayName":                model,
			"supportedGenerationMethods": []string{"generateContent"},
		})
	}
	return map[string]any{"models": data}
}

func firstModelID(models []string) string {
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func lastModelID(models []string) string {
	if len(models) == 0 {
		return ""
	}
	return models[len(models)-1]
}

func writeRelayRouteTraceHeaders(headers http.Header, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, upstreamStatus int, cacheStatus string) {
	if !relayRouteTraceRequested(req) {
		return
	}
	if strings.TrimSpace(cacheStatus) == "" {
		cacheStatus = relayCacheBypass
	}
	plan := relayTransformPlanForRoute(selection.route, req.ProviderKind)
	headers.Set("X-Soha-Route-ID", selection.route.ID)
	headers.Set("X-Soha-Upstream-ID", selection.upstream.ID)
	headers.Set("X-Soha-Upstream-Name", selection.upstream.Name)
	headers.Set("X-Soha-Provider-Kind", selection.upstream.ProviderKind)
	headers.Set("X-Soha-Public-Model", publicModel)
	headers.Set("X-Soha-Upstream-Model", selection.route.UpstreamModel)
	headers.Set("X-Soha-Relay-Endpoint", req.Endpoint)
	headers.Set("X-Soha-Relay-Provider-Kind", normalizeRelayProviderKind(req.ProviderKind))
	headers.Set("X-Soha-Relay-Stream", fmt.Sprint(stream))
	headers.Set("X-Soha-Upstream-Status", fmt.Sprint(upstreamStatus))
	headers.Set("X-Soha-Cache-Status", cacheStatus)
	if plan.enabled {
		headers.Set("X-Soha-Transform", plan.requestProvider+"-to-"+plan.upstreamProvider)
		headers.Set("X-Soha-Upstream-Endpoint", plan.upstreamEndpoint)
	}
}

func relayHeaderValue(headers http.Header, name string) string {
	if headers == nil {
		return ""
	}
	if value := headers.Get(name); value != "" {
		return value
	}
	for key, values := range headers {
		if !strings.EqualFold(strings.TrimSpace(key), name) {
			continue
		}
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				return value
			}
		}
	}
	return ""
}

func relayRouteTraceRequested(req LLMRelayHTTPRequest) bool {
	return boolFromAny(relayHeaderValue(req.Headers, relayHeaderRouteTrace))
}
