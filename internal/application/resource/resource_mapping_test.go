package resource

import (
	"reflect"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
)

func TestFilterScopedNamespaceItems(t *testing.T) {
	t.Parallel()

	items := []struct {
		name      string
		namespace string
	}{
		{name: "a", namespace: "team-a"},
		{name: "b", namespace: "team-b"},
		{name: "c", namespace: "team-c"},
	}
	filtered := filterScopedNamespaceItems(items, domainaccess.Decision{
		ResourceScope: &domainaccess.ResourceScope{Namespaces: []string{"team-b", "team-c"}},
	}, func(item struct {
		name      string
		namespace string
	}) string {
		return item.namespace
	})
	if !reflect.DeepEqual(filtered, items[1:]) {
		t.Fatalf("filtered = %#v, want %#v", filtered, items[1:])
	}

	unscoped := filterScopedNamespaceItems(items, domainaccess.Decision{}, func(item struct {
		name      string
		namespace string
	}) string {
		return item.namespace
	})
	if !reflect.DeepEqual(unscoped, items) {
		t.Fatalf("unscoped = %#v, want original items", unscoped)
	}
}
