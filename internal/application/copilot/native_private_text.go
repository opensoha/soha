package copilot

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
)

func nativeHasProtectedCapability(run domaincopilot.AgentRun) bool {
	snapshot, _, err := nativeCatalogFrom(run)
	if err != nil {
		return true
	}
	for _, name := range []string{"k8s.pods.exec", "k8s.resources.conditional.update"} {
		if _, ok := snapshot.Tasks[name]; ok {
			return true
		}
	}
	return false
}

func nativePrivateText(cp nativeCheckpoint, text string) string {
	if text == "" {
		return text
	}
	values := map[string]bool{}
	protectedManifest := false
	for _, child := range cp.Tasks {
		for _, step := range child.Input.Plan.Steps {
			protectedManifest = nativePrivateInputValues(step.Call.ToolName, step.Call.Input, values) || protectedManifest
		}
	}
	for _, pending := range cp.Pending {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(pending.Call.Arguments))
		decoder.UseNumber()
		if decoder.Decode(&value) == nil {
			if input, ok := value.(map[string]any); ok {
				protectedManifest = nativePrivateInputValues(pending.ToolName, input, values) || protectedManifest
			}
			protectedManifest = nativePrivateCallValues(value, values, 0) || protectedManifest
		}
	}
	if protectedManifest {
		// YAML can contain arbitrary structured values. Literal replacement of
		// the document cannot protect fragments, escaped values or paraphrases.
		// Keep authoritative task/receipt metadata; do not declassify free text.
		return "[PROTECTED] Review the original task receipt for the result."
	}
	if len(values) == 0 {
		return text
	}
	literals := make([]string, 0, len(values))
	for value := range values {
		if value != "" {
			literals = append(literals, value)
		}
	}
	slices.SortFunc(literals, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(a, b)
	})
	replacements := make([]string, 0, len(literals)*2)
	for _, value := range literals {
		replacements = append(replacements, value, "[PROTECTED]")
	}
	return strings.NewReplacer(replacements...).Replace(text)
}

func nativePrivateCallValues(value any, values map[string]bool, depth int) bool {
	if depth > 32 {
		return false
	}
	protectedManifest := false
	switch item := value.(type) {
	case map[string]any:
		if name, ok := item["toolName"].(string); ok {
			if input, ok := item["input"].(map[string]any); ok {
				protectedManifest = nativePrivateInputValues(name, input, values) || protectedManifest
			}
		}
		for _, nested := range item {
			protectedManifest = nativePrivateCallValues(nested, values, depth+1) || protectedManifest
		}
	case []any:
		for _, nested := range item {
			protectedManifest = nativePrivateCallValues(nested, values, depth+1) || protectedManifest
		}
	}
	return protectedManifest
}

func nativePrivateInputValues(tool string, input map[string]any, values map[string]bool) bool {
	switch tool {
	case "k8s.resources.conditional.update":
		if content, ok := input["content"].(string); ok && content != "" {
			values[content] = true
		}
		return true
	case "k8s.pods.exec":
		execution, _ := input["execution"].(map[string]any)
		switch argv := execution["argv"].(type) {
		case []any:
			for _, arg := range argv {
				if value, ok := arg.(string); ok && value != "" {
					values[value] = true
				}
			}
		case []string:
			for _, value := range argv {
				if value != "" {
					values[value] = true
				}
			}
		}
		if encoded, ok := execution["stdinBase64"].(string); ok && encoded != "" {
			values[encoded] = true
			if raw, err := base64.StdEncoding.Strict().DecodeString(encoded); err == nil && len(raw) > 0 && utf8.Valid(raw) {
				values[string(raw)] = true
			}
		}
	}
	return false
}

func nativePrivateEvent(cp nativeCheckpoint, event domaincopilot.WorkbenchStreamEvent) domaincopilot.WorkbenchStreamEvent {
	event.Content = nativePrivateText(cp, event.Content)
	event.ContentDelta = nativePrivateText(cp, event.ContentDelta)
	event.TextDelta = nativePrivateText(cp, event.TextDelta)
	event.OutputDelta = nativePrivateText(cp, event.OutputDelta)
	event.LogDelta = nativePrivateText(cp, event.LogDelta)
	event.Message = nativePrivateText(cp, event.Message)
	event.Summary = nativePrivateText(cp, event.Summary)
	return event
}
