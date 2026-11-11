import { getMenuSectionOrder, normalizeMenuSection } from '@/features/system/menu-schema'
import type {
  BusinessWorkspaceType,
  PermissionSnapshot,
  RouteMeta,
  RuntimeMenuNode,
  VisibleMenu,
  WorkspaceType,
} from '@/types'
import { registeredRouteDefinitions, routeMeta } from './registry'

const WORKSPACE_PERMISSION_KEYS: Record<BusinessWorkspaceType, string> = {
  application: 'workspace.application.view',
  resource: 'workspace.resource.view',
}

const DEFAULT_WORKSPACE_PATHS: Record<BusinessWorkspaceType, string> = {
  application: '/applications',
  resource: '/',
}

const RESOURCE_DEFAULT_ROLES = new Set(['admin', 'ops', 'readonly', 'auditor'])
const APPLICATION_PATH_PREFIXES = [
  '/delivery/workflows',
  '/delivery/batches',
  '/applications',
  '/application-environments',
  '/build-templates',
  '/deployment-templates',
  '/delivery/overview',
  '/delivery/onboarding',
  '/delivery/testing',
  '/delivery/analysis',
  '/delivery/blueprints',
  '/builds',
  '/delivery/release-bundles',
  '/delivery/execution-tasks',
  '/workflow-templates',
  '/release-board',
  '/execution-history',
  '/workflows',
  '/releases',
  '/registries',
]

const WORKBENCH_DEFAULT_PATHS = {
  home: '/portal',
  platform: '/',
  compute: '/compute',
  delivery: '/delivery/overview',
  ai: '/ai-workbench/overview',
  monitoring: '/monitoring-workbench',
  settings: '/settings/overview',
  security: '/internal-workbench/overview',
} as const

const WORKBENCH_FALLBACK_PATHS: Partial<
  Record<keyof typeof WORKBENCH_DEFAULT_PATHS, readonly string[]>
> = {
  settings: ['/settings/login'],
}

// Keep only the legacy tree attachment here. Route identity, path and
// permissions stay owned by the route registry.
const FRONTEND_MENU_COMPATIBILITY = [
  ['ai-workbench-knowledge-pipelines', 'ai-workbench'],
  ['ai-workbench-evaluation-lifecycle', 'ai-workbench'],
  ['ai-workbench-memory', 'ai-workbench'],
  ['ai-workbench-mcp', 'ai-workbench'],
  ['ai-workbench-data-sources', 'ai-workbench'],
  ['ai-workbench-skills', 'ai-workbench'],
  ['ai-workbench-provider-fleet', 'ai-workbench'],
  ['ai-workbench-environments', 'ai-workbench'],
  ['ai-workbench-production-operations', 'ai-workbench'],
  ['monitoring-workbench-log-data-sources', 'monitoring-workbench'],
] as const

function getCompatibleVisibleMenus(snapshot?: PermissionSnapshot | null): VisibleMenu[] {
  if (!snapshot) return []
  const visibleMenuIds = new Set(snapshot.visibleMenuIds)
  return FRONTEND_MENU_COMPATIBILITY.flatMap(([routeId, requiredParentId]) => {
    const route = routeMeta.find((item) => item.id === routeId)
    const menuId = route?.menuId
    if (!route || !menuId || visibleMenuIds.has(menuId) || !visibleMenuIds.has(requiredParentId)) {
      return []
    }
    const permissionKeys = route.permissionKeysAny?.length
      ? route.permissionKeysAny
      : [resolveRoutePermission(route)]
    if (!permissionKeys.some((key) => key && snapshot.permissionKeys.includes(key))) return []
    if (route.permissionKeysAll?.some((key) => !snapshot.permissionKeys.includes(key))) return []

    return [
      {
        id: menuId,
        parentId: requiredParentId,
        path: route.path,
        labelZh: route.title,
        sortOrder: routeMeta.indexOf(route),
      },
    ]
  })
}

export type WorkbenchId = keyof typeof WORKBENCH_DEFAULT_PATHS

