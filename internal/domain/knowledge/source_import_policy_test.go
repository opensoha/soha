package knowledge

import (
	"errors"
	"strings"
	"testing"
)

func TestSourceImportPolicyValidation(t *testing.T) {
	if policy, err := ParseSourceImportPolicy(nil); err != nil || policy != nil {
		t.Fatalf("omitted policy: %v", err)
	}
	for _, value := range []any{nil, "public", map[string]any{}, map[string]any{"schemaVersion": sourceImportPolicyVersion, "scope": nil}, map[string]any{"schemaVersion": sourceImportPolicyVersion, "scope": map[string]any{"visibility": "public", "users": nil}}, map[string]any{"schemaVersion": sourceImportPolicyVersion, "scope": map[string]any{"visibility": "private", "users": []string{"reader", "reader"}}}, map[string]any{"schemaVersion": sourceImportPolicyVersion, "scope": map[string]any{"visibility": "private", "users": []string{strings.Repeat("a", 129)}}}, map[string]any{"schemaVersion": sourceImportPolicyVersion, "scope": map[string]any{"visibility": "private", "users": []string{"bad id"}}}} {
		if _, err := ParseSourceImportPolicy(map[string]any{"importPolicy": value}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid policy accepted: %v", err)
		}
	}
	for _, visibility := range []string{"private", "scoped", "public"} {
		policy, err := ParseSourceImportPolicy(map[string]any{"importPolicy": map[string]any{"schemaVersion": sourceImportPolicyVersion, "scope": map[string]any{"visibility": visibility}}})
		if err != nil || policy.Scope.Visibility != visibility {
			t.Fatalf("valid policy: %v", err)
		}
	}
}

func TestSourceImportPolicyRejectsCaseVariantFields(t *testing.T) {
	for _, value := range []any{
		map[string]any{"SchemaVersion": sourceImportPolicyVersion, "scope": map[string]any{"visibility": "public"}},
		map[string]any{"schemaVersion": sourceImportPolicyVersion, "Scope": map[string]any{"visibility": "public"}},
		map[string]any{"schemaVersion": sourceImportPolicyVersion, "scope": map[string]any{"Visibility": "public"}},
		map[string]any{"schemaVersion": sourceImportPolicyVersion, "scope": map[string]any{"visibility": "public", "Users": []string{"reader"}}},
	} {
		if _, err := ParseSourceImportPolicy(map[string]any{"importPolicy": value}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case variant bypassed schema: %v", err)
		}
	}
}
