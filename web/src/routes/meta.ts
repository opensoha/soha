import {
  getMenuSectionOrder,
  normalizeMenuSection,
} from "@/features/system/menu-schema";
import { routeMeta } from "./route-meta-data";
import type {
  BusinessWorkspaceType,
  PermissionSnapshot,
  RouteMeta,
  RuntimeMenuNode,
  VisibleMenu,
  WorkspaceType,
} from "@/types";

const WORKSPACE_PERMISSION_KEYS: Record<BusinessWorkspaceType, string> = {
  application: "workspace.application.view",
  resource: "workspace.resource.view",
};

const DEFAULT_WORKSPACE_PATHS: Record<BusinessWorkspaceType, string> = {
  application: "/applications",
  resource: "/",
};

const RESOURCE_DEFAULT_ROLES = new Set(["admin", "ops", "readonly", "auditor"]);
const APPLICATION_PATH_PREFIXES = [
  "/applications",
  "/application-management",
  "/application-environments",
  "/build-templates",
  "/delivery/blueprints",
  "/delivery/release-bundles",
  "/delivery/execution-tasks",
  "/delivery/approval-policies",
  "/workflow-templates",
  "/release-board",
  "/workflows",
  "/releases",
  "/registries",
];

const WORKBENCH_DEFAULT_PATHS = {
  platform: "/",
  virtualization: "/virtualization",
  docker: "/docker",
  delivery: "/applications",
  ai: "/ai-workbench",
  aiGateway: "/ai-gateway/overview",
  monitoring: "/monitoring-workbench",
  settings: "/settings",
} as const;

export type WorkbenchId = keyof typeof WORKBENCH_DEFAULT_PATHS;

function matchesRoutePrefix(pathname: string, prefixes: string[]) {
  return prefixes.some((prefix) => pathname.startsWith(prefix));
}

export { routeMeta } from "./route-meta-data";

export function getRouteMeta(pathname: string): RouteMeta {
  const candidates = [...routeMeta].sort(
    (a, b) => b.path.length - a.path.length,
  );
  return (
    candidates.find((r) => {
      if (r.path === "/") return pathname === "/";
      const routeSegments = r.path.split("/").filter(Boolean);
      const pathSegments = pathname.split("/").filter(Boolean);
      if (routeSegments.length > pathSegments.length) return false;
      return routeSegments.every(
        (segment, index) =>
          segment.startsWith(":") || segment === pathSegments[index],
      );
    }) ?? routeMeta[0]
  );
}

export function getParentRouteMeta(route: RouteMeta): RouteMeta | null {
  return route.parentId
    ? (routeMeta.find((r) => r.id === route.parentId) ?? null)
    : null;
}

export function resolveRoutePermission(route: RouteMeta): string | undefined {
  if (route.permissionKey) {
    return route.permissionKey;
  }
  const parent = getParentRouteMeta(route);
  return parent ? resolveRoutePermission(parent) : undefined;
}

export function resolveRouteMenuId(route: RouteMeta): string | undefined {
  if (route.menuId) {
    return route.menuId;
  }
  const parent = getParentRouteMeta(route);
  return parent ? resolveRouteMenuId(parent) : undefined;
}

function deriveWorkspaceFromPath(
  pathname: string,
  requiresAuth: boolean,
): WorkspaceType | null {
  if (
    pathname === "/login" ||
    pathname.startsWith("/auth/") ||
    pathname.startsWith("/login/") ||
    pathname.startsWith("/account/")
  ) {
    return null;
  }
  if (
    pathname.startsWith("/access") ||
    pathname.startsWith("/system") ||
    pathname.startsWith("/settings")
  ) {
    return "system";
  }
  if (matchesRoutePrefix(pathname, APPLICATION_PATH_PREFIXES)) {
    return "application";
  }
  return requiresAuth ? "resource" : null;
}

export function getRouteWorkspace(route: RouteMeta): WorkspaceType | null {
  if (route.workspace) {
    return route.workspace;
  }
  const parent = getParentRouteMeta(route);
  if (parent) {
    return getRouteWorkspace(parent);
  }
  return deriveWorkspaceFromPath(route.path, route.requiresAuth);
}