export const WORKBENCH_ENTRY_PERMISSION_KEYS: Record<WorkbenchId, string> = {
  home: 'workbench.home.view',
  platform: 'workbench.platform.view',
  compute: 'workbench.compute.view',
  delivery: 'workbench.delivery.view',
  ai: 'workbench.ai.view',
  monitoring: 'workbench.monitoring.view',
  settings: 'workbench.settings.view',
  security: 'workbench.security.view',
}

function matchesRoutePrefix(pathname: string, prefixes: string[]) {
  return prefixes.some((prefix) => pathname.startsWith(prefix))
}

export { routeMeta } from './registry'

export function getRouteMeta(pathname: string): RouteMeta {
  const candidates = registeredRouteDefinitions
    .flatMap((definition) =>
      [definition.meta.path, ...(definition.aliases ?? [])].map((path) => ({
        meta: definition.meta,
        path,
      })),
    )
    .sort((a, b) => {
      const aSegments = a.path.split('/').filter(Boolean)
      const bSegments = b.path.split('/').filter(Boolean)
      return (
        bSegments.length - aSegments.length ||
        aSegments.filter((segment) => segment.startsWith(':')).length -
          bSegments.filter((segment) => segment.startsWith(':')).length ||
        b.path.length - a.path.length
      )
    })
  return (
    candidates.find((candidate) => {
      if (candidate.path === '/') return pathname === '/'
      const routeSegments = candidate.path.split('/').filter(Boolean)
      const pathSegments = pathname.split('/').filter(Boolean)
      if (routeSegments.length > pathSegments.length) return false
      return routeSegments.every(
        (segment, index) => segment.startsWith(':') || segment === pathSegments[index],
      )
    })?.meta ?? routeMeta[0]
  )
}

export function getParentRouteMeta(route: RouteMeta): RouteMeta | null {
  return route.parentId ? (routeMeta.find((r) => r.id === route.parentId) ?? null) : null
}

export function resolveRoutePermission(route: RouteMeta): string | undefined {
  if (route.permissionKey) {
    return route.permissionKey
  }
  const parent = getParentRouteMeta(route)
  return parent ? resolveRoutePermission(parent) : undefined
}

export function resolveRouteMenuId(route: RouteMeta): string | undefined {
  if (route.menuId) {
    return route.menuId
  }
  const parent = getParentRouteMeta(route)
  return parent ? resolveRouteMenuId(parent) : undefined
}

function deriveWorkspaceFromPath(pathname: string, requiresAuth: boolean): WorkspaceType | null {
  if (
    pathname === '/login' ||
    pathname === '/about' ||
    pathname.startsWith('/auth/') ||
    pathname.startsWith('/login/') ||
    pathname.startsWith('/account/')
  ) {
    return null
  }
  if (
    pathname.startsWith('/access') ||
    pathname.startsWith('/identity') ||
    pathname.startsWith('/system') ||
    pathname.startsWith('/settings')
  ) {
    return 'system'
  }
  if (matchesRoutePrefix(pathname, APPLICATION_PATH_PREFIXES)) {
    return 'application'
  }
  return requiresAuth ? 'resource' : null
}

export function getRouteWorkspace(route: RouteMeta): WorkspaceType | null {
  if (route.workspace) {
    return route.workspace
  }
  const parent = getParentRouteMeta(route)
  if (parent) {
    return getRouteWorkspace(parent)
  }
  return deriveWorkspaceFromPath(route.path, route.requiresAuth)
}

