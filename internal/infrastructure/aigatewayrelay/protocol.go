package aigatewayrelay

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/opensoha/soha/internal/application/aigateway/relayport"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func relayRealtimeEndpointURL(target relayport.Target, publicModel string) (string, error) {
	targetURL, err := relayEndpointURLForTarget(target, "openai", "realtime")
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(targetURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%w: relay realtime upstream URL is invalid", apperrors.ErrInvalidArgument)
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("%w: relay realtime upstream URL scheme is not supported", apperrors.ErrInvalidArgument)
	}
	query := parsed.Query()
	query.Set("model", strings.TrimSpace(firstNonEmpty(target.Model, publicModel)))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func relayEndpointURLForTarget(target relayport.Target, providerKind, endpoint string) (string, error) {
	providerKind = relayport.NormalizeProvider(providerKind)
	if providerKind == "azure-openai" {
		return azureOpenAIEndpointURL(
			target.BaseURL,
			target.Model,
			endpoint,
			target.Metadata,
			target.RouteMetadata,
		)
	}
	if providerKind == "gemini" {
		return geminiEndpointURL(
			target.BaseURL,
			target.Model,
			endpoint,
			target.Metadata,
			target.RouteMetadata,
		)
	}
	if providerKind == "cohere" {
		return cohereEndpointURL(target.BaseURL, endpoint)
	}
	return relayEndpointURL(target.BaseURL, providerKind, endpoint)
}

func relayEndpointURL(baseURL, providerKind, endpoint string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%w: relay upstream base URL is required", apperrors.ErrInvalidArgument)
	}
	switch providerKind {
	case "anthropic":
		if strings.HasSuffix(baseURL, "/v1") {
			return baseURL + anthropicEndpointPath(endpoint), nil
		}
		return baseURL + "/v1" + anthropicEndpointPath(endpoint), nil
	default:
		if strings.HasSuffix(baseURL, "/v1") {
			return baseURL + openAIEndpointPath(endpoint), nil
		}
		return baseURL + "/v1" + openAIEndpointPath(endpoint), nil
	}
}

func azureOpenAIEndpointURL(baseURL, deployment, endpoint string, upstreamMetadata, routeMetadata map[string]any) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%w: relay upstream base URL is required", apperrors.ErrInvalidArgument)
	}
	values := azureOpenAIConfigValues(upstreamMetadata, routeMetadata)
	apiStyle := strings.ToLower(strings.TrimSpace(gatewayFirstString(values, "apiStyle", "api_style", "style", "mode")))
	apiVersion := strings.TrimSpace(gatewayFirstString(values, "apiVersion", "api_version", "azureApiVersion", "azure_api_version"))
	if apiStyle == "" {
		apiStyle = "v1"
		if apiVersion != "" && azureOpenAIEndpointSupportsDeployment(endpoint) {
			apiStyle = "deployment"
		}
	}
	switch apiStyle {
	case "deployment", "deployments", "versioned", "api-version", "api_version":
		if azureOpenAIEndpointSupportsDeployment(endpoint) {
			return azureOpenAIDeploymentEndpointURL(baseURL, deployment, endpoint, apiVersion, values)
		}
	}
	return azureOpenAIV1EndpointURL(baseURL, endpoint), nil
}

func azureOpenAIConfigValues(upstreamMetadata, routeMetadata map[string]any) map[string]any {
	out := gatewayConditionValues(upstreamMetadata, "azureOpenAI", "azure_openai", "azure")
	for key, value := range gatewayConditionValues(routeMetadata, "azureOpenAI", "azure_openai", "azure") {
		out[key] = value
	}
	return out
}

