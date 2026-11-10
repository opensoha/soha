package relayport

import (
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func RequiresMultipart(endpoint string) bool {
	switch strings.TrimSpace(endpoint) {
	case "audio/transcriptions", "audio/translations", "images/edits", "images/variations":
		return true
	default:
		return false
	}
}

func MultipartBoundary(contentType string) (string, error) {
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

func SensitiveHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "x-api-key", "x-goog-api-key", "google-api-key", "gemini-api-key", "cohere-api-key", "api-key", "openai-api-key", "anthropic-api-key", "cookie", "set-cookie":
		return true
	default:
		return false
	}
}

func NormalizeProvider(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "azure_openai" {
		value = "azure-openai"
	}
	switch value {
	case "openai", "anthropic", "openai-compatible", "deepseek", "qwen", "openrouter", "azure-openai", "gemini", "cohere":
		return value
	default:
		return ""
	}
}

func UsesOpenAIProtocol(providerKind string) bool {
	switch NormalizeProvider(providerKind) {
	case "openai", "openai-compatible", "deepseek", "qwen", "openrouter", "azure-openai":
		return true
	default:
		return false
	}
}

func ContainsNonTextPayload(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "tools", "tool_choice", "tool_calls", "function_call", "functions", "file", "files", "file_id", "file_data", "filedata", "inlinedata", "inline_data", "cachedcontent", "cached_content", "image", "images", "image_url", "input_image", "audio", "input_audio":
				return true
			case "type":
				text := strings.ToLower(strings.TrimSpace(fmt.Sprint(item)))
				if strings.Contains(text, "image") || strings.Contains(text, "file") || strings.Contains(text, "audio") || strings.Contains(text, "tool") {
					return true
				}
			}
			if ContainsNonTextPayload(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if ContainsNonTextPayload(item) {
				return true
			}
		}
	}
	return false
}

func CopyResponseHeaders(dst, src http.Header, defaultContentType string) {
	for _, name := range []string{
		"Content-Type",
		"Cache-Control",
		"X-Request-Id",
		"Openai-Request-Id",
		"Request-Id",
		"Anthropic-Organization-Id",
	} {
		for _, value := range src.Values(name) {
			if strings.TrimSpace(value) != "" {
				dst.Add(name, value)
			}
		}
	}
	if dst.Get("Content-Type") == "" {
		dst.Set("Content-Type", defaultContentType)
	}
}

func ResponseContentType(endpoint string) string {
	if strings.TrimSpace(endpoint) == "audio/speech" {
		return "application/octet-stream"
	}
	return "application/json"
}

type MultipartPolicy struct {
	RequiredFileFields []string
	RequiredTextFields []string
	TrackedTextFields  map[string]bool
}

func MultipartPolicyForEndpoint(endpoint string) MultipartPolicy {
	policy := MultipartPolicy{
		TrackedTextFields: map[string]bool{
			"model":         true,
			"stream":        true,
			"stream_format": true,
			"prompt":        true,
		},
	}
	switch strings.TrimSpace(endpoint) {
	case "images/edits":
		policy.RequiredFileFields = []string{"image"}
		policy.RequiredTextFields = []string{"prompt"}
	case "images/variations":
		policy.RequiredFileFields = []string{"image"}
	default:
		policy.RequiredFileFields = []string{"file"}
	}
	return policy
}

func TextFromContent(value any) (string, bool) {
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
