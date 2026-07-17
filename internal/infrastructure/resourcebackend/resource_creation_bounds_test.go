package resourcebackend

import (
	"context"
	"strings"
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestResolveCreateManifestRejectsListAndOversizedDocument(t *testing.T) {
	t.Parallel()
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)

	tests := []struct {
		name   string
		object map[string]any
	}{
		{
			name: "kind list",
			object: map[string]any{
				"apiVersion": "v1", "kind": "List", "items": []any{},
			},
		},
		{
			name: "single document size",
			object: map[string]any{
				"apiVersion": "v1", "kind": "ConfigMap",
				"metadata": map[string]any{"name": "oversized"},
				"data":     map[string]any{"payload": strings.Repeat("x", domainresource.ResourceCreateMaxDocumentBytes)},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := resolveCreateManifest(mapper, 0, test.object); err == nil {
				t.Fatal("resolveCreateManifest() error = nil")
			}
		})
	}
}

func TestAgentResourceCreatorRejectsInvalidEmptyAndExcessDocuments(t *testing.T) {
	t.Parallel()
	creator := &agentResourceCreator{}
	tests := []struct {
		name    string
		content string
		limit   int
	}{
		{name: "invalid yaml", content: "metadata: [", limit: domainresource.ResourceCreateMaxDocuments},
		{name: "empty document", content: "---\n", limit: domainresource.ResourceCreateMaxDocuments},
		{name: "document count", content: repeatedConfigMaps(domainresource.ResourceCreateMaxDocuments + 1), limit: domainresource.ResourceCreateMaxDocuments},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := creator.ResolveCreateManifests(context.Background(), test.content, test.limit); err == nil {
				t.Fatal("ResolveCreateManifests() error = nil")
			}
		})
	}
}

func repeatedConfigMaps(count int) string {
	var content strings.Builder
	for index := 0; index < count; index++ {
		content.WriteString("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: item-")
		content.WriteString(strings.Repeat("x", index+1))
		content.WriteString("\n---\n")
	}
	return content.String()
}
