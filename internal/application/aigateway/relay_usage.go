package aigateway

import (
	"encoding/json"
	"net/http"
	"strings"
)

type relayUsage struct {
	promptTokens      int
	completionTokens  int
	totalTokens       int
	reasoningTokens   int
	cachedReadTokens  int
	cachedWriteTokens int
}

type relayUsageAnalyzer struct{}

func (relayUsageAnalyzer) analyze(req LLMRelayHTTPRequest, output []byte, statusCode int, status string) (relayUsage, bool) {
	usage := relayUsageFromBody(output)
	estimated := false
	if !relayUsageHasTokens(usage) && relayShouldEstimateUsage(statusCode, status) {
		usage = estimateRelayUsage(req, output)
		estimated = relayUsageHasTokens(usage)
	}
	if usage.totalTokens == 0 && (usage.promptTokens > 0 || usage.completionTokens > 0) {
		usage.totalTokens = usage.promptTokens + usage.completionTokens
	}
	return usage, estimated
}

func relayUsageHasTokens(usage relayUsage) bool {
	return usage.promptTokens > 0 || usage.completionTokens > 0 || usage.totalTokens > 0 || usage.reasoningTokens > 0
}

func relayShouldEstimateUsage(statusCode int, status string) bool {
	return statusCode > 0 && statusCode < http.StatusBadRequest && strings.EqualFold(strings.TrimSpace(status), "success")
}

func relayUsageFromBody(body []byte) relayUsage {
	var payload map[string]any
	if len(body) == 0 {
		return relayUsage{}
	}
	if json.Unmarshal(body, &payload) != nil {
		return relayUsageFromSSE(body)
	}
	return relayUsageFromPayload(payload)
}

func relayUsageFromSSE(body []byte) relayUsage {
	var out relayUsage
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(data), &payload) != nil {
			continue
		}
		mergeRelayUsage(&out, relayUsageFromPayload(payload))
	}
	if sum := out.promptTokens + out.completionTokens; sum > out.totalTokens {
		out.totalTokens = sum
	}
	return out
}

func relayUsageFromPayload(payload map[string]any) relayUsage {
	usageRaw, ok := payload["usage"].(map[string]any)
	if !ok {
		usageRaw, ok = payload["total_usage"].(map[string]any)
	}
	if !ok {
		message, hasMessage := payload["message"].(map[string]any)
		if hasMessage {
			usageRaw, ok = message["usage"].(map[string]any)
		}
	}
	if !ok {
		if geminiUsage, hasGeminiUsage := payload["usageMetadata"].(map[string]any); hasGeminiUsage {
			return relayUsageFromGeminiUsageMetadata(geminiUsage)
		}
		if geminiUsage, hasGeminiUsage := payload["usage_metadata"].(map[string]any); hasGeminiUsage {
			return relayUsageFromGeminiUsageMetadata(geminiUsage)
		}
	}
	if !ok {
		return relayUsage{}
	}
	return relayUsageFromUsageMap(usageRaw)
}

func relayUsageFromUsageMap(usageRaw map[string]any) relayUsage {
	out := relayUsage{
		promptTokens:     jsonNumberInt(usageRaw["prompt_tokens"], usageRaw["input_tokens"], usageRaw["total_input_tokens"]),
		completionTokens: jsonNumberInt(usageRaw["completion_tokens"], usageRaw["output_tokens"], usageRaw["total_output_tokens"]),
		totalTokens:      jsonNumberInt(usageRaw["total_tokens"]),
		reasoningTokens:  jsonNumberInt(usageRaw["total_thought_tokens"], usageRaw["thought_tokens"]),
		cachedReadTokens: jsonNumberInt(usageRaw["total_cached_tokens"], usageRaw["cached_tokens"]),
	}
	if out.totalTokens == 0 {
		out.totalTokens = out.promptTokens + out.completionTokens
	}
	if details, ok := usageRaw["completion_tokens_details"].(map[string]any); ok {
		out.reasoningTokens = jsonNumberInt(details["reasoning_tokens"])
	}
	if details, ok := usageRaw["prompt_tokens_details"].(map[string]any); ok {
		out.cachedReadTokens = jsonNumberInt(details["cached_tokens"])
	}
	out.cachedReadTokens = firstNonZeroInt(out.cachedReadTokens, jsonNumberInt(usageRaw["cache_read_input_tokens"]))
	out.cachedWriteTokens = firstNonZeroInt(out.cachedWriteTokens, jsonNumberInt(usageRaw["cache_creation_input_tokens"]))
	return out
}

