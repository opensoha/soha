package menu

import (
	"context"
	"testing"

	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainmenu "github.com/kubecrux/kubecrux/internal/domain/menu"
)

type stubRepository struct {
	items []domainmenu.Record
}

func (s stubRepository) List(context.Context) ([]domainmenu.Record, error) {
	return append([]domainmenu.Record(nil), s.items...), nil
}

func (s stubRepository) Get(context.Context, string) (domainmenu.Record, error) {
	return domainmenu.Record{}, nil
}

func (s stubRepository) Create(context.Context, domainmenu.Record) (domainmenu.Record, error) {
	return domainmenu.Record{}, nil
}

func (s stubRepository) Update(context.Context, string, domainmenu.Record) (domainmenu.Record, error) {
	return domainmenu.Record{}, nil
}

func (s stubRepository) Delete(context.Context, string) error {
	return nil
}

type stubRolePermissionReader struct {
	matrix map[string][]string
}

func (s stubRolePermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s.matrix, nil
}

func TestListVisibleDerivesMenusFromPermissionKeys(t *testing.T) {
	service := New(stubRepository{
		items: []domainmenu.Record{
			{ID: "dashboard", Path: "/", SortOrder: 1, Enabled: true},
			{ID: "workloads", Path: "/workloads", SortOrder: 2, Enabled: true},
			{ID: "workloads-pods", ParentID: "workloads", Path: "/workloads/pods", SortOrder: 1, Enabled: true},
			{ID: "settings", Path: "/settings", SortOrder: 3, Enabled: true},
			{ID: "settings-ai", ParentID: "settings", Path: "/settings/ai", SortOrder: 1, Enabled: true},
		},
	}, appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"custom": {appaccess.PermPlatformWorkloadsView, appaccess.PermSettingsAIView},
		},
	}), nil, nil)

	items, err := service.ListVisible(context.Background(), domainidentity.Principal{Roles: []string{"custom"}})
	if err != nil {
		t.Fatalf("ListVisible returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("root menus = %d, want 2", len(items))
	}
	workloads := findMenu(items, "workloads")
	if workloads == nil {
		t.Fatalf("workloads menu not found: %#v", items)
	}
	if len(workloads.Children) != 1 || workloads.Children[0].ID != "workloads-pods" {
		t.Fatalf("workloads children = %#v, want workloads-pods", workloads.Children)
	}
	settings := findMenu(items, "settings")
	if settings == nil {
		t.Fatalf("settings menu not found: %#v", items)
	}
	if len(settings.Children) != 1 || settings.Children[0].ID != "settings-ai" {
		t.Fatalf("settings children = %#v, want settings-ai", settings.Children)
	}
}

func TestListVisibleFallsBackToExplicitBindingsForMappedMenus(t *testing.T) {
	service := New(stubRepository{
		items: []domainmenu.Record{
			{ID: "system", Path: "/system", Enabled: true},
			{ID: "announcements", ParentID: "system", Path: "/system/announcements", Enabled: true, RoleIDs: []string{"ops"}},
			{ID: "menus", ParentID: "system", Path: "/system/menus", Enabled: true},
		},
	}, appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"ops": {},
		},
	}), nil, nil)

	items, err := service.ListVisible(context.Background(), domainidentity.Principal{Roles: []string{"ops"}})
	if err != nil {
		t.Fatalf("ListVisible returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("root menus = %d, want 1", len(items))
	}
	if items[0].ID != "system" {
		t.Fatalf("root menu = %s, want system", items[0].ID)
	}
	if len(items[0].Children) != 1 || items[0].Children[0].ID != "announcements" {
		t.Fatalf("system children = %#v, want announcements only", items[0].Children)
	}
}

func TestListVisibleIncludesNetworkTopologyUnderNetwork(t *testing.T) {
	service := New(stubRepository{
		items: []domainmenu.Record{
			{ID: "network", Path: "/network", SortOrder: 50, Enabled: true},
			{ID: "network-topology", ParentID: "network", Path: "/network/topology", SortOrder: 40, Enabled: true},
			{ID: "network-services", ParentID: "network", Path: "/network/services", SortOrder: 41, Enabled: true},
		},
	}, appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"custom": {appaccess.PermPlatformNetworkView},
		},
	}), nil, nil)

	items, err := service.ListVisible(context.Background(), domainidentity.Principal{Roles: []string{"custom"}})
	if err != nil {
		t.Fatalf("ListVisible returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("root menus = %d, want 1", len(items))
	}
	network := findMenu(items, "network")
	if network == nil {
		t.Fatalf("network menu not found: %#v", items)
	}
	if len(network.Children) != 2 {
		t.Fatalf("network children = %#v, want topology and services", network.Children)
	}
	if network.Children[0].ID != "network-topology" || network.Children[1].ID != "network-services" {
		t.Fatalf("network children order = %#v, want network-topology then network-services", network.Children)
	}
}

func TestListVisibleRestrictsRBACMenusToExplicitPlatformBindings(t *testing.T) {
	service := New(stubRepository{
		items: []domainmenu.Record{
			{ID: "platform-access-control", Path: "/platform-access-control", Enabled: true, RoleIDs: []string{"admin", "ops", "developer", "readonly"}},
			{ID: "platform-access-control-clusterroles", ParentID: "platform-access-control", Path: "/platform-access-control/clusterroles", Enabled: true, RoleIDs: []string{"admin", "ops", "developer", "readonly"}},
		},
	}, appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"readonly": {},
			"auditor":  {},
		},
	}), nil, nil)

	readonlyItems, err := service.ListVisible(context.Background(), domainidentity.Principal{Roles: []string{"readonly"}})
	if err != nil {
		t.Fatalf("ListVisible returned error for readonly: %v", err)
	}
	if len(readonlyItems) != 1 {
		t.Fatalf("readonly visible roots = %d, want 1", len(readonlyItems))
	}
	if readonlyItems[0].ID != "platform-access-control" {
		t.Fatalf("readonly root = %s, want platform-access-control", readonlyItems[0].ID)
	}
	if len(readonlyItems[0].Children) != 1 || readonlyItems[0].Children[0].ID != "platform-access-control-clusterroles" {
		t.Fatalf("readonly children = %#v, want clusterroles child", readonlyItems[0].Children)
	}

	auditorItems, err := service.ListVisible(context.Background(), domainidentity.Principal{Roles: []string{"auditor"}})
	if err != nil {
		t.Fatalf("ListVisible returned error for auditor: %v", err)
	}
	if len(auditorItems) != 0 {
		t.Fatalf("auditor visible menus = %#v, want none", auditorItems)
	}
}

func findMenu(items []domainmenu.Record, menuID string) *domainmenu.Record {
	for index := range items {
		if items[index].ID == menuID {
			return &items[index]
		}
	}
	return nil
}

func TestListVisiblePreservesUnmappedMenusWithoutBindings(t *testing.T) {
	service := New(stubRepository{
		items: []domainmenu.Record{
			{ID: "custom-catalog", Path: "/custom-catalog", Enabled: true},
			{ID: "custom-delivery", Path: "/custom-delivery", Enabled: true},
		},
	}, appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"readonly": {},
		},
	}), nil, nil)

	items, err := service.ListVisible(context.Background(), domainidentity.Principal{Roles: []string{"readonly"}})
	if err != nil {
		t.Fatalf("ListVisible returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("visible menus = %d, want 2", len(items))
	}
	if items[0].ID != "custom-catalog" || items[1].ID != "custom-delivery" {
		t.Fatalf("visible menus = %#v, want unmapped menus preserved", items)
	}
}
