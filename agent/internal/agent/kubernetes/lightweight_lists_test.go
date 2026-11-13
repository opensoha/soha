package kubernetes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	discoveryfake "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
)

func TestListTablePaginates(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("limit") != "500" {
			t.Fatalf("limit = %q", r.URL.Query().Get("limit"))
		}
		name := "first"
		continueToken := "next"
		if requests == 2 {
			if r.URL.Query().Get("continue") != "next" {
				t.Fatalf("continue = %q", r.URL.Query().Get("continue"))
			}
			name, continueToken = "second", ""
		}
		metadata := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "platform"}}
		table := metav1.Table{
			ListMeta:          metav1.ListMeta{Continue: continueToken},
			ColumnDefinitions: []metav1.TableColumnDefinition{{Name: "Name"}, {Name: "Data"}},
			Rows:              []metav1.TableRow{{Cells: []any{name, int64(1)}, Object: runtime.RawExtension{Object: metadata}}},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(table); err != nil {
			t.Fatalf("encode table: %v", err)
		}
	}))
	defer server.Close()

	typed, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	items, err := (&Client{typed: typed}).ListConfigMaps(context.Background(), "platform")
	if err != nil {
		t.Fatalf("ListConfigMaps() error = %v", err)
	}
	if requests != 2 || len(items) != 2 || items[0].Name != "first" || items[1].Name != "second" {
		t.Fatalf("requests = %d, items = %#v", requests, items)
	}
}

