package menu

import (
	"testing"

	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainmenu "github.com/kubecrux/kubecrux/internal/domain/menu"
)

func TestApplicationMenusRequireWorkspaceApplicationPermission(t *testing.T) {
	item := domainmenu.Record{ID: "builds", Path: "/applications"}

	if isVisibleByPermissions(item, []string{appaccess.PermDeliveryApplicationsView}) {
		t.Fatalf("application menu should require %s", appaccess.PermWorkspaceApplicationView)
	}
	if !isVisibleByPermissions(item, []string{appaccess.PermWorkspaceApplicationView, appaccess.PermDeliveryApplicationsView}) {
		t.Fatalf("application menu should be visible when workspace and page permissions are both present")
	}
}

func TestResourceMenusRequireWorkspaceResourcePermission(t *testing.T) {
	item := domainmenu.Record{ID: "workloads", Path: "/workloads"}

	if isVisibleByPermissions(item, []string{appaccess.PermPlatformWorkloadsView}) {
		t.Fatalf("resource menu should require %s", appaccess.PermWorkspaceResourceView)
	}
	if !isVisibleByPermissions(item, []string{appaccess.PermWorkspaceResourceView, appaccess.PermPlatformWorkloadsView}) {
		t.Fatalf("resource menu should be visible when workspace and page permissions are both present")
	}
}

func TestSystemMenusDoNotRequireWorkspacePermission(t *testing.T) {
	item := domainmenu.Record{ID: "menus", Path: "/system/menus"}

	if !isVisibleByPermissions(item, []string{appaccess.PermSystemMenusView}) {
		t.Fatalf("system menu should remain visible without workspace permissions")
	}
}
