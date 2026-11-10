package aigatewayrelay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/application/aigateway/relayport"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func rewriteRelayMultipartRequestBody(endpoint string, body []byte, contentType, upstreamModel string) ([]byte, string, error) {
	boundary, err := relayport.MultipartBoundary(contentType)
	if err != nil {
		return nil, "", err
	}
	policy := relayport.MultipartPolicyForEndpoint(endpoint)
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
	for _, field := range policy.RequiredFileFields {
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
	if stream && includeOpenAIUsage && relayport.UsesOpenAIProtocol(providerKind) {
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

func relayGeminiRequestBody(req relayport.Request, upstreamModel string) ([]byte, error) {
	var payload any
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		return nil, fmt.Errorf("%w: invalid relay request JSON", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(req.Endpoint) == "interactions" {
		return rewriteRelayRequestBody(req.Body, upstreamModel, "gemini", false, false)
	}
	return req.Body, nil
}

func relayTransformRequestBody(body []byte, upstreamModel string, sourceProvider, targetProvider string) ([]byte, error) {
	switch sourceProvider + "->" + targetProvider {
	case "openai->anthropic":
		return relayTransformOpenAIChatRequestToAnthropic(body, upstreamModel)
	case "anthropic->openai":
		return relayTransformAnthropicMessagesRequestToOpenAI(body, upstreamModel)
	default:
		return nil, fmt.Errorf("%w: unsupported relay format conversion", apperrors.ErrInvalidArgument)
	}
}

func relayTransformResponseBody(body []byte, sourceProvider, targetProvider string) ([]byte, error) {
	if sourceProvider == targetProvider {
		return body, nil
	}
	switch sourceProvider + "->" + targetProvider {
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
	if relayport.ContainsNonTextPayload(payload) {
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
		text, ok := relayport.TextFromContent(message["content"])
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
	if relayport.ContainsNonTextPayload(payload) {
		return nil, fmt.Errorf("%w: relay format conversion only supports text-only requests", apperrors.ErrInvalidArgument)
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return nil, fmt.Errorf("%w: anthropic messages are required for relay format conversion", apperrors.ErrInvalidArgument)
	}
	converted := make([]any, 0, len(messages)+1)
	if system, ok := relayport.TextFromContent(payload["system"]); ok && system != "" {
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
		text, ok := relayport.TextFromContent(message["content"])
		if !ok {
			return nil, fmt.Errorf("%w: relay format conversion only supports text message content", apperrors.ErrInvalidArgument)
		}
		converted = append(converted, map[string]any{"role": role, "content": text})
	}
	out := map[string]any{
		"model":    upstreamModel,
		"messages": converted,
	}
	if maxTokens := jsonInt(payload["max_tokens"]); maxTokens > 0 {
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
	contentText, ok := relayport.TextFromContent(payload["content"])
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
		promptTokens := jsonInt(usage["input_tokens"])
		completionTokens := jsonInt(usage["output_tokens"])
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
			text, ok := relayport.TextFromContent(message["content"])
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
			"input_tokens":  jsonInt(usage["prompt_tokens"]),
			"output_tokens": jsonInt(usage["completion_tokens"]),
		}
	}
	return json.Marshal(out)
}

func relayOpenAISystemPrompt(messages []any) string {
	var parts []string
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(message["role"])), "system") {
			if text, ok := relayport.TextFromContent(message["content"]); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func relayOpenAIMaxTokens(payload map[string]any) int {
	value := jsonInt(payload["max_tokens"])
	if value == 0 {
		value = jsonInt(payload["max_completion_tokens"])
	}
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

func jsonInt(value any) int {
	// Provider payloads are decoded by encoding/json into float64.
	number, _ := value.(float64)
	return int(number)
}
