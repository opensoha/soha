import type { CSSProperties, ReactNode } from 'react'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { Link, Outlet, useLocation, useMatch, useNavigate } from 'react-router-dom'
import { Avatar, Breadcrumb, Button, Dropdown, Layout, Menu, Spin } from 'antd'
import { useQuery } from '@tanstack/react-query'
import type { MenuProps } from 'antd'
import {
  AlertOutlined,
  AppstoreOutlined,
  CloudServerOutlined,
  DownOutlined,
  FileAddOutlined,
  HomeOutlined,
  InfoCircleOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  MoonOutlined,
  QuestionCircleOutlined,
  RobotOutlined,
  SettingOutlined,
  SlidersOutlined,
  SunOutlined,
  TranslationOutlined,
  LogoutOutlined,
  LockOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { HeaderActionButton } from '@/components/header-action-button'
import { PlatformScopeTrigger } from '@/components/platform-scope-toolbar'
import { AnnouncementBell } from '@/features/announcements/announcement-center'
import { logoutAuthSession } from '@/features/auth/auth-api'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { AssistantCompanionOverlay } from '@/features/companion'
import { deliveryQueries } from '@/features/delivery/queries'
import {
  isComputeWorkbenchMenuGroup,
  normalizeComputeWorkbenchNav,
} from '@/features/compute/navigation'
import { GlobalAIAssistantProvider } from '@/features/copilot/global-assistant'
import {
  filterMenuByModuleFeatures,
  GLOBAL_ASSISTANT_FEATURE,
  getWorkbenchModuleFeature,
  useModuleStatuses,
} from '@/features/modules'
import { resolveMenuIcon } from '@/features/system/menu-icons'
import { MENU_WORKBENCH_ORDER } from '@/features/system'
import { resolveMenuSectionLabel } from '@/features/system/menu-schema'
import {
  RealtimeSessionDockProvider,
  RealtimeSessionDockTrigger,
} from '@/features/platform/session-dock'
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
  canAccessRoute,
  type WorkbenchId,
} from '@/routes/meta'
import { getNormalizedBranding, useBrandingSettings } from '@/features/settings'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { resolveThemeMode, watchSystemThemeMode } from '@/theme/app-theme'
import { resolveBrandingLogos } from '@/utils/branding'
import type { BusinessWorkspaceType, RuntimeMenuNode } from '@/types'

const { Sider, Header, Content } = Layout
const SIDEBAR_WIDTH = 200
const SIDEBAR_COLLAPSED_WIDTH = 55
const ApplicationNavigation = lazy(async () => {
  const module = await import('@/features/delivery/navigation')
  return { default: module.ApplicationNavigation }
})
const ApplicationIdentity = lazy(async () => {
  const module = await import('@/features/delivery/navigation')
  return { default: module.ApplicationIdentity }
})
const GlobalResourceCreateModal = lazy(async () => {
  const module =
    await import('@/features/platform/resource-creation/components/global-create-modal')
  return { default: module.GlobalResourceCreateModal }
})
const BREADCRUMB_WORKBENCH_ROOT_ROUTE_IDS: Partial<Record<WorkbenchId, string[]>> = {
  home: ['provider-portal'],
  ai: ['ai-workbench'],
  compute: ['compute-workbench'],
  monitoring: ['monitoring-workbench'],
  settings: ['settings', 'extension-center'],
  security: ['internal-workbench'],
}

interface WorkbenchOption {
  description: string
  icon: ReactNode
  key: WorkbenchId | 'account'
  label: string
}

const RUNTIME_MENU_LABEL_OVERRIDES: Record<
  string,
  { en: string; legacyEn: string[]; legacyZh: string[]; zh: string }
> = {
  operations: {
    zh: '操作日志',
    en: 'Operation Logs',
    legacyZh: ['操作'],
    legacyEn: ['Operations'],
  },
  audit: { zh: '审计日志', en: 'Audit Logs', legacyZh: ['审计'], legacyEn: ['Audit'] },
  'delivery-blueprints': {
    zh: '应用接入模板',
    en: 'Onboarding Templates',
    legacyZh: ['交付蓝图'],
    legacyEn: ['Delivery Blueprints'],
  },
  'release-board': {
    zh: '工作流中心',
    en: 'Workflow Center',
    legacyZh: ['发布看板', '构建发布', '工作流与构建'],
    legacyEn: ['Release Board', 'Build & Release', 'Workflows & Builds'],
  },
  releases: {
    zh: '发布记录',
    en: 'Release Records',
    legacyZh: ['发布', '发布管理'],
    legacyEn: ['Releases', 'Release Management'],
  },
}

function buildAIWorkbenchSearch(search: string) {
  const source = new URLSearchParams(search)
  const next = new URLSearchParams()
  ;[
    'session',
    'clusterId',
    'namespace',
    'workload',
    'service',
    'pod',
    'node',
    'alertId',
    'timeRangeMinutes',
    'sourceWorkbench',
    'entityKind',
    'entityName',
  ].forEach((key) => {
    const value = source.get(key)
    if (value) {
      next.set(key, value)
    }
  })
  const suffix = next.toString()
  return suffix ? `?${suffix}` : ''
}

