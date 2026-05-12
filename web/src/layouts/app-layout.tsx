import type { ReactNode } from 'react'
import { useEffect, useMemo, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Avatar, Breadcrumb, Button, Dropdown, Layout, Menu, Spin } from 'antd'
import type { MenuProps } from 'antd'
import {
  AlertOutlined,
  AppstoreOutlined,
  CloudServerOutlined,
  DownOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  MoonOutlined,
  QuestionCircleOutlined,
  RobotOutlined,
  SunOutlined,
  TranslationOutlined,
} from '@ant-design/icons'
import { HeaderPreferenceButton } from '@/components/header-preference-button'
import { AnnouncementBell } from '@/features/announcements/announcement-center'
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
  getRouteWorkbenchId,
  getRouteWorkspace,
  resolveRouteMenuId,
  type WorkbenchId,
} from '@/routes/meta'
import { useBrandingSettings } from '@/features/settings/use-branding-settings'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { resolveThemeMode, watchSystemThemeMode } from '@/theme/app-theme'
import type { BusinessWorkspaceType, RuntimeMenuNode } from '@/types'
import { getNormalizedBranding } from '@/features/settings/use-branding-settings'

const { Sider, Header, Content } = Layout

interface WorkbenchOption {
  description: string
  icon: ReactNode
  key: WorkbenchId
  label: string
}

function buildMenuNodeItem(node: RuntimeMenuNode, localeCode: 'zh_CN' | 'en_US'): NonNullable<MenuProps['items']>[number] {
  const label = localeCode === 'en_US' && node.labelEn ? node.labelEn : node.labelZh
  if (node.children?.length) {
    return {
      key: node.id,
      icon: resolveMenuIcon(node.iconKey),
      label,
      children: node.children.map((child) => buildMenuNodeItem(child, localeCode)),
    }
  }
  return {
    key: node.id,
    icon: resolveMenuIcon(node.iconKey),
    label,
  }
}

