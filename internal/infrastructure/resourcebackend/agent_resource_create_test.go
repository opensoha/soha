package resourcebackend

import (
	"context"
	"strings"
	"testing"

	"errors"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestAgentResourceManifestDecoderRejectsBoundedInvalidInputs(t *testing.T) {
	t.Parallel()
	creator := &agentResourceCreator{}
	tests := []struct {
		name    string
		content string
		limit   int
	}{
		{name: "invalid yaml", content: "metadata: [", limit: 20},
		{name: "empty documents", content: "---\n---\n", limit: 20},
		{name: "kind list", content: "apiVersion: v1\nkind: List\nmetadata:\n  name: resources\n", limit: 20},
		{name: "document count", content: repeatedConfigMapDocuments(3), limit: 2},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, err := creator.ResolveCreateManifests(context.Background(), test.content, test.limit)
			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("ResolveCreateManifests() error = %v, want invalid argument", err)
			}
		})
	}
}

func repeatedConfigMapDocuments(count int) string {
	var builder strings.Builder
	for index := 0; index < count; index++ {
		if index > 0 {
			builder.WriteString("---\n")
		}
		builder.WriteString("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: item-")
		builder.WriteString(string(rune('a' + index)))
		builder.WriteString("\n")
	}
	return builder.String()
}
