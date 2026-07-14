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

func TestDeliveryWorkbenchTaskMenusUseAnyEvidencePermission(t *testing.T) {
	testingMenu := domainmenu.Record{ID: "delivery-testing", Path: "/delivery/testing"}
	analysisMenu := domainmenu.Record{ID: "delivery-analysis", Path: "/delivery/analysis"}

	if isVisibleByPermissions(testingMenu, []string{appaccess.PermDeliveryReleaseBundlesView}) {
		t.Fatalf("delivery testing menu should require %s", appaccess.PermWorkspaceApplicationView)
	}
	if !isVisibleByPermissions(testingMenu, []string{appaccess.PermWorkspaceApplicationView, appaccess.PermDeliveryReleaseBundlesView}) {
		t.Fatalf("delivery testing menu should be visible with release bundle evidence permission")
	}
	if !isVisibleByPermissions(testingMenu, []string{appaccess.PermWorkspaceApplicationView, appaccess.PermDeliveryExecutionTasksView}) {
		t.Fatalf("delivery testing menu should be visible with execution task evidence permission")
	}
	if !isVisibleByPermissions(analysisMenu, []string{appaccess.PermWorkspaceApplicationView, appaccess.PermDeliveryReleaseBoardView}) {
		t.Fatalf("delivery analysis menu should be visible with release board evidence permission")
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

func TestUnifiedAIWorkbenchRootAcceptsAIOrGatewayPermissions(t *testing.T) {
	root := domainmenu.Record{ID: "ai-workbench", Path: "/ai-workbench"}
	overview := domainmenu.Record{ID: "ai-workbench-overview", Path: "/ai-workbench/overview"}

	for _, permission := range []string{
		appaccess.PermObserveAIChatUse,
		appaccess.PermAIKnowledgeView,
		appaccess.PermAIContextInspect,
		appaccess.PermAIEvaluationsView,
		appaccess.PermAIGatewayView,
		appaccess.PermAIGatewayRelayView,
	} {
		permissions := []string{appaccess.PermWorkspaceResourceView, permission}
		if !isVisibleByPermissions(root, permissions) || !isVisibleByPermissions(overview, permissions) {
			t.Fatalf("unified AI workbench should be visible with %s", permission)
		}
	}
}

func TestUnifiedAIWorkbenchLeafPermissionsStayNarrow(t *testing.T) {
	knowledge := domainmenu.Record{ID: "ai-workbench-knowledge", Path: "/ai-workbench/knowledge"}
	contextInspector := domainmenu.Record{ID: "ai-workbench-context", Path: "/ai-workbench/context"}
	evaluations := domainmenu.Record{ID: "ai-workbench-evaluations", Path: "/ai-workbench/evaluations"}
	relay := domainmenu.Record{ID: "ai-gateway-relay", Path: "/ai-gateway/relay"}
	workspace := appaccess.PermWorkspaceResourceView

	if isVisibleByPermissions(knowledge, []string{workspace, appaccess.PermObserveAIView}) {
		t.Fatal("Knowledge Center should not inherit generic AI view permission")
	}
	if !isVisibleByPermissions(knowledge, []string{workspace, appaccess.PermAIKnowledgeView}) {
		t.Fatal("Knowledge Center should accept knowledge view permission")
	}
	if !isVisibleByPermissions(contextInspector, []string{workspace, appaccess.PermAIContextInspect}) {
		t.Fatal("Context Inspector should accept context inspect permission")
	}
	if isVisibleByPermissions(evaluations, []string{workspace, appaccess.PermAIKnowledgeView}) || isVisibleByPermissions(evaluations, []string{workspace, appaccess.PermAIContextInspect}) {
		t.Fatal("Evaluation should not borrow knowledge or context permissions")
	}
	if !isVisibleByPermissions(evaluations, []string{workspace, appaccess.PermAIEvaluationsView}) {
		t.Fatal("Evaluation should accept its dedicated view permission")
	}
	if isVisibleByPermissions(relay, []string{workspace, appaccess.PermAIGatewayView}) {
		t.Fatal("model relay should require a relay-specific permission")
	}
	if !isVisibleByPermissions(relay, []string{workspace, appaccess.PermAIGatewayRelayView}) {
		t.Fatal("model relay should accept relay view permission")
	}
}

func TestExtensionMarketplaceMenuRequiresPluginViewPermission(t *testing.T) {
	item := domainmenu.Record{ID: "settings-extensions-marketplace", Path: "/plugins/marketplace"}
	if isVisibleByPermissions(item, nil) {
		t.Fatalf("%s should require %s", item.ID, appaccess.PermPluginView)
	}
	if !isVisibleByPermissions(item, []string{appaccess.PermPluginView}) {
		t.Fatalf("%s should be visible with plugin view permission", item.ID)
	}
}

func TestExtensionCenterVisibleWithPluginViewPermission(t *testing.T) {
	item := domainmenu.Record{ID: "settings-extensions", Path: "/settings/extensions"}
	if !isVisibleByPermissions(item, []string{appaccess.PermPluginView}) {
		t.Fatalf("extension center should be visible with %s", appaccess.PermPluginView)
	}
}

func TestSystemMenusDoNotRequireWorkspacePermission(t *testing.T) {
	item := domainmenu.Record{ID: "menus", Path: "/system/menus"}

	if !isVisibleByPermissions(item, []string{appaccess.PermSystemMenusView}) {
		t.Fatalf("system menu should remain visible without workspace permissions")
	}
}
