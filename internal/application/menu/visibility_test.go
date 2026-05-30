package menu

import (
	"testing"

	appaccess "github.com/soha/soha/internal/application/access"
	domainmenu "github.com/soha/soha/internal/domain/menu"
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

func TestVirtualizationMenusRequireWorkspaceResourcePermission(t *testing.T) {
	item := domainmenu.Record{ID: "virtualization-workbench-vms", Path: "/virtualization/vms"}

	if isVisibleByPermissions(item, []string{appaccess.PermVirtualizationVMsView}) {
		t.Fatalf("virtualization menu should require %s", appaccess.PermWorkspaceResourceView)
	}
	if !isVisibleByPermissions(item, []string{appaccess.PermWorkspaceResourceView, appaccess.PermVirtualizationVMsView}) {
		t.Fatalf("virtualization menu should be visible when workspace and page permissions are both present")
	}
}

func TestVirtualizationRootMenuVisibleWithAnyVirtualizationPermission(t *testing.T) {
	item := domainmenu.Record{ID: "virtualization-workbench", Path: "/virtualization"}

	if !isVisibleByPermissions(item, []string{appaccess.PermWorkspaceResourceView, appaccess.PermVirtualizationSyncView}) {
		t.Fatalf("virtualization root menu should be visible with any virtualization view permission")
	}
}

func TestVirtualizationSyncMenuVisibleWithViewOrManagePermission(t *testing.T) {
	item := domainmenu.Record{ID: "virtualization-workbench-sync", Path: "/virtualization/sync"}

	if !isVisibleByPermissions(item, []string{appaccess.PermWorkspaceResourceView, appaccess.PermVirtualizationSyncView}) {
		t.Fatalf("virtualization sync menu should be visible with sync view permission")
	}
	if !isVisibleByPermissions(item, []string{appaccess.PermWorkspaceResourceView, appaccess.PermVirtualizationSyncManage}) {
		t.Fatalf("virtualization sync menu should be visible with sync manage permission")
	}
}

func TestAIGatewayMenuRequiresWorkspaceAndGatewayViewPermission(t *testing.T) {
	item := domainmenu.Record{ID: "ai-workbench-gateway", Path: "/ai-workbench/gateway"}

	if isVisibleByPermissions(item, []string{appaccess.PermWorkspaceResourceView}) {
		t.Fatalf("AI Gateway menu should require %s", appaccess.PermAIGatewayView)
	}
	if isVisibleByPermissions(item, []string{appaccess.PermAIGatewayView}) {
		t.Fatalf("AI Gateway menu should require %s", appaccess.PermWorkspaceResourceView)
	}
	if !isVisibleByPermissions(item, []string{appaccess.PermWorkspaceResourceView, appaccess.PermAIGatewayView}) {
		t.Fatalf("AI Gateway menu should be visible when workspace and gateway view permissions are both present")
	}
}

func TestSystemMenusDoNotRequireWorkspacePermission(t *testing.T) {
	item := domainmenu.Record{ID: "menus", Path: "/system/menus"}

	if !isVisibleByPermissions(item, []string{appaccess.PermSystemMenusView}) {
		t.Fatalf("system menu should remain visible without workspace permissions")
	}
}