function deriveWorkbenchIdFromPath(pathname: string): WorkbenchId | null {
  if (
    pathname === "/" ||
    pathname.startsWith("/cluster-resources") ||
    pathname.startsWith("/workloads") ||
    pathname.startsWith("/configuration") ||
    pathname.startsWith("/network") ||
    pathname.startsWith("/storage") ||
    pathname.startsWith("/platform-access-control") ||
    pathname.startsWith("/helm") ||
    pathname.startsWith("/extensions") ||
    pathname.startsWith("/clusters")
  ) {
    return "platform";
  }
  if (matchesRoutePrefix(pathname, APPLICATION_PATH_PREFIXES)) {
    return "delivery";
  }
  if (pathname.startsWith("/virtualization")) {
    return "virtualization";
  }
  if (pathname.startsWith("/docker")) {
    return "docker";
  }
  if (pathname.startsWith("/ai-gateway")) {
    return "aiGateway";
  }
  if (
    pathname.startsWith("/ai-workbench") ||
    pathname.startsWith("/ai-observe") ||
    pathname.startsWith("/chat")
  ) {
    return "ai";
  }
  if (
    pathname.startsWith("/monitoring-workbench") ||
    pathname.startsWith("/observability")
  ) {
    return "monitoring";
  }
  if (pathname.startsWith("/system") || pathname.startsWith("/settings")) {
    return "settings";
  }
  return null;
}

function rankMenuRouteCandidates(candidates: RouteMeta[], menuPath: string) {
  return [...candidates].sort((left, right) => {
    const leftExactPath = left.path === menuPath ? 0 : 1;
    const rightExactPath = right.path === menuPath ? 0 : 1;
    if (leftExactPath !== rightExactPath) return leftExactPath - rightExactPath;

    const leftNavVisible = left.navVisible ? 0 : 1;
    const rightNavVisible = right.navVisible ? 0 : 1;
    if (leftNavVisible !== rightNavVisible)
      return leftNavVisible - rightNavVisible;

    const leftRedirect = left.redirectTo ? 1 : 0;
    const rightRedirect = right.redirectTo ? 1 : 0;
    if (leftRedirect !== rightRedirect) return leftRedirect - rightRedirect;

    return left.path.localeCompare(right.path);
  });
}

function findBestRouteForMenuMeta(menu: Pick<VisibleMenu, "id" | "path">) {
  const candidates = routeMeta.filter((route) => {
    const routeMenuId = resolveRouteMenuId(route);
    return routeMenuId === menu.id || route.path === menu.path;
  });
  if (candidates.length === 0) {
    return undefined;
  }
  return rankMenuRouteCandidates(candidates, menu.path)[0];
}

export function getMenuWorkspace(
  menu: Pick<VisibleMenu, "id" | "path">,
): WorkspaceType | null {
  const route = findBestRouteForMenuMeta(menu);
  if (route) {
    return getRouteWorkspace(route);
  }
  return deriveWorkspaceFromPath(menu.path, true);
}

export function getMenuWorkbenchId(
  menu: Pick<VisibleMenu, "id" | "path">,
): WorkbenchId | null {
  const route = findBestRouteForMenuMeta(menu);
  if (route) {
    return getRouteWorkbenchId(route);
  }
  return deriveWorkbenchIdFromPath(menu.path);
}

export function getRouteWorkbenchId(route: RouteMeta): WorkbenchId | null {
  if (route.workbenchId && route.workbenchId in WORKBENCH_DEFAULT_PATHS) {
    return route.workbenchId as WorkbenchId;
  }
  const parent = getParentRouteMeta(route);
  if (parent) {
    return getRouteWorkbenchId(parent);
  }
  return deriveWorkbenchIdFromPath(route.path);
}