func geminiEndpointURL(baseURL, model, endpoint string, upstreamMetadata, routeMetadata map[string]any) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%w: relay upstream base URL is required", apperrors.ErrInvalidArgument)
	}
	values := geminiConfigValues(upstreamMetadata, routeMetadata)
	apiVersion := strings.Trim(strings.TrimSpace(gatewayFirstString(values, "apiVersion", "api_version", "version")), "/")
	if apiVersion == "" {
		apiVersion = "v1beta"
	}
	resourceBaseURL := geminiResourceBaseURL(baseURL, apiVersion)
	switch endpoint {
	case "models":
		return resourceBaseURL + "/models", nil
	case "interactions":
		return resourceBaseURL + "/interactions", nil
	case "generateContent", "streamGenerateContent":
		model = strings.TrimSpace(strings.TrimPrefix(model, "models/"))
		if model == "" {
			return "", fmt.Errorf("%w: Gemini relay upstream model is required", apperrors.ErrInvalidArgument)
		}
		targetURL := resourceBaseURL + "/models/" + url.PathEscape(model) + ":" + endpoint
		if endpoint == "streamGenerateContent" {
			targetURL += "?alt=sse"
		}
		return targetURL, nil
	default:
		return "", fmt.Errorf("%w: Gemini relay endpoint %s is not supported", apperrors.ErrInvalidArgument, endpoint)
	}
}

func geminiResourceBaseURL(baseURL, apiVersion string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(baseURL)
	apiVersion = strings.Trim(apiVersion, "/")
	if strings.HasSuffix(lower, "/"+strings.ToLower(apiVersion)) {
		return baseURL
	}
	return baseURL + "/" + apiVersion
}

func geminiConfigValues(upstreamMetadata, routeMetadata map[string]any) map[string]any {
	out := gatewayConditionValues(upstreamMetadata, "gemini", "googleAI", "google_ai")
	for key, value := range gatewayConditionValues(routeMetadata, "gemini", "googleAI", "google_ai") {
		out[key] = value
	}
	return out
}

func cohereEndpointURL(baseURL, endpoint string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%w: relay upstream base URL is required", apperrors.ErrInvalidArgument)
	}
	switch endpoint {
	case "models":
		baseURL = trimURLSuffixFold(baseURL, "/v2")
		if strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
			return baseURL + "/models", nil
		}
		return baseURL + "/v1/models", nil
	case "rerank":
		baseURL = trimURLSuffixFold(baseURL, "/v1")
		if strings.HasSuffix(strings.ToLower(baseURL), "/v2") {
			return baseURL + "/rerank", nil
		}
		return baseURL + "/v2/rerank", nil
	default:
		return "", fmt.Errorf("%w: Cohere relay endpoint %s is not supported", apperrors.ErrInvalidArgument, endpoint)
	}
}

func trimURLSuffixFold(value, suffix string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if strings.HasSuffix(strings.ToLower(value), strings.ToLower(suffix)) {
		return value[:len(value)-len(suffix)]
	}
	return value
}

func azureOpenAIEndpointSupportsDeployment(endpoint string) bool {
	switch endpoint {
	case "chat/completions", "embeddings", "images/generations", "images/edits", "images/variations", "audio/speech", "audio/transcriptions", "audio/translations":
		return true
	default:
		return false
	}
}

func azureOpenAIV1EndpointURL(baseURL, endpoint string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(baseURL)
	switch {
	case strings.HasSuffix(lower, "/openai/v1"):
		return baseURL + openAIEndpointPath(endpoint)
	case strings.HasSuffix(lower, "/openai"):
		return baseURL + "/v1" + openAIEndpointPath(endpoint)
	default:
		return baseURL + "/openai/v1" + openAIEndpointPath(endpoint)
	}
}

func azureOpenAIDeploymentEndpointURL(baseURL, deployment, endpoint, apiVersion string, values map[string]any) (string, error) {
	deployment = strings.TrimSpace(firstNonEmpty(
		gatewayFirstString(values, "deployment", "deploymentID", "deploymentId", "deploymentName"),
		deployment,
	))
	if deployment == "" {
		return "", fmt.Errorf("%w: Azure OpenAI deployment is required", apperrors.ErrInvalidArgument)
	}
	apiVersion = strings.TrimSpace(apiVersion)
	if apiVersion == "" {
		return "", fmt.Errorf("%w: Azure OpenAI apiVersion is required for deployment style", apperrors.ErrInvalidArgument)
	}
	resourceBaseURL := azureOpenAIResourceBaseURL(baseURL)
	targetURL := resourceBaseURL + "/openai/deployments/" + url.PathEscape(deployment) + openAIEndpointPath(endpoint)
	return appendRelayQuery(targetURL, "api-version", apiVersion), nil
}

func azureOpenAIResourceBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(baseURL)
	switch {
	case strings.HasSuffix(lower, "/openai/v1"):
		return strings.TrimRight(baseURL[:len(baseURL)-len("/openai/v1")], "/")
	case strings.HasSuffix(lower, "/openai"):
		return strings.TrimRight(baseURL[:len(baseURL)-len("/openai")], "/")
	default:
		return baseURL
	}
}

func appendRelayQuery(targetURL, key, value string) string {
	separator := "?"
	if strings.Contains(targetURL, "?") {
		separator = "&"
	}
	return targetURL + separator + url.QueryEscape(key) + "=" + url.QueryEscape(value)
}

func openAIEndpointPath(endpoint string) string {
	switch endpoint {
	case "models":
		return "/models"
	case "realtime":
		return "/realtime"
	case "responses":
		return "/responses"
	case "embeddings":
		return "/embeddings"
	case "images/generations":
		return "/images/generations"
	case "images/edits":
		return "/images/edits"
	case "images/variations":
		return "/images/variations"
	case "audio/speech":
		return "/audio/speech"
	case "audio/transcriptions":
		return "/audio/transcriptions"
	case "audio/translations":
		return "/audio/translations"
	default:
		return "/chat/completions"
	}
}

func anthropicEndpointPath(endpoint string) string {
	switch endpoint {
	case "models":
		return "/models"
	default:
		return "/messages"
	}
}

func applyRelayUpstreamHeaders(headers http.Header, req relayport.Request, defaultHeaders map[string]any, apiKey string) {
	contentType := "application/json"
	if relayport.RequiresMultipart(req.Endpoint) {
		contentType = strings.TrimSpace(req.Headers.Get("Content-Type"))
	}
	headers.Set("Accept", firstNonEmpty(req.Headers.Get("Accept"), relayDefaultUpstreamAccept(req.Endpoint)))
	for key, value := range defaultHeaders {
		name := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if name == "" || relayport.SensitiveHeader(name) {
			continue
		}
		headers.Set(name, fmt.Sprint(value))
	}
	headers.Set("Content-Type", firstNonEmpty(contentType, "application/json"))
	if req.ProviderKind == "anthropic" {
		headers.Set("x-api-key", apiKey)
		headers.Set("anthropic-version", firstNonEmpty(req.Headers.Get("anthropic-version"), headers.Get("anthropic-version"), "2023-06-01"))
		if beta := req.Headers.Get("anthropic-beta"); beta != "" {
			headers.Set("anthropic-beta", beta)
		}
		return
	}
	if relayport.NormalizeProvider(req.ProviderKind) == "azure-openai" {
		headers.Set("api-key", apiKey)
		return
	}
	if relayport.NormalizeProvider(req.ProviderKind) == "gemini" {
		headers.Set("x-goog-api-key", apiKey)
		return
	}
	headers.Set("Authorization", "Bearer "+apiKey)
	if organization := req.Headers.Get("OpenAI-Organization"); organization != "" && relayport.UsesOpenAIProtocol(req.ProviderKind) {
		headers.Set("OpenAI-Organization", organization)
	}
}

func relayDefaultUpstreamAccept(endpoint string) string {
	if strings.TrimSpace(endpoint) == "audio/speech" {
		return "*/*"
	}
	return "application/json"
}

func gatewayConditionValues(conditions map[string]any, aliases ...string) map[string]any {
	out := make(map[string]any, len(conditions)+4)
	for key, value := range conditions {
		out[key] = value
	}
	for _, alias := range aliases {
		raw, ok := gatewayConditionRaw(conditions, alias)
		if !ok {
			continue
		}
		nested, _ := raw.(map[string]any)
		for key, value := range nested {
			out[key] = value
		}
	}
	return out
}

func gatewayConditionRaw(values map[string]any, key string) (any, bool) {
	if values == nil {
		return nil, false
	}
	if value, ok := values[key]; ok {
		return value, true
	}
	normalized := normalizeGatewayConditionKey(key)
	for candidate, value := range values {
		if normalizeGatewayConditionKey(candidate) == normalized {
			return value, true
		}
	}
	return nil, false
}

func gatewayFirstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok || raw == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(raw))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func normalizeGatewayConditionKey(key string) string {
	replacer := strings.NewReplacer("_", "", "-", "", " ", "", ".", "")
	return replacer.Replace(strings.ToLower(strings.TrimSpace(key)))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