func TestLightweightPlatformListsUseTableMetadata(t *testing.T) {
	created := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadata := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
			Name: "app", Namespace: "platform", UID: "table-uid", CreationTimestamp: metav1.NewTime(created),
		}}
		table := metav1.Table{Rows: []metav1.TableRow{{Object: runtime.RawExtension{Object: metadata}}}}
		switch r.URL.Path {
		case "/apis/apps/v1/namespaces/platform/deployments":
			table.ColumnDefinitions = tableColumns("Name", "Ready", "Up-to-date", "Available")
			table.Rows[0].Cells = []any{"app", "2/3", int64(2), int64(2)}
		case "/apis/apps/v1/namespaces/platform/daemonsets":
			table.ColumnDefinitions = tableColumns("Name", "Desired", "Current", "Ready", "Up-to-date", "Available")
			table.Rows[0].Cells = []any{"app", int64(4), int64(4), int64(3), int64(4), int64(3)}
		case "/api/v1/namespaces/platform/serviceaccounts":
			table.ColumnDefinitions = tableColumns("Name")
			table.Rows[0].Cells = []any{"app"}
		case "/apis/rbac.authorization.k8s.io/v1/namespaces/platform/roles":
			table.ColumnDefinitions = tableColumns("Name")
			table.Rows[0].Cells = []any{"app"}
		case "/apis/rbac.authorization.k8s.io/v1/namespaces/platform/rolebindings":
			table.ColumnDefinitions = tableColumns("Name", "Role")
			table.Rows[0].Cells = []any{"app", "Role/reader"}
		case "/apis/rbac.authorization.k8s.io/v1/clusterroles":
			table.ColumnDefinitions = tableColumns("Name")
			table.Rows[0].Cells = []any{"app"}
		case "/apis/rbac.authorization.k8s.io/v1/clusterrolebindings":
			table.ColumnDefinitions = tableColumns("Name", "Role")
			table.Rows[0].Cells = []any{"app", "ClusterRole/reader"}
		case "/apis/storage.k8s.io/v1/storageclasses":
			table.ColumnDefinitions = tableColumns("Name", "Provisioner", "ReclaimPolicy", "VolumeBindingMode", "AllowVolumeExpansion")
			table.Rows[0].Cells = []any{"app (default)", "csi.example.com", "Delete", "WaitForFirstConsumer", true}
		case "/apis/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations",
			"/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations":
			table.ColumnDefinitions = tableColumns("Name", "Webhooks")
			table.Rows[0].Cells = []any{"app", int64(2)}
		default:
			t.Fatalf("unexpected table path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(table); err != nil {
			t.Fatalf("encode table: %v", err)
		}
	}))
	defer server.Close()

	typed, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	client := &Client{typed: typed}
	deployments, err := client.ListDeployments(context.Background(), "platform")
	if err != nil || len(deployments) != 1 || deployments[0].UID != "table-uid" || deployments[0].DesiredReplicas != 3 || deployments[0].ReadyReplicas != 2 {
		t.Fatalf("deployments = %#v, error = %v", deployments, err)
	}
	daemonSets, err := client.ListDaemonSets(context.Background(), "platform")
	if err != nil || len(daemonSets) != 1 || daemonSets[0].AvailableNumber != 3 {
		t.Fatalf("daemonsets = %#v, error = %v", daemonSets, err)
	}
	serviceAccounts, err := client.ListServiceAccounts(context.Background(), "platform")
	if err != nil || len(serviceAccounts) != 1 {
		t.Fatalf("serviceaccounts = %#v, error = %v", serviceAccounts, err)
	}
	roles, err := client.ListRoles(context.Background(), "platform")
	if err != nil || len(roles) != 1 {
		t.Fatalf("roles = %#v, error = %v", roles, err)
	}
	bindings, err := client.ListRoleBindings(context.Background(), "platform")
	if err != nil || len(bindings) != 1 || bindings[0].RoleRef != "Role/reader" {
		t.Fatalf("rolebindings = %#v, error = %v", bindings, err)
	}
	clusterRoles, err := client.ListClusterRoles(context.Background())
	if err != nil || len(clusterRoles) != 1 {
		t.Fatalf("clusterroles = %#v, error = %v", clusterRoles, err)
	}
	clusterBindings, err := client.ListClusterRoleBindings(context.Background())
	if err != nil || len(clusterBindings) != 1 || clusterBindings[0].RoleRef != "ClusterRole/reader" {
		t.Fatalf("clusterrolebindings = %#v, error = %v", clusterBindings, err)
	}
	storageClasses, err := client.ListStorageClasses(context.Background())
	if err != nil || len(storageClasses) != 1 || storageClasses[0].Name != "app" || !storageClasses[0].AllowVolumeExpansion {
		t.Fatalf("storageclasses = %#v, error = %v", storageClasses, err)
	}
	mutating, err := client.ListMutatingWebhookConfigurations(context.Background())
	if err != nil || len(mutating) != 1 || mutating[0].Webhooks != 2 {
		t.Fatalf("mutating webhooks = %#v, error = %v", mutating, err)
	}
	validating, err := client.ListValidatingWebhookConfigurations(context.Background())
	if err != nil || len(validating) != 1 || validating[0].Webhooks != 2 {
		t.Fatalf("validating webhooks = %#v, error = %v", validating, err)
	}
}

func TestBindingSubjectFilters(t *testing.T) {
	typed := kubernetesfake.NewSimpleClientset(
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "platform"}, RoleRef: rbacv1.RoleRef{Kind: "Role", Name: "reader"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "app"}}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "platform"}, RoleRef: rbacv1.RoleRef{Kind: "Role", Name: "reader"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "other", Namespace: "platform"}}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "global"}, RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", Name: "reader"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "app", Namespace: "platform"}}},
	)
	client := &Client{typed: typed}
	roleBindings, err := client.ListRoleBindingsForSubject(context.Background(), "platform", "ServiceAccount", "app", "platform")
	if err != nil || len(roleBindings) != 1 || roleBindings[0].Name != "local" {
		t.Fatalf("rolebindings = %#v, error = %v", roleBindings, err)
	}
	clusterBindings, err := client.ListClusterRoleBindingsForSubject(context.Background(), "ServiceAccount", "app", "platform")
	if err != nil || len(clusterBindings) != 1 || clusterBindings[0].Name != "global" {
		t.Fatalf("clusterrolebindings = %#v, error = %v", clusterBindings, err)
	}
}

