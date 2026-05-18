import { describe, expect, it } from "vitest";
import type { PermissionSnapshot, RouteMeta } from "@/types";
import {
  canAccessRoute,
  filterSidebarNavByWorkbench,
  filterSidebarNavByWorkspace,
  findFirstAccessiblePathForWorkspace,
  findLandingPath,
  findPreferredWorkspace,
  getAccessibleSidebarNav,
  getMenuWorkbenchId,
  getAccessibleWorkspaces,
  getRouteScopeMode,
  getRouteWorkspace,
  routeMeta,
} from "./meta";

function buildSnapshot(
  overrides?: Partial<PermissionSnapshot>,
): PermissionSnapshot {
  return {
    permissionKeys: [],
    visibleMenuIds: [],
    visibleMenus: [],
    ...overrides,
  };
}

function getRoute(id: string): RouteMeta {
  const route = routeMeta.find((item) => item.id === id);
  if (!route) {
    throw new Error(`missing route meta: ${id}`);
  }
  return route;
}

describe("access route authorization", () => {
  it("allows the access parent route when any visible child permission is present", () => {
    const snapshot = buildSnapshot({
      permissionKeys: ["access.roles.view"],
      visibleMenuIds: ["access"],
      visibleMenus: [{ id: "access", path: "/access" }],
    });

    expect(canAccessRoute(getRoute("access"), snapshot)).toBe(true);
    expect(canAccessRoute(getRoute("access-roles"), snapshot)).toBe(true);
    expect(canAccessRoute(getRoute("access-users"), snapshot)).toBe(false);
  });

  it("allows login settings and branding routes from dedicated visible menu bindings", () => {
    const snapshot = buildSnapshot({
      permissionKeys: ["settings.identity.view", "settings.branding.view"],
      visibleMenuIds: ["settings", "settings-login", "settings-branding"],
      visibleMenus: [
        { id: "settings", path: "/settings" },
        { id: "settings-login", parentId: "settings", path: "/settings/login" },
        {
          id: "settings-branding",
          parentId: "settings",
          path: "/settings/branding",
        },
      ],
    });

    expect(canAccessRoute(getRoute("settings"))).toBe(false);
    expect(canAccessRoute(getRoute("settings-login"), snapshot)).toBe(true);
    expect(canAccessRoute(getRoute("settings-branding"), snapshot)).toBe(true);
  });

  it("keeps the access parent route blocked when the access menu binding is missing", () => {
    const snapshot = buildSnapshot({
      permissionKeys: ["access.roles.view"],
      visibleMenuIds: [],
      visibleMenus: [],
    });

    expect(canAccessRoute(getRoute("access"), snapshot)).toBe(false);
    expect(canAccessRoute(getRoute("access-roles"), snapshot)).toBe(false);
  });

  it("allows scope-grants direct routing from its dedicated view permission", () => {
    const snapshot = buildSnapshot({
      permissionKeys: ["access.scope-grants.view"],
    });

    expect(canAccessRoute(getRoute("access-scope-grants"), snapshot)).toBe(
      true,
    );
    expect(canAccessRoute(getRoute("access"), snapshot)).toBe(false);
  });

  it("allows RBAC platform child routes from visible menu bindings without a dedicated permission key", () => {
    const snapshot = buildSnapshot({
      permissionKeys: ["workspace.resource.view"],
      visibleMenuIds: ["platform-access-control"],
      visibleMenus: [
        { id: "platform-access-control", path: "/platform-access-control" },
      ],
    });

    expect(canAccessRoute(getRoute("platform-access-control"), snapshot)).toBe(
      true,
    );
    expect(
      canAccessRoute(
        getRoute("platform-access-control-clusterroles"),
        snapshot,
      ),
    ).toBe(true);
  });

  it("inherits RBAC list access for hidden detail routes", () => {
    const snapshot = buildSnapshot({
      permissionKeys: ["workspace.resource.view"],
      visibleMenuIds: ["platform-access-control"],
      visibleMenus: [
        { id: "platform-access-control", path: "/platform-access-control" },
      ],
    });

    expect(
      canAccessRoute(
        getRoute("platform-access-control-serviceaccount-detail"),
        snapshot,
      ),
    ).toBe(true);
    expect(
      canAccessRoute(
        getRoute("platform-access-control-rolebinding-detail"),
        snapshot,
      ),
    ).toBe(true);
  });

  it("blocks RBAC platform child routes when the RBAC menu binding is missing", () => {
    const snapshot = buildSnapshot({
      visibleMenuIds: [],
      visibleMenus: [],
    });

    expect(canAccessRoute(getRoute("platform-access-control"), snapshot)).toBe(
      false,
    );
    expect(
      canAccessRoute(
        getRoute("platform-access-control-rolebindings"),
        snapshot,
      ),
    ).toBe(false);
  });

  it("builds sidebar nav from visible menu tree instead of flattening children", () => {
    const snapshot = buildSnapshot({
      permissionKeys: ["system.menus.view", "system.audit.view"],
      visibleMenuIds: ["system", "menus", "audit"],
      visibleMenus: [
        {
          id: "system",
          path: "/system",
          labelZh: "系统管理",
          labelEn: "System",
          iconKey: "panels-top-left",
          section: "admin",
          sortOrder: 10,
          enabled: true,
        },
        {
          id: "audit",
          parentId: "system",
          path: "/system/audit",
          labelZh: "审计日志",
          labelEn: "Audit",
          iconKey: "file-clock",
          section: "admin",
          sortOrder: 2,
          enabled: true,
        },
        {
          id: "menus",
          parentId: "system",
          path: "/system/menus",
          labelZh: "菜单管理",
          labelEn: "Menus",
          iconKey: "menu-square",
          section: "admin",
          sortOrder: 1,
          enabled: true,
        },
      ],
    });

    const nav = getAccessibleSidebarNav(snapshot);
    expect(nav).toHaveLength(1);
    expect(nav[0].id).toBe("system");
    expect(nav[0].children?.map((item) => item.id)).toEqual(["menus", "audit"]);
  });

  it("orders runtime roots by backend section and preserves backend icon keys", () => {
    const snapshot = buildSnapshot({
      permissionKeys: [
        "workspace.application.view",
        "delivery.applications.view",
        "system.menus.view",
      ],
      visibleMenuIds: ["builds", "system", "menus"],
      visibleMenus: [
        {
          id: "system",
          path: "/system",
          labelZh: "系统管理",
          labelEn: "System",
          iconKey: "panels-top-left",
          section: "admin",
          sortOrder: 50,
          enabled: true,
        },
        {
          id: "menus",
          parentId: "system",
          path: "/system/menus",
          labelZh: "菜单管理",
          labelEn: "Menus",
          iconKey: "menu-square",
          section: "admin",
          sortOrder: 10,
          enabled: true,
        },
        {
          id: "builds",
          path: "/applications",
          labelZh: "应用中心",
          labelEn: "Applications",
          iconKey: "blocks",
          section: "deliver",
          sortOrder: 5,
          enabled: true,
        },
      ],
    });

    const nav = getAccessibleSidebarNav(snapshot);
    expect(nav.map((item) => item.id)).toEqual(["builds", "system"]);
    expect(nav[0].iconKey).toBe("blocks");
    expect(nav[1].iconKey).toBe("panels-top-left");
  });

  it("derives route workspace ownership for application, resource, and system routes", () => {
    expect(getRouteWorkspace(getRoute("applications"))).toBe("application");
    expect(getRouteWorkspace(getRoute("workloads-pods"))).toBe("resource");
    expect(getRouteWorkspace(getRoute("system-menus"))).toBe("system");
  });

  it("requires workspace permissions for business routes", () => {
    const appSnapshot = buildSnapshot({
      permissionKeys: ["delivery.applications.view"],
      visibleMenuIds: ["builds"],
      visibleMenus: [{ id: "builds", path: "/applications" }],
    });
    const resourceSnapshot = buildSnapshot({
      permissionKeys: ["platform.workloads.view"],
      visibleMenuIds: ["workloads"],
      visibleMenus: [{ id: "workloads", path: "/workloads" }],
    });

    expect(canAccessRoute(getRoute("applications"), appSnapshot)).toBe(false);
    expect(canAccessRoute(getRoute("workloads"), resourceSnapshot)).toBe(false);
  });

  it("filters business and system sidebar trees by workspace", () => {
    const snapshot = buildSnapshot({
      permissionKeys: [
        "workspace.application.view",
        "workspace.resource.view",
        "delivery.applications.view",
        "delivery.application-environments.view",
        "system.menus.view",
      ],
      visibleMenuIds: ["builds", "application-environments", "system", "menus"],
      visibleMenus: [
        {
          id: "builds",
          path: "/applications",
          labelZh: "应用中心",
          labelEn: "Applications",
          iconKey: "blocks",
          section: "deliver",
          sortOrder: 5,
          enabled: true,
        },
        {
          id: "application-environments",
          path: "/application-environments",
          labelZh: "应用环境绑定",
          labelEn: "Application Bindings",
          iconKey: "blocks",
          section: "catalog",
          sortOrder: 99,
          enabled: true,
        },
        {
          id: "system",
          path: "/system",
          labelZh: "系统管理",
          labelEn: "System",
          iconKey: "panels-top-left",
          section: "admin",
          sortOrder: 50,
          enabled: true,
        },
        {
          id: "menus",
          parentId: "system",
          path: "/system/menus",
          labelZh: "菜单管理",
          labelEn: "Menus",
          iconKey: "menu-square",
          section: "admin",
          sortOrder: 10,
          enabled: true,
        },
      ],
    });

    const nav = getAccessibleSidebarNav(snapshot);
    const applicationNav = filterSidebarNavByWorkspace(nav, "application");
    const systemNav = filterSidebarNavByWorkspace(nav, "system");

    expect(applicationNav.map((item) => item.id)).toEqual([
      "builds",
      "application-environments",
    ]);
    expect(applicationNav[0].section).toBe("deliver");
    expect(applicationNav[1].section).toBe("catalog");
    expect(systemNav.map((item) => item.id)).toEqual(["system"]);
  });

  it("preserves empty backend menu sections inside a workbench", () => {
    const snapshot = buildSnapshot({
      permissionKeys: [
        "workspace.resource.view",
        "overview.view",
        "platform.workloads.view",
      ],
      visibleMenuIds: ["dashboard", "workloads"],
      visibleMenus: [
        {
          id: "dashboard",
          path: "/",
          labelZh: "概览",
          labelEn: "Overview",
          iconKey: "gauge",
          section: "",
          sortOrder: 1,
          enabled: true,
        },
        {
          id: "workloads",
          path: "/workloads",
          labelZh: "工作负载",
          labelEn: "Workloads",
          iconKey: "boxes",
          section: "",
          sortOrder: 2,
          enabled: true,
        },
      ],
    });

    const nav = getAccessibleSidebarNav(snapshot);
    const platformNav = filterSidebarNavByWorkbench(
      filterSidebarNavByWorkspace(nav, "resource"),
      "platform",
    );

    expect(platformNav.map((item) => item.id)).toEqual([
      "dashboard",
      "workloads",
    ]);
    expect(platformNav.every((item) => item.section === "")).toBe(true);
  });

  it("filters resource sidebar trees by workbench so AI and monitoring menus do not remain under platform", () => {
    const snapshot = buildSnapshot({
      permissionKeys: [
        "workspace.resource.view",
        "overview.view",
        "observe.ai.view",
        "observe.ai.chat",
        "observe.monitoring.view",
        "observe.alert-rules.view",
      ],
      visibleMenuIds: [
        "dashboard",
        "ai-workbench",
        "ai-workbench-chat",
        "ai-workbench-root-cause",
        "ai-workbench-performance",
        "ai-workbench-inspection",
        "ai-workbench-model-settings",
        "monitoring-workbench",
        "monitoring-workbench-overview",
        "monitoring-workbench-rules",
      ],
      visibleMenus: [
        {
          id: "dashboard",
          path: "/",
          labelZh: "概览",
          labelEn: "Overview",
          iconKey: "gauge",
          section: "platform",
          sortOrder: 1,
          enabled: true,
        },
        {
          id: "ai-workbench",
          path: "/ai-workbench",
          labelZh: "AI工作台",
          labelEn: "AI Workbench",
          iconKey: "bot",
          section: "ops",
          sortOrder: 15,
          enabled: true,
        },
        {
          id: "ai-workbench-chat",
          parentId: "ai-workbench",
          path: "/ai-workbench/chat",
          labelZh: "通用聊天",
          labelEn: "Chat",
          iconKey: "bot",
          section: "ops",
          sortOrder: 16,
          enabled: true,
        },
        {
          id: "ai-workbench-root-cause",
          parentId: "ai-workbench",
          path: "/ai-workbench/root-cause",
          labelZh: "根因分析",
          labelEn: "Root Cause",
          iconKey: "bot",
          section: "ops",
          sortOrder: 17,
          enabled: true,
        },
        {
          id: "ai-workbench-performance",
          parentId: "ai-workbench",
          path: "/ai-workbench/performance",
          labelZh: "性能分析",
          labelEn: "Performance",
          iconKey: "bot",
          section: "ops",
          sortOrder: 18,
          enabled: true,
        },
        {
          id: "ai-workbench-inspection",
          parentId: "ai-workbench",
          path: "/ai-workbench/inspection",
          labelZh: "巡检",
          labelEn: "Inspection",
          iconKey: "bot",
          section: "ops",
          sortOrder: 19,
          enabled: true,
        },
        {
          id: "ai-workbench-model-settings",
          parentId: "ai-workbench",
          path: "/ai-workbench/model-settings",
          labelZh: "模型设置",
          labelEn: "Model Settings",
          iconKey: "bot",
          section: "ops",
          sortOrder: 20,
          enabled: true,
        },
        {
          id: "monitoring-workbench",
          path: "/monitoring-workbench",
          labelZh: "监控工作台",
          labelEn: "Monitoring Workbench",
          iconKey: "gauge",
          section: "ops",
          sortOrder: 60,
          enabled: true,
        },
        {
          id: "monitoring-workbench-overview",
          parentId: "monitoring-workbench",
          path: "/monitoring-workbench/overview",
          labelZh: "工作台概览",
          labelEn: "Overview",
          iconKey: "gauge",
          section: "ops",
          sortOrder: 61,
          enabled: true,
        },
        {
          id: "monitoring-workbench-rules",
          parentId: "monitoring-workbench",
          path: "/monitoring-workbench/rules",
          labelZh: "告警规则",
          labelEn: "Alert Rules",
          iconKey: "siren",
          section: "ops",
          sortOrder: 62,
          enabled: true,
        },
      ],
    });

    const nav = getAccessibleSidebarNav(snapshot);
    const resourceNav = filterSidebarNavByWorkspace(nav, "resource");
    const platformNav = filterSidebarNavByWorkbench(resourceNav, "platform");
    const aiNav = filterSidebarNavByWorkbench(resourceNav, "ai");
    const monitoringNav = filterSidebarNavByWorkbench(
      resourceNav,
      "monitoring",
    );

    expect(platformNav.map((item) => item.id)).toEqual(["dashboard"]);
    expect(aiNav.map((item) => item.id)).toEqual(["ai-workbench"]);
    expect(monitoringNav.map((item) => item.id)).toEqual([
      "monitoring-workbench-overview",
      "monitoring-workbench-rules",
    ]);
    expect(
      monitoringNav.some((item) => item.id === "monitoring-workbench"),
    ).toBe(false);
  });

  it("derives menu workbench ownership from route mappings", () => {
    expect(getMenuWorkbenchId({ id: "dashboard", path: "/" })).toBe("platform");
    expect(
      getMenuWorkbenchId({
        id: "ai-workbench-inspection",
        path: "/ai-workbench/inspection",
      }),
    ).toBe("ai");
    expect(
      getMenuWorkbenchId({
        id: "monitoring-workbench-rules",
        path: "/monitoring-workbench/rules",
      }),
    ).toBe("monitoring");
    expect(getMenuWorkbenchId({ id: "menus", path: "/system/menus" })).toBe(
      null,
    );
  });

  it("resolves accessible workspaces and preferred landing path", () => {
    const snapshot = buildSnapshot({
      permissionKeys: [
        "workspace.application.view",
        "delivery.applications.view",
        "workspace.resource.view",
        "overview.view",
      ],
      visibleMenuIds: ["builds", "dashboard"],
      visibleMenus: [
        {
          id: "dashboard",
          path: "/",
          labelZh: "概览",
          labelEn: "Overview",
          iconKey: "gauge",
          section: "platform",
          sortOrder: 1,
          enabled: true,
        },
        {
          id: "builds",
          path: "/applications",
          labelZh: "应用中心",
          labelEn: "Applications",
          iconKey: "blocks",
          section: "deliver",
          sortOrder: 2,
          enabled: true,
        },
      ],
    });

    expect(getAccessibleWorkspaces(snapshot)).toEqual([
      "application",
      "resource",
    ]);
    expect(findPreferredWorkspace(snapshot, "application", ["ops"])).toBe(
      "application",
    );
    expect(findPreferredWorkspace(snapshot, null, ["developer"])).toBe(
      "application",
    );
    expect(findFirstAccessiblePathForWorkspace("application", snapshot)).toBe(
      "/applications",
    );
    expect(findFirstAccessiblePathForWorkspace("resource", snapshot)).toBe("/");
    expect(findLandingPath(snapshot, "application", ["ops"])).toBe(
      "/applications",
    );
  });

  it("derives cluster scope for dashboard and cluster-scoped platform pages", () => {
    expect(getRouteScopeMode(getRoute("overview"))).toBe("cluster");
    expect(getRouteScopeMode(getRoute("storage-pv"))).toBe("cluster");
    expect(getRouteScopeMode(getRoute("network-ingressclasses"))).toBe(
      "cluster",
    );
  });

  it("derives namespace scope for namespaced platform pages and detail routes", () => {
    expect(getRouteScopeMode(getRoute("workloads-pods"))).toBe("namespace");
    expect(getRouteScopeMode(getRoute("network-service-detail"))).toBe(
      "namespace",
    );
    expect(
      getRouteScopeMode(getRoute("platform-access-control-rolebindings")),
    ).toBe("namespace");
  });

  it("derives passive and hidden scope modes for non-platform workspaces", () => {
    expect(getRouteScopeMode(getRoute("applications"))).toBe("passive");
    expect(getRouteScopeMode(getRoute("system-menus"))).toBe("passive");
    expect(getRouteScopeMode(getRoute("login"))).toBe("hidden");
  });
});
