package resourcebackend

import (
	"testing"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestMapCRDMetadataCombinesDiscoveryWithoutSchemas(t *testing.T) {
	t.Parallel()
	created := metav1.NewTime(time.Now().Add(-time.Hour))
	items := []metav1.PartialObjectMetadata{{ObjectMeta: metav1.ObjectMeta{Name: "widgets.example.io", CreationTimestamp: created}}}
	resources := []*metav1.APIResourceList{
		{GroupVersion: "example.io/v1", APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true}}},
		{GroupVersion: "example.io/v1beta1", APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true}, {Name: "widgets/status", Kind: "Widget", Namespaced: true}}},
	}

	views, missing := mapCRDMetadata(items, resources)
	if len(missing) != 0 || len(views) != 1 {
		t.Fatalf("mapCRDMetadata() views=%#v missing=%#v", views, missing)
	}
	view := views[0]
	if view.Name != "widgets.example.io" || view.Group != "example.io" || view.Plural != "widgets" || view.Kind != "Widget" || view.Scope != "Namespaced" {
		t.Fatalf("mapCRDMetadata() = %#v", view)
	}
	if len(view.Versions) != 2 || view.Version != "v1" || view.Versions[1] != "v1beta1" {
		t.Fatalf("mapCRDMetadata() versions = %#v", view.Versions)
	}
}

func TestMapPartialCustomResourcesUsesDefinitionIdentity(t *testing.T) {
	t.Parallel()
	created := metav1.NewTime(time.Now().Add(-time.Minute))
	items := []metav1.PartialObjectMetadata{{ObjectMeta: metav1.ObjectMeta{
		Name: "example", Namespace: "team-a", Labels: map[string]string{"app": "demo"}, CreationTimestamp: created,
	}}}

	views := mapPartialCustomResources(items, domainresource.CRDResourceDefinition{Group: "example.io", Version: "v1", Kind: "Widget"})
	if len(views) != 1 || views[0].APIVersion != "example.io/v1" || views[0].Kind != "Widget" || views[0].Name != "example" || views[0].Namespace != "team-a" || views[0].Labels["app"] != "demo" {
		t.Fatalf("mapPartialCustomResources() = %#v", views)
	}
}