func TestRoleBindingSubjectFilterPaginates(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("limit") != "500" {
			t.Fatalf("limit = %q", r.URL.Query().Get("limit"))
		}
		name, subjectName, continueToken := "other", "other", "next"
		if requests == 2 {
			if r.URL.Query().Get("continue") != "next" {
				t.Fatalf("continue = %q", r.URL.Query().Get("continue"))
			}
			name, subjectName, continueToken = "matching", "app", ""
		}
		items := rbacv1.RoleBindingList{
			ListMeta: metav1.ListMeta{Continue: continueToken},
			Items: []rbacv1.RoleBinding{{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "platform"},
				RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "reader"},
				Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: subjectName, Namespace: "platform"}},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(items); err != nil {
			t.Fatalf("encode rolebindings: %v", err)
		}
	}))
	defer server.Close()

	typed, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	items, err := (&Client{typed: typed}).ListRoleBindingsForSubject(
		context.Background(),
		"platform",
		"ServiceAccount",
		"app",
		"platform",
	)
	if err != nil || requests != 2 || len(items) != 1 || items[0].Name != "matching" {
		t.Fatalf("requests = %d, items = %#v, error = %v", requests, items, err)
	}
}

func tableColumns(names ...string) []metav1.TableColumnDefinition {
	columns := make([]metav1.TableColumnDefinition, 0, len(names))
	for _, name := range names {
		columns = append(columns, metav1.TableColumnDefinition{Name: name})
	}
	return columns
}

func TestLightweightCoreListsUseTableMetadata(t *testing.T) {
	created := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != agentTableAcceptType || r.URL.Query().Get("includeObject") != string(metav1.IncludeMetadata) {
			t.Fatalf("table headers/query = %q %q", r.Header.Get("Accept"), r.URL.RawQuery)
		}
		metadata := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
			Name: "app", Namespace: "platform", CreationTimestamp: metav1.NewTime(created),
			Labels: map[string]string{"owner": "helm", "name": "edge", "version": "2", "status": "deployed"},
		}}
		table := metav1.Table{Rows: []metav1.TableRow{{Object: runtime.RawExtension{Object: metadata}}}}
		switch r.URL.Path {
		case "/api/v1/namespaces/platform/configmaps":
			table.ColumnDefinitions = []metav1.TableColumnDefinition{{Name: "Name"}, {Name: "Data"}}
			table.Rows[0].Cells = []any{"app", int64(3)}
		case "/api/v1/namespaces/platform/secrets":
			if r.URL.Query().Get("labelSelector") == "owner=helm" {
				metadata.Name = "sh.helm.release.v1.edge.v2"
			} else {
				table.ColumnDefinitions = []metav1.TableColumnDefinition{{Name: "Name"}, {Name: "Type"}, {Name: "Data"}}
				table.Rows[0].Cells = []any{"app", "Opaque", int64(4)}
			}
		default:
			t.Fatalf("unexpected table path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(table); err != nil {
			t.Fatalf("encode table: %v", err)
		}
	}))
	defer server.Close()

	typed, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	client := &Client{typed: typed}
	configMaps, err := client.ListConfigMaps(context.Background(), "platform")
	if err != nil || len(configMaps) != 1 || configMaps[0].DataEntries != 3 {
		t.Fatalf("configmaps = %#v, error = %v", configMaps, err)
	}
	secrets, err := client.ListSecrets(context.Background(), "platform")
	if err != nil || len(secrets) != 1 || secrets[0].DataEntries != 4 || secrets[0].Immutable != nil {
		t.Fatalf("secrets = %#v, error = %v", secrets, err)
	}
	releases, err := client.ListHelmReleases(context.Background(), "platform")
	if err != nil || len(releases) != 1 || releases[0].Name != "edge" || releases[0].Revision != "2" {
		t.Fatalf("releases = %#v, error = %v", releases, err)
	}
}

