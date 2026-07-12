package aigateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"

	"github.com/google/uuid"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func relayRequestModel(req LLMRelayHTTPRequest) (string, bool, error) {
	if pathModel := strings.TrimSpace(req.PathModel); pathModel != "" {
		return pathModel, relayEndpointSupportsStreaming(req.Endpoint), nil
	}
	if relayEndpointRequiresMultipart(req.Endpoint) {
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

func relayEndpointRequiresMultipart(endpoint string) bool {
	switch strings.TrimSpace(endpoint) {
	case "audio/transcriptions", "audio/translations", "images/edits", "images/variations":
		return true
	default:
		return false
	}
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
	boundary, err := relayMultipartBoundary(contentType)
	if err != nil {
		return nil, err
	}
	policy := relayMultipartPolicyForEndpoint(endpoint)
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
		if !policy.trackedTextFields[name] {
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
	for _, field := range policy.requiredFileFields {
		if !fileFields[field] {
			return nil, fmt.Errorf("%w: relay multipart %s file is required", apperrors.ErrInvalidArgument, field)
		}
	}
	for _, field := range policy.requiredTextFields {
		if strings.TrimSpace(fields[field]) == "" {
			return nil, fmt.Errorf("%w: relay multipart %s field is required", apperrors.ErrInvalidArgument, field)
		}
	}
	return fields, nil
}

type relayMultipartEndpointPolicy struct {
	requiredFileFields []string
	requiredTextFields []string
	trackedTextFields  map[string]bool
}

func relayMultipartPolicyForEndpoint(endpoint string) relayMultipartEndpointPolicy {
	policy := relayMultipartEndpointPolicy{
		trackedTextFields: map[string]bool{
			"model":         true,
			"stream":        true,
			"stream_format": true,
			"prompt":        true,
		},
	}
	switch strings.TrimSpace(endpoint) {
	case "images/edits":
		policy.requiredFileFields = []string{"image"}
		policy.requiredTextFields = []string{"prompt"}
	case "images/variations":
		policy.requiredFileFields = []string{"image"}
	default:
		policy.requiredFileFields = []string{"file"}
	}
	return policy
}

func relayMultipartBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

func relayMultipartBoundary(contentType string) (string, error) {
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil {
		return "", fmt.Errorf("%w: invalid relay multipart content type", apperrors.ErrInvalidArgument)
	}
	if !strings.EqualFold(mediaType, "multipart/form-data") {
		return "", fmt.Errorf("%w: relay endpoint requires multipart/form-data", apperrors.ErrInvalidArgument)
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return "", fmt.Errorf("%w: relay multipart boundary is required", apperrors.ErrInvalidArgument)
	}
	return boundary, nil
}

func relayEndpointSupportsStreaming(endpoint string) bool {
	switch endpoint {
	case "chat/completions", "responses", "messages", "streamGenerateContent":
		return true
	default:
		return false
	}
}

func rewriteRelayMultipartRequestBody(endpoint string, body []byte, contentType, upstreamModel string) ([]byte, string, error) {
	boundary, err := relayMultipartBoundary(contentType)
	if err != nil {
		return nil, "", err
	}
	policy := relayMultipartPolicyForEndpoint(endpoint)
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, "", fmt.Errorf("%w: invalid relay multipart boundary", apperrors.ErrInvalidArgument)
	}
	foundModel := false
	fileFields := map[string]bool{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("%w: invalid relay multipart request", apperrors.ErrInvalidArgument)
		}
		if part.FileName() != "" {
			fileFields[part.FormName()] = true
		}
		if part.FormName() == "model" && part.FileName() != "" {
			return nil, "", fmt.Errorf("%w: relay request model must be a text field", apperrors.ErrInvalidArgument)
		}
		partWriter, err := writer.CreatePart(cloneMIMEHeader(part.Header))
		if err != nil {
			return nil, "", fmt.Errorf("create relay multipart part: %w", err)
		}
		if part.FormName() == "model" && part.FileName() == "" {
			if foundModel {
				return nil, "", fmt.Errorf("%w: relay request model must not be duplicated", apperrors.ErrInvalidArgument)
			}
			if _, err := io.WriteString(partWriter, upstreamModel); err != nil {
				return nil, "", fmt.Errorf("write relay multipart model: %w", err)
			}
			foundModel = true
			continue
		}
		if _, err := io.Copy(partWriter, part); err != nil {
			return nil, "", fmt.Errorf("copy relay multipart part: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close relay multipart body: %w", err)
	}
	if !foundModel {
		return nil, "", fmt.Errorf("%w: relay request model is required", apperrors.ErrInvalidArgument)
	}
	for _, field := range policy.requiredFileFields {
		if !fileFields[field] {
			return nil, "", fmt.Errorf("%w: relay multipart %s file is required", apperrors.ErrInvalidArgument, field)
		}
	}
	return out.Bytes(), writer.FormDataContentType(), nil
}

func cloneMIMEHeader(header textproto.MIMEHeader) textproto.MIMEHeader {
	out := make(textproto.MIMEHeader, len(header))
	for key, values := range header {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func rewriteRelayRequestBody(body []byte, upstreamModel, providerKind string, stream, includeOpenAIUsage bool) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: invalid relay request JSON", apperrors.ErrInvalidArgument)
	}
	payload["model"] = upstreamModel
	if stream && includeOpenAIUsage && relayProviderUsesOpenAIWireProtocol(providerKind) {
		options, _ := payload["stream_options"].(map[string]any)
		if options == nil {
			options = map[string]any{}
		}
		options["include_usage"] = true
		payload["stream_options"] = options
	}
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal relay request body: %w", err)
	}
	return rewritten, nil
}

type relayTransformPlan struct {
	enabled          bool
	requestProvider  string
	requestEndpoint  string
	upstreamProvider string
	upstreamEndpoint string
}

func relayTransformPlanForRoute(route domainaigateway.LLMModelRoute, requestProvider string) relayTransformPlan {
	requestProvider = normalizeRelayProviderKind(requestProvider)
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
	targetProvider := normalizeRelayProviderKind(gatewayFirstString(values, "targetProviderKind", "targetProvider", "target", "upstreamProviderKind", "upstreamProvider", "providerKind"))
	if targetProvider == "" {
		routeProvider := normalizeRelayProviderKind(route.ProviderKind)
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

func relayTransformUpstreamRequest(req LLMRelayHTTPRequest, selection relaySelection, stream bool, includeOpenAIUsage bool) ([]byte, string, string, string, error) {
	plan, err := relayRequestTransformPlan(req, selection, stream)
	if err != nil {
		return nil, "", "", "", err
	}
	if !plan.enabled {
		if normalizeRelayProviderKind(req.ProviderKind) == "gemini" {
			body, err := relayGeminiRequestBody(req, selection.route.UpstreamModel)
			return body, "gemini", strings.TrimSpace(req.Endpoint), "", err
		}
		if relayEndpointRequiresMultipart(req.Endpoint) {
			body, contentType, err := rewriteRelayMultipartRequestBody(
				req.Endpoint,
				req.Body,
				req.Headers.Get("Content-Type"),
				selection.route.UpstreamModel,
			)
			return body, normalizeRelayProviderKind(req.ProviderKind), strings.TrimSpace(req.Endpoint), contentType, err
		}
		body, err := rewriteRelayRequestBody(req.Body, selection.route.UpstreamModel, req.ProviderKind, stream, includeOpenAIUsage)
		return body, normalizeRelayProviderKind(req.ProviderKind), strings.TrimSpace(req.Endpoint), "", err
	}
	body, err := relayTransformRequestBody(req.Body, selection.route.UpstreamModel, plan)
	if err != nil {
		return nil, "", "", "", err
	}
	return body, plan.upstreamProvider, plan.upstreamEndpoint, "", nil
}

func relayGeminiRequestBody(req LLMRelayHTTPRequest, upstreamModel string) ([]byte, error) {
	var payload any
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		return nil, fmt.Errorf("%w: invalid relay request JSON", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(req.Endpoint) == "interactions" {
		return rewriteRelayRequestBody(req.Body, upstreamModel, "gemini", false, false)
	}
	return req.Body, nil
}

func relayTransformRequestBody(body []byte, upstreamModel string, plan relayTransformPlan) ([]byte, error) {
	switch plan.requestProvider + "->" + plan.upstreamProvider {
	case "openai->anthropic":
		return relayTransformOpenAIChatRequestToAnthropic(body, upstreamModel)
	case "anthropic->openai":
		return relayTransformAnthropicMessagesRequestToOpenAI(body, upstreamModel)
	default:
		return nil, fmt.Errorf("%w: unsupported relay format conversion", apperrors.ErrInvalidArgument)
	}
}

func relayTransformResponseBody(body []byte, plan relayTransformPlan) ([]byte, error) {
	if !plan.enabled {
		return body, nil
	}
	switch plan.requestProvider + "->" + plan.upstreamProvider {
	case "openai->anthropic":
		return relayTransformAnthropicMessageResponseToOpenAI(body)
	case "anthropic->openai":
		return relayTransformOpenAIChatResponseToAnthropic(body)
	default:
		return nil, fmt.Errorf("%w: unsupported relay format conversion", apperrors.ErrInvalidArgument)
	}
}

func relayTransformOpenAIChatRequestToAnthropic(body []byte, upstreamModel string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: invalid relay request JSON", apperrors.ErrInvalidArgument)
	}
	if relayJSONContainsCacheUnsafeValue(payload) {
		return nil, fmt.Errorf("%w: relay format conversion only supports text-only requests", apperrors.ErrInvalidArgument)
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return nil, fmt.Errorf("%w: openai messages are required for relay format conversion", apperrors.ErrInvalidArgument)
	}
	out := map[string]any{
		"model":      upstreamModel,
		"messages":   []any{},
		"max_tokens": relayOpenAIMaxTokens(payload),
	}
	if system := relayOpenAISystemPrompt(messages); system != "" {
		out["system"] = system
	}
	converted := make([]any, 0, len(messages))
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: relay format conversion only supports text messages", apperrors.ErrInvalidArgument)
		}
		role := strings.ToLower(strings.TrimSpace(fmt.Sprint(message["role"])))
		if role == "system" {
			continue
		}
		if role != "user" && role != "assistant" {
			return nil, fmt.Errorf("%w: relay format conversion only supports user and assistant messages", apperrors.ErrInvalidArgument)
		}
		text, ok := relayTextFromContent(message["content"])
		if !ok {
			return nil, fmt.Errorf("%w: relay format conversion only supports text message content", apperrors.ErrInvalidArgument)
		}
		converted = append(converted, map[string]any{"role": role, "content": text})
	}
	out["messages"] = converted
	if temperature, ok := payload["temperature"]; ok {
		out["temperature"] = temperature
	}
	if topP, ok := payload["top_p"]; ok {
		out["top_p"] = topP
	}
	return json.Marshal(out)
}

func relayTransformAnthropicMessagesRequestToOpenAI(body []byte, upstreamModel string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: invalid relay request JSON", apperrors.ErrInvalidArgument)
	}
	if relayJSONContainsCacheUnsafeValue(payload) {
		return nil, fmt.Errorf("%w: relay format conversion only supports text-only requests", apperrors.ErrInvalidArgument)
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return nil, fmt.Errorf("%w: anthropic messages are required for relay format conversion", apperrors.ErrInvalidArgument)
	}
	converted := make([]any, 0, len(messages)+1)
	if system, ok := relayTextFromContent(payload["system"]); ok && system != "" {
		converted = append(converted, map[string]any{"role": "system", "content": system})
	}
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: relay format conversion only supports text messages", apperrors.ErrInvalidArgument)
		}
		role := strings.ToLower(strings.TrimSpace(fmt.Sprint(message["role"])))
		if role != "user" && role != "assistant" {
			return nil, fmt.Errorf("%w: relay format conversion only supports user and assistant messages", apperrors.ErrInvalidArgument)
		}
		text, ok := relayTextFromContent(message["content"])
		if !ok {
			return nil, fmt.Errorf("%w: relay format conversion only supports text message content", apperrors.ErrInvalidArgument)
		}
		converted = append(converted, map[string]any{"role": role, "content": text})
	}
	out := map[string]any{
		"model":    upstreamModel,
		"messages": converted,
	}
	if maxTokens := jsonNumberInt(payload["max_tokens"]); maxTokens > 0 {
		out["max_tokens"] = maxTokens
	}
	if temperature, ok := payload["temperature"]; ok {
		out["temperature"] = temperature
	}
	if topP, ok := payload["top_p"]; ok {
		out["top_p"] = topP
	}
	return json.Marshal(out)
}