function buildMenuItems(
  sidebarNav: RuntimeMenuNode[],
  localeCode: 'zh_CN' | 'en_US',
  options: { grouped?: boolean } = {},
): MenuProps['items'] {
  if (options.grouped === false) {
    return sidebarNav.map((item) => buildMenuNodeItem(item, localeCode))
  }
  const groups = new Map<string, NonNullable<MenuProps['items']>[number][]>()

  for (const item of sidebarNav) {
    const groupKey = item.section || 'control'
    const menuItem = buildMenuNodeItem(item, localeCode)
    const current = groups.get(groupKey) ?? []
    current.push(menuItem)
    groups.set(groupKey, current)
  }

  return Array.from(groups.entries()).map(([groupKey, items]) => ({
    key: `group-${groupKey}`,
    type: 'group' as const,
    label: <span className="kc-nav-section-title">{resolveMenuSectionLabel(groupKey, localeCode)}</span>,
    children: items,
  }))
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

function wrapSystemNav(sidebarNav: RuntimeMenuNode[]): RuntimeMenuNode[] {
  if (sidebarNav.length === 0) {
    return []
  }
  return [
    {
      id: 'system-shell',
      path: '/system',
      labelZh: '系统管理',
      labelEn: 'System',
      iconKey: 'panels-top-left',
      section: 'admin',
      sortOrder: 0,
      enabled: true,
      workspace: 'system',
      children: sidebarNav,
    },
  ]
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
      { key: 'platform', label: 'Platform Workbench', description: 'Clusters, workloads, network, storage, and runtime resources', icon: <AppstoreOutlined /> },
      { key: 'delivery', label: 'Delivery Workbench', description: 'Applications, build sources, bindings, and release orchestration', icon: <CloudServerOutlined /> },
      { key: 'ai', label: 'AI Workbench', description: 'Investigation, automation, tools, and skills', icon: <RobotOutlined /> },
      { key: 'monitoring', label: 'Monitoring Workbench', description: 'Alerts, routes, notifications, and on-call flows', icon: <AlertOutlined /> },
    ]
  }
  return [
    { key: 'platform', label: '平台工作台', description: '集群、工作负载、网络、存储与运行资源', icon: <AppstoreOutlined /> },
    { key: 'delivery', label: '应用交付工作台', description: '应用、构建来源、环境绑定与发布编排', icon: <CloudServerOutlined /> },
    { key: 'ai', label: 'AI工作台', description: '调查、自动化、工具与技能', icon: <RobotOutlined /> },
    { key: 'monitoring', label: '监控工作台', description: '告警、路由、通知和值班协同', icon: <AlertOutlined /> },
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
      <div className="kc-workspace-option">
        <span className="kc-workspace-option__icon">{option.icon}</span>
        <span className="kc-workspace-option__copy">
          <span className="kc-workspace-option__label">{option.label}</span>
          <span className="kc-workspace-option__desc">{option.description}</span>
        </span>
      </div>
    ),
  }))

  const trigger = (
    <Button className="kc-workbench-switcher" type="text">
      <span className="kc-workbench-switcher__icon">{current.icon}</span>
      {!collapsed ? (
        <span className="kc-workbench-switcher__copy">
          <span className="kc-workbench-switcher__label">{current.label}</span>
          <span className="kc-workbench-switcher__desc">{current.description}</span>
        </span>
      ) : null}
      {options.length > 1 ? <DownOutlined className="kc-workbench-switcher__arrow" /> : null}
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
  const { user, clearAuth } = useAuthStore()
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
  const currentRouteWorkspace = getRouteWorkspace(currentMeta)
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
      return (['platform', 'ai', 'monitoring'] as const).find((item) => accessibleWorkbenchIds.includes(item)) ?? null
    }
    return accessibleWorkbenchIds[0] ?? null
  }, [accessibleWorkbenchIds, activeWorkspace, currentWorkbenchId])
  const isAIWorkbenchRoute = activeWorkbenchId === 'ai'

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
  const systemMenuTree = useMemo(() => (isAIWorkbenchRoute ? [] : wrapSystemNav(systemNav)), [isAIWorkbenchRoute, systemNav])
  const businessMenuItems = useMemo(() => buildMenuItems(businessNav, localeCode), [businessNav, localeCode])
  const systemMenuItems = useMemo(() => buildMenuItems(systemMenuTree, localeCode, { grouped: false }), [systemMenuTree, localeCode])
  const businessItemKeyToPath = useMemo(() => buildItemKeyToPath(businessNav), [businessNav])
  const systemItemKeyToPath = useMemo(() => buildItemKeyToPath(systemMenuTree), [systemMenuTree])
  const combinedNav = useMemo(() => [...businessNav, ...systemMenuTree], [businessNav, systemMenuTree])
  const combinedItemKeyToPath = useMemo(
    () => ({ ...businessItemKeyToPath, ...systemItemKeyToPath }),
    [businessItemKeyToPath, systemItemKeyToPath],
  )
  const parentByID = useMemo(() => buildParentMap(combinedNav), [combinedNav])
  const businessNodeIDs = useMemo(() => collectNodeIDs(businessNav), [businessNav])
  const businessExpandableNodeIDs = useMemo(() => collectExpandableNodeIDs(businessNav), [businessNav])
  const systemNodeIDs = useMemo(() => collectNodeIDs(systemMenuTree), [systemMenuTree])
  const systemExpandableNodeIDs = useMemo(() => collectExpandableNodeIDs(systemMenuTree), [systemMenuTree])
  const resolvedThemeMode = useMemo(() => resolveThemeMode(themeMode), [themeMode, systemThemeVersion])

  const parentMeta = getParentRouteMeta(currentMeta)
  const currentMenuID = useMemo(
    () => findMenuIDByRoutePath(combinedNav, currentMeta.path) ?? resolveRouteMenuId(currentMeta) ?? currentMeta.menuId ?? currentMeta.id,
    [combinedNav, currentMeta],
  )

  const selectedKeys = useMemo(() => {
    if (currentMenuID && combinedItemKeyToPath[currentMenuID]) {
      return [currentMenuID]
    }
    if (!currentMeta.navVisible && parentMeta?.navVisible && combinedItemKeyToPath[parentMeta.id]) {
      return [parentMeta.id]
    }
    return [currentMeta.id]
  }, [combinedItemKeyToPath, currentMenuID, currentMeta, parentMeta])

  const routeOpenKeys = useMemo(() => {
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

  const breadcrumbRoutes = useMemo(() => {
    const routes: Array<{ name: string; path?: string }> = []
    if (parentMeta) {
      routes.push({ name: t(`route.${parentMeta.id}.title`, parentMeta.title), path: parentMeta.redirectTo ?? parentMeta.path })
    }
    routes.push({ name: t(`route.${currentMeta.id}.title`, currentMeta.title) })
    return routes
  }, [currentMeta, parentMeta, t])

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
  const currentWorkbenchOption = workbenchOptions.find((option) => option.key === activeWorkbenchId) ?? workbenchOptions[0] ?? null
  const businessSelectedKeys = selectedKeys.filter((key) => businessNodeIDs.has(key))
  const systemSelectedKeys = selectedKeys.filter((key) => systemNodeIDs.has(key))

  if (permissionSnapshotQuery.isLoading) {
    return (
      <div className="flex items-center justify-center h-screen">
        <Spin size="large" />
      </div>
    )
  }

  return (
    <Layout className="kc-shell">
      <Sider
        className="kc-sider"
        collapsible
        collapsed={sidebarCollapsed}
        onCollapse={(collapsed) => setSidebarCollapsed(collapsed)}
        style={{ backgroundColor: 'transparent' }}
        trigger={null}
        width={248}
      >
        <div className="kc-nav" style={{ height: '100%' }}>
          <div className="kc-sider-topbar">
            <div className="kc-sider-brand">
              {activeLogo ? (
                <img className="kc-brand-logo" src={activeLogo} alt={branding.sidebarTitle} />
              ) : (
                <div className="kc-brand-mark">KC</div>
              )}
            </div>
          </div>

          {workbenchOptions.length > 0 && currentWorkbenchOption ? (
            <div className="kc-workbench-switcher-shell">
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

          {!isAIWorkbenchRoute ? (
            <div className="kc-nav-business">
              <Menu
                className="kc-nav-menu kc-nav-menu--business"
                mode="inline"
                items={businessMenuItems}
                selectedKeys={businessSelectedKeys}
                openKeys={sidebarCollapsed ? [] : businessOpenKeys}
                onOpenChange={(keys) => setBusinessOpenKeys(keys as string[])}
                onClick={({ key }) => {
                  const path = businessItemKeyToPath[String(key)]
                  if (path) navigate(path)
                }}
                inlineCollapsed={sidebarCollapsed}
                theme={resolvedThemeMode}
              />
            </div>
          ) : null}

          {!isAIWorkbenchRoute && (systemMenuItems?.length ?? 0) > 0 ? (
            <div className="kc-nav-system">
              <Menu
                className="kc-nav-menu kc-nav-menu--system"
                mode="inline"
                items={systemMenuItems}
                selectedKeys={systemSelectedKeys}
                openKeys={sidebarCollapsed ? [] : systemOpenKeys}
                onOpenChange={(keys) => setSystemOpenKeys(keys as string[])}
                onClick={({ key }) => {
                  const path = systemItemKeyToPath[String(key)]
                  if (path) navigate(path)
                }}
                inlineCollapsed={sidebarCollapsed}
                theme={resolvedThemeMode}
              />
            </div>
          ) : null}
        </div>
      </Sider>

      <Layout className="kc-main">
        <Header className="kc-header">
          <div className="kc-header-top-row">
            <div className="kc-header-main">
              <div className="kc-header-breadcrumb-row">
                <Button
                  aria-label={sidebarCollapsed ? t('layout.expand', 'Expand sidebar') : t('layout.collapse', 'Collapse sidebar')}
                  className="kc-header-action kc-header-sider-toggle"
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
            <div className="kc-header-right">
              <Button
                className="kc-header-action"
                size="small"
                type="text"
                icon={<QuestionCircleOutlined />}
                onClick={() => window.open('/docs/', '_blank', 'noopener,noreferrer')}
              >
                {t('layout.docs', 'Docs')}
              </Button>
              <div className="kc-header-preferences">
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
                    { key: 'logout', label: t('layout.logout', 'Sign out') },
                  ],
                  onClick: ({ key }) => {
                    if (key === 'logout') {
                      clearAuth()
                      navigate('/login')
                    }
                  },
                }}
                placement="bottomRight"
              >
                <Button
                  className="kc-header-action kc-user-trigger"
                  size="small"
                  type="text"
                  icon={
                    <Avatar className="kc-user-avatar" size="small">
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

        <Content className="kc-content">
          <div className="kc-content-inner kc-pro-content-host">
            <Outlet />
          </div>
        </Content>
      </Layout>
    </Layout>
  )
}