function deriveWorkbenchIdFromPath(pathname: string): WorkbenchId | null {
  // The legacy About URL is an account utility, independent of settings permissions.
  if (pathname === '/settings/about') return null
  if (pathname.startsWith('/portal')) {
    return 'home'
  }
  if (
    pathname === '/' ||
    pathname.startsWith('/cluster-resources') ||
    pathname.startsWith('/workloads') ||
    pathname.startsWith('/configuration') ||
    pathname.startsWith('/network') ||
    pathname.startsWith('/storage') ||
    pathname.startsWith('/platform-access-control') ||
    pathname.startsWith('/helm') ||
    pathname.startsWith('/extensions') ||
    pathname.startsWith('/clusters')
  ) {
    return 'platform'
  }
  if (matchesRoutePrefix(pathname, APPLICATION_PATH_PREFIXES)) {
    return 'delivery'
  }
  if (pathname.startsWith('/compute')) {
    return 'compute'
  }
  if (pathname.startsWith('/ai-gateway')) {
    return 'ai'
  }
  if (pathname.startsWith('/plugins')) {
    return 'settings'
  }
  if (pathname.startsWith('/ai-workbench')) {
    return 'ai'
  }
  if (pathname.startsWith('/monitoring-workbench') || pathname.startsWith('/observability')) {
    return 'monitoring'
  }
  if (
    pathname.startsWith('/access') ||
    pathname.startsWith('/identity/audit') ||
    pathname.startsWith('/system') ||
    pathname.startsWith('/settings')
  ) {
    return 'settings'
  }
  if (pathname.startsWith('/identity') || pathname.startsWith('/internal-workbench')) {
    return 'security'
  }
  return null
}

function rankMenuRouteCandidates(candidates: RouteMeta[], menuPath: string) {
  return [...candidates].sort((left, right) => {
    const leftExactPath = left.path === menuPath ? 0 : 1
    const rightExactPath = right.path === menuPath ? 0 : 1
    if (leftExactPath !== rightExactPath) return leftExactPath - rightExactPath

    const leftNavVisible = left.navVisible ? 0 : 1
    const rightNavVisible = right.navVisible ? 0 : 1
    if (leftNavVisible !== rightNavVisible) return leftNavVisible - rightNavVisible

    const leftRedirect = left.redirectTo ? 1 : 0
    const rightRedirect = right.redirectTo ? 1 : 0
    if (leftRedirect !== rightRedirect) return leftRedirect - rightRedirect

    return left.path.localeCompare(right.path)
  })
}

function findBestRouteForMenuMeta(menu: Pick<VisibleMenu, 'id' | 'path'>) {
  const candidates = routeMeta.filter((route) => {
    const routeMenuId = resolveRouteMenuId(route)
    return routeMenuId === menu.id || route.path === menu.path
  })
  if (candidates.length === 0) {
    return undefined
  }
  return rankMenuRouteCandidates(candidates, menu.path)[0]
}

export function getMenuWorkspace(menu: Pick<VisibleMenu, 'id' | 'path'>): WorkspaceType | null {
  const route = findBestRouteForMenuMeta(menu)
  if (route) {
    return getRouteWorkspace(route)
  }
  return deriveWorkspaceFromPath(menu.path, true)
}

export function getMenuWorkbenchId(menu: Pick<VisibleMenu, 'id' | 'path'>): WorkbenchId | null {
  const route = findBestRouteForMenuMeta(menu)
  if (route) {
    return getRouteWorkbenchId(route)
  }
  return deriveWorkbenchIdFromPath(menu.path)
}

export function getRouteWorkbenchId(route: RouteMeta): WorkbenchId | null {
  if (route.workbenchId && route.workbenchId in WORKBENCH_DEFAULT_PATHS) {
    return route.workbenchId as WorkbenchId
  }
  const parent = getParentRouteMeta(route)
  if (parent) {
    return getRouteWorkbenchId(parent)
  }
  return deriveWorkbenchIdFromPath(route.path)
}