export function resolveDynamicBreadcrumbName(routePath: string, pathname: string) {
  const routeSegments = routePath.split('/').filter(Boolean)
  const pathSegments = pathname.split('/').filter(Boolean)
  const finalRouteSegment = routeSegments[routeSegments.length - 1]
  if (!finalRouteSegment?.startsWith(':') || routeSegments.length !== pathSegments.length) {
    return undefined
  }

  const value = pathSegments[pathSegments.length - 1]
  if (!value) return undefined
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

function resolveRuntimeMenuLabel(node: RuntimeMenuNode, localeCode: 'zh_CN' | 'en_US') {
  const override = RUNTIME_MENU_LABEL_OVERRIDES[node.id]
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

function buildMenuNodeItem(
  node: RuntimeMenuNode,
  localeCode: 'zh_CN' | 'en_US',
): NonNullable<MenuProps['items']>[number] {
  const label = resolveRuntimeMenuLabel(node, localeCode)
  const icon = resolveMenuIcon(node.iconKey)
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

function buildMenuItems(
  sidebarNav: RuntimeMenuNode[],
  localeCode: 'zh_CN' | 'en_US',
): MenuProps['items'] {
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
    label: (
      <span className="soha-nav-section-title">
        {resolveMenuSectionLabel(groupKey, localeCode)}
      </span>
    ),
    children: items,
  }))

  return [...directItems, ...groupedItems]
}

function buildComputeMenuItems(
  sidebarNav: RuntimeMenuNode[],
  localeCode: 'zh_CN' | 'en_US',
): MenuProps['items'] {
  return sidebarNav.flatMap((node) => {
    if (!isComputeWorkbenchMenuGroup(node.id)) {
      return [buildMenuNodeItem(node, localeCode)]
    }
    if (!node.children?.length) {
      return []
    }
    return [
      {
        key: `group-${node.id}`,
        type: 'group' as const,
        label: (
          <span className="soha-nav-section-title">
            {resolveRuntimeMenuLabel(node, localeCode)}
          </span>
        ),
        children: node.children.map((child) => buildMenuNodeItem(child, localeCode)),
      },
    ]
  })
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
  return merged.length === current.length && merged.every((key, index) => key === current[index])
    ? current
    : merged
}

function buildWorkbenchOptions(localeCode: 'zh_CN' | 'en_US'): WorkbenchOption[] {
  if (localeCode === 'en_US') {
    return [
      {
        key: 'home',
        label: 'Portal',
        description: 'Application portal and unified workspace entry',
        icon: <HomeOutlined />,
      },
      {
        key: 'platform',
        label: 'K8s Workbench',
        description:
          'Operations dashboard for clusters, workloads, network, storage, and runtime resources',
        icon: <AppstoreOutlined />,
      },
      {
        key: 'delivery',
        label: 'Continuous Delivery Workbench',
        description: 'Build, test, and release applications',
        icon: <CloudServerOutlined />,
      },
      {
        key: 'monitoring',
        label: 'Observability Workbench',
        description: 'Logs, alerts, routes, notifications, and on-call flows',
        icon: <AlertOutlined />,
      },
      {
        key: 'compute',
        label: 'Compute Resources',
        description: 'Virtualization, runtime hosts, containers, and operation tasks',
        icon: <SlidersOutlined />,
      },
      {
        key: 'ai',
        label: 'AI Workbench',
        description: 'Investigation, automation, tools, and skills',
        icon: <img src="/heya-avatar.svg" width={24} height={24} alt="" />,
      },
      {
        key: 'security',
        label: 'Internal Workbench',
        description: 'Identity providers, applications, and access integrations',
        icon: <LockOutlined />,
      },
      {
        key: 'settings',
        label: 'Settings Center',
        description: 'Login, branding, monitoring, and AI settings',
        icon: <SettingOutlined />,
      },
    ]
  }
  return [
    {
      key: 'home',
      label: '门户',
      description: '应用门户与统一工作台入口',
      icon: <HomeOutlined />,
    },
    {
      key: 'platform',
      label: 'k8s工作台',
      description: '面向运维视角的集群、工作负载、网络、存储与运行资源',
      icon: <AppstoreOutlined />,
    },
    {
      key: 'delivery',
      label: '持续交付工作台',
      description: '应用构建、测试与发布',
      icon: <CloudServerOutlined />,
    },
    {
      key: 'monitoring',
      label: '可观测性工作台',
      description: '日志、告警、路由、通知和值班协同',
      icon: <AlertOutlined />,
    },
    {
      key: 'compute',
      label: '计算资源工作台',
      description: '虚拟化、运行时主机、容器管理与任务追踪',
      icon: <SlidersOutlined />,
    },
    {
      key: 'ai',
      label: 'AI工作台',
      description: '调查、自动化、工具与技能',
      icon: <RobotOutlined />,
    },
    {
      key: 'security',
      label: '内网工作台',
      description: '身份提供商、应用目录与接入集成',
      icon: <LockOutlined />,
    },
    {
      key: 'settings',
      label: '设置中心',
      description: '登录、品牌、监控与 AI 设置',
      icon: <SettingOutlined />,
    },
  ]
}

