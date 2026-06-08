package menu

import (
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainmenu "github.com/opensoha/soha/internal/domain/menu"
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
	item := domainmenu.Record{ID: "ai-gateway", Path: "/ai-gateway"}

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

func TestAIGatewayChildMenusUseSpecificPermissions(t *testing.T) {
	overview := domainmenu.Record{ID: "ai-gateway-overview", Path: "/ai-gateway/overview"}
	clients := domainmenu.Record{ID: "ai-gateway-clients", Path: "/ai-gateway/clients"}
	tokens := domainmenu.Record{ID: "ai-gateway-tokens", Path: "/ai-gateway/tokens"}
	governance := domainmenu.Record{ID: "ai-gateway-governance", Path: "/ai-gateway/governance"}
	callLogs := domainmenu.Record{ID: "ai-gateway-call-logs", Path: "/ai-gateway/call-logs"}

	if !isVisibleByPermissions(overview, []string{appaccess.PermWorkspaceResourceView, appaccess.PermAIGatewayView}) {
		t.Fatalf("AI Gateway overview should be visible with view permission")
	}
	if isVisibleByPermissions(clients, []string{appaccess.PermWorkspaceResourceView, appaccess.PermAIGatewayView}) {
		t.Fatalf("AI Gateway clients should require manage permission")
	}
	if !isVisibleByPermissions(clients, []string{appaccess.PermWorkspaceResourceView, appaccess.PermAIGatewayManage}) {
		t.Fatalf("AI Gateway clients should be visible with manage permission")
	}
	if !isVisibleByPermissions(tokens, []string{appaccess.PermWorkspaceResourceView, appaccess.PermAIGatewayInvoke}) {
		t.Fatalf("AI Gateway tokens should be visible with invoke permission")
	}
	if !isVisibleByPermissions(governance, []string{appaccess.PermWorkspaceResourceView, appaccess.PermAIGatewayManage}) {
		t.Fatalf("AI Gateway governance should be visible with manage permission")
	}
	if isVisibleByPermissions(callLogs, []string{appaccess.PermWorkspaceResourceView, appaccess.PermAIGatewayView}) {
		t.Fatalf("AI Gateway call logs should require manage permission")
	}
	if !isVisibleByPermissions(callLogs, []string{appaccess.PermWorkspaceResourceView, appaccess.PermAIGatewayManage}) {
		t.Fatalf("AI Gateway call logs should be visible with manage permission")
	}
}

func TestPluginMenusRequireWorkspaceAndPluginViewPermission(t *testing.T) {
	for _, item := range []domainmenu.Record{
		{ID: "plugins", Path: "/plugins"},
		{ID: "plugins-marketplace", Path: "/plugins/marketplace"},
		{ID: "plugins-installed", Path: "/plugins/installed"},
	} {
		if isVisibleByPermissions(item, []string{appaccess.PermWorkspaceResourceView}) {
			t.Fatalf("%s should require %s", item.ID, appaccess.PermPluginView)
		}
		if isVisibleByPermissions(item, []string{appaccess.PermPluginView}) {
			t.Fatalf("%s should require %s", item.ID, appaccess.PermWorkspaceResourceView)
		}
		if !isVisibleByPermissions(item, []string{appaccess.PermWorkspaceResourceView, appaccess.PermPluginView}) {
			t.Fatalf("%s should be visible with workspace and plugin view permissions", item.ID)
		}
	}
}

func TestSystemMenusDoNotRequireWorkspacePermission(t *testing.T) {
	item := domainmenu.Record{ID: "menus", Path: "/system/menus"}

	if !isVisibleByPermissions(item, []string{appaccess.PermSystemMenusView}) {
		t.Fatalf("system menu should remain visible without workspace permissions")
	}
}