export function getRouteScopeMode(route: RouteMeta): NonNullable<RouteMeta['scopeMode']> {
  if (route.scopeMode) {
    return route.scopeMode
  }
  const parent = getParentRouteMeta(route)
  if (parent) {
    return getRouteScopeMode(parent)
  }
  const pathname = route.path
  if (pathname === '/login' || pathname.startsWith('/auth/') || pathname.startsWith('/login/')) {
    return 'hidden'
  }
  if (
    pathname === '/' ||
    pathname === '/clusters' ||
    pathname.startsWith('/cluster-resources/nodes') ||
    pathname.startsWith('/cluster-resources/namespaces') ||
    pathname.startsWith('/extensions') ||
    pathname.startsWith('/platform-access-control/clusterroles') ||
    pathname.startsWith('/platform-access-control/clusterrolebindings') ||
    pathname.startsWith('/storage/persistentvolumes') ||
    pathname.startsWith('/storage/storageclasses')
  ) {
    return 'cluster'
  }
  if (
    pathname.startsWith('/access') ||
    pathname.startsWith('/identity') ||
    pathname.startsWith('/system') ||
    pathname.startsWith('/settings') ||
    pathname.startsWith('/applications') ||
    pathname.startsWith('/application-environments') ||
    pathname.startsWith('/build-templates') ||
    pathname.startsWith('/deployment-templates') ||
    pathname.startsWith('/delivery/blueprints') ||
    pathname.startsWith('/builds') ||
    pathname.startsWith('/delivery/release-bundles') ||
    pathname.startsWith('/delivery/execution-tasks') ||
    pathname.startsWith('/workflow-templates') ||
    pathname.startsWith('/release-board') ||
    pathname.startsWith('/execution-history') ||
    pathname.startsWith('/releases') ||
    pathname.startsWith('/registries') ||
    pathname.startsWith('/monitoring-workbench') ||
    pathname.startsWith('/observability') ||
    pathname.startsWith('/compute') ||
    pathname.startsWith('/plugins') ||
    pathname.startsWith('/ai-gateway') ||
    pathname.startsWith('/ai-workbench') ||
    pathname.startsWith('/account')
  ) {
    return 'passive'
  }
  return 'namespace'
}

export function getAccessibleWorkbenchIds(
  snapshot?: PermissionSnapshot | null,
): Array<keyof typeof WORKBENCH_DEFAULT_PATHS> {
  const seen = new Set<keyof typeof WORKBENCH_DEFAULT_PATHS>()
  for (const route of routeMeta) {
    const workbenchId = getRouteWorkbenchId(route)
    if (!workbenchId || seen.has(workbenchId)) {
      continue
    }
    if (!route.requiresAuth || !canAccessRoute(route, snapshot)) {
      continue
    }
    seen.add(workbenchId)
  }
  return Array.from(seen).filter((workbenchId) =>
    Boolean(findFirstAccessiblePathForWorkbench(workbenchId, snapshot)),
  )
}

export function findFirstAccessiblePathForWorkbench(
  workbenchId: keyof typeof WORKBENCH_DEFAULT_PATHS,
  snapshot?: PermissionSnapshot | null,
): string | null {
  const defaultPath = WORKBENCH_DEFAULT_PATHS[workbenchId]
  const defaultRoute = routeMeta.find((route) => route.path === defaultPath)
  if (defaultRoute && canAccessRoute(defaultRoute, snapshot)) {
    const defaultAccessiblePath = resolveAccessibleRoutePath(defaultRoute, snapshot)
    if (defaultAccessiblePath) {
      return defaultAccessiblePath
    }
  }
  for (const fallbackPath of WORKBENCH_FALLBACK_PATHS[workbenchId] ?? []) {
    const fallbackRoute = routeMeta.find((route) => route.path === fallbackPath)
    if (!fallbackRoute || !canAccessRoute(fallbackRoute, snapshot)) {
      continue
    }
    const fallbackAccessiblePath = resolveAccessibleRoutePath(fallbackRoute, snapshot)
    if (fallbackAccessiblePath) {
      return fallbackAccessiblePath
    }
  }
  for (const route of routeMeta) {
    if (
      route.requiresAuth &&
      route.navVisible &&
      getRouteWorkbenchId(route) === workbenchId &&
      canAccessRoute(route, snapshot)
    ) {
      const accessiblePath = resolveAccessibleRoutePath(route, snapshot)
      if (accessiblePath) {
        return accessiblePath
      }
    }
  }
  return null
}

function resolveAccessibleRoutePath(
  route: RouteMeta,
  snapshot?: PermissionSnapshot | null,
): string | null {
  if (!route.redirectTo) {
    return route.path
  }
  const redirectRoute = routeMeta.find((item) => item.path === route.redirectTo)
  if (!redirectRoute) {
    return route.redirectTo
  }
  return canAccessRoute(redirectRoute, snapshot) ? route.redirectTo : null
}

export function getWorkspacePermissionKey(workspace: BusinessWorkspaceType) {
  return WORKSPACE_PERMISSION_KEYS[workspace]
}

