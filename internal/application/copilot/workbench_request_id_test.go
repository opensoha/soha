package copilot

import (
	"strings"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/copilot"
)

func TestNativeRequestIDMatchesSchemaCharacterLimit(t *testing.T) {
	for _, item := range []struct {
		name, id string
		valid    bool
	}{
		{"legacy-empty", "", true}, {"ascii-boundary", strings.Repeat("a", 128), true},
		{"ascii-over", strings.Repeat("a", 129), false}, {"unicode-boundary", strings.Repeat("验", 128), true},
		{"unicode-over", strings.Repeat("验", 129), false}, {"four-byte-boundary", strings.Repeat("🙂", 128), true},
		{"invalid-utf8", string([]byte{0xff}), false},
	} {
		t.Run(item.name, func(t *testing.T) {
			err := validateNativeMessage(domain.WorkbenchSendMessageInput{Content: "Bounded turn", RequestID: item.id})
			if (err == nil) != item.valid {
				t.Fatalf("request ID accepted=%t want=%t", err == nil, item.valid)
			}
		})
	}
}
