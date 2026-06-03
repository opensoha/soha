import { describe, expect, it } from "vitest";
import type { PermissionSnapshot, RouteMeta } from "@/types";
import {
  canAccessRoute,
  filterSidebarNavByWorkbench,
  filterSidebarNavByWorkspace,
  findFirstAccessiblePathForWorkbench,
  findFirstAccessiblePathForWorkspace,
  findLandingPath,
  findPreferredWorkspace,
  getAccessibleSidebarNav,
  getAccessibleWorkbenchIds,
  getAccessibleWorkspaces,
  getMenuWorkbenchId,
  getRouteScopeMode,
  getRouteWorkbenchId,
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
      visibleMenuIds: ["access-roles"],
      visibleMenus: [{ id: "access-roles", path: "/access/roles" }],
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

  it("treats settings center as a workbench with access-control, system, and setting menus", () => {
    const snapshot = buildSnapshot({
      permissionKeys: [
        "access.users.view",
        "access.roles.view",
        "access.groups.view",
        "access.policies.view",
        "system.online-users.view",
        "system.announcements.view",
        "system.menus.view",
        "system.audit.view",
        "system.operations.view",
        "settings.identity.view",
        "settings.branding.view",
      ],
      visibleMenuIds: [
        "access",
        "access-users",
        "access-roles",
        "access-teams",
        "access-policies",
        "system",
        "system-online-users",
        "announcements",
        "menus",
        "audit",
        "operations",
        "settings",
        "settings-login",
        "settings-branding",
      ],
      visibleMenus: [
        { id: "access", path: "/access", section: "admin", sortOrder: 240 },
        { id: "access-users", path: "/access/users", section: "admin", sortOrder: 226 },
        { id: "access-roles", path: "/access/roles", section: "admin", sortOrder: 227 },
        { id: "access-teams", path: "/access/teams", section: "admin", sortOrder: 228 },
        { id: "access-policies", path: "/access/policies", section: "admin", sortOrder: 229 },
        { id: "system", path: "/system", section: "admin", sortOrder: 225 },
        { id: "system-online-users", parentId: "system", path: "/system/online-users", section: "admin", sortOrder: 256 },
        { id: "announcements", parentId: "system", path: "/system/announcements", section: "admin", sortOrder: 230 },
        { id: "menus", parentId: "system", path: "/system/menus", section: "admin", sortOrder: 250 },
        { id: "audit", parentId: "system", path: "/system/audit", section: "admin", sortOrder: 258 },
        { id: "operations", parentId: "system", path: "/system/operations", section: "admin", sortOrder: 257 },
        { id: "settings", path: "/settings", section: "admin", sortOrder: 260 },
        { id: "settings-login", parentId: "settings", path: "/settings/login", section: "admin", sortOrder: 261 },
        {
          id: "settings-branding",
          parentId: "settings",
          path: "/settings/branding",
          section: "admin",
          sortOrder: 262,
        },
      ],
    });

    const systemNav = filterSidebarNavByWorkspace(
      getAccessibleSidebarNav(snapshot),
      "system",
    );
    const settingsNav = filterSidebarNavByWorkbench(systemNav, "settings");

    expect(getRouteWorkbenchId(getRoute("settings-login"))).toBe("settings");
    expect(getRouteWorkbenchId(getRoute("access-users"))).toBe("settings");
    expect(getRouteWorkbenchId(getRoute("system-menus"))).toBe("settings");
    expect(
      getMenuWorkbenchId({ id: "settings-login", path: "/settings/login" }),
    ).toBe("settings");
    expect(getMenuWorkbenchId({ id: "access", path: "/access" })).toBe(
      "settings",
    );
    expect(getMenuWorkbenchId({ id: "menus", path: "/system/menus" })).toBe(
      "settings",
    );
    expect(getAccessibleWorkbenchIds(snapshot)).toContain("settings");
    expect(findFirstAccessiblePathForWorkbench("settings", snapshot)).toBe(
      "/settings/login",
    );
    expect(settingsNav.map((item) => item.id)).toEqual([
      "access-users",
      "access-roles",
      "access-teams",
      "access-policies",
      "announcements",
      "menus",
      "system-online-users",
      "operations",
      "audit",
      "settings-login",
      "settings-branding",
    ]);
    expect(settingsNav.find((item) => item.id === "access")).toBeUndefined();
    expect(settingsNav.find((item) => item.id === "access-users")?.children).toBeUndefined();
  });

  it("does not expose settings center without a navigable settings route", () => {
    const resourceOnlySnapshot = buildSnapshot({
      permissionKeys: ["workspace.resource.view", "overview.view"],
      visibleMenuIds: ["dashboard"],
      visibleMenus: [{ id: "dashboard", path: "/" }],
    });
    const hiddenSettingsOnlySnapshot = buildSnapshot({
      permissionKeys: ["settings.ai.view"],
      visibleMenuIds: ["settings"],
      visibleMenus: [{ id: "settings", path: "/settings" }],
    });

    expect(canAccessRoute(getRoute("system"), resourceOnlySnapshot)).toBe(
      false,
    );
    expect(getAccessibleWorkbenchIds(resourceOnlySnapshot)).not.toContain(
      "settings",
    );
    expect(
      getAccessibleWorkbenchIds(hiddenSettingsOnlySnapshot),
    ).not.toContain("settings");
    expect(
      findFirstAccessiblePathForWorkbench(
        "settings",
        hiddenSettingsOnlySnapshot,
      ),
    ).toBeNull();
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
    ]);
    expect(applicationNav[0].section).toBe("deliver");
    expect(systemNav.map((item) => item.id)).toEqual(["system"]);
  });

  it("pins application center to the first delivery workbench menu row", () => {
    const snapshot = buildSnapshot({
      permissionKeys: [
        "workspace.application.view",
        "delivery.applications.view",
        "delivery.application-environments.view",
        "delivery.release-board.view",
      ],
      visibleMenuIds: ["release-board", "application-environments", "builds"],
      visibleMenus: [
        {
          id: "release-board",
          path: "/release-board",
          labelZh: "发布看板",
          labelEn: "Release Board",
          iconKey: "activity",
          section: "deliver",
          sortOrder: 1,
          enabled: true,
        },
        {
          id: "application-environments",
          path: "/application-environments",
          labelZh: "应用环境绑定",
          labelEn: "Application Bindings",
          iconKey: "blocks",
          section: "deliver",
          sortOrder: 2,
          enabled: true,
        },
        {
          id: "builds",
          path: "/applications",
          labelZh: "应用中心",
          labelEn: "Applications",
          iconKey: "blocks",
          section: "deliver",
          sortOrder: 99,
          enabled: true,
        },
      ],
    });

    const deliveryNav = filterSidebarNavByWorkbench(
      filterSidebarNavByWorkspace(getAccessibleSidebarNav(snapshot), "application"),
      "delivery",
    );

    expect(deliveryNav.map((item) => item.id)).toEqual([
      "builds",
      "release-board",
    ]);
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
        "virtualization.overview.view",
        "virtualization.vms.view",
        "virtualization.operations.view",
        "virtualization.sync.view",
        "docker.overview.view",
        "docker.hosts.view",
        "docker.projects.view",
        "docker.services.view",
        "docker.ports.view",
        "docker.templates.view",
        "docker.operations.view",
        "observe.ai.view",
        "observe.ai.chat",
        "ai.gateway.view",
        "ai.gateway.manage",
        "observe.monitoring.view",
        "observe.alert-integrations.view",
        "observe.alert-rules.view",
      ],
      visibleMenuIds: [
        "dashboard",
        "virtualization-workbench",
        "virtualization-workbench-overview",
        "virtualization-workbench-vms",
        "virtualization-workbench-operations",
        "virtualization-workbench-sync",
        "docker-workbench",
        "docker-workbench-overview",
        "docker-workbench-hosts",
        "docker-workbench-projects",
        "docker-workbench-templates",
        "docker-workbench-operations",
        "ai-workbench",
        "ai-workbench-chat",
        "ai-workbench-root-cause",
        "ai-workbench-performance",
        "ai-workbench-inspection",
        "ai-workbench-tool-settings",
        "ai-workbench-model-settings",
        "ai-gateway",
        "ai-gateway-overview",
        "ai-gateway-manifest",
        "ai-gateway-clients",
        "ai-gateway-tokens",
        "ai-gateway-governance",
        "ai-gateway-call-logs",
        "monitoring-workbench",
        "monitoring-workbench-overview",
        "monitoring-workbench-integrations",
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
          id: "virtualization-workbench",
          path: "/virtualization",
          labelZh: "虚拟化",
          labelEn: "Virtualization",
          iconKey: "server",
          section: "ops",
          sortOrder: 10,
          enabled: true,
        },
        {
          id: "virtualization-workbench-overview",
          parentId: "virtualization-workbench",
          path: "/virtualization/overview",
          labelZh: "总览",
          labelEn: "Overview",
          iconKey: "server",
          section: "ops",
          sortOrder: 11,
          enabled: true,
        },
        {
          id: "virtualization-workbench-vms",
          parentId: "virtualization-workbench",
          path: "/virtualization/vms",
          labelZh: "虚拟机",
          labelEn: "VMs",
          iconKey: "server",
          section: "ops",
          sortOrder: 12,
          enabled: true,
        },
        {
          id: "virtualization-workbench-operations",
          parentId: "virtualization-workbench",
          path: "/virtualization/operations",
          labelZh: "操作记录",
          labelEn: "Operations",
          iconKey: "file-clock",
          section: "ops",
          sortOrder: 13,
          enabled: true,
        },
        {
          id: "virtualization-workbench-sync",
          parentId: "virtualization-workbench",
          path: "/virtualization/sync",
          labelZh: "同步任务",
          labelEn: "Sync",
          iconKey: "activity",
          section: "ops",
          sortOrder: 14,
          enabled: true,
        },
        {
          id: "docker-workbench",
          path: "/docker",
          labelZh: "Docker 工作台",
          labelEn: "Docker Workbench",
          iconKey: "docker",
          section: "ops",
          sortOrder: 30,
          enabled: true,
        },
        {
          id: "docker-workbench-overview",
          parentId: "docker-workbench",
          path: "/docker/overview",
          labelZh: "总览",
          labelEn: "Overview",
          iconKey: "gauge",
          section: "ops",
          sortOrder: 31,
          enabled: true,
        },
        {
          id: "docker-workbench-hosts",
          parentId: "docker-workbench",
          path: "/docker/hosts",
          labelZh: "Docker 主机",
          labelEn: "Docker Hosts",
          iconKey: "server",
          section: "ops",
          sortOrder: 32,
          enabled: true,
        },
        {
          id: "docker-workbench-projects",
          parentId: "docker-workbench",
          path: "/docker/projects",
          labelZh: "容器管理",
          labelEn: "Container Management",
          iconKey: "docker",
          section: "ops",
          sortOrder: 33,
          enabled: true,
        },
        {
          id: "docker-workbench-templates",
          parentId: "docker-workbench",
          path: "/docker/templates",
          labelZh: "模板",
          labelEn: "Templates",
          iconKey: "code",
          section: "ops",
          sortOrder: 36,
          enabled: true,
        },
        {
          id: "docker-workbench-operations",
          parentId: "docker-workbench",
          path: "/docker/operations",
          labelZh: "操作记录",
          labelEn: "Operations",
          iconKey: "history",
          section: "ops",
          sortOrder: 37,
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
          id: "ai-workbench-tool-settings",
          parentId: "ai-workbench",
          path: "/ai-workbench/tool-settings",
          labelZh: "工具与技能",
          labelEn: "Tools & Skills",
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
          sortOrder: 21,
          enabled: true,
        },
        {
          id: "ai-gateway",
          path: "/ai-gateway",
          labelZh: "AI Gateway",
          labelEn: "AI Gateway",
          iconKey: "shield",
          section: "ops",
          sortOrder: 22,
          enabled: true,
        },
        {
          id: "ai-gateway-overview",
          parentId: "ai-gateway",
          path: "/ai-gateway/overview",
          labelZh: "概览",
          labelEn: "Overview",
          iconKey: "gauge",
          section: "ops",
          sortOrder: 23,
          enabled: true,
        },
        {
          id: "ai-gateway-manifest",
          parentId: "ai-gateway",
          path: "/ai-gateway/manifest",
          labelZh: "能力清单",
          labelEn: "Manifest",
          iconKey: "shield",
          section: "ops",
          sortOrder: 24,
          enabled: true,
        },
        {
          id: "ai-gateway-clients",
          parentId: "ai-gateway",
          path: "/ai-gateway/clients",
          labelZh: "AI Clients",
          labelEn: "AI Clients",
          iconKey: "link",
          section: "ops",
          sortOrder: 25,
          enabled: true,
        },
        {
          id: "ai-gateway-tokens",
          parentId: "ai-gateway",
          path: "/ai-gateway/tokens",
          labelZh: "Tokens",
          labelEn: "Tokens",
          iconKey: "key",
          section: "ops",
          sortOrder: 26,
          enabled: true,
        },
        {
          id: "ai-gateway-governance",
          parentId: "ai-gateway",
          path: "/ai-gateway/governance",
          labelZh: "Governance",
          labelEn: "Governance",
          iconKey: "shield",
          section: "ops",
          sortOrder: 27,
          enabled: true,
        },
        {
          id: "ai-gateway-call-logs",
          parentId: "ai-gateway",
          path: "/ai-gateway/call-logs",
          labelZh: "调用日志",
          labelEn: "Call Logs",
          iconKey: "history",
          section: "ops",
          sortOrder: 28,
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
          labelZh: "总览",
          labelEn: "Overview",
          iconKey: "gauge",
          section: "ops",
          sortOrder: 61,
          enabled: true,
        },
        {
          id: "monitoring-workbench-integrations",
          parentId: "monitoring-workbench",
          path: "/monitoring-workbench/integrations",
          labelZh: "告警集成",
          labelEn: "Alert Integrations",
          iconKey: "link",
          section: "ops",
          sortOrder: 62,
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
          sortOrder: 63,
          enabled: true,
        },
      ],
    });

    const nav = getAccessibleSidebarNav(snapshot);
    const resourceNav = filterSidebarNavByWorkspace(nav, "resource");
    const platformNav = filterSidebarNavByWorkbench(resourceNav, "platform");
    const aiNav = filterSidebarNavByWorkbench(resourceNav, "ai");
    const virtualizationNav = filterSidebarNavByWorkbench(
      resourceNav,
      "virtualization",
    );
    const dockerNav = filterSidebarNavByWorkbench(resourceNav, "docker");
    const aiGatewayNav = filterSidebarNavByWorkbench(resourceNav, "aiGateway");
    const monitoringNav = filterSidebarNavByWorkbench(
      resourceNav,
      "monitoring",
    );

    expect(platformNav.map((item) => item.id)).toEqual(["dashboard"]);
    expect(aiNav.map((item) => item.id)).toEqual(["ai-workbench"]);
    expect(virtualizationNav.map((item) => item.id)).toEqual([
      "virtualization-workbench-overview",
      "virtualization-workbench-vms",
      "virtualization-workbench-operations",
      "virtualization-workbench-sync",
    ]);
    expect(
      virtualizationNav.some((item) => item.id === "virtualization-workbench"),
    ).toBe(false);
    expect(dockerNav.map((item) => item.id)).toEqual([
      "docker-workbench-overview",
      "docker-workbench-hosts",
      "docker-workbench-projects",
      "docker-workbench-templates",
      "docker-workbench-operations",
    ]);
    expect(dockerNav.some((item) => item.id === "docker-workbench")).toBe(
      false,
    );
    expect(aiGatewayNav.map((item) => item.id)).toEqual([
      "ai-gateway-overview",
      "ai-gateway-manifest",
      "ai-gateway-clients",
      "ai-gateway-tokens",
      "ai-gateway-governance",
      "ai-gateway-call-logs",
    ]);
    expect(aiGatewayNav.some((item) => item.id === "ai-gateway")).toBe(false);
    expect(monitoringNav.map((item) => item.id)).toEqual([
      "monitoring-workbench-overview",
      "monitoring-workbench-integrations",
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
    expect(
      getMenuWorkbenchId({
        id: "monitoring-workbench-integrations",
        path: "/monitoring-workbench/integrations",
      }),
    ).toBe("monitoring");
    expect(
      getMenuWorkbenchId({
        id: "virtualization-workbench-vms",
        path: "/virtualization/vms",
      }),
    ).toBe("virtualization");
    expect(
      getMenuWorkbenchId({
        id: "virtualization-workbench-sync",
        path: "/virtualization/sync",
      }),
    ).toBe("virtualization");
    expect(
      getMenuWorkbenchId({
        id: "docker-workbench-projects",
        path: "/docker/projects",
      }),
    ).toBe("docker");
    expect(
      getMenuWorkbenchId({
        id: "ai-gateway",
        path: "/ai-gateway",
      }),
    ).toBe("aiGateway");
    expect(
      getMenuWorkbenchId({
        id: "ai-gateway-governance",
        path: "/ai-gateway/governance",
      }),
    ).toBe("aiGateway");
    expect(getMenuWorkbenchId({ id: "menus", path: "/system/menus" })).toBe(
      "settings",
    );
  });

  it("requires resource workspace, AI Gateway view permission, and menu binding", () => {
    const route = getRoute("ai-gateway-overview");
    const parentRoute = getRoute("ai-gateway");
    const allowedSnapshot = buildSnapshot({
      permissionKeys: ["workspace.resource.view", "ai.gateway.view"],
      visibleMenuIds: ["ai-gateway", "ai-gateway-overview"],
      visibleMenus: [
        {
          id: "ai-gateway",
          path: "/ai-gateway",
        },
        {
          id: "ai-gateway-overview",
          parentId: "ai-gateway",
          path: "/ai-gateway/overview",
        },
      ],
    });

    expect(getRouteWorkspace(route)).toBe("resource");
    expect(getRouteWorkbenchId(route)).toBe("aiGateway");
    expect(getRouteScopeMode(route)).toBe("passive");
    expect(canAccessRoute(route, allowedSnapshot)).toBe(true);
    expect(parentRoute.redirectTo).toBe("/ai-gateway/overview");
    expect(canAccessRoute(parentRoute, allowedSnapshot)).toBe(true);
    expect(
      findFirstAccessiblePathForWorkbench("aiGateway", allowedSnapshot),
    ).toBe("/ai-gateway/overview");

    const compatRoute = getRoute("ai-workbench-gateway-compat");
    expect(compatRoute.navVisible).toBe(false);
    expect(compatRoute.redirectTo).toBe("/ai-gateway/overview");
    expect(getRouteWorkbenchId(compatRoute)).toBe("aiGateway");
    expect(canAccessRoute(compatRoute, allowedSnapshot)).toBe(true);
    expect(
      canAccessRoute(
        route,
        buildSnapshot({
          permissionKeys: ["ai.gateway.view"],
          visibleMenuIds: ["ai-gateway", "ai-gateway-overview"],
          visibleMenus: [
            {
              id: "ai-gateway",
              path: "/ai-gateway",
            },
            {
              id: "ai-gateway-overview",
              parentId: "ai-gateway",
              path: "/ai-gateway/overview",
            },
          ],
        }),
      ),
    ).toBe(false);
    expect(
      canAccessRoute(
        route,
        buildSnapshot({
          permissionKeys: ["workspace.resource.view"],
          visibleMenuIds: ["ai-gateway", "ai-gateway-overview"],
          visibleMenus: [
            {
              id: "ai-gateway",
              path: "/ai-gateway",
            },
            {
              id: "ai-gateway-overview",
              parentId: "ai-gateway",
              path: "/ai-gateway/overview",
            },
          ],
        }),
      ),
    ).toBe(false);
    expect(
      canAccessRoute(
        route,
        buildSnapshot({
          permissionKeys: ["workspace.resource.view", "ai.gateway.view"],
          visibleMenuIds: [],
          visibleMenus: [],
        }),
      ),
    ).toBe(false);
  });

  it("allows AI Gateway token routing from invoke-only permission", () => {
    const tokenRoute = getRoute("ai-gateway-tokens");
    const parentRoute = getRoute("ai-gateway");
    const snapshot = buildSnapshot({
      permissionKeys: ["workspace.resource.view", "ai.gateway.invoke"],
      visibleMenuIds: ["ai-gateway", "ai-gateway-tokens"],
      visibleMenus: [
        {
          id: "ai-gateway",
          path: "/ai-gateway",
        },
        {
          id: "ai-gateway-tokens",
          parentId: "ai-gateway",
          path: "/ai-gateway/tokens",
        },
      ],
    });

    expect(canAccessRoute(tokenRoute, snapshot)).toBe(true);
    expect(canAccessRoute(parentRoute, snapshot)).toBe(true);
    expect(findFirstAccessiblePathForWorkbench("aiGateway", snapshot)).toBe(
      "/ai-gateway/tokens",
    );
    expect(canAccessRoute(getRoute("ai-gateway-overview"), snapshot)).toBe(
      false,
    );
  });

  it("requires AI Gateway manage permission for call logs", () => {
    const route = getRoute("ai-gateway-call-logs");
    const snapshot = buildSnapshot({
      permissionKeys: ["workspace.resource.view", "ai.gateway.manage"],
      visibleMenuIds: ["ai-gateway", "ai-gateway-call-logs"],
      visibleMenus: [
        {
          id: "ai-gateway",
          path: "/ai-gateway",
        },
        {
          id: "ai-gateway-call-logs",
          parentId: "ai-gateway",
          path: "/ai-gateway/call-logs",
        },
      ],
    });

    expect(getRouteWorkbenchId(route)).toBe("aiGateway");
    expect(canAccessRoute(route, snapshot)).toBe(true);
    expect(
      canAccessRoute(
        route,
        buildSnapshot({
          permissionKeys: ["workspace.resource.view", "ai.gateway.view"],
          visibleMenuIds: ["ai-gateway", "ai-gateway-call-logs"],
          visibleMenus: snapshot.visibleMenus,
        }),
      ),
    ).toBe(false);
  });

  it("requires virtualization workspace permission, route permission, and menu binding", () => {
    const route = getRoute("virtualization-workbench-vms");
    const allowedSnapshot = buildSnapshot({
      permissionKeys: ["workspace.resource.view", "virtualization.vms.view"],
      visibleMenuIds: ["virtualization-workbench-vms"],
      visibleMenus: [
        {
          id: "virtualization-workbench-vms",
          parentId: "virtualization-workbench",
          path: "/virtualization/vms",
        },
      ],
    });

    expect(getRouteWorkspace(route)).toBe("resource");
    expect(getRouteWorkbenchId(route)).toBe("virtualization");
    expect(getRouteScopeMode(route)).toBe("passive");
    expect(canAccessRoute(route, allowedSnapshot)).toBe(true);
    expect(
      canAccessRoute(
        route,
        buildSnapshot({
          permissionKeys: ["workspace.resource.view", "virtualization.vms.view"],
          visibleMenuIds: [],
          visibleMenus: [],
        }),
      ),
    ).toBe(false);
    expect(
      canAccessRoute(
        route,
        buildSnapshot({
          permissionKeys: ["workspace.resource.view"],
          visibleMenuIds: ["virtualization-workbench-vms"],
          visibleMenus: [
            {
              id: "virtualization-workbench-vms",
              parentId: "virtualization-workbench",
              path: "/virtualization/vms",
            },
          ],
        }),
      ),
    ).toBe(false);
  });

  it("maps virtualization sync to backend menu id and view permission", () => {
    const route = getRoute("virtualization-workbench-sync");
    const snapshot = buildSnapshot({
      permissionKeys: ["workspace.resource.view", "virtualization.sync.view"],
      visibleMenuIds: ["virtualization-workbench-sync"],
      visibleMenus: [
        {
          id: "virtualization-workbench-sync",
          parentId: "virtualization-workbench",
          path: "/virtualization/sync",
        },
      ],
    });

    expect(route.menuId).toBe("virtualization-workbench-sync");
    expect(route.permissionKey).toBe("virtualization.sync.view");
    expect(getRouteWorkbenchId(route)).toBe("virtualization");
    expect(getRouteScopeMode(route)).toBe("passive");
    expect(canAccessRoute(route, snapshot)).toBe(true);
  });

  it("requires Docker workspace permission, route permission, and menu binding", () => {
    const route = getRoute("docker-workbench-projects");
    const allowedSnapshot = buildSnapshot({
      permissionKeys: ["workspace.resource.view", "docker.projects.view"],
      visibleMenuIds: ["docker-workbench-projects"],
      visibleMenus: [
        {
          id: "docker-workbench-projects",
          parentId: "docker-workbench",
          path: "/docker/projects",
        },
      ],
    });

    expect(getRouteWorkspace(route)).toBe("resource");
    expect(getRouteWorkbenchId(route)).toBe("docker");
    expect(getRouteScopeMode(route)).toBe("passive");
    expect(canAccessRoute(route, allowedSnapshot)).toBe(true);
    expect(
      canAccessRoute(
        route,
        buildSnapshot({
          permissionKeys: ["workspace.resource.view", "docker.projects.view"],
          visibleMenuIds: [],
          visibleMenus: [],
        }),
      ),
    ).toBe(false);
    expect(
      canAccessRoute(
        route,
        buildSnapshot({
          permissionKeys: ["workspace.resource.view"],
          visibleMenuIds: ["docker-workbench-projects"],
          visibleMenus: [
            {
              id: "docker-workbench-projects",
              parentId: "docker-workbench",
              path: "/docker/projects",
            },
          ],
        }),
      ),
    ).toBe(false);
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
    expect(getRouteScopeMode(getRoute("network-gateway-api-gatewayclasses"))).toBe(
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
