package protectedpayload

import (
	"encoding/json"
	"slices"
	"strings"
)

// ContainsNamedCall checks structured call names, never arbitrary message text.
func ContainsNamedCall(raw json.RawMessage, names ...string) bool {
	var value any
	if Decode(raw, &value) != nil {
		return false
	}
	return containsCall(value, names)
}

func containsCall(value any, names []string) bool {
	switch item := value.(type) {
	case map[string]any:
		for key, nested := range item {
			if name, ok := nested.(string); ok && (strings.EqualFold(key, "name") || strings.EqualFold(key, "toolName")) && slices.Contains(names, name) {
				return true
			}
			if containsCall(nested, names) {
				return true
			}
		}
	case []any:
		for _, nested := range item {
			if containsCall(nested, names) {
				return true
			}
		}
	}
	return false
}