export function getRouteScopeMode(
  route: RouteMeta,
): NonNullable<RouteMeta["scopeMode"]> {
  if (route.scopeMode) {
    return route.scopeMode;
  }
  const parent = getParentRouteMeta(route);
  if (parent) {
    return getRouteScopeMode(parent);
  }
  const pathname = route.path;
  if (
    pathname === "/login" ||
    pathname.startsWith("/auth/") ||
    pathname.startsWith("/login/")
  ) {
    return "hidden";
  }
  if (
    pathname === "/" ||
    pathname === "/clusters" ||
    pathname.startsWith("/cluster-resources/nodes") ||
    pathname.startsWith("/cluster-resources/namespaces") ||
    pathname.startsWith("/extensions") ||
    pathname.startsWith("/platform-access-control/clusterroles") ||
    pathname.startsWith("/platform-access-control/clusterrolebindings") ||
    pathname.startsWith("/storage/persistentvolumes") ||
    pathname.startsWith("/storage/storageclasses")
  ) {
    return "cluster";
  }
  if (
    pathname.startsWith("/access") ||
    pathname.startsWith("/system") ||
    pathname.startsWith("/settings") ||
    pathname.startsWith("/applications") ||
    pathname.startsWith("/application-environments") ||
    pathname.startsWith("/build-templates") ||
    pathname.startsWith("/delivery/blueprints") ||
    pathname.startsWith("/delivery/release-bundles") ||
    pathname.startsWith("/delivery/execution-tasks") ||
    pathname.startsWith("/delivery/approval-policies") ||
    pathname.startsWith("/workflow-templates") ||
    pathname.startsWith("/release-board") ||
    pathname.startsWith("/releases") ||
    pathname.startsWith("/registries") ||
    pathname.startsWith("/monitoring-workbench") ||
    pathname.startsWith("/observability") ||
    pathname.startsWith("/virtualization") ||
    pathname.startsWith("/docker") ||
    pathname.startsWith("/ai-gateway") ||
    pathname.startsWith("/ai-workbench") ||
    pathname.startsWith("/ai-observe") ||
    pathname.startsWith("/account")
  ) {
    return "passive";
  }
  return "namespace";
}

export function getAccessibleWorkbenchIds(
  snapshot?: PermissionSnapshot | null,
): Array<keyof typeof WORKBENCH_DEFAULT_PATHS> {
  const seen = new Set<keyof typeof WORKBENCH_DEFAULT_PATHS>();
  for (const route of routeMeta) {
    const workbenchId = getRouteWorkbenchId(route);
    if (!workbenchId || seen.has(workbenchId)) {
      continue;
    }
    if (!route.requiresAuth || !canAccessRoute(route, snapshot)) {
      continue;
    }
    seen.add(workbenchId);
  }
  return Array.from(seen).filter((workbenchId) =>
    Boolean(findFirstAccessiblePathForWorkbench(workbenchId, snapshot)),
  );
}

export function findFirstAccessiblePathForWorkbench(
  workbenchId: keyof typeof WORKBENCH_DEFAULT_PATHS,
  snapshot?: PermissionSnapshot | null,
): string | null {
  const defaultPath = WORKBENCH_DEFAULT_PATHS[workbenchId];
  const defaultRoute = routeMeta.find((route) => route.path === defaultPath);
  if (defaultRoute && canAccessRoute(defaultRoute, snapshot)) {
    const defaultAccessiblePath = resolveAccessibleRoutePath(
      defaultRoute,
      snapshot,
    );
    if (defaultAccessiblePath) {
      return defaultAccessiblePath;
    }
  }
  for (const route of routeMeta) {
    if (
      route.requiresAuth &&
      route.navVisible &&
      getRouteWorkbenchId(route) === workbenchId &&
      canAccessRoute(route, snapshot)
    ) {
      const accessiblePath = resolveAccessibleRoutePath(route, snapshot);
      if (accessiblePath) {
        return accessiblePath;
      }
    }
  }
  return null;
}

function resolveAccessibleRoutePath(
  route: RouteMeta,
  snapshot?: PermissionSnapshot | null,
): string | null {
  if (!route.redirectTo) {
    return route.path;
  }
  const redirectRoute = routeMeta.find((item) => item.path === route.redirectTo);
  if (!redirectRoute) {
    return route.redirectTo;
  }
  return canAccessRoute(redirectRoute, snapshot) ? route.redirectTo : null;
}

export function getWorkspacePermissionKey(workspace: BusinessWorkspaceType) {
  return WORKSPACE_PERMISSION_KEYS[workspace];
}

export function hasWorkspaceAccess(
  workspace: BusinessWorkspaceType,
  snapshot?: PermissionSnapshot | null,
) {
  return (
    snapshot?.permissionKeys.includes(WORKSPACE_PERMISSION_KEYS[workspace]) ??
    false
  );
}