func relayTransformAnthropicMessageResponseToOpenAI(body []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode relay transform response: %w", err)
	}
	contentText, ok := relayTextFromContent(payload["content"])
	if !ok {
		return nil, fmt.Errorf("%w: relay format conversion only supports text response content", apperrors.ErrInvalidArgument)
	}
	out := map[string]any{
		"id":      firstNonEmpty(strings.TrimSpace(fmt.Sprint(payload["id"])), "chatcmpl-"+uuid.NewString()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   strings.TrimSpace(fmt.Sprint(payload["model"])),
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": contentText,
				},
				"finish_reason": relayOpenAIFinishReasonFromAnthropic(payload["stop_reason"]),
			},
		},
	}
	if usage, ok := payload["usage"].(map[string]any); ok {
		promptTokens := jsonNumberInt(usage["input_tokens"])
		completionTokens := jsonNumberInt(usage["output_tokens"])
		out["usage"] = map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		}
	}
	return json.Marshal(out)
}

func relayTransformOpenAIChatResponseToAnthropic(body []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode relay transform response: %w", err)
	}
	choices, _ := payload["choices"].([]any)
	contentText := ""
	stopReason := "end_turn"
	if len(choices) > 0 {
		choice, _ := choices[0].(map[string]any)
		if reason := strings.TrimSpace(fmt.Sprint(choice["finish_reason"])); reason != "" && reason != "<nil>" {
			stopReason = relayAnthropicStopReasonFromOpenAI(reason)
		}
		if message, ok := choice["message"].(map[string]any); ok {
			text, ok := relayTextFromContent(message["content"])
			if !ok {
				return nil, fmt.Errorf("%w: relay format conversion only supports text response content", apperrors.ErrInvalidArgument)
			}
			contentText = text
		}
	}
	out := map[string]any{
		"id":            firstNonEmpty(strings.TrimSpace(fmt.Sprint(payload["id"])), "msg_"+uuid.NewString()),
		"type":          "message",
		"role":          "assistant",
		"model":         strings.TrimSpace(fmt.Sprint(payload["model"])),
		"content":       []any{map[string]any{"type": "text", "text": contentText}},
		"stop_reason":   stopReason,
		"stop_sequence": nil,
	}
	if usage, ok := payload["usage"].(map[string]any); ok {
		out["usage"] = map[string]any{
			"input_tokens":  jsonNumberInt(usage["prompt_tokens"]),
			"output_tokens": jsonNumberInt(usage["completion_tokens"]),
		}
	}
	return json.Marshal(out)
}

