package resource

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestParseCRDResourceDefinitionPrefersServedStorageVersion(t *testing.T) {
	t.Parallel()

	definition, err := parseCRDResourceDefinition(unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name": "widgets.example.io",
		},
		"spec": map[string]any{
			"group": "example.io",
			"scope": "Namespaced",
			"names": map[string]any{
				"kind":   "Widget",
				"plural": "widgets",
			},
			"versions": []any{
				map[string]any{"name": "v1beta1", "served": true, "storage": false},
				map[string]any{"name": "v1", "served": true, "storage": true},
			},
		},
	}})
	if err != nil {
		t.Fatalf("parseCRDResourceDefinition returned error: %v", err)
	}
	if definition.Kind != "Widget" {
		t.Fatalf("Kind = %q, want Widget", definition.Kind)
	}
	if definition.Group != "example.io" {
		t.Fatalf("Group = %q, want example.io", definition.Group)
	}
	if definition.Version != "v1" {
		t.Fatalf("Version = %q, want v1", definition.Version)
	}
	if definition.Resource != "widgets" {
		t.Fatalf("Resource = %q, want widgets", definition.Resource)
	}
	if !definition.Namespaced {
		t.Fatal("Namespaced = false, want true")
	}
}

func TestBuildCustomResourceFromYAMLValidatesScopeAndName(t *testing.T) {
	t.Parallel()

	definition := crdResourceDefinition{
		Kind:       "Widget",
		Group:      "example.io",
		Version:    "v1",
		Resource:   "widgets",
		Namespaced: true,
	}
	item, namespace, err := buildCustomResourceFromYAML(definition, "apiVersion: example.io/v1\nkind: Widget\nmetadata:\n  name: sample\n", "team-a", "sample")
	if err != nil {
		t.Fatalf("buildCustomResourceFromYAML returned error: %v", err)
	}
	if namespace != "team-a" {
		t.Fatalf("namespace = %q, want team-a", namespace)
	}
	if item.GetNamespace() != "team-a" {
		t.Fatalf("item.GetNamespace() = %q, want team-a", item.GetNamespace())
	}
	if item.GetName() != "sample" {
		t.Fatalf("item.GetName() = %q, want sample", item.GetName())
	}
}

func TestRequiredCustomResourceNamespaceRejectsClusterScopedNamespace(t *testing.T) {
	t.Parallel()

	_, err := requiredCustomResourceNamespace(crdResourceDefinition{Kind: "ClusterWidget", Namespaced: false}, "team-a")
	if err == nil {
		t.Fatal("requiredCustomResourceNamespace returned nil error, want scope validation failure")
	}
}
