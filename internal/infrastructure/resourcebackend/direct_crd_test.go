package resourcebackend

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestParseCRDDefinitionPrefersServedStorageVersion(t *testing.T) {
	t.Parallel()

	definition, err := parseCRDDefinition(unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "widgets.example.io"},
		"spec": map[string]any{
			"group": "example.io",
			"scope": "Namespaced",
			"names": map[string]any{"kind": "Widget", "plural": "widgets"},
			"versions": []any{
				map[string]any{"name": "v1beta1", "served": true, "storage": false},
				map[string]any{"name": "v1", "served": true, "storage": true},
			},
		},
	}})
	if err != nil {
		t.Fatalf("parseCRDDefinition returned error: %v", err)
	}
	if definition.Kind != "Widget" || definition.Group != "example.io" || definition.Version != "v1" || definition.Resource != "widgets" || !definition.Namespaced {
		t.Fatalf("definition = %#v", definition)
	}
}