func TestListCRDsCombinesMetadataWithDiscovery(t *testing.T) {
	created := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	object := &metav1.PartialObjectMetadata{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apiextensions.k8s.io/v1", Kind: "CustomResourceDefinition"},
		ObjectMeta: metav1.ObjectMeta{Name: "widgets.example.com", CreationTimestamp: metav1.NewTime(created)},
	}
	scheme := runtime.NewScheme()
	if err := metav1.AddMetaToScheme(scheme); err != nil {
		t.Fatalf("add metadata types: %v", err)
	}
	metadataClient := metadatafake.NewSimpleMetadataClient(scheme, object)
	discoveryClient := &discoveryfake.FakeDiscovery{Fake: &clienttesting.Fake{}}
	discoveryClient.Resources = []*metav1.APIResourceList{
		{GroupVersion: "example.com/v1", APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true}}},
		{GroupVersion: "example.com/v1beta1", APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true}}},
	}
	client := &Client{metadata: metadataClient, discovery: discoveryClient}

	items, err := client.ListCRDs(context.Background())
	if err != nil {
		t.Fatalf("ListCRDs() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "widgets.example.com" || items[0].Kind != "Widget" || items[0].Scope != "Namespaced" || items[0].Version != "v1" || len(items[0].Versions) != 2 {
		t.Fatalf("items = %#v", items)
	}
}

func TestListCustomResourcesUsesPartialMetadata(t *testing.T) {
	object := &metav1.PartialObjectMetadata{
		TypeMeta:   metav1.TypeMeta{APIVersion: "example.com/v1", Kind: "Widget"},
		ObjectMeta: metav1.ObjectMeta{Name: "sample", Namespace: "platform", Labels: map[string]string{"app": "demo"}},
	}
	scheme := runtime.NewScheme()
	if err := metav1.AddMetaToScheme(scheme); err != nil {
		t.Fatalf("add metadata types: %v", err)
	}
	metadataClient := metadatafake.NewSimpleMetadataClient(scheme, object)
	client := &Client{metadata: metadataClient}
	items, err := client.ListCustomResources(context.Background(), testWidgetDefinition(), "platform")
	if err != nil {
		t.Fatalf("ListCustomResources() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "sample" || items[0].Labels["app"] != "demo" {
		t.Fatalf("items = %#v", items)
	}
	actions := metadataClient.Actions()
	if len(actions) != 1 || actions[0].GetResource() != (schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}) {
		t.Fatalf("actions = %#v", actions)
	}
}

func TestListCustomResourcesUsesAllNamespacesWhenNamespaceIsEmpty(t *testing.T) {
	objects := []runtime.Object{
		&metav1.PartialObjectMetadata{
			TypeMeta:   metav1.TypeMeta{APIVersion: "example.com/v1", Kind: "Widget"},
			ObjectMeta: metav1.ObjectMeta{Name: "first", Namespace: "team-a"},
		},
		&metav1.PartialObjectMetadata{
			TypeMeta:   metav1.TypeMeta{APIVersion: "example.com/v1", Kind: "Widget"},
			ObjectMeta: metav1.ObjectMeta{Name: "second", Namespace: "team-b"},
		},
	}
	scheme := runtime.NewScheme()
	if err := metav1.AddMetaToScheme(scheme); err != nil {
		t.Fatalf("add metadata types: %v", err)
	}
	client := &Client{metadata: metadatafake.NewSimpleMetadataClient(scheme, objects...)}

	items, err := client.ListCustomResources(context.Background(), testWidgetDefinition(), "")
	if err != nil {
		t.Fatalf("ListCustomResources(all namespaces) error = %v", err)
	}
	if len(items) != 2 || items[0].Namespace != "team-a" || items[1].Namespace != "team-b" {
		t.Fatalf("items = %#v, want resources from both namespaces", items)
	}
}

func testWidgetDefinition() domainresource.CRDResourceDefinition {
	return domainresource.CRDResourceDefinition{Group: "example.com", Version: "v1", Resource: "widgets", Kind: "Widget", Namespaced: true}
}