export function hasWorkspaceAccess(
  workspace: BusinessWorkspaceType,
  snapshot?: PermissionSnapshot | null,
) {
  return snapshot?.permissionKeys.includes(WORKSPACE_PERMISSION_KEYS[workspace]) ?? false
}

export function getAccessibleWorkspaces(
  snapshot?: PermissionSnapshot | null,
): BusinessWorkspaceType[] {
  const workspaces: BusinessWorkspaceType[] = []
  ;(['application', 'resource'] as const).forEach((workspace) => {
    if (findFirstAccessiblePathForWorkspace(workspace, snapshot)) {
      workspaces.push(workspace)
    }
  })
  return workspaces
}

export function getDefaultWorkspaceForRoles(roles: string[] = []): BusinessWorkspaceType {
  return roles.some((role) => RESOURCE_DEFAULT_ROLES.has(String(role || '').trim()))
    ? 'resource'
    : 'application'
}

export function findPreferredWorkspace(
  snapshot?: PermissionSnapshot | null,
  persistedWorkspace?: BusinessWorkspaceType | null,
  roles: string[] = [],
): BusinessWorkspaceType | null {
  const accessibleWorkspaces = getAccessibleWorkspaces(snapshot)
  if (accessibleWorkspaces.length === 0) {
    return null
  }
  if (persistedWorkspace && accessibleWorkspaces.includes(persistedWorkspace)) {
    return persistedWorkspace
  }
  const roleDefault = getDefaultWorkspaceForRoles(roles)
  if (accessibleWorkspaces.includes(roleDefault)) {
    return roleDefault
  }
  return accessibleWorkspaces[0]
}

export function canAccessRoute(route: RouteMeta, snapshot?: PermissionSnapshot | null): boolean {
  if (!route.requiresAuth) {
    return true
  }
  if (!snapshot) {
    return false
  }
  if (route.permissionKeysAll?.some((key) => !snapshot.permissionKeys.includes(key))) return false
  if (route.permissionStrategy === 'any-child') {
    return routeMeta
      .filter((child) => child.parentId === route.id)
      .some((child) => canAccessRoute(child, snapshot))
  }
  const workbenchId = getRouteWorkbenchId(route)
  const hasWorkbenchEntry =
    !workbenchId || snapshot.permissionKeys.includes(WORKBENCH_ENTRY_PERMISSION_KEYS[workbenchId])
  const permissionKey = resolveRoutePermission(route)
  const permissionKeysAny = route.permissionKeysAny ?? []
  const hasPermission =
    permissionKeysAny.length > 0
      ? permissionKeysAny.some((key) => snapshot.permissionKeys.includes(key))
      : !permissionKey || snapshot.permissionKeys.includes(permissionKey)
  return hasWorkbenchEntry && hasPermission
}

function getSectionOrder(section?: string) {
  const normalized = normalizeMenuSection(String(section || ''))
  return normalized ? getMenuSectionOrder(normalized) : -1
}

function sortRuntimeMenuTree(items: RuntimeMenuNode[]): RuntimeMenuNode[] {
  return [...items]
    .sort((left, right) => {
      const sectionCompare = getSectionOrder(left.section) - getSectionOrder(right.section)
      if (sectionCompare !== 0) return sectionCompare
      if (left.sortOrder !== right.sortOrder) return left.sortOrder - right.sortOrder
      return left.path.localeCompare(right.path)
    })
    .map(
      (item): RuntimeMenuNode => ({
        ...item,
        children:
          item.children && item.children.length > 0
            ? sortRuntimeMenuTree(item.children)
            : undefined,
      }),
    )
}

const APPLICATION_SECTION_ORDER: Record<string, number> = {
  'delivery-overview': 0,
  builds: 10,
  applications: 10,
  'release-board': 50,
  'execution-history': 5,
  'delivery-testing': 60,
  'delivery-analysis': 70,
  'release-bundles': 10,
  releases: 40,
  'delivery-blueprints': 10,
  'build-templates': 20,
  'deployment-templates': 25,
  'workflow-templates': 30,
  registries: 40,
}

