package aigateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"strings"

	"github.com/opensoha/soha/internal/application/aigateway/relayport"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func relayRequestModel(req LLMRelayHTTPRequest) (string, bool, error) {
	if pathModel := strings.TrimSpace(req.PathModel); pathModel != "" {
		return pathModel, relayEndpointSupportsStreaming(req.Endpoint), nil
	}
	if relayport.RequiresMultipart(req.Endpoint) {
		return relayMultipartRequestModel(req)
	}
	var payload map[string]any
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		return "", false, fmt.Errorf("%w: invalid relay request JSON", apperrors.ErrInvalidArgument)
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return "", false, fmt.Errorf("%w: relay request model is required", apperrors.ErrInvalidArgument)
	}
	stream, _ := payload["stream"].(bool)
	switch strings.TrimSpace(req.Endpoint) {
	case "interactions":
		if stream {
			return "", false, fmt.Errorf("%w: Gemini interactions relay streaming is not supported", apperrors.ErrInvalidArgument)
		}
		if boolFromAny(payload["background"]) {
			return "", false, fmt.Errorf("%w: Gemini interactions relay background mode is not supported", apperrors.ErrInvalidArgument)
		}
	case "images/generations":
		if stream {
			return "", false, fmt.Errorf("%w: image generation relay streaming is not supported", apperrors.ErrInvalidArgument)
		}
	case "audio/speech":
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(req.Headers.Get("Content-Type"))), "multipart/") {
			return "", false, fmt.Errorf("%w: audio speech relay only supports JSON requests", apperrors.ErrInvalidArgument)
		}
		if stream {
			return "", false, fmt.Errorf("%w: audio speech relay streaming is not supported", apperrors.ErrInvalidArgument)
		}
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(payload["stream_format"])), "sse") {
			return "", false, fmt.Errorf("%w: audio speech relay SSE streaming is not supported", apperrors.ErrInvalidArgument)
		}
	}
	return model, stream && relayEndpointSupportsStreaming(req.Endpoint), nil
}

func relayRealtimeRequestModel(req LLMRelayHTTPRequest) (string, error) {
	model := strings.TrimSpace(req.QueryModel)
	if model == "" {
		model = strings.TrimSpace(req.PathModel)
	}
	if model == "" {
		return "", fmt.Errorf("%w: realtime relay model query parameter is required", apperrors.ErrInvalidArgument)
	}
	return model, nil
}

func relayMultipartRequestModel(req LLMRelayHTTPRequest) (string, bool, error) {
	fields, err := relayMultipartRequestFields(req.Endpoint, req.Body, req.Headers.Get("Content-Type"))
	if err != nil {
		return "", false, err
	}
	model := strings.TrimSpace(fields["model"])
	if model == "" {
		return "", false, fmt.Errorf("%w: relay request model is required", apperrors.ErrInvalidArgument)
	}
	if relayMultipartBool(fields["stream"]) {
		return "", false, fmt.Errorf("%w: multipart relay streaming is not supported", apperrors.ErrInvalidArgument)
	}
	if strings.EqualFold(strings.TrimSpace(fields["stream_format"]), "sse") {
		return "", false, fmt.Errorf("%w: multipart relay SSE streaming is not supported", apperrors.ErrInvalidArgument)
	}
	return model, false, nil
}

func relayMultipartRequestFields(endpoint string, body []byte, contentType string) (map[string]string, error) {
	boundary, err := relayport.MultipartBoundary(contentType)
	if err != nil {
		return nil, err
	}
	policy := relayport.MultipartPolicyForEndpoint(endpoint)
	fields := make(map[string]string, 4)
	modelCount := 0
	fileFields := map[string]bool{}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: invalid relay multipart request", apperrors.ErrInvalidArgument)
		}
		name := part.FormName()
		if part.FileName() != "" {
			fileFields[name] = true
			if name == "model" {
				return nil, fmt.Errorf("%w: relay request model must be a text field", apperrors.ErrInvalidArgument)
			}
			continue
		}
		if name == "model" {
			modelCount++
			if modelCount > 1 {
				return nil, fmt.Errorf("%w: relay request model must not be duplicated", apperrors.ErrInvalidArgument)
			}
		}
		if !policy.TrackedTextFields[name] {
			continue
		}
		value, err := io.ReadAll(io.LimitReader(part, 64*1024+1))
		if err != nil {
			return nil, fmt.Errorf("%w: invalid relay multipart field", apperrors.ErrInvalidArgument)
		}
		if len(value) > 64*1024 {
			return nil, fmt.Errorf("%w: relay multipart field is too large", apperrors.ErrInvalidArgument)
		}
		fields[name] = string(value)
	}
	for _, field := range policy.RequiredFileFields {
		if !fileFields[field] {
			return nil, fmt.Errorf("%w: relay multipart %s file is required", apperrors.ErrInvalidArgument, field)
		}
	}
	for _, field := range policy.RequiredTextFields {
		if strings.TrimSpace(fields[field]) == "" {
			return nil, fmt.Errorf("%w: relay multipart %s field is required", apperrors.ErrInvalidArgument, field)
		}
	}
	return fields, nil
}

func relayMultipartBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

func relayEndpointSupportsStreaming(endpoint string) bool {
	switch endpoint {
	case "chat/completions", "responses", "messages", "streamGenerateContent":
		return true
	default:
		return false
	}
}

type relayTransformPlan struct {
	enabled          bool
	requestProvider  string
	requestEndpoint  string
	upstreamProvider string
	upstreamEndpoint string
}

func relayTransformPlanForRoute(route domainaigateway.LLMModelRoute, requestProvider string) relayTransformPlan {
	requestProvider = relayport.NormalizeProvider(requestProvider)
	if requestProvider == "" {
		return relayTransformPlan{}
	}
	values := gatewayConditionValues(route.TransformPolicy, "transform", "conversion", "formatConversion", "format_conversion")
	mode := strings.ToLower(gatewayFirstString(values, "mode", "type", "strategy"))
	if mode == "" {
		if enabled, ok := gatewayConditionRaw(values, "enabled"); !ok || !boolFromAny(enabled) {
			return relayTransformPlan{}
		}
	}
	if mode == "passthrough" || mode == "native" || mode == "none" || mode == "off" {
		return relayTransformPlan{}
	}
	targetProvider := relayport.NormalizeProvider(gatewayFirstString(values, "targetProviderKind", "targetProvider", "target", "upstreamProviderKind", "upstreamProvider", "providerKind"))
	if targetProvider == "" {
		routeProvider := relayport.NormalizeProvider(route.ProviderKind)
		if routeProvider != "" && !relayRouteProviderMatches(routeProvider, requestProvider) {
			targetProvider = routeProvider
		}
	}
	if targetProvider == "" || targetProvider == requestProvider {
		return relayTransformPlan{}
	}
	if targetProvider == "openai-compatible" {
		targetProvider = "openai"
	}
	switch requestProvider + "->" + targetProvider {
	case "openai->anthropic":
		return relayTransformPlan{
			enabled:          true,
			requestProvider:  "openai",
			requestEndpoint:  "chat/completions",
			upstreamProvider: "anthropic",
			upstreamEndpoint: "messages",
		}
	case "anthropic->openai":
		return relayTransformPlan{
			enabled:          true,
			requestProvider:  "anthropic",
			requestEndpoint:  "messages",
			upstreamProvider: "openai",
			upstreamEndpoint: "chat/completions",
		}
	default:
		return relayTransformPlan{}
	}
}

func relayRequestTransformPlan(req LLMRelayHTTPRequest, selection relaySelection, stream bool) (relayTransformPlan, error) {
	plan := relayTransformPlanForRoute(selection.route, req.ProviderKind)
	if !plan.enabled {
		return relayTransformPlan{}, nil
	}
	if stream {
		return relayTransformPlan{}, fmt.Errorf("%w: relay format conversion only supports non-streaming requests", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(req.Endpoint) != plan.requestEndpoint {
		return relayTransformPlan{}, fmt.Errorf("%w: relay format conversion does not support endpoint %s", apperrors.ErrInvalidArgument, req.Endpoint)
	}
	if upstreamProvider := selection.upstreamProviderKind(); upstreamProvider != "" && !relayRouteProviderMatches(upstreamProvider, plan.upstreamProvider) {
		return relayTransformPlan{}, fmt.Errorf("%w: relay transform upstream provider mismatch", apperrors.ErrInvalidArgument)
	}
	return plan, nil
}

func normalizeRelayProviderKind(value string) string {
	return relayport.NormalizeProvider(value)
}

func relayProviderUsesOpenAIWireProtocol(value string) bool {
	return relayport.UsesOpenAIProtocol(value)
}

func relayEndpointRequiresMultipart(endpoint string) bool {
	return relayport.RequiresMultipart(endpoint)
}

func relayTextFromContent(value any) (string, bool) {
	return relayport.TextFromContent(value)
}
