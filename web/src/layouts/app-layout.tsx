import type { ReactNode } from 'react'
import { useEffect, useMemo, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Avatar, Breadcrumb, Button, Dropdown, Layout, Menu, Spin } from 'antd'
import type { MenuProps } from 'antd'
import {
  AlertOutlined,
  AppstoreOutlined,
  CloudServerOutlined,
  DockerOutlined,
  DownOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  MoonOutlined,
  QuestionCircleOutlined,
  RobotOutlined,
  SafetyOutlined,
  SettingOutlined,
  SlidersOutlined,
  SunOutlined,
  TranslationOutlined,
  LogoutOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { HeaderPreferenceButton } from '@/components/header-preference-button'
import { PlatformScopeTrigger } from '@/components/platform-scope-toolbar'
import { AnnouncementBell } from '@/features/announcements/announcement-center'
import { logoutAuthSession } from '@/features/auth/auth-api'
import { usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { resolveMenuIcon } from '@/features/system/menu-icons'
import { resolveMenuSectionLabel } from '@/features/system/menu-schema'
import { useI18n } from '@/i18n'
import {
  filterSidebarNavByWorkbench,
  filterSidebarNavByWorkspace,
  findFirstAccessiblePathForWorkbench,
  findPreferredWorkspace,
  getAccessibleSidebarNav,
  getAccessibleWorkspaces,
  getAccessibleWorkbenchIds,
  getParentRouteMeta,
  getRouteMeta,
  getRouteScopeMode,
  getRouteWorkbenchId,
  getRouteWorkspace,
  resolveRouteMenuId,
  type WorkbenchId,
} from '@/routes/meta'
import { useBrandingSettings } from '@/features/settings/use-branding-settings'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { resolveThemeMode, watchSystemThemeMode } from '@/theme/app-theme'
import type { BusinessWorkspaceType, PermissionSnapshot, RuntimeMenuNode } from '@/types'
import { getNormalizedBranding } from '@/features/settings/use-branding-settings'

const { Sider, Header, Content } = Layout
const SIDEBAR_WIDTH = 200
const SIDEBAR_COLLAPSED_WIDTH = 55
const BREADCRUMB_WORKBENCH_ROOT_ROUTE_IDS: Partial<Record<WorkbenchId, string[]>> = {
  ai: ['ai-workbench'],
  aiGateway: ['ai-gateway'],
  docker: ['docker-workbench'],
  monitoring: ['monitoring-workbench'],
  settings: ['settings'],
  virtualization: ['virtualization-workbench'],
}

interface WorkbenchOption {
  description: string
  icon: ReactNode
  key: WorkbenchId
  label: string
}

const AI_WORKBENCH_MENU_ENTRIES = [
  { key: 'ai-workbench-chat', iconKey: 'bot', label: '通用聊天', path: '/ai-workbench/chat', permissionKey: 'observe.ai.chat', legacyMenuIds: ['ai-workbench-investigation'] },
  { key: 'ai-workbench-inspection', iconKey: 'inspect', label: '巡检', path: '/ai-workbench/inspection', permissionKey: 'observe.ai.view', legacyMenuIds: ['ai-workbench-operations'] },
  { key: 'ai-workbench-tool-settings', iconKey: 'wrench', label: '工具与技能', path: '/ai-workbench/tool-settings', permissionKey: 'observe.ai.view', legacyMenuIds: ['ai-workbench-tools'] },
  { key: 'ai-workbench-model-settings', iconKey: 'settings', label: 'AI 设置', path: '/ai-workbench/model-settings', permissionKey: 'settings.ai.view', legacyMenuIds: ['ai-workbench-tools'] },
] as const

const SYSTEM_LOG_MENU_LABEL_OVERRIDES: Record<string, { en: string; legacyEn: string[]; legacyZh: string[]; zh: string }> = {
  operations: { zh: '操作日志', en: 'Operation Logs', legacyZh: ['操作'], legacyEn: ['Operations'] },
  audit: { zh: '审计日志', en: 'Audit Logs', legacyZh: ['审计'], legacyEn: ['Audit'] },
}

function canUseAIWorkbenchMenuEntry(
  item: (typeof AI_WORKBENCH_MENU_ENTRIES)[number],
  snapshot?: PermissionSnapshot | null,
) {
  const hasPermissionKey = snapshot?.permissionKeys.includes(item.permissionKey) ?? false
  const visibleMenuIds = snapshot?.visibleMenuIds ?? []
  const hasVisibleMenu = visibleMenuIds.includes(item.key) || item.legacyMenuIds.some((id) => visibleMenuIds.includes(id))
  return hasPermissionKey && hasVisibleMenu
}

function buildAIWorkbenchMenuItems(snapshot?: PermissionSnapshot | null): MenuProps['items'] {
  return AI_WORKBENCH_MENU_ENTRIES.filter((item) => canUseAIWorkbenchMenuEntry(item, snapshot)).map((item) => ({
    key: item.key,
    icon: resolveMenuIcon(item.iconKey),
    label: item.label,
  }))
}

function buildAIWorkbenchItemKeyToPath(snapshot?: PermissionSnapshot | null) {
  return Object.fromEntries(
    AI_WORKBENCH_MENU_ENTRIES
      .filter((item) => canUseAIWorkbenchMenuEntry(item, snapshot))
      .map((item) => [item.key, item.path]),
  ) as Record<string, string>
}

function buildAIWorkbenchSearch(search: string) {
  const source = new URLSearchParams(search)
  const next = new URLSearchParams()
  ;['session', 'clusterId', 'namespace', 'workload', 'alertId', 'timeRangeMinutes'].forEach((key) => {
    const value = source.get(key)
    if (value) {
      next.set(key, value)
    }
  })
  const suffix = next.toString()
  return suffix ? `?${suffix}` : ''
}

function findAIWorkbenchMenuKey(pathname: string, search: string) {
  if (pathname === '/ai-workbench/investigation') {
    const mode = new URLSearchParams(search).get('mode')
    if (mode === 'inspection_review') return 'ai-workbench-inspection'
    return 'ai-workbench-chat'
  }
  if (pathname === '/ai-workbench/root-cause' || pathname === '/ai-workbench/performance') {
    return 'ai-workbench-chat'
  }
  return AI_WORKBENCH_MENU_ENTRIES.find((item) => item.path === pathname)?.key ?? null
}

function resolveRuntimeMenuLabel(node: RuntimeMenuNode, localeCode: 'zh_CN' | 'en_US') {
  const override = SYSTEM_LOG_MENU_LABEL_OVERRIDES[node.id]
  if (override) {
    if (localeCode === 'en_US') {
      const label = String(node.labelEn || '').trim()
      if (!label || override.legacyEn.includes(label)) return override.en
      return node.labelEn
    }
    const label = String(node.labelZh || '').trim()
    if (!label || override.legacyZh.includes(label)) return override.zh
    return node.labelZh
  }
  return localeCode === 'en_US' && node.labelEn ? node.labelEn : node.labelZh
}

function buildMenuNodeItem(node: RuntimeMenuNode, localeCode: 'zh_CN' | 'en_US'): NonNullable<MenuProps['items']>[number] {
  const label = resolveRuntimeMenuLabel(node, localeCode)
  const icon = resolveMenuIcon(resolveRuntimeMenuIconKey(node))
  if (node.children?.length) {
    return {
      key: node.id,
      icon,
      label,
      children: node.children.map((child) => buildMenuNodeItem(child, localeCode)),
    }
  }
  return {
    key: node.id,
    icon,
    label,
  }
}

const VIRTUALIZATION_MENU_ICON_OVERRIDES: Record<string, { iconKey: string; legacy: string[] }> = {
  'virtualization-workbench-overview': { iconKey: 'gauge', legacy: ['server', 'gauge'] },
  'virtualization-workbench-vms': { iconKey: 'desktop', legacy: ['server'] },
  'virtualization-workbench-clusters': { iconKey: 'cluster', legacy: ['globe'] },
  'virtualization-workbench-images': { iconKey: 'image', legacy: ['blocks'] },
  'virtualization-workbench-flavors': { iconKey: 'flavor', legacy: ['code'] },
  'virtualization-workbench-operations': { iconKey: 'history', legacy: ['activity', 'file-clock'] },
  'virtualization-workbench-sync': { iconKey: 'sync', legacy: ['activity', 'file-clock'] },
}

function resolveRuntimeMenuIconKey(node: RuntimeMenuNode) {
  const override = VIRTUALIZATION_MENU_ICON_OVERRIDES[node.id]
  if (!override) return node.iconKey
  const iconKey = String(node.iconKey || '').trim()
  if (!iconKey || override.legacy.includes(iconKey)) {
    return override.iconKey
  }
  return iconKey
}

function buildMenuItems(
  sidebarNav: RuntimeMenuNode[],
  localeCode: 'zh_CN' | 'en_US',
  options: { grouped?: boolean } = {},
): MenuProps['items'] {
  if (options.grouped === false) {
    return sidebarNav.map((item) => buildMenuNodeItem(item, localeCode))
  }
  const directItems: NonNullable<MenuProps['items']>[number][] = []
  const groups = new Map<string, NonNullable<MenuProps['items']>[number][]>()

  for (const item of sidebarNav) {
    const menuItem = buildMenuNodeItem(item, localeCode)
    const groupKey = String(item.section || '').trim()
    if (!groupKey) {
      directItems.push(menuItem)
      continue
    }
    const current = groups.get(groupKey) ?? []
    current.push(menuItem)
    groups.set(groupKey, current)
  }

  const groupedItems = Array.from(groups.entries()).map(([groupKey, items]) => ({
    key: `group-${groupKey}`,
    type: 'group' as const,
    label: <span className="soha-nav-section-title">{resolveMenuSectionLabel(groupKey, localeCode)}</span>,
    children: items,
  }))

  return [...directItems, ...groupedItems]
}

function buildItemKeyToPath(sidebarNav: RuntimeMenuNode[]): Record<string, string> {
  const map: Record<string, string> = {}
  const visit = (node: RuntimeMenuNode) => {
    if (!node.children?.length && node.route) {
      map[node.id] = node.route.redirectTo ?? node.route.path
    }
    node.children?.forEach(visit)
  }
  sidebarNav.forEach(visit)
  return map
}

function buildParentMap(sidebarNav: RuntimeMenuNode[]) {
  const parentByID = new Map<string, string>()
  const visit = (node: RuntimeMenuNode, parentID?: string) => {
    if (parentID) {
      parentByID.set(node.id, parentID)
    }
    node.children?.forEach((child) => visit(child, node.id))
  }
  sidebarNav.forEach((node) => visit(node))
  return parentByID
}

function buildNodeByID(sidebarNav: RuntimeMenuNode[]) {
  const nodeByID = new Map<string, RuntimeMenuNode>()
  const visit = (node: RuntimeMenuNode) => {
    nodeByID.set(node.id, node)
    node.children?.forEach(visit)
  }
  sidebarNav.forEach(visit)
  return nodeByID
}

function getRuntimeMenuNodeLabel(node: RuntimeMenuNode, localeCode: 'zh_CN' | 'en_US') {
  return resolveRuntimeMenuLabel(node, localeCode)
}

function findMenuIDByRoutePath(sidebarNav: RuntimeMenuNode[], routePath: string) {
  let matched: string | null = null
  const visit = (node: RuntimeMenuNode) => {
    if (matched) return
    if (node.route?.path === routePath || node.path === routePath) {
      matched = node.id
      return
    }
    node.children?.forEach(visit)
  }
  sidebarNav.forEach(visit)
  return matched
}

function collectNodeIDs(sidebarNav: RuntimeMenuNode[]) {
  const ids = new Set<string>()
  const visit = (node: RuntimeMenuNode) => {
    ids.add(node.id)
    node.children?.forEach(visit)
  }
  sidebarNav.forEach(visit)
  return ids
}

function collectExpandableNodeIDs(sidebarNav: RuntimeMenuNode[]) {
  const ids = new Set<string>()
  const visit = (node: RuntimeMenuNode) => {
    if (node.children?.length) {
      ids.add(node.id)
      node.children.forEach(visit)
    }
  }
  sidebarNav.forEach(visit)
  return ids
}

function mergeOpenKeys(current: string[], desired: string[]) {
  if (desired.length === 0) {
    return current
  }
  const merged = Array.from(new Set([...current, ...desired]))
  return merged.length === current.length && merged.every((key, index) => key === current[index]) ? current : merged
}

function buildWorkbenchOptions(localeCode: 'zh_CN' | 'en_US'): WorkbenchOption[] {
  if (localeCode === 'en_US') {
    return [
      { key: 'platform', label: 'K8s Workbench', description: 'Operations dashboard for clusters, workloads, network, storage, and runtime resources', icon: <AppstoreOutlined /> },
      { key: 'virtualization', label: 'Virtualization Workbench', description: 'Virtual machines, clusters, images, flavors, and operation records', icon: <SlidersOutlined /> },
      { key: 'docker', label: 'Docker Workbench', description: 'Docker hosts, container management, templates, and operations', icon: <DockerOutlined /> },
      { key: 'delivery', label: 'Delivery Workbench', description: 'Applications, build sources, bindings, and release orchestration', icon: <CloudServerOutlined /> },
      { key: 'ai', label: 'AI Workbench', description: 'Investigation, automation, tools, and skills', icon: <RobotOutlined /> },
      { key: 'aiGateway', label: 'AI Gateway', description: 'AI clients, MCP access, tokens, policies, approvals, and call logs', icon: <SafetyOutlined /> },
      { key: 'monitoring', label: 'Monitoring Workbench', description: 'Alerts, routes, notifications, and on-call flows', icon: <AlertOutlined /> },
      { key: 'settings', label: 'Settings Center', description: 'Login, branding, monitoring, and AI settings', icon: <SettingOutlined /> },
    ]
  }
  return [
    { key: 'platform', label: 'k8s工作台', description: '面向运维视角的集群、工作负载、网络、存储与运行资源', icon: <AppstoreOutlined /> },
    { key: 'virtualization', label: '虚拟化管理工作台', description: '虚拟机、集群、镜像、规格与操作记录', icon: <SlidersOutlined /> },
    { key: 'docker', label: 'Docker 工作台', description: '主机、容器管理、模板与操作记录', icon: <DockerOutlined /> },
    { key: 'delivery', label: '应用交付工作台', description: '应用、构建来源、环境绑定与发布编排', icon: <CloudServerOutlined /> },
    { key: 'ai', label: 'AI工作台', description: '调查、自动化、工具与技能', icon: <RobotOutlined /> },
    { key: 'aiGateway', label: 'AI Gateway', description: '外部 AI 客户端、MCP、令牌、策略、审批与调用日志', icon: <SafetyOutlined /> },
    { key: 'monitoring', label: '监控工作台', description: '告警、路由、通知和值班协同', icon: <AlertOutlined /> },
    { key: 'settings', label: '设置中心', description: '登录、品牌、监控与 AI 设置', icon: <SettingOutlined /> },
  ]
}

function WorkbenchSwitcher({
  collapsed,
  current,
  onSelect,
  options,
}: {
  collapsed: boolean
  current: WorkbenchOption
  onSelect: (workbench: WorkbenchOption['key']) => void
  options: WorkbenchOption[]
}) {
  const dropdownItems: MenuProps['items'] = options.map((option) => ({
    key: option.key,
    label: (
      <div className="soha-workspace-option">
        <span className="soha-workspace-option__icon">{option.icon}</span>
        <span className="soha-workspace-option__copy">
          <span className="soha-workspace-option__label">{option.label}</span>
        </span>
      </div>
    ),
  }))

  const trigger = (
    <Button className="soha-workbench-switcher" type="text">
      <span className="soha-workbench-switcher__icon">{current.icon}</span>
      {!collapsed ? (
        <span className="soha-workbench-switcher__copy">
          <span className="soha-workbench-switcher__label">{current.label}</span>
          <span className="soha-workbench-switcher__desc">{current.description}</span>
        </span>
      ) : null}
      {!collapsed && options.length > 1 ? <DownOutlined className="soha-workbench-switcher__arrow" /> : null}
    </Button>
  )

  if (options.length <= 1) {
    return trigger
  }

  return (
    <Dropdown
      menu={{
        items: dropdownItems,
        selectable: true,
        selectedKeys: [current.key],
        onClick: ({ key }) => onSelect(String(key) as WorkbenchOption['key']),
      }}
      placement="bottomLeft"
      trigger={['click']}
    >
      {trigger}
    </Dropdown>
  )
}

export function AppLayout() {
  const navigate = useNavigate()
  const location = useLocation()
  const user = useAuthStore((state) => state.user)
  const permissionSnapshotQuery = usePermissionSnapshot()
  const brandingQuery = useBrandingSettings()
  const localeCode = usePreferencesStore((state) => state.localeCode)
  const setLocaleCode = usePreferencesStore((state) => state.setLocaleCode)
  const themeMode = usePreferencesStore((state) => state.themeMode)
  const setThemeMode = usePreferencesStore((state) => state.setThemeMode)
  const sidebarCollapsed = usePreferencesStore((state) => state.sidebarCollapsed)
  const setSidebarCollapsed = usePreferencesStore((state) => state.setSidebarCollapsed)
  const currentWorkspace = usePreferencesStore((state) => state.currentWorkspace)
  const setCurrentWorkspace = usePreferencesStore((state) => state.setCurrentWorkspace)
  const { t } = useI18n()
  const [businessOpenKeys, setBusinessOpenKeys] = useState<string[]>([])
  const [systemOpenKeys, setSystemOpenKeys] = useState<string[]>([])
  const [systemThemeVersion, setSystemThemeVersion] = useState(0)

  const snapshot = permissionSnapshotQuery.data?.data
  const fullSidebarNav = useMemo(() => getAccessibleSidebarNav(snapshot), [snapshot])
  const accessibleWorkspaces = useMemo(() => getAccessibleWorkspaces(snapshot), [snapshot])
  const accessibleWorkbenchIds = useMemo(() => getAccessibleWorkbenchIds(snapshot), [snapshot])
  const workbenchOptions = useMemo(() => buildWorkbenchOptions(localeCode).filter((item) => accessibleWorkbenchIds.includes(item.key)), [accessibleWorkbenchIds, localeCode])
  const preferredWorkspace = useMemo(
    () => findPreferredWorkspace(snapshot, currentWorkspace, user?.roles ?? []),
    [snapshot, currentWorkspace, user?.roles],
  )
  const currentMeta = getRouteMeta(location.pathname)
  const currentWorkbenchId = getRouteWorkbenchId(currentMeta)
  const currentScopeMode = getRouteScopeMode(currentMeta)
  const currentRouteWorkspace = getRouteWorkspace(currentMeta)
  const isSystemWorkspaceRoute = currentRouteWorkspace === 'system'
  const activeWorkspace = useMemo<BusinessWorkspaceType | null>(() => {
    if ((currentRouteWorkspace === 'application' || currentRouteWorkspace === 'resource') && accessibleWorkspaces.includes(currentRouteWorkspace)) {
      return currentRouteWorkspace
    }
    return preferredWorkspace
  }, [accessibleWorkspaces, currentRouteWorkspace, preferredWorkspace])
  const activeWorkbenchId = useMemo<WorkbenchId | null>(() => {
    if (currentWorkbenchId && accessibleWorkbenchIds.includes(currentWorkbenchId)) {
      return currentWorkbenchId
    }
    if (activeWorkspace === 'application' && accessibleWorkbenchIds.includes('delivery')) {
      return 'delivery'
    }
    if (activeWorkspace === 'resource') {
      return (['platform', 'virtualization', 'docker', 'ai', 'aiGateway', 'monitoring'] as const).find((item) => accessibleWorkbenchIds.includes(item)) ?? null
    }
    return accessibleWorkbenchIds[0] ?? null
  }, [accessibleWorkbenchIds, activeWorkspace, currentWorkbenchId])
  const businessWorkspaceNav = useMemo(
    () => (activeWorkspace ? filterSidebarNavByWorkspace(fullSidebarNav, activeWorkspace) : []),
    [activeWorkspace, fullSidebarNav],
  )
  const businessNav = useMemo(() => {
    if (!activeWorkbenchId) {
      return businessWorkspaceNav
    }
    return filterSidebarNavByWorkbench(businessWorkspaceNav, activeWorkbenchId)
  }, [activeWorkbenchId, businessWorkspaceNav])
  const systemNav = useMemo(
    () => filterSidebarNavByWorkspace(fullSidebarNav, 'system'),
    [fullSidebarNav],
  )
  const systemWorkbenchNav = useMemo(() => {
    if (!activeWorkbenchId) {
      return systemNav
    }
    return filterSidebarNavByWorkbench(systemNav, activeWorkbenchId)
  }, [activeWorkbenchId, systemNav])
  const primaryNav = useMemo(() => {
    if (isSystemWorkspaceRoute) {
      return systemWorkbenchNav.length > 0 ? systemWorkbenchNav : systemNav
    }
    return businessNav
  }, [businessNav, isSystemWorkspaceRoute, systemNav, systemWorkbenchNav])
  const primaryMenuItems = useMemo(
    () => activeWorkbenchId === 'ai'
      ? buildAIWorkbenchMenuItems(snapshot)
      : buildMenuItems(primaryNav, localeCode, { grouped: !isSystemWorkspaceRoute && activeWorkbenchId !== 'virtualization' && activeWorkbenchId !== 'docker' }),
    [activeWorkbenchId, isSystemWorkspaceRoute, localeCode, primaryNav, snapshot],
  )
  const primaryItemKeyToPath = useMemo(
    () => {
      if (activeWorkbenchId === 'ai') {
        const suffix = buildAIWorkbenchSearch(location.search)
        return Object.fromEntries(Object.entries(buildAIWorkbenchItemKeyToPath(snapshot)).map(([key, path]) => [key, `${path}${suffix}`]))
      }
      return buildItemKeyToPath(primaryNav)
    },
    [activeWorkbenchId, location.search, primaryNav, snapshot],
  )
  const combinedNav = useMemo(() => [...businessNav, ...systemNav], [businessNav, systemNav])
  const combinedItemKeyToPath = useMemo(
    () => ({ ...buildItemKeyToPath(businessNav), ...buildItemKeyToPath(systemNav) }),
    [businessNav, systemNav],
  )
  const parentByID = useMemo(() => buildParentMap(combinedNav), [combinedNav])
  const nodeByID = useMemo(() => buildNodeByID(combinedNav), [combinedNav])
  const businessNodeIDs = useMemo(() => collectNodeIDs(businessNav), [businessNav])
  const businessExpandableNodeIDs = useMemo(() => collectExpandableNodeIDs(businessNav), [businessNav])
  const systemNodeIDs = useMemo(() => collectNodeIDs(systemNav), [systemNav])
  const systemExpandableNodeIDs = useMemo(() => collectExpandableNodeIDs(systemNav), [systemNav])
  const resolvedThemeMode = useMemo(() => resolveThemeMode(themeMode), [themeMode, systemThemeVersion])

  const parentMeta = getParentRouteMeta(currentMeta)
  const currentMenuID = useMemo(() => {
    if (activeWorkbenchId === 'ai') {
      return findAIWorkbenchMenuKey(location.pathname, location.search)
        ?? resolveRouteMenuId(currentMeta)
        ?? currentMeta.menuId
        ?? currentMeta.id
    }
    return findMenuIDByRoutePath(combinedNav, currentMeta.path) ?? resolveRouteMenuId(currentMeta) ?? currentMeta.menuId ?? currentMeta.id
  }, [activeWorkbenchId, combinedNav, currentMeta, location.pathname, location.search])

  const selectedKeys = useMemo(() => {
    if (activeWorkbenchId === 'ai' && currentMenuID && primaryItemKeyToPath[currentMenuID]) {
      return [currentMenuID]
    }
    if (currentMenuID && combinedItemKeyToPath[currentMenuID]) {
      return [currentMenuID]
    }
    if (!currentMeta.navVisible && parentMeta?.navVisible && combinedItemKeyToPath[parentMeta.id]) {
      return [parentMeta.id]
    }
    return [currentMeta.id]
  }, [activeWorkbenchId, combinedItemKeyToPath, currentMenuID, currentMeta, parentMeta, primaryItemKeyToPath])

  const routeOpenKeys = useMemo(() => {
    if (activeWorkbenchId === 'ai') {
      return []
    }
    const keys: string[] = []
    let pointer = currentMenuID
    while (pointer && parentByID.has(pointer)) {
      const parentID = parentByID.get(pointer)
      if (!parentID) break
      keys.unshift(parentID)
      pointer = parentID
    }
    if (currentMenuID && (businessExpandableNodeIDs.has(currentMenuID) || systemExpandableNodeIDs.has(currentMenuID))) {
      keys.push(currentMenuID)
    }
    return keys
  }, [activeWorkbenchId, businessExpandableNodeIDs, currentMenuID, parentByID, systemExpandableNodeIDs])

  useEffect(() => {
    if (sidebarCollapsed) {
      setBusinessOpenKeys([])
      setSystemOpenKeys([])
      return
    }
    const desiredBusiness = routeOpenKeys.filter((key) => businessNodeIDs.has(key))
    const desiredSystem = routeOpenKeys.filter((key) => systemNodeIDs.has(key))
    setBusinessOpenKeys((current) => mergeOpenKeys(current, desiredBusiness))
    setSystemOpenKeys((current) => mergeOpenKeys(current, desiredSystem))
  }, [businessNodeIDs, routeOpenKeys, sidebarCollapsed, systemNodeIDs])

  useEffect(() => {
    if (themeMode !== 'system') return undefined
    return watchSystemThemeMode(() => setSystemThemeVersion((current) => current + 1))
  }, [themeMode])

  useEffect(() => {
    if (!snapshot) {
      return
    }
    if ((currentRouteWorkspace === 'application' || currentRouteWorkspace === 'resource') && accessibleWorkspaces.includes(currentRouteWorkspace)) {
      if (currentWorkspace !== currentRouteWorkspace) {
        setCurrentWorkspace(currentRouteWorkspace)
      }
      return
    }
    if ((currentWorkspace == null || !accessibleWorkspaces.includes(currentWorkspace)) && preferredWorkspace && currentWorkspace !== preferredWorkspace) {
      setCurrentWorkspace(preferredWorkspace)
    }
  }, [accessibleWorkspaces, currentRouteWorkspace, currentWorkspace, preferredWorkspace, setCurrentWorkspace, snapshot])

  const currentWorkbenchOption = workbenchOptions.find((option) => option.key === activeWorkbenchId) ?? workbenchOptions[0] ?? null
  const breadcrumbRoutes = useMemo(() => {
    const routes: Array<{ name: string; path?: string }> = []
    const resolveBreadcrumbTitle = (route: typeof currentMeta) => {
      const menuID = route.navVisible !== false
        ? findMenuIDByRoutePath(combinedNav, route.path) ?? route.menuId
        : undefined
      const menuNode = menuID ? nodeByID.get(menuID) : null
      if (menuNode) {
        return getRuntimeMenuNodeLabel(menuNode, localeCode)
      }
      if (route.id === 'overview') {
        return t('route.overview.breadcrumbTitle', localeCode === 'zh_CN' ? '总览' : 'Overview')
      }
      return t(`route.${route.id}.title`, route.title)
    }

    if (currentWorkbenchOption) {
      routes.push({ name: currentWorkbenchOption.label })
    }

    const routeChain: typeof currentMeta[] = []
    const seenRouteIds = new Set<string>()
    let pointer: typeof currentMeta | null = currentMeta
    while (pointer && !seenRouteIds.has(pointer.id)) {
      routeChain.unshift(pointer)
      seenRouteIds.add(pointer.id)
      pointer = getParentRouteMeta(pointer)
    }

    const workbenchRootRouteIds = activeWorkbenchId ? (BREADCRUMB_WORKBENCH_ROOT_ROUTE_IDS[activeWorkbenchId] ?? []) : []

    for (const route of routeChain) {
      if (workbenchRootRouteIds.includes(route.id)) {
        continue
      }
      const name = resolveBreadcrumbTitle(route)
      const previous = routes[routes.length - 1]
      if (currentWorkbenchOption?.label === name || previous?.name === name) {
        continue
      }
      routes.push({
        name,
        path: route.id === currentMeta.id ? undefined : route.redirectTo ?? route.path,
      })
    }

    return routes
  }, [activeWorkbenchId, combinedNav, currentMeta, currentWorkbenchOption, localeCode, nodeByID, t])

  const userDisplayName = user?.userName ?? user?.email ?? 'User'
  const branding = getNormalizedBranding(brandingQuery.data?.data)
  const expandedLogo = branding.expandedLogoUrl
  const collapsedLogo = branding.collapsedLogoUrl || branding.expandedLogoUrl
  const activeLogo = sidebarCollapsed ? (collapsedLogo || expandedLogo) : (expandedLogo || collapsedLogo)
  const languageSwitchLabel = localeCode === 'zh_CN' ? 'EN' : '中文'
  const languageSwitchTitle = localeCode === 'zh_CN'
    ? t('layout.switchLanguageToEnglish', 'Switch to English')
    : t('layout.switchLanguageToChinese', '切换到中文')
  const themeSwitchTitle = resolvedThemeMode === 'dark'
    ? t('layout.switchThemeToLight', 'Switch to light mode')
    : t('layout.switchThemeToDark', '切换到深色模式')
  const businessSelectedKeys = selectedKeys.filter((key) => businessNodeIDs.has(key))
  const systemSelectedKeys = selectedKeys.filter((key) => systemNodeIDs.has(key))
  const aiSelectedKeys = selectedKeys.filter((key) => primaryItemKeyToPath[key])
  const primarySelectedKeys = activeWorkbenchId === 'ai' ? aiSelectedKeys : isSystemWorkspaceRoute ? systemSelectedKeys : businessSelectedKeys
  const primaryOpenKeys = isSystemWorkspaceRoute ? systemOpenKeys : businessOpenKeys
  const platformHeaderScopeMode = activeWorkbenchId === 'platform' && (currentScopeMode === 'cluster' || currentScopeMode === 'namespace')
    ? currentScopeMode
    : 'hidden'

  if (permissionSnapshotQuery.isLoading) {
    return (
      <div className="flex items-center justify-center h-screen">
        <Spin size="large" />
      </div>
    )
  }

  return (
    <Layout className="soha-shell">
      <Sider
        className="soha-sider"
        collapsible
        collapsed={sidebarCollapsed}
        collapsedWidth={SIDEBAR_COLLAPSED_WIDTH}
        onCollapse={(collapsed) => setSidebarCollapsed(collapsed)}
        style={{ backgroundColor: 'transparent' }}
        trigger={null}
        width={SIDEBAR_WIDTH}
      >
          <div className="soha-nav" style={{ height: '100%' }}>
            <div className="soha-sider-topbar">
              <button
                type="button"
                className="soha-sider-brand"
                aria-label={localeCode === 'zh_CN' ? '返回首页' : 'Go to overview'}
                onClick={() => navigate('/')}
              >
                {activeLogo ? (
                  <img className="soha-brand-logo" src={activeLogo} alt={branding.sidebarTitle} />
                ) : (
                  <div className="soha-brand-mark">SOHA</div>
                )}
              </button>
            </div>

          {workbenchOptions.length > 0 && currentWorkbenchOption ? (
            <div className="soha-workbench-switcher-shell">
              <WorkbenchSwitcher
                collapsed={sidebarCollapsed}
                current={currentWorkbenchOption}
                options={workbenchOptions}
                onSelect={(workbench) => {
                  const targetPath = findFirstAccessiblePathForWorkbench(workbench, snapshot)
                  if (!targetPath) {
                    return
                  }
                  navigate(targetPath)
                }}
              />
            </div>
          ) : null}

          {(
            <div className={['soha-nav-business', isSystemWorkspaceRoute ? 'is-system' : ''].filter(Boolean).join(' ')}>
              <Menu
                className={['soha-nav-menu', isSystemWorkspaceRoute ? 'soha-nav-menu--system-workspace' : 'soha-nav-menu--business'].join(' ')}
                mode="inline"
                items={primaryMenuItems}
                selectedKeys={primarySelectedKeys}
                openKeys={sidebarCollapsed ? [] : primaryOpenKeys}
                onOpenChange={(keys) => {
                  if (isSystemWorkspaceRoute) {
                    setSystemOpenKeys(keys as string[])
                    return
                  }
                  setBusinessOpenKeys(keys as string[])
                }}
                onClick={({ key }) => {
                  const path = primaryItemKeyToPath[String(key)]
                  if (path) navigate(path)
                }}
                inlineIndent={isSystemWorkspaceRoute ? 16 : 8}
                inlineCollapsed={sidebarCollapsed}
                theme={resolvedThemeMode}
              />
            </div>
          )}
        </div>
      </Sider>

      <Layout className="soha-main">
        <Header className="soha-header">
          <div className="soha-header-top-row">
            <div className="soha-header-main">
              <div className="soha-header-breadcrumb-row">
                <Button
                  aria-label={sidebarCollapsed ? t('layout.expand', 'Expand sidebar') : t('layout.collapse', 'Collapse sidebar')}
                  className="soha-header-action soha-header-sider-toggle"
                  type="text"
                  icon={sidebarCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
                  onClick={() => setSidebarCollapsed(!sidebarCollapsed)}
                />
                <Breadcrumb
                  items={breadcrumbRoutes.map((route) => ({
                    title: route.path ? (
                      <a
                        href={route.path}
                        onClick={(event) => {
                          event.preventDefault()
                          navigate(route.path!)
                        }}
                      >
                        {route.name}
                      </a>
                    ) : route.name,
                  }))}
                />
              </div>
            </div>
            {platformHeaderScopeMode !== 'hidden' ? (
              <div className="soha-header-context">
                <PlatformScopeTrigger scopeMode={platformHeaderScopeMode} />
              </div>
            ) : null}
            <div className="soha-header-right">
              <Button
                className="soha-header-action"
                size="small"
                type="text"
                icon={<QuestionCircleOutlined />}
                onClick={() => window.open('/docs/', '_blank', 'noopener,noreferrer')}
              >
                {t('layout.docs', 'Docs')}
              </Button>
              <div className="soha-header-preferences">
                <HeaderPreferenceButton
                  ariaLabel={languageSwitchTitle}
                  title={languageSwitchTitle}
                  inset
                  icon={<TranslationOutlined />}
                  label={languageSwitchLabel}
                  onClick={() => setLocaleCode(localeCode === 'zh_CN' ? 'en_US' : 'zh_CN')}
                />
                <HeaderPreferenceButton
                  ariaLabel={themeSwitchTitle}
                  title={themeSwitchTitle}
                  icon={resolvedThemeMode === 'dark' ? <SunOutlined /> : <MoonOutlined />}
                  onClick={() => setThemeMode(resolvedThemeMode === 'dark' ? 'light' : 'dark')}
                  pressed={resolvedThemeMode === 'dark'}
                />
              </div>
              <AnnouncementBell />
              <Dropdown
                menu={{
                  items: [
                    { key: 'user', label: userDisplayName, disabled: true },
                    { type: 'divider' },
                    { key: 'profile', icon: <UserOutlined />, label: t('layout.profile', '个人中心') },
                    { type: 'divider' },
                    { key: 'logout', icon: <LogoutOutlined />, label: t('layout.logout', 'Sign out') },
                  ],
                  onClick: ({ key }) => {
                    if (key === 'profile') {
                      navigate('/account/profile')
                      return
                    }
                    if (key === 'logout') {
                      void logoutAuthSession().finally(() => navigate('/login'))
                    }
                  },
                }}
                placement="bottomRight"
                trigger={['click']}
              >
                <Button
                  className="soha-header-action soha-user-trigger"
                  size="small"
                  type="text"
                  icon={
                    <Avatar className="soha-user-avatar" size="small">
                      {userDisplayName.charAt(0).toUpperCase()}
                    </Avatar>
                  }
                >
                  {userDisplayName}
                </Button>
              </Dropdown>
            </div>
          </div>
        </Header>

        <Content className="soha-content">
          <div className="soha-content-inner soha-pro-content-host">
            <Outlet />
          </div>
        </Content>
      </Layout>
    </Layout>
  )
}