const APPLICATION_MENU_SECTION_OVERRIDES: Record<string, string> = {
  'delivery-overview': '',
  builds: 'delivery',
  applications: 'delivery',
  'release-board': 'delivery',
  'execution-history': 'delivery-records',
  'delivery-testing': 'delivery',
  'delivery-analysis': 'delivery',
  'release-bundles': 'delivery-records',
  releases: 'delivery-records',
  'delivery-blueprints': 'delivery-platform',
  'build-templates': 'delivery-platform',
  'deployment-templates': 'delivery-platform',
  'workflow-templates': 'delivery-platform',
  registries: 'delivery-platform',
}

const AI_MENU_SECTION_OVERRIDES: Record<string, string> = {
  'ai-workbench-chat': 'ai-interaction',
  'ai-workbench-companion': 'ai-interaction',
  'ai-workbench-inspection': 'ai-interaction',
  'ai-workbench-knowledge': 'ai-interaction',
  'ai-workbench-agent-runs': 'ai-interaction',
  'ai-workbench-evaluations': 'ai-interaction',
  'ai-workbench-context': 'ai-engineering',
  'ai-workbench-mcp': 'ai-engineering',
  'ai-workbench-data-sources': 'ai-engineering',
  'ai-workbench-skills': 'ai-engineering',
  'ai-workbench-tool-settings': 'ai-engineering',
  'ai-workbench-agent-providers': 'ai-engineering',
  'ai-workbench-environments': 'ai-engineering',
  'ai-workbench-knowledge-pipelines': 'ai-interaction',
  'ai-workbench-evaluation-lifecycle': 'ai-interaction',
  'ai-workbench-memory': 'ai-engineering',
  'ai-workbench-provider-fleet': 'ai-engineering',
  'ai-workbench-production-operations': 'ai-governance',
  'ai-gateway-relay': 'ai-model-access',
  'ai-gateway-clients': 'ai-model-access',
  'ai-gateway-tokens': 'ai-model-access',
  'ai-gateway-manifest': 'ai-governance',
  'ai-gateway-governance': 'ai-governance',
  'ai-gateway-call-logs': 'ai-governance',
}

const SYSTEM_ROOT_ORDER: Record<string, number> = {
  access: 10,
  'access-users': 11,
  'access-roles': 12,
  'access-teams': 13,
  'access-policies': 14,
  'access-directory-sync': 15,
  identity: 20,
  'identity-overview': 20,
  'identity-applications': 21,
  'identity-providers': 22,
  'identity-outposts': 23,
  system: 30,
  settings: 40,
}

function deriveRuntimeMenuSection(node: RuntimeMenuNode, workspace: WorkspaceType | null): string {
  if (node.id in AI_MENU_SECTION_OVERRIDES) {
    return AI_MENU_SECTION_OVERRIDES[node.id]
  }
  if (workspace === 'application' && node.id in APPLICATION_MENU_SECTION_OVERRIDES) {
    return APPLICATION_MENU_SECTION_OVERRIDES[node.id]
  }
  return normalizeMenuSection(node.section || '')
}

function deriveRuntimeMenuSortOrder(
  node: RuntimeMenuNode,
  workspace: WorkspaceType | null,
): number {
  if (workspace === 'application' && node.id in APPLICATION_SECTION_ORDER) {
    return APPLICATION_SECTION_ORDER[node.id]
  }
  if (
    workspace === 'system' &&
    (!node.parentId || node.parentId === 'access') &&
    node.id in SYSTEM_ROOT_ORDER
  ) {
    return SYSTEM_ROOT_ORDER[node.id]
  }
  return node.sortOrder
}

function findBestRouteForMenu(
  menu: VisibleMenu,
  snapshot?: PermissionSnapshot | null,
): RouteMeta | undefined {
  const candidates = routeMeta
    .filter((route) => {
      const routeMenuId = resolveRouteMenuId(route)
      return routeMenuId === menu.id || route.path === menu.path
    })
    .filter((route) => canAccessRoute(route, snapshot))

  if (candidates.length === 0) return undefined

  return rankMenuRouteCandidates(candidates, menu.path)[0]
}