func relayTextFromContent(value any) (string, bool) {
	switch typed := value.(type) {
	case nil:
		return "", true
	case string:
		return typed, true
	case []any:
		var builder strings.Builder
		for _, item := range typed {
			part, ok := relayTextFromContentBlock(item)
			if !ok {
				return "", false
			}
			builder.WriteString(part)
		}
		return builder.String(), true
	default:
		return "", false
	}
}

func relayTextFromContentBlock(value any) (string, bool) {
	block, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	blockType := strings.ToLower(strings.TrimSpace(fmt.Sprint(block["type"])))
	switch blockType {
	case "", "text", "input_text", "output_text":
		text, ok := block["text"].(string)
		return text, ok
	default:
		return "", false
	}
}

func relayOpenAISystemPrompt(messages []any) string {
	var parts []string
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(message["role"])), "system") {
			if text, ok := relayTextFromContent(message["content"]); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func relayOpenAIMaxTokens(payload map[string]any) int {
	value := firstNonZeroInt(jsonNumberInt(payload["max_tokens"]), jsonNumberInt(payload["max_completion_tokens"]))
	if value <= 0 {
		return 1024
	}
	return value
}

func relayOpenAIFinishReasonFromAnthropic(value any) string {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}

func relayAnthropicStopReasonFromOpenAI(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	default:
		return "end_turn"
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