export function getAccessibleWorkspaces(
  snapshot?: PermissionSnapshot | null,
): BusinessWorkspaceType[] {
  const workspaces: BusinessWorkspaceType[] = [];
  (["application", "resource"] as const).forEach((workspace) => {
    if (!hasWorkspaceAccess(workspace, snapshot)) {
      return;
    }
    if (findFirstAccessiblePathForWorkspace(workspace, snapshot)) {
      workspaces.push(workspace);
    }
  });
  return workspaces;
}

export function getDefaultWorkspaceForRoles(
  roles: string[] = [],
): BusinessWorkspaceType {
  return roles.some((role) =>
    RESOURCE_DEFAULT_ROLES.has(String(role || "").trim()),
  )
    ? "resource"
    : "application";
}

export function findPreferredWorkspace(
  snapshot?: PermissionSnapshot | null,
  persistedWorkspace?: BusinessWorkspaceType | null,
  roles: string[] = [],
): BusinessWorkspaceType | null {
  const accessibleWorkspaces = getAccessibleWorkspaces(snapshot);
  if (accessibleWorkspaces.length === 0) {
    return null;
  }
  if (persistedWorkspace && accessibleWorkspaces.includes(persistedWorkspace)) {
    return persistedWorkspace;
  }
  const roleDefault = getDefaultWorkspaceForRoles(roles);
  if (accessibleWorkspaces.includes(roleDefault)) {
    return roleDefault;
  }
  return accessibleWorkspaces[0];
}

export function canAccessRoute(
  route: RouteMeta,
  snapshot?: PermissionSnapshot | null,
): boolean {
  if (!route.requiresAuth) {
    return true;
  }
  if (!snapshot) {
    return false;
  }
  if (route.permissionStrategy === "any-child") {
    return routeMeta
      .filter((child) => child.parentId === route.id)
      .some((child) => canAccessRoute(child, snapshot));
  }
  const permissionKey = resolveRoutePermission(route);
  const permissionKeysAny = route.permissionKeysAny ?? [];
  const menuId = resolveRouteMenuId(route);
  const workspace = getRouteWorkspace(route);
  const hasWorkspacePermission =
    workspace === "application" || workspace === "resource"
      ? hasWorkspaceAccess(workspace, snapshot)
      : true;
  const hasPermission = permissionKeysAny.length > 0
    ? permissionKeysAny.some((key) => snapshot.permissionKeys.includes(key))
    : !permissionKey || snapshot.permissionKeys.includes(permissionKey);
  const hasMenu = !menuId || snapshot.visibleMenuIds.includes(menuId);
  return hasWorkspacePermission && hasPermission && hasMenu;
}

function getSectionOrder(section?: string) {
  const normalized = normalizeMenuSection(String(section || ""));
  return normalized ? getMenuSectionOrder(normalized) : -1;
}

function sortRuntimeMenuTree(items: RuntimeMenuNode[]): RuntimeMenuNode[] {
  return [...items]
    .sort((left, right) => {
      const sectionCompare =
        getSectionOrder(left.section) - getSectionOrder(right.section);
      if (sectionCompare !== 0) return sectionCompare;
      if (left.sortOrder !== right.sortOrder)
        return left.sortOrder - right.sortOrder;
      return left.path.localeCompare(right.path);
    })
    .map(
      (item): RuntimeMenuNode => ({
        ...item,
        children:
          item.children && item.children.length > 0
            ? sortRuntimeMenuTree(item.children)
            : undefined,
      }),
    );
}

const APPLICATION_SECTION_ORDER: Record<string, number> = {
  builds: 10,
  applications: 10,
  "application-management": 15,
  "application-environments": 20,
  "release-board": 30,
  workflows: 40,
  releases: 50,
  "build-templates": 60,
  "approval-policies": 70,
  "release-bundles": 80,
  "execution-tasks": 90,
  registries: 100,
};

const SYSTEM_ROOT_ORDER: Record<string, number> = {
  access: 10,
  "access-users": 11,
  "access-roles": 12,
  "access-teams": 13,
  "access-policies": 14,
  system: 20,
  settings: 30,
};

function deriveRuntimeMenuSection(node: RuntimeMenuNode): string {
  return normalizeMenuSection(node.section || "");
}

