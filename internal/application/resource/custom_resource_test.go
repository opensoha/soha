package resource

import (
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestCRDResourceDefinitionFromDomain(t *testing.T) {
	t.Parallel()

	definition, err := crdResourceDefinitionFromDomain(domainresource.CRDResourceDefinition{
		CRDName: "widgets.example.io", Kind: "Widget", Group: "example.io",
		Version: "v1", Resource: "widgets", Namespaced: true,
	})
	if err != nil {
		t.Fatalf("crdResourceDefinitionFromDomain returned error: %v", err)
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
	item, namespace, err := inspectCustomResourceYAML(definition, "apiVersion: example.io/v1\nkind: Widget\nmetadata:\n  name: sample\n", "team-a", "sample")
	if err != nil {
		t.Fatalf("inspectCustomResourceYAML returned error: %v", err)
	}
	if namespace != "team-a" {
		t.Fatalf("namespace = %q, want team-a", namespace)
	}
	if item.Namespace != "team-a" {
		t.Fatalf("item.Namespace = %q, want team-a", item.Namespace)
	}
	if item.Name != "sample" {
		t.Fatalf("item.Name = %q, want sample", item.Name)
	}
}

func TestRequiredCustomResourceNamespaceRejectsClusterScopedNamespace(t *testing.T) {
	t.Parallel()

	_, err := requiredCustomResourceNamespace(crdResourceDefinition{Kind: "ClusterWidget", Namespaced: false}, "team-a")
	if err == nil {
		t.Fatal("requiredCustomResourceNamespace returned nil error, want scope validation failure")
	}
}