func relayUsageFromGeminiUsageMetadata(usageRaw map[string]any) relayUsage {
	out := relayUsage{
		promptTokens:     jsonNumberInt(usageRaw["promptTokenCount"], usageRaw["prompt_token_count"]),
		completionTokens: jsonNumberInt(usageRaw["candidatesTokenCount"], usageRaw["candidates_token_count"]),
		totalTokens:      jsonNumberInt(usageRaw["totalTokenCount"], usageRaw["total_token_count"]),
		reasoningTokens:  jsonNumberInt(usageRaw["thoughtsTokenCount"], usageRaw["thoughts_token_count"]),
		cachedReadTokens: jsonNumberInt(usageRaw["cachedContentTokenCount"], usageRaw["cached_content_token_count"]),
	}
	if out.totalTokens == 0 {
		out.totalTokens = out.promptTokens + out.completionTokens
	}
	return out
}

func mergeRelayUsage(out *relayUsage, next relayUsage) {
	out.promptTokens = max(out.promptTokens, next.promptTokens)
	out.completionTokens = max(out.completionTokens, next.completionTokens)
	out.totalTokens = max(out.totalTokens, next.totalTokens)
	out.reasoningTokens = max(out.reasoningTokens, next.reasoningTokens)
	out.cachedReadTokens = max(out.cachedReadTokens, next.cachedReadTokens)
	out.cachedWriteTokens = max(out.cachedWriteTokens, next.cachedWriteTokens)
}

func estimateRelayUsage(req LLMRelayHTTPRequest, output []byte) relayUsage {
	out := relayUsage{
		promptTokens:     estimateRelayPromptTokens(req),
		completionTokens: estimateRelayCompletionTokens(req.Endpoint, output),
	}
	out.totalTokens = out.promptTokens + out.completionTokens
	return out
}

func estimateRelayPromptTokens(req LLMRelayHTTPRequest) int {
	endpoint := strings.TrimSpace(req.Endpoint)
	body := req.Body
	if len(body) == 0 {
		return 0
	}
	if relayEndpointRequiresMultipart(endpoint) {
		return estimateRelayMultipartPromptTokens(req)
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return estimateRelayTextTokens(string(body))
	}
	switch endpoint {
	case "chat/completions":
		return estimateRelaySelectedJSONTokens(payload, "messages", "tools", "tool_choice", "response_format")
	case "responses":
		return estimateRelaySelectedJSONTokens(payload, "instructions", "input", "tools")
	case "messages":
		return estimateRelaySelectedJSONTokens(payload, "system", "messages", "tools")
	case "embeddings":
		return estimateRelaySelectedJSONTokens(payload, "input")
	case "generateContent", "streamGenerateContent":
		return estimateRelayGeminiPromptTokens(payload)
	case "audio/speech":
		return estimateRelaySelectedJSONTokens(payload, "input")
	case "images/generations":
		return estimateRelaySelectedJSONTokens(payload, "prompt")
	case "rerank":
		return estimateRelaySelectedJSONTokens(payload, "query", "documents", "rank_fields")
	default:
		return estimateRelayJSONTokens(payload)
	}
}

func estimateRelayMultipartPromptTokens(req LLMRelayHTTPRequest) int {
	fields, err := relayMultipartRequestFields(req.Endpoint, req.Body, req.Headers.Get("Content-Type"))
	if err != nil {
		return 0
	}
	switch strings.TrimSpace(req.Endpoint) {
	case "audio/transcriptions", "audio/translations", "images/edits":
		return estimateRelayTextTokens(fields["prompt"])
	default:
		return 0
	}
}

func estimateRelayCompletionTokens(endpoint string, body []byte) int {
	if len(body) == 0 {
		return 0
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		if relayEndpointRequiresMultipart(endpoint) {
			return estimateRelayTextTokens(string(body))
		}
		return estimateRelayCompletionTokensFromSSE(endpoint, body)
	}
	return estimateRelayCompletionTokensFromPayload(endpoint, payload)
}

