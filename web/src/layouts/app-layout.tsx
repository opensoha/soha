import type { CSSProperties, ReactNode } from 'react'
import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { Link, Outlet, useLocation, useMatch, useNavigate } from 'react-router-dom'
import {
  Avatar,
  Breadcrumb,
  Button,
  Dropdown,
  Layout,
  Modal,
  Popover,
  Segmented,
  Slider,
  Spin,
  Switch,
  Tooltip,
} from 'antd'
import { useQuery } from '@tanstack/react-query'
import type { MenuProps } from 'antd'
import {
  BgColorsOutlined,
  CheckOutlined,
  FileAddOutlined,
  FullscreenExitOutlined,
  FullscreenOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  MoonOutlined,
  QuestionCircleOutlined,
  ReloadOutlined,
  SearchOutlined,
  SunOutlined,
  TranslationOutlined,
  LogoutOutlined,
} from '@ant-design/icons'
import { HeaderActionButton } from '@/components/header-action-button'
import { PlatformScopeTrigger } from '@/components/platform-scope-toolbar'
import { SplitCardDrawerHost } from '@/components/split-card-drawer'
import { AnnouncementBell } from '@/features/announcements/announcement-center'
import { DualRailSidebar, type UnifiedNavLeaf } from './dual-rail-sidebar'
import { logoutAuthSession } from '@/features/auth/auth-api'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { deliveryQueries } from '@/features/delivery/queries'
import { normalizeComputeWorkbenchNav } from '@/features/compute/navigation'
import { GlobalAIAssistantProvider } from '@/features/copilot/global-assistant'
import {
  filterMenuByModuleFeatures,
  GLOBAL_ASSISTANT_FEATURE,
  getWorkbenchModuleFeature,
  useModuleStatuses,
} from '@/features/modules'
import { resolveMenuIcon, resolveWorkbenchIcon } from '@/features/system/menu-icons'
import { MENU_WORKBENCH_ORDER } from '@/features/system'
import { resolveMenuSectionLabel } from '@/features/system/menu-schema'
import {
  RealtimeSessionDockProvider,
  RealtimeSessionDockTrigger,
  type RealtimeSession,
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
  type WorkbenchId,
} from '@/routes/meta'
import { getNormalizedBranding, useBrandingSettings } from '@/features/settings'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import {
  ACCENT_PRESETS,
  CORNER_RADIUS_PRESETS,
  DARK_CANVAS_PRESETS,
  DEFAULT_ACCENT_COLOR,
  DEFAULT_CORNER_RADIUS,
  DEFAULT_DARK_CANVAS_COLOR,
  DEFAULT_LIGHT_CANVAS_COLOR,
  DEFAULT_OVERVIEW_GRADIENT_STOPS,
  DEFAULT_OVERVIEW_HERO_STYLE,
  LIGHT_CANVAS_PRESETS,
  OVERVIEW_GRADIENT_PRESETS,
  resolveThemeMode,
  watchSystemThemeMode,
  type OverviewHeroStyle,
  type ThemeMode,
} from '@/theme/app-theme'
import { resolveBrandingLogos } from '@/utils/branding'
import type { BusinessWorkspaceType, RuntimeMenuNode } from '@/types'

