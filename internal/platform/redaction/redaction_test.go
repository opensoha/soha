package redaction

import (
	"strings"
	"testing"
)

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
	nested := redacted["nested"].(map[string]any)
	if strings.Contains(nested["note"].(string), "raw-bearer") {
		t.Fatalf("nested text was not redacted: %#v", nested)
	}
	events := redacted["events"].([]any)
	if events[0].(map[string]any)["password"] != Redacted {
		t.Fatalf("nested password was not redacted: %#v", events[0])
	}
	if strings.Contains(events[1].(string), "raw-token") {
		t.Fatalf("slice text was not redacted: %#v", events[1])
	}
}

func TestTextRedactsCommonSecretPatterns(t *testing.T) {
	value := "token=raw-token password: raw-password Authorization: Bearer raw-bearer kubeconfig=raw-kubeconfig"
	redacted := Text(value)
	for _, leaked := range []string{"raw-token", "raw-password", "raw-bearer", "raw-kubeconfig"} {
		if strings.Contains(redacted, leaked) {
			t.Fatalf("text leaked %q in %q", leaked, redacted)
		}
	}
	if strings.Count(redacted, Redacted) != 4 {
		t.Fatalf("redaction count mismatch in %q", redacted)
	}
}