function buildRuntimeMenuTree(snapshot?: PermissionSnapshot | null): RuntimeMenuNode[] {
  const visibleMenus = [...(snapshot?.visibleMenus ?? []), ...getCompatibleVisibleMenus(snapshot)]
  const nodes = new Map<string, RuntimeMenuNode>()

  visibleMenus
    .filter((menu) => menu.enabled ?? true)
    .forEach((menu) => {
      const route = findBestRouteForMenu(menu, snapshot)
      nodes.set(menu.id, {
        id: menu.id,
        parentId: menu.parentId,
        path: menu.path,
        labelZh: menu.labelZh || menu.id,
        labelEn: menu.labelEn || menu.labelZh || menu.id,
        iconKey: menu.iconKey || '',
        section: normalizeMenuSection(menu.section || ''),
        sortOrder: typeof menu.sortOrder === 'number' ? menu.sortOrder : 0,
        enabled: menu.enabled ?? true,
        workspace: route ? (getRouteWorkspace(route) ?? undefined) : undefined,
        workbenchId: route ? (getRouteWorkbenchId(route) ?? undefined) : undefined,
        route,
        children: [],
      })
    })

  const ingress = nodes.get('network-ingresses')
  const ingressClass = nodes.get('network-ingressclasses')
  const gatewayClass = nodes.get('network-gateway-api-gatewayclasses')
  if (
    (!ingress || (ingress.parentId === 'network' && ingress.sortOrder === 42)) &&
    ingressClass?.parentId === 'network' &&
    ingressClass.path === '/network/ingressclasses' &&
    ingressClass.sortOrder === 54 &&
    (!gatewayClass || (gatewayClass.parentId === 'network' && gatewayClass.sortOrder === 43))
  ) {
    ingressClass.sortOrder = 42.5
  }

  const roots: RuntimeMenuNode[] = []
  nodes.forEach((node) => {
    if (node.parentId && nodes.has(node.parentId)) {
      nodes.get(node.parentId)?.children?.push(node)
      return
    }
    roots.push(node)
  })

  const prune = (node: RuntimeMenuNode): RuntimeMenuNode | null => {
    const nextChildren = (node.children ?? [])
      .map(prune)
      .filter((item): item is RuntimeMenuNode => Boolean(item))
    const keepAsContainer = nextChildren.length > 0
    const keepAsLeaf = Boolean(node.route)
    if (!keepAsContainer && !keepAsLeaf) {
      return null
    }
    return {
      ...node,
      children: nextChildren.length > 0 ? nextChildren : undefined,
    }
  }

  return sortRuntimeMenuTree(
    roots.map(prune).filter((item): item is RuntimeMenuNode => Boolean(item)),
  )
}

export function getAccessibleSidebarNav(snapshot?: PermissionSnapshot | null): RuntimeMenuNode[] {
  return buildRuntimeMenuTree(snapshot)
}

export function filterSidebarNavByWorkspace(
  sidebarNav: RuntimeMenuNode[],
  workspace: WorkspaceType,
): RuntimeMenuNode[] {
  const filterNode = (node: RuntimeMenuNode): RuntimeMenuNode | null => {
    const nextChildren = (node.children ?? [])
      .map(filterNode)
      .filter((item): item is RuntimeMenuNode => Boolean(item))
    const routeWorkspace = node.route ? getRouteWorkspace(node.route) : (node.workspace ?? null)
    const keepLeaf =
      Boolean(node.route && node.route.navVisible !== false) && routeWorkspace === workspace
    if (!keepLeaf && nextChildren.length === 0) {
      return null
    }
    return {
      ...node,
      section: deriveRuntimeMenuSection(node, workspace),
      sortOrder: deriveRuntimeMenuSortOrder(node, workspace),
      workspace: workspace,
      workbenchId: node.route ? (getRouteWorkbenchId(node.route) ?? undefined) : node.workbenchId,
      children: nextChildren.length > 0 ? sortRuntimeMenuTree(nextChildren) : undefined,
    }
  }

  return sortRuntimeMenuTree(
    sidebarNav.map(filterNode).filter((item): item is RuntimeMenuNode => Boolean(item)),
  )
}

