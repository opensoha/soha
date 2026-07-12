package redaction

import (
	"regexp"
	"strings"
)

const Redacted = "[REDACTED]"

var (
	authorizationValuePattern = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(?:[a-z]+\s+)?([^\s,;]+)`)
	sensitiveValuePattern     = regexp.MustCompile(`(?i)(token|password|passwd|secret|api[_-]?key|credential|kubeconfig|env(?:ironment)?[_-]?var(?:iable)?)(\s*[:=]\s*)([^\s,;]+)`)
	sensitiveQueryPattern     = regexp.MustCompile(`(?i)([?&](?:code|access_token|refresh_token|api[_-]?key)=)([^&\s]+)`)
)

func Map(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		if SensitiveKey(key) {
			out[key] = Redacted
			continue
		}
		out[key] = Value(value)
	}
	return out
}

func Value(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return Map(typed)
	case map[string]string:
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			if SensitiveKey(key) {
				out[key] = Redacted
				continue
			}
			out[key] = Text(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = Value(item)
		}
		return out
	case []string:
		out := make([]string, len(typed))
		for index, item := range typed {
			out[index] = Text(item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(typed))
		for index, item := range typed {
			out[index] = Map(item)
		}
		return out
	case string:
		return Text(typed)
	default:
		return value
	}
}

func Text(value string) string {
	value = authorizationValuePattern.ReplaceAllString(value, "$1"+Redacted)
	value = sensitiveValuePattern.ReplaceAllString(value, "$1$2"+Redacted)
	return sensitiveQueryPattern.ReplaceAllString(value, "$1"+Redacted)
}

func SensitiveKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	for _, needle := range []string{"token", "password", "passwd", "secret", "credential", "apikey", "authorization", "kubeconfig", "envvar", "environmentvariable"} {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	return false
}