function deriveRuntimeMenuSortOrder(
  node: RuntimeMenuNode,
  workspace: WorkspaceType | null,
): number {
  if (workspace === "application" && node.id in APPLICATION_SECTION_ORDER) {
    return APPLICATION_SECTION_ORDER[node.id];
  }
  if (
    workspace === "system" &&
    (!node.parentId || node.parentId === "access") &&
    node.id in SYSTEM_ROOT_ORDER
  ) {
    return SYSTEM_ROOT_ORDER[node.id];
  }
  return node.sortOrder;
}

function findBestRouteForMenu(
  menu: VisibleMenu,
  snapshot?: PermissionSnapshot | null,
): RouteMeta | undefined {
  const candidates = routeMeta
    .filter((route) => {
      const routeMenuId = resolveRouteMenuId(route);
      return routeMenuId === menu.id || route.path === menu.path;
    })
    .filter((route) => canAccessRoute(route, snapshot));

  if (candidates.length === 0) return undefined;

  return rankMenuRouteCandidates(candidates, menu.path)[0];
}

function buildRuntimeMenuTree(
  snapshot?: PermissionSnapshot | null,
): RuntimeMenuNode[] {
  const visibleMenus = snapshot?.visibleMenus ?? [];
  const nodes = new Map<string, RuntimeMenuNode>();

  visibleMenus.forEach((menu) => {
    const route = findBestRouteForMenu(menu, snapshot);
    nodes.set(menu.id, {
      id: menu.id,
      parentId: menu.parentId,
      path: menu.path,
      labelZh: menu.labelZh || menu.id,
      labelEn: menu.labelEn || menu.labelZh || menu.id,
      iconKey: menu.iconKey || "",
      section: normalizeMenuSection(menu.section || ""),
      sortOrder: typeof menu.sortOrder === "number" ? menu.sortOrder : 0,
      enabled: menu.enabled ?? true,
      workspace: route ? (getRouteWorkspace(route) ?? undefined) : undefined,
      workbenchId: route
        ? (getRouteWorkbenchId(route) ?? undefined)
        : undefined,
      route,
      children: [],
    });
  });

  const roots: RuntimeMenuNode[] = [];
  nodes.forEach((node) => {
    if (node.parentId && nodes.has(node.parentId)) {
      nodes.get(node.parentId)?.children?.push(node);
      return;
    }
    roots.push(node);
  });

  const prune = (node: RuntimeMenuNode): RuntimeMenuNode | null => {
    const nextChildren = (node.children ?? [])
      .map(prune)
      .filter((item): item is RuntimeMenuNode => Boolean(item));
    const keepAsContainer = nextChildren.length > 0;
    const keepAsLeaf = Boolean(node.route);
    if (!keepAsContainer && !keepAsLeaf) {
      return null;
    }
    return {
      ...node,
      children: nextChildren.length > 0 ? nextChildren : undefined,
    };
  };

  return sortRuntimeMenuTree(
    roots.map(prune).filter((item): item is RuntimeMenuNode => Boolean(item)),
  );
}

export function getAccessibleSidebarNav(
  snapshot?: PermissionSnapshot | null,
): RuntimeMenuNode[] {
  return buildRuntimeMenuTree(snapshot);
}

export function filterSidebarNavByWorkspace(
  sidebarNav: RuntimeMenuNode[],
  workspace: WorkspaceType,
): RuntimeMenuNode[] {
  const filterNode = (node: RuntimeMenuNode): RuntimeMenuNode | null => {
    const nextChildren = (node.children ?? [])
      .map(filterNode)
      .filter((item): item is RuntimeMenuNode => Boolean(item));
    const routeWorkspace = node.route
      ? getRouteWorkspace(node.route)
      : (node.workspace ?? null);
    const keepLeaf =
      Boolean(node.route && node.route.navVisible !== false) &&
      routeWorkspace === workspace;
    if (!keepLeaf && nextChildren.length === 0) {
      return null;
    }
    return {
      ...node,
      section: deriveRuntimeMenuSection(node),
      sortOrder: deriveRuntimeMenuSortOrder(node, workspace),
      workspace: workspace,
      workbenchId: node.route
        ? (getRouteWorkbenchId(node.route) ?? undefined)
        : node.workbenchId,
      children:
        nextChildren.length > 0 ? sortRuntimeMenuTree(nextChildren) : undefined,
    };
  };

  return sortRuntimeMenuTree(
    sidebarNav
      .map(filterNode)
      .filter((item): item is RuntimeMenuNode => Boolean(item)),
  );
}