export function filterSidebarNavByWorkbench(
  sidebarNav: RuntimeMenuNode[],
  workbenchId: WorkbenchId,
): RuntimeMenuNode[] {
  const filterNode = (node: RuntimeMenuNode): RuntimeMenuNode | null => {
    const nextChildren = (node.children ?? [])
      .map(filterNode)
      .filter((item): item is RuntimeMenuNode => Boolean(item))
    const nodeWorkbenchId = node.route
      ? getRouteWorkbenchId(node.route)
      : (node.workbenchId ?? getMenuWorkbenchId(node))
    const keepLeaf =
      Boolean(node.route && node.route.navVisible !== false) && nodeWorkbenchId === workbenchId
    if (!keepLeaf && nextChildren.length === 0) {
      return null
    }
    return {
      ...node,
      workbenchId: nodeWorkbenchId ?? undefined,
      children: nextChildren.length > 0 ? sortRuntimeMenuTree(nextChildren) : undefined,
    }
  }

  const filteredTree = sortRuntimeMenuTree(
    sidebarNav.map(filterNode).filter((item): item is RuntimeMenuNode => Boolean(item)),
  )

  const flattenedWorkbenchRootIds: Partial<Record<WorkbenchId, string>> = {
    compute: 'compute-workbench',
    monitoring: 'monitoring-workbench',
    settings: 'settings',
    security: 'identity',
  }
  const flattenedRootIds = [
    flattenedWorkbenchRootIds[workbenchId],
    ...(workbenchId === 'ai' ? ['ai-workbench'] : []),
    ...(workbenchId === 'settings' ? ['system', 'access', 'settings-extensions'] : []),
  ].filter((item): item is string => Boolean(item))

  if (flattenedRootIds.length === 0) {
    return filteredTree
  }

  return sortRuntimeMenuTree(
    filteredTree.flatMap((node) => {
      if (flattenedRootIds.includes(node.id) && node.children?.length) {
        return node.children
      }
      return [node]
    }),
  )
}

export function findFirstAccessiblePathForWorkspace(
  workspace: BusinessWorkspaceType,
  snapshot?: PermissionSnapshot | null,
): string | null {
  const defaultRoute = routeMeta.find((route) => route.path === DEFAULT_WORKSPACE_PATHS[workspace])
  if (defaultRoute && canAccessRoute(defaultRoute, snapshot)) {
    const defaultAccessiblePath = resolveAccessibleRoutePath(defaultRoute, snapshot)
    if (defaultAccessiblePath) {
      return defaultAccessiblePath
    }
  }
  for (const route of routeMeta) {
    if (!route.requiresAuth || !route.navVisible) {
      continue
    }
    if (getRouteWorkspace(route) === workspace && canAccessRoute(route, snapshot)) {
      const accessiblePath = resolveAccessibleRoutePath(route, snapshot)
      if (accessiblePath) {
        return accessiblePath
      }
    }
  }
  return null
}

export function findFirstAccessiblePath(
  snapshot?: PermissionSnapshot | null,
  preferredWorkspace?: BusinessWorkspaceType | null,
): string | null {
  if (preferredWorkspace) {
    const preferredPath = findFirstAccessiblePathForWorkspace(preferredWorkspace, snapshot)
    if (preferredPath) {
      return preferredPath
    }
  }
  for (const route of routeMeta) {
    if (route.requiresAuth && route.navVisible && canAccessRoute(route, snapshot)) {
      const accessiblePath = resolveAccessibleRoutePath(route, snapshot)
      if (accessiblePath) {
        return accessiblePath
      }
    }
  }
  return null
}

export function findLandingPath(
  snapshot?: PermissionSnapshot | null,
  persistedWorkspace?: BusinessWorkspaceType | null,
  roles: string[] = [],
): string | null {
  const homePath = findFirstAccessiblePathForWorkbench('home', snapshot)
  if (homePath) {
    return homePath
  }
  const workspace = findPreferredWorkspace(snapshot, persistedWorkspace, roles)
  return findFirstAccessiblePath(snapshot, workspace)
}