const { Sider, Header, Content } = Layout
const ApplicationNavigation = lazy(async () => {
  const module = await import('@/features/delivery/navigation')
  return { default: module.ApplicationNavigation }
})
const ApplicationIdentity = lazy(async () => {
  const module = await import('@/features/delivery/navigation')
  return { default: module.ApplicationIdentity }
})
const AssistantCompanionOverlay = lazy(async () => {
  const module = await import('@/features/companion')
  return { default: module.AssistantCompanionOverlay }
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

function buildWorkbenchOptions(localeCode: 'zh_CN' | 'en_US'): WorkbenchOption[] {
  if (localeCode === 'en_US') {
    return [
      {
        key: 'home',
        label: 'Portal',
        description: 'Application portal and unified workspace entry',
        icon: resolveWorkbenchIcon('home'),
      },
      {
        key: 'platform',
        label: 'K8s Workbench',
        description:
          'Operations dashboard for clusters, workloads, network, storage, and runtime resources',
        icon: resolveWorkbenchIcon('platform'),
      },
      {
        key: 'delivery',
        label: 'Continuous Delivery Workbench',
        description: 'Build, test, and release applications',
        icon: resolveWorkbenchIcon('delivery'),
      },
      {
        key: 'monitoring',
        label: 'Observability Workbench',
        description: 'Logs, alerts, routes, notifications, and on-call flows',
        icon: resolveWorkbenchIcon('monitoring'),
      },
      {
        key: 'compute',
        label: 'Compute Resources',
        description: 'Virtualization, runtime hosts, containers, and operation tasks',
        icon: resolveWorkbenchIcon('compute'),
      },
      {
        key: 'ai',
        label: 'AI Workbench',
        description: 'Investigation, automation, tools, and skills',
        icon: resolveWorkbenchIcon('ai'),
      },
      {
        key: 'security',
        label: 'Internal Workbench',
        description: 'Identity providers, applications, and access integrations',
        icon: resolveWorkbenchIcon('security'),
      },
      {
        key: 'settings',
        label: 'Settings Center',
        description: 'Login, branding, monitoring, and AI settings',
        icon: resolveWorkbenchIcon('settings'),
      },
    ]
  }
  return [
    {
      key: 'home',
      label: '门户',
      description: '应用门户与统一工作台入口',
      icon: resolveWorkbenchIcon('home'),
    },
    {
      key: 'platform',
      label: 'k8s工作台',
      description: '面向运维视角的集群、工作负载、网络、存储与运行资源',
      icon: resolveWorkbenchIcon('platform'),
    },
    {
      key: 'delivery',
      label: '持续交付工作台',
      description: '应用构建、测试与发布',
      icon: resolveWorkbenchIcon('delivery'),
    },
    {
      key: 'monitoring',
      label: '可观测性工作台',
      description: '日志、告警、路由、通知和值班协同',
      icon: resolveWorkbenchIcon('monitoring'),
    },
    {
      key: 'compute',
      label: '计算资源工作台',
      description: '虚拟化、运行时主机、容器管理与任务追踪',
      icon: resolveWorkbenchIcon('compute'),
    },
    {
      key: 'ai',
      label: 'AI工作台',
      description: '调查、自动化、工具与技能',
      icon: resolveWorkbenchIcon('ai'),
    },
    {
      key: 'security',
      label: '内网工作台',
      description: '身份提供商、应用目录与接入集成',
      icon: resolveWorkbenchIcon('security'),
    },
    {
      key: 'settings',
      label: '设置中心',
      description: '登录、品牌、监控与 AI 设置',
      icon: resolveWorkbenchIcon('settings'),
    },
  ]
}

function ApplicationHeaderFrame({ brand, children }: { brand?: ReactNode; children: ReactNode }) {
  if (!brand) return children
  return (
    <div className="soha-application-topbar">
      <div className="soha-sider-topbar soha-application-brand">{brand}</div>
      {children}
    </div>
  )
}

async function validateSessionReference(session: RealtimeSession, localeCode: string) {
  const { validateDeliverySessionReference } =
    await import('@/features/delivery/applications/reference-session')
  await validateDeliverySessionReference(session, localeCode)
}

interface CommandEntry {
  group: string
  icon: ReactNode
  id: string
  label: string
  meta?: string
  onRun: () => void
}

function CommandPaletteModal({
  entries,
  localeCode,
  onClose,
  open,
}: {
  entries: CommandEntry[]
  localeCode: 'zh_CN' | 'en_US'
  onClose: () => void
  open: boolean
}) {
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!open) return
    setQuery('')
    setActiveIndex(0)
    const timer = window.setTimeout(() => inputRef.current?.focus(), 20)
    return () => window.clearTimeout(timer)
  }, [open])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return entries
    return entries.filter(
      (item) =>
        item.label.toLowerCase().includes(q) ||
        (item.meta && item.meta.toLowerCase().includes(q)) ||
        item.group.toLowerCase().includes(q),
    )
  }, [entries, query])

  const groups = useMemo(() => {
    const map = new Map<string, CommandEntry[]>()
    filtered.forEach((item) => {
      const list = map.get(item.group) ?? []
      list.push(item)
      map.set(item.group, list)
    })
    return Array.from(map.entries())
  }, [filtered])

  const safeIndex = Math.min(activeIndex, Math.max(0, filtered.length - 1))

  return (
    <Modal
      className="soha-cmdk-modal"
      closable={false}
      destroyOnHidden
      footer={null}
      onCancel={onClose}
      open={open}
      style={{ top: 76 }}
      width={560}
    >
      <div className="soha-cmdk-surface">
        <div className="soha-cmdk-input-row">
          <span className="soha-cmdk-input-icon">
            <SearchOutlined />
          </span>
          <input
            ref={inputRef}
            aria-label={localeCode === 'zh_CN' ? '搜索页面或操作' : 'Search pages or commands'}
            className="soha-cmdk-input"
            onChange={(event) => {
              setQuery(event.target.value)
              setActiveIndex(0)
            }}
            onKeyDown={(event) => {
              if (event.key === 'ArrowDown') {
                event.preventDefault()
                setActiveIndex((current) =>
                  filtered.length === 0 ? 0 : (current + 1) % filtered.length,
                )
              } else if (event.key === 'ArrowUp') {
                event.preventDefault()
                setActiveIndex((current) =>
                  filtered.length === 0 ? 0 : (current - 1 + filtered.length) % filtered.length,
                )
              } else if (event.key === 'Enter' && filtered[safeIndex]) {
                event.preventDefault()
                filtered[safeIndex].onRun()
                onClose()
              }
            }}
            placeholder={
              localeCode === 'zh_CN'
                ? '跳转工作台、资源页面或切换主题...'
                : 'Jump to workbench, resource page, or toggle theme...'
            }
            value={query}
          />
          <span className="soha-cmdk-kbd">ESC</span>
        </div>

        <div className="soha-cmdk-list">
          {filtered.length === 0 ? (
            <div className="soha-cmdk-empty">
              {localeCode === 'zh_CN' ? '未找到匹配的导航或命令' : 'No matching commands found'}
            </div>
          ) : (
            groups.map(([group, items]) => (
              <div key={group}>
                <div className="soha-cmdk-group-title">{group}</div>
                {items.map((item) => {
                  const index = filtered.indexOf(item)
                  const active = index === safeIndex
                  return (
                    <button
                      key={item.id}
                      type="button"
                      className={`soha-cmdk-item ${active ? 'is-active' : ''}`}
                      onMouseEnter={() => setActiveIndex(index)}
                      onClick={() => {
                        item.onRun()
                        onClose()
                      }}
                    >
                      <span className="soha-cmdk-item__main">
                        <span className="soha-cmdk-item__icon">{item.icon}</span>
                        <span>{item.label}</span>
                      </span>
                      {item.meta ? <span className="soha-cmdk-item__meta">{item.meta}</span> : null}
                    </button>
                  )
                })}
              </div>
            ))
          )}
        </div>

        <div className="soha-cmdk-footer">
          <span>Soha Command Surface</span>
          <span className="soha-cmdk-footer-hints">
            <span>↑↓ {localeCode === 'zh_CN' ? '选择' : 'Navigate'}</span>
            <span>↵ {localeCode === 'zh_CN' ? '跳转' : 'Open'}</span>
          </span>
        </div>
      </div>
    </Modal>
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
  const accentColor = usePreferencesStore((state) => state.accentColor ?? DEFAULT_ACCENT_COLOR)
  const setAccentColor = usePreferencesStore((state) => state.setAccentColor)
  const lightCanvasColor = usePreferencesStore(
    (state) => state.lightCanvasColor ?? DEFAULT_LIGHT_CANVAS_COLOR,
  )
  const setLightCanvasColor = usePreferencesStore((state) => state.setLightCanvasColor)
  const darkCanvasColor = usePreferencesStore(
    (state) => state.darkCanvasColor ?? DEFAULT_DARK_CANVAS_COLOR,
  )
  const setDarkCanvasColor = usePreferencesStore((state) => state.setDarkCanvasColor)
  const paperGrain = usePreferencesStore((state) => state.paperGrain ?? true)
  const setPaperGrain = usePreferencesStore((state) => state.setPaperGrain)
  const overviewHeroStyle = usePreferencesStore(
    (state) => state.overviewHeroStyle ?? DEFAULT_OVERVIEW_HERO_STYLE,
  )
  const setOverviewHeroStyle = usePreferencesStore((state) => state.setOverviewHeroStyle)
  const overviewGradientPreset = usePreferencesStore(
    (state) => state.overviewGradientPreset ?? 'workbench',
  )
  const setOverviewGradientPreset = usePreferencesStore(
    (state) => state.setOverviewGradientPreset,
  )
  const overviewGradientStops = usePreferencesStore(
    (state) => state.overviewGradientStops ?? DEFAULT_OVERVIEW_GRADIENT_STOPS,
  )
  const setOverviewGradientStop = usePreferencesStore((state) => state.setOverviewGradientStop)
  const resetAppearanceDefaults = usePreferencesStore((state) => state.resetAppearanceDefaults)
  const dualRailStage = usePreferencesStore((state) => state.dualRailStage ?? 'expanded')
  const setDualRailStage = usePreferencesStore((state) => state.setDualRailStage)
  const cornerRadius = usePreferencesStore((state) => state.cornerRadius ?? DEFAULT_CORNER_RADIUS)
  const setCornerRadius = usePreferencesStore((state) => state.setCornerRadius)
  // Narrow-screen navigation is temporary; keep the user's desktop preference.
  const [narrowSidebarCollapsed, setNarrowSidebarCollapsed] = useState<boolean>()
  const effectiveDualRailStage =
    narrowSidebarCollapsed === undefined
      ? dualRailStage
      : narrowSidebarCollapsed
        ? 'rail-only'
        : 'expanded'
  const sidebarCollapsed = effectiveDualRailStage !== 'expanded'
  const currentWorkspace = usePreferencesStore((state) => state.currentWorkspace)
  const [resourceCreateOpenedByAction, setResourceCreateOpenedByAction] = useState(false)
  const resourceCreateRequested = useMemo(
    () => new URLSearchParams(location.search).get('createResource') === '1',
    [location.search],
  )
  const resourceCreateOpen = resourceCreateOpenedByAction || resourceCreateRequested
  const setCurrentWorkspace = usePreferencesStore((state) => state.setCurrentWorkspace)
  const { t } = useI18n()
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
      icon: resolveWorkbenchIcon('account'),
    }),
    [t],
  )
  const accountLeaves = useMemo<UnifiedNavLeaf[]>(
    () => [
      {
        id: '/account/profile',
        label: t('layout.profile', '个人中心'),
        icon: resolveMenuIcon('user'),
        path: '/account/profile',
      },
      {
        id: '/about',
        label: t('layout.about', '关于'),
        icon: resolveMenuIcon('info'),
        path: '/about',
      },
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
  const applicationId = applicationMatch?.params.applicationId ?? ''
  const sessionEnvironmentQuery = useQuery(
    deliveryQueries.applications.environmentNavigation(applicationId, canReadApplicationSessions),
  )
  const sessionEnvironment = sessionEnvironmentId
    ? sessionEnvironmentQuery.data?.find(
        (environment) => environment.applicationEnvironmentId === sessionEnvironmentId,
      )
    : sessionEnvironmentQuery.data?.[0]
  const deliverySessionScope =
    canReadApplicationSessions &&
    !sessionEnvironmentQuery.isError &&
    applicationMatch?.params.applicationId &&
    sessionEnvironment
      ? JSON.stringify([
          applicationMatch.params.applicationId,
          sessionEnvironment.applicationEnvironmentId,
        ])
      : import.meta.env.VITE_UI_PILOT_MOCK === '1'
        ? JSON.stringify(['pilot-delivery-app', 'prod-shanghai'])
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
  const nodeByID = useMemo(() => buildNodeByID(combinedNav), [combinedNav])
  const businessNodeIDs = useMemo(() => collectNodeIDs(businessNav), [businessNav])
  const systemNodeIDs = useMemo(() => collectNodeIDs(systemNav), [systemNav])
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

    if (currentMeta.id === 'provider-portal') {
      const portalParams = new URLSearchParams(location.search)
      const portalGroup = portalParams.get('group')
      const portalTag = portalParams.get('tag')?.trim()
      const portalLeaf =
        portalGroup === 'favorites'
          ? t('providerPortal.home.favorites', localeCode === 'zh_CN' ? '收藏' : 'Favorites')
          : portalTag || t('providerPortal.home.allTags', localeCode === 'zh_CN' ? '全部' : 'All')
      routes.push({
        name: currentWorkbenchOption?.label ?? t('route.provider-portal.title', '门户首页'),
        path: portalGroup || portalTag ? '/portal' : undefined,
      })
      routes.push({ name: portalLeaf })
      return routes
    }

    if (
      currentMeta.id === 'provider-portal-security' ||
      currentMeta.id === 'provider-portal-application-detail'
    ) {
      routes.push({
        name: currentWorkbenchOption?.label ?? t('route.provider-portal.title', '门户首页'),
        path: '/portal',
      })
      routes.push({
        name:
          currentMeta.id === 'provider-portal-security'
            ? localeCode === 'en_US'
              ? 'Account Security'
              : '账户安全'
            : resolveBreadcrumbTitle(currentMeta),
      })
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
    location.search,
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
  const [commandOpen, setCommandOpen] = useState(false)

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setCommandOpen((current) => !current)
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [])

  const commandEntries = useMemo<CommandEntry[]>(() => {
    const entries: CommandEntry[] = []
    const navGroup = localeCode === 'zh_CN' ? '当前导航' : 'Current Navigation'
    const visitNav = (nodes: RuntimeMenuNode[], prefix = '') => {
      nodes.forEach((node) => {
        const label = resolveRuntimeMenuLabel(node, localeCode)
        const fullLabel = prefix ? `${prefix} / ${label}` : label
        if (!node.children?.length && node.route) {
          const path = node.route.redirectTo ?? node.route.path
          entries.push({
            group: navGroup,
            icon: resolveMenuIcon(node.iconKey) ?? <SearchOutlined />,
            id: `nav-${node.id}`,
            label: fullLabel,
            meta: path,
            onRun: () => navigate(path),
          })
        }
        if (node.children?.length) {
          visitNav(node.children, label)
        }
      })
    }
    visitNav(primaryNav)

    const wbGroup = localeCode === 'zh_CN' ? '切换工作台' : 'Switch Workbench'
    workbenchOptions.forEach((wb) => {
      entries.push({
        group: wbGroup,
        icon: wb.icon,
        id: `wb-${wb.key}`,
        label: wb.label,
        meta: wb.key,
        onRun: () => {
          if (wb.key === 'account') return
          const targetPath = findFirstAccessiblePathForWorkbench(wb.key, snapshot)
          if (targetPath) navigate(targetPath)
        },
      })
    })

    const prefGroup = localeCode === 'zh_CN' ? '界面偏好' : 'Preferences'
    entries.push({
      group: prefGroup,
      icon: resolvedThemeMode === 'dark' ? <SunOutlined /> : <MoonOutlined />,
      id: 'pref-theme',
      label:
        resolvedThemeMode === 'dark'
          ? localeCode === 'zh_CN'
            ? '切换至暖白纸感模式'
            : 'Switch to Warm Cream Mode'
          : localeCode === 'zh_CN'
            ? '切换至苔林夜色模式'
            : 'Switch to Moss Nocturne Mode',
      meta: resolvedThemeMode === 'dark' ? 'Light' : 'Dark',
      onRun: () => setThemeMode(resolvedThemeMode === 'dark' ? 'light' : 'dark'),
    })
    entries.push({
      group: prefGroup,
      icon: <TranslationOutlined />,
      id: 'pref-locale',
      label: localeCode === 'zh_CN' ? '切换语言为 English' : 'Switch language to 中文',
      meta: localeCode === 'zh_CN' ? 'EN' : 'ZH',
      onRun: () => setLocaleCode(localeCode === 'zh_CN' ? 'en_US' : 'zh_CN'),
    })
    return entries
  }, [
    localeCode,
    navigate,
    primaryNav,
    resolvedThemeMode,
    setLocaleCode,
    setThemeMode,
    snapshot,
    workbenchOptions,
  ])

  const quickJumpTrigger = (
    <button
      type="button"
      className="soha-cmdk-trigger"
      aria-label={localeCode === 'zh_CN' ? '快速跳转与命令面板' : 'Quick jump and command palette'}
      title={localeCode === 'zh_CN' ? '快速跳转 (⌘K)' : 'Quick jump (⌘K)'}
      onClick={() => setCommandOpen(true)}
    >
      <span className="soha-cmdk-trigger__left">
        <SearchOutlined />
        <span className="soha-cmdk-trigger__text">
          {localeCode === 'zh_CN' ? '搜索与跳转...' : 'Quick jump...'}
        </span>
      </span>
      <span className="soha-cmdk-kbd">⌘K</span>
    </button>
  )

  const [immersiveFullscreen, setImmersiveFullscreen] = useState(false)

  useEffect(() => {
    if (!immersiveFullscreen) return undefined
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !commandOpen && !resourceCreateOpen) {
        setImmersiveFullscreen(false)
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [commandOpen, immersiveFullscreen, resourceCreateOpen])

  const userDropdownMenu: MenuProps = {
    items: [
      { key: 'user', label: userDisplayName, disabled: true },
      { type: 'divider' },
      {
        key: 'accountSettings',
        icon: resolveMenuIcon('settings'),
        label: t('layout.accountSettings', '个人设置'),
      },
      { type: 'divider' },
      {
        key: 'logout',
        icon: <LogoutOutlined />,
        label: t('layout.logout', 'Sign out'),
      },
    ],
    onClick: ({ key }) => {
      if (key === 'accountSettings') {
        navigate('/account/settings')
        return
      }
      if (key === 'logout') {
        void logoutAuthSession().finally(() => navigate('/login'))
      }
    },
  }

  const dualRailUserPod = (
    <Dropdown menu={userDropdownMenu} placement="topLeft" trigger={['click']}>
      <Tooltip placement="right" title={userDisplayName}>
        <button
          type="button"
          aria-label={userDisplayName}
          className="soha-dual-rail__user-btn"
        >
          <Avatar
            className="soha-user-avatar soha-dual-rail__avatar"
            size={28}
            src={user?.avatarUrl || undefined}
            style={userAvatarStyle}
          >
            {userDisplayName.charAt(0).toUpperCase()}
          </Avatar>
          <span className="soha-dual-rail__user-dot" aria-hidden="true" />
        </button>
      </Tooltip>
    </Dropdown>
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
      <div className="soha-viewport-center">
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
          <Suspense fallback={null}>
            <AssistantCompanionOverlay {...props} />
          </Suspense>
        )
      }
    >
      <RealtimeSessionDockProvider
        ownerKey={user?.userId}
        workbench={currentWorkbenchId === 'delivery' ? 'delivery' : 'platform'}
        scopeKey={currentWorkbenchId === 'delivery' ? deliverySessionScope : undefined}
        validateReference={validateSessionReference}
        visible={currentWorkbenchId === 'platform' || currentWorkbenchId === 'delivery'}
      >
        <Layout
          className={`soha-shell${immersiveFullscreen ? ' is-immersive-fullscreen' : ''}`}
          hasSider={!isApplicationWorkspace}
        >
          {!isApplicationWorkspace && (
            <Sider
              breakpoint="md"
              className="soha-sider"
              collapsible
              collapsed={effectiveDualRailStage !== 'expanded'}
              collapsedWidth={115}
              onBreakpoint={(narrow) => setNarrowSidebarCollapsed(narrow ? true : undefined)}
              style={{ backgroundColor: 'transparent' }}
              trigger={null}
              width={244}
            >
              <DualRailSidebar
                stage={effectiveDualRailStage}
                workbenches={
                  isAccountUtilityRoute
                    ? [...workbenchOptions, accountWorkbenchOption]
                    : workbenchOptions
                }
                userPod={dualRailUserPod}
                activeWorkbenchKey={
                  isAccountUtilityRoute ? 'account' : (currentWorkbenchOption?.key ?? null)
                }
                activeWorkbenchLabel={
                  isAccountUtilityRoute
                    ? accountWorkbenchOption.label
                    : (currentWorkbenchOption?.label ?? 'Soha')
                }
                fullSidebarNav={fullSidebarNav}
                accountLeaves={accountLeaves}
                selectedKeys={isAccountUtilityRoute ? [location.pathname] : primarySelectedKeys}
                currentPathname={location.pathname}
                currentSearch={location.search}
                aiSearchSuffix={buildAIWorkbenchSearch(location.search)}
                localeCode={localeCode}
                snapshot={snapshot}
                logoUrl={collapsedLogo || activeLogo}
                brandTitle={branding.sidebarTitle || 'Soha'}
                homePath={homePath ?? '/'}
                quickJumpTrigger={quickJumpTrigger}
                onOpenCommand={() => setCommandOpen(true)}
                onNavigate={(path) => navigate(path)}
                resolveNodeLabel={(node) => resolveRuntimeMenuLabel(node, localeCode)}
              />
            </Sider>
          )}

          <Layout
            className="soha-main"
            style={isApplicationWorkspace ? { width: '100%' } : undefined}
          >
            <ApplicationHeaderFrame
              brand={
                isApplicationWorkspace ? (
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
                      <div className="soha-brand-lockup">
                        <div className="soha-brand-mark">S</div>
                        {!sidebarCollapsed && (
                          <div className="soha-brand-wordmark">
                            <span className="soha-brand-wordmark__title">
                              {branding.sidebarTitle || 'Soha'}
                            </span>
                            <span className="soha-brand-wordmark__edition">Atelier</span>
                          </div>
                        )}
                      </div>
                    )}
                  </button>
                ) : undefined
              }
            >
              <Header className="soha-header">
                <div className="soha-header-top-row">
                  <div className="soha-header-main">
                    <div className="soha-header-breadcrumb-row">
                      {!isApplicationWorkspace && (
                        <Tooltip
                          placement="bottom"
                          title={
                            effectiveDualRailStage === 'expanded'
                              ? localeCode === 'zh_CN'
                                ? '收窄右侧子菜单栏'
                                : 'Narrow sub-menu'
                              : localeCode === 'zh_CN'
                                ? '展开右侧子菜单栏'
                                : 'Expand sub-menu'
                          }
                        >
                          <Button
                            aria-label={
                              effectiveDualRailStage !== 'expanded'
                                ? t('layout.expand', 'Expand sidebar')
                                : t('layout.collapse', 'Collapse sidebar')
                            }
                            aria-expanded={effectiveDualRailStage === 'expanded'}
                            className="soha-header-action soha-header-sider-toggle"
                            type="text"
                            icon={
                              effectiveDualRailStage !== 'expanded' ? (
                                <MenuUnfoldOutlined />
                              ) : (
                                <MenuFoldOutlined />
                              )
                            }
                            onClick={() => {
                              if (narrowSidebarCollapsed !== undefined) {
                                setNarrowSidebarCollapsed(!narrowSidebarCollapsed)
                                return
                              }
                              setDualRailStage(
                                dualRailStage === 'expanded' ? 'rail-only' : 'expanded',
                              )
                            }}
                          />
                        </Tooltip>
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
                      <Popover
                        placement="bottomRight"
                        trigger="click"
                        classNames={{ container: 'soha-palette-popover-body' }}
                        content={
                          <div className="soha-palette-panel">
                            <div className="soha-palette-header">
                              <div className="soha-palette-header__copy">
                                <span className="soha-palette-header__title">
                                  {localeCode === 'zh_CN' ? '外观与主题调色板' : 'Appearance Palette'}
                                </span>
                                <span className="soha-palette-header__sub">
                                  {localeCode === 'zh_CN'
                                    ? '实时定制品牌主色与深浅画布色温'
                                    : 'Live brand accent & canvas tone'}
                                </span>
                              </div>
                              <button
                                type="button"
                                className="soha-palette-reset-btn"
                                onClick={() => resetAppearanceDefaults()}
                                title={
                                  localeCode === 'zh_CN' ? '恢复官方默认外观' : 'Reset defaults'
                                }
                              >
                                <ReloadOutlined />
                                <span>{localeCode === 'zh_CN' ? '默认' : 'Reset'}</span>
                              </button>
                            </div>

                            <div className="soha-palette-section">
                              <div className="soha-palette-section__head">
                                <span className="soha-palette-section__title">
                                  {localeCode === 'zh_CN' ? '外观模式' : 'Mode'}
                                </span>
                              </div>
                              <Segmented<ThemeMode>
                                block
                                size="small"
                                className="soha-palette-segmented"
                                value={themeMode}
                                onChange={(mode) => setThemeMode(mode)}
                                options={[
                                  {
                                    label: localeCode === 'zh_CN' ? '浅色' : 'Light',
                                    value: 'light',
                                  },
                                  {
                                    label: localeCode === 'zh_CN' ? '深色' : 'Dark',
                                    value: 'dark',
                                  },
                                  {
                                    label: localeCode === 'zh_CN' ? '跟随系统' : 'System',
                                    value: 'system',
                                  },
                                ]}
                              />
                            </div>

                            <div className="soha-palette-section is-divider">
                              <div className="soha-palette-section__head">
                                <span className="soha-palette-section__title">
                                  {localeCode === 'zh_CN' ? '主品牌强调色' : 'Brand Accent'}
                                </span>
                                <span className="soha-palette-section__hex">{accentColor}</span>
                              </div>
                              <div className="soha-palette-swatches">
                                {ACCENT_PRESETS.map((preset) => {
                                  const selected =
                                    accentColor.toLowerCase() === preset.lightHex.toLowerCase()
                                  return (
                                    <button
                                      key={preset.id}
                                      type="button"
                                      className={`soha-palette-swatch-row ${
                                        selected ? 'is-selected' : ''
                                      }`}
                                      onClick={() => setAccentColor(preset.lightHex)}
                                    >
                                      <span
                                        className="soha-palette-swatch-dot"
                                        style={{
                                          backgroundColor:
                                            resolvedThemeMode === 'dark'
                                              ? preset.darkHex
                                              : preset.lightHex,
                                        }}
                                      >
                                        {selected ? <CheckOutlined /> : null}
                                      </span>
                                      <span className="soha-palette-swatch-copy">
                                        <span className="soha-palette-swatch-name">
                                          {localeCode === 'zh_CN' ? preset.labelZh : preset.labelEn}
                                        </span>
                                        <span className="soha-palette-swatch-desc">
                                          {preset.descZh}
                                        </span>
                                      </span>
                                    </button>
                                  )
                                })}
                              </div>
                              <label className="soha-palette-custom-row">
                                <span>
                                  {localeCode === 'zh_CN'
                                    ? '自定义强调色调色盘'
                                    : 'Custom Accent Picker'}
                                </span>
                                <span className="soha-palette-custom-row__right">
                                  <span className="soha-palette-section__hex">{accentColor}</span>
                                  <input
                                    type="color"
                                    aria-label={
                                      localeCode === 'zh_CN' ? '自定义主色调' : 'Custom accent color'
                                    }
                                    className="soha-palette-color-input"
                                    value={accentColor}
                                    onChange={(event) => setAccentColor(event.target.value)}
                                  />
                                </span>
                              </label>
                            </div>

                            <div className="soha-palette-section is-divider">
                              <div className="soha-palette-section__head">
                                <span className="soha-palette-section__title">
                                  {resolvedThemeMode === 'dark'
                                    ? localeCode === 'zh_CN'
                                      ? '深色画布色温'
                                      : 'Dark Canvas Tone'
                                    : localeCode === 'zh_CN'
                                      ? '浅色画布色温'
                                      : 'Light Canvas Tone'}
                                </span>
                                <span className="soha-palette-section__hex">
                                  {resolvedThemeMode === 'dark'
                                    ? darkCanvasColor
                                    : lightCanvasColor}
                                </span>
                              </div>
                              <div className="soha-palette-canvas-pills">
                                {(resolvedThemeMode === 'dark'
                                  ? DARK_CANVAS_PRESETS
                                  : LIGHT_CANVAS_PRESETS
                                ).map((item) => {
                                  const activeHex =
                                    resolvedThemeMode === 'dark'
                                      ? darkCanvasColor
                                      : lightCanvasColor
                                  const selected =
                                    activeHex.toLowerCase() === item.hex.toLowerCase()
                                  return (
                                    <button
                                      key={item.id}
                                      type="button"
                                      className={`soha-palette-canvas-pill ${
                                        selected ? 'is-selected' : ''
                                      }`}
                                      onClick={() =>
                                        resolvedThemeMode === 'dark'
                                          ? setDarkCanvasColor(item.hex)
                                          : setLightCanvasColor(item.hex)
                                      }
                                    >
                                      <span
                                        className="soha-palette-canvas-pill__chip"
                                        style={{ backgroundColor: item.hex }}
                                      />
                                      <span className="soha-palette-canvas-pill__name">
                                        {localeCode === 'zh_CN' ? item.labelZh : item.labelEn}
                                      </span>
                                      <span className="soha-palette-canvas-pill__hex">
                                        {item.hex}
                                      </span>
                                    </button>
                                  )
                                })}
                              </div>
                              <label className="soha-palette-custom-row">
                                <span>
                                  {resolvedThemeMode === 'dark'
                                    ? localeCode === 'zh_CN'
                                      ? '深色画布自由调色盘'
                                      : 'Custom Dark Canvas'
                                    : localeCode === 'zh_CN'
                                      ? '浅色画布自由调色盘'
                                      : 'Custom Light Canvas'}
                                </span>
                                <span className="soha-palette-custom-row__right">
                                  <span className="soha-palette-section__hex">
                                    {resolvedThemeMode === 'dark'
                                      ? darkCanvasColor
                                      : lightCanvasColor}
                                  </span>
                                  <input
                                    type="color"
                                    aria-label={
                                      resolvedThemeMode === 'dark'
                                        ? localeCode === 'zh_CN'
                                          ? '自定义深色画布背景色'
                                          : 'Custom dark canvas color'
                                        : localeCode === 'zh_CN'
                                          ? '自定义浅色画布背景色'
                                          : 'Custom light canvas color'
                                    }
                                    className="soha-palette-color-input"
                                    value={
                                      resolvedThemeMode === 'dark'
                                        ? darkCanvasColor
                                        : lightCanvasColor
                                    }
                                    onChange={(event) =>
                                      resolvedThemeMode === 'dark'
                                        ? setDarkCanvasColor(event.target.value)
                                        : setLightCanvasColor(event.target.value)
                                    }
                                  />
                                </span>
                              </label>
                            </div>

                            <div className="soha-palette-section is-divider">
                              <div className="soha-palette-section__head">
                                <span className="soha-palette-section__title">
                                  {localeCode === 'zh_CN'
                                    ? '总览 3D 空间渐变调色盘'
                                    : 'Overview 3D Gradient Palette'}
                                </span>
                                <span className="soha-palette-section__hex">
                                  {overviewGradientPreset === 'workbench'
                                    ? localeCode === 'zh_CN'
                                      ? '自适应三色'
                                      : 'Adaptive'
                                    : localeCode === 'zh_CN'
                                      ? '三位混色'
                                      : '3-Stop'}
                                </span>
                              </div>
                              <Segmented<OverviewHeroStyle>
                                block
                                size="small"
                                className="soha-palette-segmented"
                                value={overviewHeroStyle}
                                onChange={(val) => setOverviewHeroStyle(val)}
                                options={[
                                  {
                                    label: localeCode === 'zh_CN' ? '冰晶浅舱' : 'Prism',
                                    value: 'prism',
                                  },
                                  {
                                    label: localeCode === 'zh_CN' ? '极光暗舱' : 'Spotlight',
                                    value: 'spotlight',
                                  },
                                ]}
                              />

                              <div className="soha-palette-grad-presets">
                                {OVERVIEW_GRADIENT_PRESETS.map((preset) => {
                                  const selected = overviewGradientPreset === preset.id
                                  return (
                                    <button
                                      key={preset.id}
                                      type="button"
                                      className={`soha-palette-grad-pill ${
                                        selected ? 'is-selected' : ''
                                      }`}
                                      onClick={() => setOverviewGradientPreset(preset.id)}
                                      title={preset.descZh}
                                    >
                                      <span
                                        className="soha-palette-grad-pill__bar"
                                        style={{
                                          background: `linear-gradient(125deg, ${preset.stops[0]} 0%, ${preset.stops[1]} 50%, ${preset.stops[2]} 100%)`,
                                        }}
                                      />
                                      <span className="soha-palette-grad-pill__label">
                                        {localeCode === 'zh_CN' ? preset.labelZh : preset.labelEn}
                                      </span>
                                    </button>
                                  )
                                })}
                              </div>

                              <div className="soha-palette-tri-mixer">
                                <div className="soha-palette-tri-mixer__head">
                                  <span>
                                    {localeCode === 'zh_CN'
                                      ? '三位颜色渐变混合（联动全屏 3D 舞台）'
                                      : '3-Color Gradient Blend (3D Stage)'}
                                  </span>
                                </div>
                                <div className="soha-palette-tri-mixer__stops">
                                  {(
                                    [
                                      {
                                        idx: 0 as const,
                                        labelZh: '起点 1',
                                        labelEn: 'Stop 1',
                                      },
                                      {
                                        idx: 1 as const,
                                        labelZh: '中调 2',
                                        labelEn: 'Stop 2',
                                      },
                                      {
                                        idx: 2 as const,
                                        labelZh: '尾韵 3',
                                        labelEn: 'Stop 3',
                                      },
                                    ]
                                  ).map((slot) => (
                                    <label key={slot.idx} className="soha-palette-tri-stop">
                                      <input
                                        type="color"
                                        aria-label={
                                          localeCode === 'zh_CN'
                                            ? `总览渐变${slot.labelZh}`
                                            : `Overview gradient ${slot.labelEn}`
                                        }
                                        className="soha-palette-color-input"
                                        value={overviewGradientStops[slot.idx]}
                                        onChange={(e) =>
                                          setOverviewGradientStop(slot.idx, e.target.value)
                                        }
                                      />
                                      <span className="soha-palette-tri-stop__copy">
                                        <strong>
                                          {localeCode === 'zh_CN' ? slot.labelZh : slot.labelEn}
                                        </strong>
                                        <code>{overviewGradientStops[slot.idx]}</code>
                                      </span>
                                    </label>
                                  ))}
                                </div>
                              </div>

                              <div className="soha-palette-section is-divider" style={{ marginTop: 10 }}>
                                <div className="soha-palette-section__head">
                                  <span className="soha-palette-section__title">
                                    {localeCode === 'zh_CN'
                                      ? '全局圆角 R 角'
                                      : 'Corner Radius (R)'}
                                  </span>
                                  <span className="soha-palette-section__hex">
                                    R = {cornerRadius}px
                                  </span>
                                </div>
                                <div className="soha-palette-radius-presets">
                                  {CORNER_RADIUS_PRESETS.map((preset) => {
                                    const selected = cornerRadius === preset.value
                                    return (
                                      <button
                                        key={preset.id}
                                        type="button"
                                        className={`soha-palette-radius-pill ${
                                          selected ? 'is-selected' : ''
                                        }`}
                                        onClick={() => setCornerRadius(preset.value)}
                                      >
                                        <span
                                          className="soha-palette-radius-pill__glyph"
                                          style={{
                                            borderTopLeftRadius: Math.max(2, Math.round(preset.value * 0.55)),
                                          }}
                                        />
                                        <span>
                                          {localeCode === 'zh_CN' ? preset.labelZh : preset.labelEn}
                                        </span>
                                      </button>
                                    )
                                  })}
                                </div>
                                <Slider
                                  min={4}
                                  max={20}
                                  step={1}
                                  value={cornerRadius}
                                  onChange={(val) => setCornerRadius(val)}
                                  tooltip={{ formatter: (val) => `${val}px` }}
                                  style={{ margin: '4px 4px 2px' }}
                                />
                              </div>

                              <div className="soha-palette-toggle-row" style={{ marginTop: 10 }}>
                                <span className="soha-palette-toggle-row__label">
                                  {localeCode === 'zh_CN'
                                    ? '纸张微噪点肌理'
                                    : 'Tactile Paper Grain'}
                                </span>
                                <Switch
                                  size="small"
                                  checked={paperGrain}
                                  onChange={(checked) => setPaperGrain(checked)}
                                />
                              </div>
                            </div>
                          </div>
                        }
                      >
                        <HeaderActionButton
                          ariaLabel={localeCode === 'zh_CN' ? '主题调色板' : 'Theme palette'}
                          title={localeCode === 'zh_CN' ? '主题调色板' : 'Theme palette'}
                          className="soha-header-palette-trigger"
                          icon={<BgColorsOutlined />}
                        />
                      </Popover>
                      <HeaderActionButton
                        ariaLabel={localeCode === 'zh_CN' ? '沉浸式全屏' : 'Immersive fullscreen'}
                        title={
                          localeCode === 'zh_CN'
                            ? '沉浸式全屏（同时收起左侧双轨与顶栏，按 Esc 退出）'
                            : 'Immersive fullscreen (hide sidebar & top bar, Esc to exit)'
                        }
                        icon={<FullscreenOutlined />}
                        onClick={() => setImmersiveFullscreen(true)}
                      />
                      <HeaderActionButton
                        ariaLabel={themeSwitchTitle}
                        title={themeSwitchTitle}
                        icon={resolvedThemeMode === 'dark' ? <SunOutlined /> : <MoonOutlined />}
                        onClick={() =>
                          setThemeMode(resolvedThemeMode === 'dark' ? 'light' : 'dark')
                        }
                        pressed={resolvedThemeMode === 'dark'}
                      />
                    </div>
                    <AnnouncementBell />
                    {isApplicationWorkspace ? (
                      <Dropdown
                        menu={userDropdownMenu}
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
                    ) : null}
                  </div>
                </div>
              </Header>
            </ApplicationHeaderFrame>
            <div className="soha-workspace-body">
              <Content className="soha-content" data-workbench={currentWorkbenchId}>
                <div className="soha-content-inner soha-pro-content-host">
                  <Outlet />
                </div>
              </Content>
              <SplitCardDrawerHost />
            </div>
            {immersiveFullscreen ? (
              <button
                type="button"
                aria-label={localeCode === 'zh_CN' ? '退出沉浸式全屏' : 'Exit immersive fullscreen'}
                className="soha-immersive-exit-pill"
                onClick={() => setImmersiveFullscreen(false)}
                title={localeCode === 'zh_CN' ? '退出全屏 (Esc)' : 'Exit fullscreen (Esc)'}
              >
                <FullscreenExitOutlined />
                <span>{localeCode === 'zh_CN' ? '退出全屏' : 'Exit'}</span>
              </button>
            ) : null}
          </Layout>
        </Layout>
        {resourceCreateOpen ? (
          <Suspense fallback={null}>
            <GlobalResourceCreateModal onClose={closeResourceCreate} open />
          </Suspense>
        ) : null}
        <CommandPaletteModal
          entries={commandEntries}
          localeCode={localeCode}
          onClose={() => setCommandOpen(false)}
          open={commandOpen}
        />
      </RealtimeSessionDockProvider>
    </GlobalAIAssistantProvider>
  )
}
