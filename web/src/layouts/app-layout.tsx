import { useEffect, useMemo, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Avatar, Breadcrumb, Button, Dropdown, Layout, Menu, Spin } from 'antd'
import type { MenuProps } from 'antd'
import {
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  MoonOutlined,
  QuestionCircleOutlined,
  SunOutlined,
  TranslationOutlined,
} from '@ant-design/icons'
import { HeaderPreferenceButton } from '@/components/header-preference-button'
import { AnnouncementBell } from '@/features/announcements/announcement-center'
import { resolveMenuIcon } from '@/features/system/menu-icons'
import { resolveMenuSectionLabel } from '@/features/system/menu-schema'
import { useI18n } from '@/i18n'
import { useBrandingSettings } from '@/features/settings/use-branding-settings'
import { getAccessibleSidebarNav, getRouteMeta, getParentRouteMeta, resolveRouteMenuId } from '@/routes/meta'
import { usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { resolveThemeMode, watchSystemThemeMode } from '@/theme/semi-theme'
import type { RuntimeMenuNode } from '@/types'
import { getNormalizedBranding } from '@/features/settings/use-branding-settings'

const { Sider, Header, Content } = Layout

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

function buildMenuItems(sidebarNav: RuntimeMenuNode[], localeCode: 'zh_CN' | 'en_US'): MenuProps['items'] {
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
  const { t } = useI18n()
  const [navOpenKeys, setNavOpenKeys] = useState<string[]>([])
  const [systemThemeVersion, setSystemThemeVersion] = useState(0)

  const sidebarNav = useMemo(
    () => getAccessibleSidebarNav(permissionSnapshotQuery.data?.data),
    [permissionSnapshotQuery.data?.data],
  )
  const menuItems = useMemo(() => buildMenuItems(sidebarNav, localeCode), [sidebarNav, localeCode])
  const itemKeyToPath = useMemo(() => buildItemKeyToPath(sidebarNav), [sidebarNav])
  const parentByID = useMemo(() => buildParentMap(sidebarNav), [sidebarNav])
  const resolvedThemeMode = useMemo(() => resolveThemeMode(themeMode), [themeMode, systemThemeVersion])

  const currentMeta = getRouteMeta(location.pathname)
  const parentMeta = getParentRouteMeta(currentMeta)
  const currentMenuID = useMemo(
    () => findMenuIDByRoutePath(sidebarNav, currentMeta.path) ?? resolveRouteMenuId(currentMeta) ?? currentMeta.menuId ?? currentMeta.id,
    [currentMeta, sidebarNav],
  )

  const selectedKeys = useMemo(() => {
    if (currentMenuID && itemKeyToPath[currentMenuID]) {
      return [currentMenuID]
    }
    if (!currentMeta.navVisible && parentMeta?.navVisible && itemKeyToPath[parentMeta.id]) {
      return [parentMeta.id]
    }
    return [currentMeta.id]
  }, [currentMenuID, currentMeta, itemKeyToPath, parentMeta])

  const routeOpenKeys = useMemo(() => {
    const keys: string[] = []
    let pointer = currentMenuID
    while (pointer && parentByID.has(pointer)) {
      const parentID = parentByID.get(pointer)
      if (!parentID) break
      keys.unshift(parentID)
      pointer = parentID
    }
    return keys
  }, [currentMenuID, parentByID])

  useEffect(() => {
    if (sidebarCollapsed) {
      setNavOpenKeys([])
      return
    }
    setNavOpenKeys((current) => {
      const desired = routeOpenKeys
      if (desired.length === 0) return current
      const merged = Array.from(new Set([...current, ...desired]))
      return merged.length === current.length && merged.every((key, index) => key === current[index]) ? current : merged
    })
  }, [routeOpenKeys, sidebarCollapsed])

  useEffect(() => {
    if (themeMode !== 'system') return undefined
    return watchSystemThemeMode(() => setSystemThemeVersion((current) => current + 1))
  }, [themeMode])

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
          <div className="kc-sider-brand">
            {activeLogo ? (
              <img className="kc-brand-logo" src={activeLogo} alt={branding.sidebarTitle} />
            ) : (
              <div className="kc-brand-mark">KC</div>
            )}
            {!sidebarCollapsed ? <span className="kc-sider-brand-text">{branding.sidebarTitle}</span> : null}
          </div>
          <Menu
            className="kc-nav-menu"
            mode="inline"
            items={menuItems}
            selectedKeys={selectedKeys}
            openKeys={sidebarCollapsed ? [] : navOpenKeys}
            onOpenChange={(keys) => setNavOpenKeys(keys as string[])}
            onClick={({ key }) => {
              const path = itemKeyToPath[String(key)]
              if (path) navigate(path)
            }}
            inlineCollapsed={sidebarCollapsed}
            theme={resolvedThemeMode}
          />
        </div>
      </Sider>

      <Layout className="kc-main">
        <Header className="kc-header">
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