function WorkbenchSwitcher({
  collapsed,
  current,
  onSelect,
  options,
  onAssistant,
  assistantLabel,
}: {
  collapsed: boolean
  current: WorkbenchOption
  onSelect: (workbench: WorkbenchOption['key']) => void
  options: WorkbenchOption[]
  onAssistant?: () => void
  assistantLabel?: string
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
  if (onAssistant)
    dropdownItems.splice(Math.max(0, options.findIndex((option) => option.key === 'home') + 1), 0, {
      key: 'ai-assistant',
      label: (
        <div className="soha-workspace-option">
          <span className="soha-workspace-option__icon">
            <RobotOutlined />
          </span>
          <span className="soha-workspace-option__copy">
            <span className="soha-workspace-option__label">{assistantLabel}</span>
          </span>
        </div>
      ),
    })

  const trigger = (
    <Button aria-label={current.label} className="soha-workbench-switcher" type="text">
      <span className="soha-workbench-switcher__icon">{current.icon}</span>
      {!collapsed ? (
        <span className="soha-workbench-switcher__copy">
          <span className="soha-workbench-switcher__label">{current.label}</span>
          <span className="soha-workbench-switcher__desc">{current.description}</span>
        </span>
      ) : null}
      {!collapsed && (options.length > 1 || onAssistant) ? (
        <DownOutlined className="soha-workbench-switcher__arrow" />
      ) : null}
    </Button>
  )

  if (options.length <= 1 && !onAssistant) {
    return trigger
  }

  return (
    <Dropdown
      menu={{
        items: dropdownItems,
        selectable: true,
        selectedKeys: [current.key],
        onClick: ({ key }) =>
          key === 'ai-assistant'
            ? onAssistant?.()
            : onSelect(String(key) as WorkbenchOption['key']),
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
  const moduleStatusesQuery = useModuleStatuses()
  const localeCode = usePreferencesStore((state) => state.localeCode)
  const setLocaleCode = usePreferencesStore((state) => state.setLocaleCode)
  const themeMode = usePreferencesStore((state) => state.themeMode)
  const setThemeMode = usePreferencesStore((state) => state.setThemeMode)
  const preferredSidebarCollapsed = usePreferencesStore((state) => state.sidebarCollapsed)
  const setSidebarCollapsed = usePreferencesStore((state) => state.setSidebarCollapsed)
  // Narrow-screen navigation is temporary; keep the user's desktop preference.
  const [narrowSidebarCollapsed, setNarrowSidebarCollapsed] = useState<boolean>()
  const sidebarCollapsed = narrowSidebarCollapsed ?? preferredSidebarCollapsed
  const currentWorkspace = usePreferencesStore((state) => state.currentWorkspace)
  const [resourceCreateOpenedByAction, setResourceCreateOpenedByAction] = useState(false)
  const resourceCreateRequested = useMemo(
    () => new URLSearchParams(location.search).get('createResource') === '1',
    [location.search],
  )
  const resourceCreateOpen = resourceCreateOpenedByAction || resourceCreateRequested
  const setCurrentWorkspace = usePreferencesStore((state) => state.setCurrentWorkspace)
  const { t } = useI18n()
  const [businessOpenKeys, setBusinessOpenKeys] = useState<string[]>([])
  const [systemOpenKeys, setSystemOpenKeys] = useState<string[]>([])
  const [systemThemeVersion, setSystemThemeVersion] = useState(0)

  const snapshot = permissionSnapshotQuery.data?.data
  const fullSidebarNav = useMemo(
    () => filterMenuByModuleFeatures(getAccessibleSidebarNav(snapshot), moduleStatusesQuery.data),
    [moduleStatusesQuery.data, snapshot],
  )
  const accessibleWorkspaces = useMemo(() => getAccessibleWorkspaces(snapshot), [snapshot])
  const accessibleWorkbenchIds = useMemo(() => getAccessibleWorkbenchIds(snapshot), [snapshot])
  const workbenchOptions = useMemo(
    () =>
      buildWorkbenchOptions(localeCode)
        .filter((item) => item.key !== 'account' && accessibleWorkbenchIds.includes(item.key))
        .sort(
          (left, right) =>
            MENU_WORKBENCH_ORDER.findIndex((key) => key === left.key) -
            MENU_WORKBENCH_ORDER.findIndex((key) => key === right.key),
        ),
    [accessibleWorkbenchIds, localeCode],
  )
  const accountWorkbenchOption = useMemo<WorkbenchOption>(
    () => ({
      key: 'account',
      label: t('layout.accountSettings', '个人设置'),
      description: t('layout.accountCenterDescription', '个人中心与账号信息'),
      icon: <SettingOutlined />,
    }),
    [t],
  )
  const accountSidebarItems = useMemo<NonNullable<MenuProps['items']>>(
    () => [
      {
        key: '/account/profile',
        icon: <UserOutlined />,
        label: t('layout.profile', '个人中心'),
      },
      { key: '/about', icon: <InfoCircleOutlined />, label: t('layout.about', '关于') },
    ],
    [t],
  )
  const homePath = useMemo(() => findFirstAccessiblePathForWorkbench('home', snapshot), [snapshot])
  const preferredWorkspace = useMemo(
    () => findPreferredWorkspace(snapshot, currentWorkspace, user?.roles ?? []),
    [snapshot, currentWorkspace, user?.roles],
  )
  const currentMeta = getRouteMeta(location.pathname)
  const isApplicationWorkspace = [
    'application-detail',
    'application-workload-detail',
    'application-workflow-designer',
  ].includes(currentMeta.id)
  const applicationMatch = useMatch('/applications/:applicationId/*')
  const workloadMatch = useMatch(
    '/applications/:applicationId/application-environments/:applicationEnvironmentId/workloads/:workloadName',
  )
  const applicationSearch = new URLSearchParams(location.search)
  const sessionEnvironmentId =
    workloadMatch?.params.applicationEnvironmentId ||
    applicationSearch.get('applicationEnvironmentId') ||
    (currentMeta.id === 'application-workflow-designer' ? applicationSearch.get('bindingId') : '')
  const canReadApplicationSessions =
    isApplicationWorkspace && hasPermission(snapshot, 'delivery.applications.view')
  const sessionRuntime = useQuery(
    deliveryQueries.applications.runtime(
      applicationMatch?.params.applicationId || '',
      canReadApplicationSessions,
    ),
  )
  const sessionEnvironment = sessionEnvironmentId
    ? sessionRuntime.data?.environments?.find(
        (environment) => environment.applicationEnvironmentId === sessionEnvironmentId,
      )
    : sessionRuntime.data?.environments?.[0]
  const deliverySessionScope =
    canReadApplicationSessions &&
    !sessionRuntime.isError &&
    applicationMatch?.params.applicationId &&
    sessionEnvironment
      ? JSON.stringify([
          applicationMatch.params.applicationId,
          sessionEnvironment.applicationEnvironmentId,
        ])
      : undefined
  const isAccountUtilityRoute = currentMeta.group === 'account'
  const currentWorkbenchId = getRouteWorkbenchId(currentMeta)
  const currentScopeMode = getRouteScopeMode(currentMeta)
  const currentRouteWorkspace = getRouteWorkspace(currentMeta)
  const isSystemWorkspaceRoute = currentRouteWorkspace === 'system'
  const activeWorkspace = useMemo<BusinessWorkspaceType | null>(() => {
    if (
      (currentRouteWorkspace === 'application' || currentRouteWorkspace === 'resource') &&
      accessibleWorkspaces.includes(currentRouteWorkspace)
    ) {
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
      return (
        (['platform', 'compute', 'ai', 'monitoring'] as const).find((item) =>
          accessibleWorkbenchIds.includes(item),
        ) ?? null
      )
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
    const filtered = filterSidebarNavByWorkbench(businessWorkspaceNav, activeWorkbenchId)
    return activeWorkbenchId === 'compute' ? normalizeComputeWorkbenchNav(filtered) : filtered
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
    () =>
      activeWorkbenchId === 'compute'
        ? buildComputeMenuItems(primaryNav, localeCode)
        : buildMenuItems(primaryNav, localeCode),
    [activeWorkbenchId, localeCode, primaryNav],
  )
  const primaryParentByID = useMemo(() => buildParentMap(primaryNav), [primaryNav])
  const primaryNodeByID = useMemo(() => buildNodeByID(primaryNav), [primaryNav])
  const primaryItemKeyToPath = useMemo(() => {
    const paths = buildItemKeyToPath(primaryNav)
    if (activeWorkbenchId !== 'ai') return paths
    const suffix = buildAIWorkbenchSearch(location.search)
    return Object.fromEntries(Object.entries(paths).map(([key, path]) => [key, `${path}${suffix}`]))
  }, [activeWorkbenchId, location.search, primaryNav])
  const combinedNav = useMemo(() => [...businessNav, ...systemNav], [businessNav, systemNav])
  const combinedItemKeyToPath = useMemo(
    () => ({ ...buildItemKeyToPath(businessNav), ...buildItemKeyToPath(systemNav) }),
    [businessNav, systemNav],
  )
  const parentByID = useMemo(() => buildParentMap(combinedNav), [combinedNav])
  const nodeByID = useMemo(() => buildNodeByID(combinedNav), [combinedNav])
  const businessNodeIDs = useMemo(() => collectNodeIDs(businessNav), [businessNav])
  const businessExpandableNodeIDs = useMemo(
    () => collectExpandableNodeIDs(businessNav),
    [businessNav],
  )
  const systemNodeIDs = useMemo(() => collectNodeIDs(systemNav), [systemNav])
  const systemExpandableNodeIDs = useMemo(() => collectExpandableNodeIDs(systemNav), [systemNav])
  const resolvedThemeMode = useMemo(
    () => resolveThemeMode(themeMode),
    [themeMode, systemThemeVersion],
  )

  const parentMeta = getParentRouteMeta(currentMeta)
  const currentMenuID = useMemo(() => {
    return (
      findMenuIDByRoutePath(combinedNav, currentMeta.path) ??
      resolveRouteMenuId(currentMeta) ??
      currentMeta.menuId ??
      currentMeta.id
    )
  }, [combinedNav, currentMeta])

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
  }, [
    activeWorkbenchId,
    combinedItemKeyToPath,
    currentMenuID,
    currentMeta,
    parentMeta,
    primaryItemKeyToPath,
  ])

  const routeOpenKeys = useMemo(() => {
    const keys: string[] = []
    let pointer = currentMenuID
    while (pointer && parentByID.has(pointer)) {
      const parentID = parentByID.get(pointer)
      if (!parentID) break
      keys.unshift(parentID)
      pointer = parentID
    }
    if (
      currentMenuID &&
      (businessExpandableNodeIDs.has(currentMenuID) || systemExpandableNodeIDs.has(currentMenuID))
    ) {
      keys.push(currentMenuID)
    }
    return keys
  }, [businessExpandableNodeIDs, currentMenuID, parentByID, systemExpandableNodeIDs])

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
    if (
      (currentRouteWorkspace === 'application' || currentRouteWorkspace === 'resource') &&
      accessibleWorkspaces.includes(currentRouteWorkspace)
    ) {
      if (currentWorkspace !== currentRouteWorkspace) {
        setCurrentWorkspace(currentRouteWorkspace)
      }
      return
    }
    if (
      (currentWorkspace == null || !accessibleWorkspaces.includes(currentWorkspace)) &&
      preferredWorkspace &&
      currentWorkspace !== preferredWorkspace
    ) {
      setCurrentWorkspace(preferredWorkspace)
    }
  }, [
    accessibleWorkspaces,
    currentRouteWorkspace,
    currentWorkspace,
    preferredWorkspace,
    setCurrentWorkspace,
    snapshot,
  ])

  const currentWorkbenchOption =
    workbenchOptions.find((option) => option.key === activeWorkbenchId) ??
    workbenchOptions[0] ??
    null
  const breadcrumbRoutes = useMemo(() => {
    const routes: Array<{ name: string; path?: string }> = []
    const resolveBreadcrumbTitle = (
      route: typeof currentMeta,
      options: { preferRouteTitle?: boolean } = {},
    ) => {
      if (route.id === currentMeta.id) {
        const dynamicName = resolveDynamicBreadcrumbName(route.path, location.pathname)
        if (dynamicName) return dynamicName
      }
      const menuID =
        !options.preferRouteTitle && route.navVisible !== false
          ? (findMenuIDByRoutePath(combinedNav, route.path) ?? route.menuId)
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

    if (isAccountUtilityRoute) {
      routes.push({ name: accountWorkbenchOption.label, path: '/account/profile' })
      routes.push({ name: resolveBreadcrumbTitle(currentMeta) })
      return routes
    }

    if (currentWorkbenchOption) {
      routes.push({ name: currentWorkbenchOption.label })
    }

    const menuChain: RuntimeMenuNode[] = []
    const seenMenuIds = new Set<string>()
    let menuPointer = currentMenuID ? primaryNodeByID.get(currentMenuID) : null
    while (menuPointer && !seenMenuIds.has(menuPointer.id)) {
      menuChain.unshift(menuPointer)
      seenMenuIds.add(menuPointer.id)
      const parentID = primaryParentByID.get(menuPointer.id)
      menuPointer = parentID ? (primaryNodeByID.get(parentID) ?? null) : null
    }
    const workbenchRootRouteIds = activeWorkbenchId
      ? (BREADCRUMB_WORKBENCH_ROOT_ROUTE_IDS[activeWorkbenchId] ?? [])
      : []
    if (menuChain.length > 0) {
      const menuSection = menuChain[0].section
      if (activeWorkbenchId !== 'compute' && menuSection) {
        routes.push({ name: resolveMenuSectionLabel(menuSection, localeCode) })
      }
      menuChain.forEach((menuNode) => {
        const isCurrentMenuRoute = menuNode.route?.id === currentMeta.id
        routes.push({
          name: getRuntimeMenuNodeLabel(menuNode, localeCode),
          path: isCurrentMenuRoute
            ? undefined
            : (menuNode.route?.redirectTo ?? menuNode.route?.path),
        })
      })

      const representedRouteIds = new Set(
        menuChain.map((menuNode) => menuNode.route?.id).filter(Boolean),
      )
      const missingRouteAncestors: (typeof currentMeta)[] = []
      let routePointer = getParentRouteMeta(currentMeta)
      while (routePointer && !representedRouteIds.has(routePointer.id)) {
        if (routePointer.navVisible !== false && !workbenchRootRouteIds.includes(routePointer.id)) {
          missingRouteAncestors.unshift(routePointer)
        }
        routePointer = getParentRouteMeta(routePointer)
      }
      missingRouteAncestors.forEach((route) => {
        const name = resolveBreadcrumbTitle(route, { preferRouteTitle: true })
        if (routes.some((breadcrumb) => breadcrumb.name === name)) return
        routes.push({ name, path: route.redirectTo ?? route.path })
      })

      const currentMenuRoute = menuChain[menuChain.length - 1].route
      if (currentMenuRoute?.id !== currentMeta.id) {
        routes.push({ name: resolveBreadcrumbTitle(currentMeta) })
      }
      return routes
    }

    const routeChain: (typeof currentMeta)[] = []
    const seenRouteIds = new Set<string>()
    let pointer: typeof currentMeta | null = currentMeta
    while (pointer && !seenRouteIds.has(pointer.id)) {
      routeChain.unshift(pointer)
      seenRouteIds.add(pointer.id)
      pointer = getParentRouteMeta(pointer)
    }

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
        path: route.id === currentMeta.id ? undefined : (route.redirectTo ?? route.path),
      })
    }

    return routes
  }, [
    accountWorkbenchOption,
    activeWorkbenchId,
    combinedNav,
    currentMenuID,
    currentMeta,
    currentWorkbenchOption,
    isAccountUtilityRoute,
    location.pathname,
    localeCode,
    nodeByID,
    primaryNodeByID,
    primaryParentByID,
    t,
  ])

  const userDisplayName = user?.userName ?? user?.email ?? 'User'
  const userAvatarFit =
    user?.avatarFit === 'contain' || user?.avatarFit === 'fill' ? user.avatarFit : 'cover'
  const userAvatarStyle = { '--soha-avatar-fit': userAvatarFit } as CSSProperties
  const branding = getNormalizedBranding(brandingQuery.data?.data)
  const logos = resolveBrandingLogos(branding, resolvedThemeMode)
  const expandedLogo = logos.expandedLogoUrl
  const collapsedLogo = logos.collapsedLogoUrl || logos.expandedLogoUrl
  const activeLogo = sidebarCollapsed
    ? collapsedLogo || expandedLogo
    : expandedLogo || collapsedLogo
  const languageSwitchLabel = localeCode === 'zh_CN' ? '中文' : 'EN'
  const languageSwitchTitle =
    localeCode === 'zh_CN'
      ? t('layout.switchLanguageToEnglish', 'Switch to English')
      : t('layout.switchLanguageToChinese', '切换到中文')
  const themeSwitchTitle =
    resolvedThemeMode === 'dark'
      ? t('layout.switchThemeToLight', 'Switch to light mode')
      : t('layout.switchThemeToDark', '切换到深色模式')
  const businessSelectedKeys = selectedKeys.filter((key) => businessNodeIDs.has(key))
  const systemSelectedKeys = selectedKeys.filter((key) => systemNodeIDs.has(key))
  const aiSelectedKeys = selectedKeys.filter((key) => primaryItemKeyToPath[key])
  const primarySelectedKeys =
    activeWorkbenchId === 'ai'
      ? aiSelectedKeys
      : isSystemWorkspaceRoute
        ? systemSelectedKeys
        : businessSelectedKeys
  const primaryOpenKeys = isSystemWorkspaceRoute ? systemOpenKeys : businessOpenKeys
  const resourceHeaderScopeMode =
    (activeWorkbenchId === 'platform' || activeWorkbenchId === 'monitoring') &&
    (currentScopeMode === 'cluster' || currentScopeMode === 'namespace')
      ? currentScopeMode
      : 'hidden'
  const routeOwnsClusterSearch =
    currentMeta.id === 'extensions' ||
    currentMeta.id === 'extensions-group-detail' ||
    currentMeta.id === 'resource-monitoring' ||
    currentMeta.parentId === 'resource-monitoring'
  const showResourceCreateAction =
    currentWorkbenchId === 'platform' && hasPermission(snapshot, 'platform.resource-creation.use')
  const showRealtimeSessionsAction =
    (currentWorkbenchId === 'platform' ||
      (currentWorkbenchId === 'delivery' && Boolean(deliverySessionScope))) &&
    (hasPermission(snapshot, 'platform.pods.logs') || hasPermission(snapshot, 'platform.pods.exec'))
  const globalAssistantEnabled = getWorkbenchModuleFeature(
    moduleStatusesQuery.data,
    'ai',
    GLOBAL_ASSISTANT_FEATURE,
  )

  function closeResourceCreate() {
    setResourceCreateOpenedByAction(false)
    if (!resourceCreateRequested) return
    const searchParams = new URLSearchParams(location.search)
    searchParams.delete('createResource')
    const search = searchParams.toString()
    navigate({ pathname: location.pathname, search: search ? `?${search}` : '' }, { replace: true })
  }

  if (permissionSnapshotQuery.isLoading) {
    return (
      <div className="flex items-center justify-center h-screen">
        <Spin size="large" />
      </div>
    )
  }

  return (
    <GlobalAIAssistantProvider
      enabled={globalAssistantEnabled}
      permissionSnapshot={snapshot}
      renderCompanion={(props) =>
        currentWorkbenchId === 'settings' || currentWorkbenchId === 'ai' ? null : (
          <AssistantCompanionOverlay {...props} />
        )
      }
    >
      <RealtimeSessionDockProvider
        ownerKey={user?.userId}
        workbench={currentWorkbenchId === 'delivery' ? 'delivery' : 'platform'}
        scopeKey={currentWorkbenchId === 'delivery' ? deliverySessionScope : undefined}
        visible={currentWorkbenchId === 'platform' || currentWorkbenchId === 'delivery'}
      >
        <Layout className="soha-shell" hasSider={!isApplicationWorkspace}>
          {!isApplicationWorkspace && (
            <Sider
              breakpoint="md"
              className="soha-sider"
              collapsible
              collapsed={sidebarCollapsed}
              collapsedWidth={SIDEBAR_COLLAPSED_WIDTH}
              onBreakpoint={(narrow) => setNarrowSidebarCollapsed(narrow ? true : undefined)}
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
                    onClick={() => navigate(homePath ?? '/')}
                  >
                    {activeLogo ? (
                      <img
                        className="soha-brand-logo"
                        src={activeLogo}
                        alt={branding.sidebarTitle}
                      />
                    ) : (
                      <div className="soha-brand-mark">SOHA</div>
                    )}
                  </button>
                </div>

                {isAccountUtilityRoute ? (
                  <>
                    <div className="soha-workbench-switcher-shell">
                      <WorkbenchSwitcher
                        collapsed={sidebarCollapsed}
                        current={accountWorkbenchOption}
                        assistantLabel="Heya"
                        onAssistant={
                          canAccessRoute(getRouteMeta('/heya'), snapshot)
                            ? () => navigate('/heya')
                            : undefined
                        }
                        options={[...workbenchOptions, accountWorkbenchOption]}
                        onSelect={(workbench) => {
                          if (workbench === 'account') {
                            navigate('/account/profile')
                            return
                          }
                          const targetPath = findFirstAccessiblePathForWorkbench(
                            workbench,
                            snapshot,
                          )
                          if (targetPath) navigate(targetPath)
                        }}
                      />
                    </div>
                    <div className="soha-nav-business">
                      <Menu
                        className="soha-nav-menu"
                        mode="inline"
                        items={accountSidebarItems}
                        selectedKeys={[location.pathname]}
                        onClick={({ key }) => navigate(String(key))}
                        inlineIndent={8}
                        inlineCollapsed={sidebarCollapsed}
                        theme={resolvedThemeMode}
                      />
                    </div>
                  </>
                ) : (
                  <>
                    {workbenchOptions.length > 0 && currentWorkbenchOption ? (
                      <div className="soha-workbench-switcher-shell">
                        <WorkbenchSwitcher
                          collapsed={sidebarCollapsed}
                          current={currentWorkbenchOption}
                          assistantLabel="Heya"
                          onAssistant={
                            canAccessRoute(getRouteMeta('/heya'), snapshot)
                              ? () => navigate('/heya')
                              : undefined
                          }
                          options={workbenchOptions}
                          onSelect={(workbench) => {
                            if (workbench === 'account') return
                            const targetPath = findFirstAccessiblePathForWorkbench(
                              workbench,
                              snapshot,
                            )
                            if (!targetPath) {
                              return
                            }
                            navigate(targetPath)
                          }}
                        />
                      </div>
                    ) : null}
                    <div className="soha-nav-business">
                      <Menu
                        className="soha-nav-menu"
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
                        inlineIndent={8}
                        inlineCollapsed={sidebarCollapsed}
                        theme={resolvedThemeMode}
                      />
                    </div>
                  </>
                )}
              </div>
            </Sider>
          )}

          <Layout
            className="soha-main"
            style={isApplicationWorkspace ? { width: '100%' } : undefined}
          >
            <Header className="soha-header">
              <div className="soha-header-top-row">
                <div className="soha-header-main">
                  <div className="soha-header-breadcrumb-row">
                    {isApplicationWorkspace && (
                      <button
                        type="button"
                        className="soha-sider-brand soha-application-brand"
                        aria-label={localeCode === 'zh_CN' ? '返回首页' : 'Go to overview'}
                        onClick={() => navigate(homePath ?? '/')}
                      >
                        {activeLogo ? (
                          <img
                            className="soha-brand-logo"
                            src={activeLogo}
                            alt={branding.sidebarTitle}
                          />
                        ) : (
                          <div className="soha-brand-mark">SOHA</div>
                        )}
                      </button>
                    )}
                    {!isApplicationWorkspace && (
                      <Button
                        aria-label={
                          sidebarCollapsed
                            ? t('layout.expand', 'Expand sidebar')
                            : t('layout.collapse', 'Collapse sidebar')
                        }
                        aria-expanded={!sidebarCollapsed}
                        className="soha-header-action soha-header-sider-toggle"
                        type="text"
                        icon={sidebarCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
                        onClick={() => {
                          if (narrowSidebarCollapsed !== undefined) {
                            setNarrowSidebarCollapsed(!sidebarCollapsed)
                          } else {
                            setSidebarCollapsed(!sidebarCollapsed)
                          }
                        }}
                      />
                    )}
                    {isApplicationWorkspace ? (
                      <Suspense fallback={null}>
                        <ApplicationNavigation />
                        <ApplicationIdentity />
                      </Suspense>
                    ) : (
                      <Breadcrumb
                        items={breadcrumbRoutes.map((route) => ({
                          title: route.path ? (
                            <Link to={route.path}>{route.name}</Link>
                          ) : (
                            route.name
                          ),
                        }))}
                      />
                    )}
                  </div>
                </div>
                <div className="soha-header-right">
                  {resourceHeaderScopeMode !== 'hidden' && !isAccountUtilityRoute ? (
                    <div className="soha-header-context">
                      <PlatformScopeTrigger
                        scopeMode={resourceHeaderScopeMode}
                        syncClusterSearch={!routeOwnsClusterSearch}
                      />
                    </div>
                  ) : null}
                  {showRealtimeSessionsAction ? <RealtimeSessionDockTrigger /> : null}
                  {showResourceCreateAction ? (
                    <HeaderActionButton
                      ariaLabel={t('platform.resourceCreation.createResource', '创建资源')}
                      className="soha-header-resource-create"
                      icon={<FileAddOutlined />}
                      label={t('platform.resourceCreation.createResource', '创建资源')}
                      title={t('platform.resourceCreation.createResource', '创建资源')}
                      onClick={() => setResourceCreateOpenedByAction(true)}
                    />
                  ) : null}
                  <HeaderActionButton
                    ariaLabel={t('layout.docs', 'Docs')}
                    title={t('layout.docs', 'Docs')}
                    icon={<QuestionCircleOutlined />}
                    label={t('layout.docs', 'Docs')}
                    onClick={() => window.open('/docs/', '_blank', 'noopener,noreferrer')}
                  />
                  <div className="soha-header-preferences">
                    <HeaderActionButton
                      ariaLabel={languageSwitchTitle}
                      title={languageSwitchTitle}
                      inset
                      icon={<TranslationOutlined />}
                      label={languageSwitchLabel}
                      onClick={() => setLocaleCode(localeCode === 'zh_CN' ? 'en_US' : 'zh_CN')}
                    />
                    <HeaderActionButton
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
                        ...(homePath
                          ? [
                              {
                                key: 'portal',
                                icon: <AppstoreOutlined />,
                                label: t('layout.portal', '门户首页'),
                              },
                            ]
                          : []),
                        {
                          key: 'accountSettings',
                          icon: <SettingOutlined />,
                          label: t('layout.accountSettings', '个人设置'),
                        },
                        {
                          key: 'changePassword',
                          icon: <LockOutlined />,
                          label: t('layout.changePassword', '修改密码'),
                        },
                        { type: 'divider' },
                        {
                          key: 'logout',
                          icon: <LogoutOutlined />,
                          label: t('layout.logout', 'Sign out'),
                        },
                      ],
                      onClick: ({ key }) => {
                        if (key === 'portal') {
                          navigate(homePath ?? '/')
                          return
                        }
                        if (key === 'accountSettings') {
                          navigate('/account/settings')
                          return
                        }
                        if (key === 'changePassword') {
                          navigate('/account/profile?changePassword=1')
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
                    <HeaderActionButton
                      ariaLabel={userDisplayName}
                      className="soha-user-trigger"
                      iconClassName="soha-user-trigger__avatar"
                      icon={
                        <Avatar
                          className="soha-user-avatar"
                          size="small"
                          src={user?.avatarUrl || undefined}
                          style={userAvatarStyle}
                        >
                          {userDisplayName.charAt(0).toUpperCase()}
                        </Avatar>
                      }
                      label={userDisplayName}
                    />
                  </Dropdown>
                </div>
              </div>
            </Header>
            <Content className="soha-content" data-workbench={currentWorkbenchId}>
              <div className="soha-content-inner soha-pro-content-host">
                <Outlet />
              </div>
            </Content>
          </Layout>
        </Layout>
        {resourceCreateOpen ? (
          <Suspense fallback={null}>
            <GlobalResourceCreateModal onClose={closeResourceCreate} open />
          </Suspense>
        ) : null}
      </RealtimeSessionDockProvider>
    </GlobalAIAssistantProvider>
  )
}