export function filterSidebarNavByWorkbench(
  sidebarNav: RuntimeMenuNode[],
  workbenchId: WorkbenchId,
): RuntimeMenuNode[] {
  const filterNode = (node: RuntimeMenuNode): RuntimeMenuNode | null => {
    const nextChildren = (node.children ?? [])
      .map(filterNode)
      .filter((item): item is RuntimeMenuNode => Boolean(item));
    const nodeWorkbenchId = node.route
      ? getRouteWorkbenchId(node.route)
      : (node.workbenchId ?? getMenuWorkbenchId(node));
    const keepLeaf =
      Boolean(node.route && node.route.navVisible !== false) &&
      nodeWorkbenchId === workbenchId;
    if (!keepLeaf && nextChildren.length === 0) {
      return null;
    }
    return {
      ...node,
      workbenchId: nodeWorkbenchId ?? undefined,
      children:
        nextChildren.length > 0 ? sortRuntimeMenuTree(nextChildren) : undefined,
    };
  };

  const filteredTree = sortRuntimeMenuTree(
    sidebarNav
      .map(filterNode)
      .filter((item): item is RuntimeMenuNode => Boolean(item)),
  );

  const flattenedWorkbenchRootIds: Partial<Record<WorkbenchId, string>> = {
    aiGateway: "ai-gateway",
    docker: "docker-workbench",
    monitoring: "monitoring-workbench",
    settings: "settings",
    virtualization: "virtualization-workbench",
  };
  const flattenedRootIds = [
    flattenedWorkbenchRootIds[workbenchId],
    ...(workbenchId === "settings" ? ["system", "access"] : []),
  ].filter((item): item is string => Boolean(item));

  if (flattenedRootIds.length === 0) {
    return filteredTree;
  }

  return sortRuntimeMenuTree(
    filteredTree.flatMap((node) => {
      if (flattenedRootIds.includes(node.id) && node.children?.length) {
        return node.children;
      }
      return [node];
    }),
  );
}

export function findFirstAccessiblePathForWorkspace(
  workspace: BusinessWorkspaceType,
  snapshot?: PermissionSnapshot | null,
): string | null {
  const defaultRoute = routeMeta.find(
    (route) => route.path === DEFAULT_WORKSPACE_PATHS[workspace],
  );
  if (defaultRoute && canAccessRoute(defaultRoute, snapshot)) {
    const defaultAccessiblePath = resolveAccessibleRoutePath(
      defaultRoute,
      snapshot,
    );
    if (defaultAccessiblePath) {
      return defaultAccessiblePath;
    }
  }
  for (const route of routeMeta) {
    if (!route.requiresAuth || !route.navVisible) {
      continue;
    }
    if (
      getRouteWorkspace(route) === workspace &&
      canAccessRoute(route, snapshot)
    ) {
      const accessiblePath = resolveAccessibleRoutePath(route, snapshot);
      if (accessiblePath) {
        return accessiblePath;
      }
    }
  }
  return null;
}

export function findFirstAccessiblePath(
  snapshot?: PermissionSnapshot | null,
  preferredWorkspace?: BusinessWorkspaceType | null,
): string | null {
  if (preferredWorkspace) {
    const preferredPath = findFirstAccessiblePathForWorkspace(
      preferredWorkspace,
      snapshot,
    );
    if (preferredPath) {
      return preferredPath;
    }
  }
  for (const route of routeMeta) {
    if (
      route.requiresAuth &&
      route.navVisible &&
      canAccessRoute(route, snapshot)
    ) {
      const accessiblePath = resolveAccessibleRoutePath(route, snapshot);
      if (accessiblePath) {
        return accessiblePath;
      }
    }
  }
  return null;
}

export function findLandingPath(
  snapshot?: PermissionSnapshot | null,
  persistedWorkspace?: BusinessWorkspaceType | null,
  roles: string[] = [],
): string | null {
  const workspace = findPreferredWorkspace(snapshot, persistedWorkspace, roles);
  return findFirstAccessiblePath(snapshot, workspace);
}
