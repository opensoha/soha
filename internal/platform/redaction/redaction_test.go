package redaction

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func mustRedactedValue[T any](t *testing.T, value any) T {
	t.Helper()
	result, ok := value.(T)
	if !ok {
		t.Fatalf("value has type %T, want %T", value, *new(T))
	}
	return result
}

func TestMapRedactsSensitiveKeysAndNestedText(t *testing.T) {
	values := map[string]any{
		"clusterId": "cluster-a",
		"api_key":   "raw-api-key",
		"nested": map[string]any{
			"note": "authorization: Bearer raw-bearer",
		},
		"events": []any{
			map[string]any{"password": "raw-password"},
			"token=raw-token",
		},
	}

	redacted := Map(values)
	if redacted["clusterId"] != "cluster-a" {
		t.Fatalf("non-sensitive value changed: %#v", redacted)
	}
	if redacted["api_key"] != Redacted {
		t.Fatalf("api key was not redacted: %#v", redacted)
	}
	nested := mustRedactedValue[map[string]any](t, redacted["nested"])
	if strings.Contains(mustRedactedValue[string](t, nested["note"]), "raw-bearer") {
		t.Fatalf("nested text was not redacted: %#v", nested)
	}
	events := mustRedactedValue[[]any](t, redacted["events"])
	if mustRedactedValue[map[string]any](t, events[0])["password"] != Redacted {
		t.Fatalf("nested password was not redacted: %#v", events[0])
	}
	if strings.Contains(mustRedactedValue[string](t, events[1]), "raw-token") {
		t.Fatalf("slice text was not redacted: %#v", events[1])
	}
}

func TestTextRedactsCommonSecretPatterns(t *testing.T) {
	value := "token=raw-token password: raw-password Authorization: Bearer raw-bearer kubeconfig=raw-kubeconfig https://idp.example/callback?code=raw-code"
	redacted := Text(value)
	for _, leaked := range []string{"raw-token", "raw-password", "raw-bearer", "raw-kubeconfig", "raw-code"} {
		if strings.Contains(redacted, leaked) {
			t.Fatalf("text leaked %q in %q", leaked, redacted)
		}
	}
	if strings.Count(redacted, Redacted) != 5 {
		t.Fatalf("redaction count mismatch in %q", redacted)
	}
}

func TestLogTextSanitizesAndBoundsOutput(t *testing.T) {
	value := LogText("token=raw-token\n\x00abc\u754c", 8)
	if strings.Contains(value, "raw-token") || strings.ContainsAny(value, "\n\x00") {
		t.Fatalf("LogText() did not sanitize output: %q", value)
	}
	if !strings.HasSuffix(value, "...") {
		t.Fatalf("LogText() = %q, want truncation marker", value)
	}
	if !utf8.ValidString(value) {
		t.Fatalf("LogText() returned invalid UTF-8: %q", value)
	}
}
