package resource

import (
	"errors"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestFilterNamespaceViewsDoesNotExposeNamespacesOutsideExplicitScope(t *testing.T) {
	items := []domainresource.NamespaceView{{Name: "minio"}, {Name: "ops"}, {Name: "secret-team"}}
	filtered := filterNamespaceViews(items, domainaccess.Decision{ResourceScope: &domainaccess.ResourceScope{
		Namespaces: []string{"minio", "ops"},
	}})
	if len(filtered) != 2 || filtered[0].Name != "minio" || filtered[1].Name != "ops" {
		t.Fatalf("filtered = %#v, want only minio and ops", filtered)
	}
}

func TestFindNamespaceLabelsReturnsOnlyExactNamespace(t *testing.T) {
	labels, err := findNamespaceLabels([]domainresource.NamespaceView{
		{Name: "minio-old", Labels: map[string]string{"tenant": "other"}},
		{Name: "minio", Labels: map[string]string{"tenant": "retail"}},
	}, "minio")
	if err != nil || labels["tenant"] != "retail" {
		t.Fatalf("labels = %v, err=%v", labels, err)
	}
	_, err = findNamespaceLabels(nil, "hidden")
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("error = %v, want not found", err)
	}
}

func TestFilterNamespaceViewsResolvesAllowAndDenySelectors(t *testing.T) {
	items := []domainresource.NamespaceView{
		{Name: "minio", Labels: map[string]string{"tenant": "retail"}},
		{Name: "ops", Labels: map[string]string{"tenant": "retail", "restricted": "true"}},
		{Name: "other", Labels: map[string]string{"tenant": "other"}},
	}
	filtered := filterNamespaceViews(items, domainaccess.Decision{ResourceScope: &domainaccess.ResourceScope{
		NamespaceSelectors:         []string{"tenant=retail"},
		ExcludedNamespaceSelectors: []string{"restricted=true"},
	}})
	if len(filtered) != 1 || filtered[0].Name != "minio" {
		t.Fatalf("filtered = %#v, want only minio", filtered)
	}
}

func TestFilterScopedNamespaceItemsFailsClosedForSelectorOnlyScope(t *testing.T) {
	items := []domainresource.ConfigMapView{{Name: "visible", Namespace: "minio"}, {Name: "hidden", Namespace: "other"}}
	filtered := filterScopedNamespaceItems(items, domainaccess.Decision{ResourceScope: &domainaccess.ResourceScope{
		NamespaceSelectors: []string{"tenant=retail"},
	}}, func(item domainresource.ConfigMapView) string { return item.Namespace })
	if len(filtered) != 0 {
		t.Fatalf("filtered = %#v, selector-only resource list must fail closed", filtered)
	}
}

func TestFilterScopedNamespaceItemsAppliesDenyNamesToWildcardAllow(t *testing.T) {
	items := []domainresource.ConfigMapView{{Name: "visible", Namespace: "minio"}, {Name: "hidden", Namespace: "other"}}
	filtered := filterScopedNamespaceItems(items, domainaccess.Decision{ResourceScope: &domainaccess.ResourceScope{
		ExcludedNamespaces: []string{"other"},
	}}, func(item domainresource.ConfigMapView) string { return item.Namespace })
	if len(filtered) != 1 || filtered[0].Name != "visible" {
		t.Fatalf("filtered = %#v, want only visible item", filtered)
	}
}