func estimateRelayCompletionTokensFromSSE(endpoint string, body []byte) int {
	total := 0
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(data), &payload) != nil {
			continue
		}
		if _, hasUsage := payload["usage"]; hasUsage {
			continue
		}
		total += estimateRelayCompletionTokensFromPayload(endpoint, payload)
	}
	return total
}

func estimateRelayCompletionTokensFromPayload(endpoint string, payload map[string]any) int {
	switch strings.TrimSpace(endpoint) {
	case "chat/completions":
		return estimateRelaySelectedJSONTokens(payload, "choices")
	case "responses":
		return estimateRelaySelectedJSONTokens(payload, "output_text", "output", "delta")
	case "messages":
		return estimateRelaySelectedJSONTokens(payload, "content", "delta")
	case "generateContent", "streamGenerateContent":
		return estimateRelayGeminiCompletionTokens(payload)
	case "audio/transcriptions", "audio/translations":
		return estimateRelaySelectedJSONTokens(payload, "text")
	case "embeddings", "images/generations", "images/edits", "images/variations", "audio/speech":
		return 0
	default:
		return estimateRelaySelectedJSONTokens(payload, "choices", "message", "content", "output", "output_text", "delta")
	}
}

func estimateRelayGeminiPromptTokens(payload map[string]any) int {
	total := estimateRelayGeminiTextPartsTokens(payload["systemInstruction"])
	total += estimateRelayGeminiTextPartsTokens(payload["system_instruction"])
	total += estimateRelayGeminiTextPartsTokens(payload["contents"])
	return total
}

func estimateRelayGeminiCompletionTokens(payload map[string]any) int {
	return estimateRelayGeminiTextPartsTokens(payload["candidates"])
}

func estimateRelayGeminiTextPartsTokens(value any) int {
	switch typed := value.(type) {
	case string:
		return estimateRelayTextTokens(typed)
	case []any:
		total := 0
		for _, item := range typed {
			total += estimateRelayGeminiTextPartsTokens(item)
		}
		return total
	case map[string]any:
		total := 0
		if text, ok := typed["text"].(string); ok {
			total += estimateRelayTextTokens(text)
		}
		for _, key := range []string{"content", "parts"} {
			if item, ok := typed[key]; ok {
				total += estimateRelayGeminiTextPartsTokens(item)
			}
		}
		return total
	default:
		return 0
	}
}

func estimateRelaySelectedJSONTokens(payload map[string]any, keys ...string) int {
	total := 0
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			total += estimateRelayJSONTokens(value)
		}
	}
	return total
}

func estimateRelayJSONTokens(value any) int {
	switch typed := value.(type) {
	case string:
		return estimateRelayTextTokens(typed)
	case []any:
		total := 0
		for _, item := range typed {
			total += estimateRelayJSONTokens(item)
		}
		return total
	case map[string]any:
		total := 0
		for key, item := range typed {
			if relayEstimateSkipKey(key) {
				continue
			}
			total += estimateRelayJSONTokens(item)
		}
		return total
	default:
		return 0
	}
}

func estimateRelayTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	asciiNonSpace := 0
	cjk := 0
	for _, r := range text {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if relayEstimateCJKRune(r) {
			cjk++
			continue
		}
		asciiNonSpace++
	}
	total := cjk + ceilRelayDiv(asciiNonSpace, 4)
	if total <= 0 {
		return 0
	}
	return total
}

func ceilRelayDiv(value, divisor int) int {
	if value <= 0 || divisor <= 0 {
		return 0
	}
	return (value + divisor - 1) / divisor
}

func firstNonZeroInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func jsonNumberInt(values ...any) int {
	for _, value := range values {
		switch typed := value.(type) {
		case float64:
			return int(typed)
		case int:
			return typed
		case json.Number:
			n, _ := typed.Int64()
			return int(n)
		}
	}
	return 0
}

func relayEstimateSkipKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "id", "object", "model", "role", "type", "name", "index", "finish_reason", "usage", "stream", "stream_options", "metadata",
		"file", "files", "file_id", "file_data", "filedata", "inlinedata", "inline_data", "cachedcontent", "cached_content",
		"image", "images", "image_url", "input_image", "audio", "input_audio":
		return true
	default:
		return false
	}
}

func relayEstimateCJKRune(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x3040 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7AF)
}
