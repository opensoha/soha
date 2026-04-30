import { useEffect, useMemo, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Avatar, Breadcrumb, Button, Dropdown, Layout, Menu, Spin } from 'antd'
import type { MenuProps } from 'antd'
import {
  AlertOutlined,
  AppstoreOutlined,
  BellOutlined,
  BookOutlined,
  CommentOutlined,
  DashboardOutlined,
  DesktopOutlined,
  DeploymentUnitOutlined,
  FileOutlined,
  GlobalOutlined,
  HddOutlined,
  InboxOutlined,
  LinkOutlined,
  LockOutlined,
  MenuOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  MoonOutlined,
  OrderedListOutlined,
  QuestionCircleOutlined,
  SafetyOutlined,
  SendOutlined,
  SettingOutlined,
  SunOutlined,
  TeamOutlined,
  TranslationOutlined,
  UserOutlined,
  UserSwitchOutlined,
} from '@ant-design/icons'
import type { ReactNode } from 'react'
import { HeaderPreferenceButton } from '@/components/header-preference-button'
import { useI18n } from '@/i18n'
import { useBrandingSettings } from '@/features/settings/use-branding-settings'
import { getAccessibleSidebarNav, getRouteMeta, getParentRouteMeta } from '@/routes/meta'
import { usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { resolveThemeMode, watchSystemThemeMode } from '@/theme/semi-theme'
import type { SidebarNavItem } from '@/types'
import { getNormalizedBranding } from '@/features/settings/use-branding-settings'

const { Sider, Header, Content } = Layout

const iconMap: Record<string, ReactNode> = {
  IconDesktop: <DesktopOutlined />,
  IconGlobe: <GlobalOutlined />,
  IconGridView: <AppstoreOutlined />,
  IconConnection: <LinkOutlined />,
  IconServer: <HddOutlined />,
  IconPuzzle: <DeploymentUnitOutlined />,
  IconAppCenter: <AppstoreOutlined />,
  IconFlow: <SendOutlined />,
  IconSend: <SendOutlined />,
  IconInbox: <InboxOutlined />,
  IconPulse: <DashboardOutlined />,
  IconAlertTriangle: <AlertOutlined />,
  IconBell: <BellOutlined />,
  IconUserCircle: <UserSwitchOutlined />,
  IconComment: <CommentOutlined />,
  IconShield: <SafetyOutlined />,
  IconUser: <UserOutlined />,
  IconUserGroup: <TeamOutlined />,
  IconSetting: <SettingOutlined />,
  IconMenu: <MenuOutlined />,
  IconFile: <FileOutlined />,
  IconList: <OrderedListOutlined />,
  IconBookOpen: <BookOutlined />,
  IconLock: <LockOutlined />,
}

function buildMenuItems(sidebarNav: SidebarNavItem[], t: (key: string, fallback?: string) => string): MenuProps['items'] {
  const groupLabels: Record<string, string> = {
    overview: '概览',
    platform: '平台管理',
    catalog: '基础目录',
    delivery: '应用交付',
    observe: '运维智能',
    access: '访问控制',
    system: '系统管理',
    settings: '设置中心',
  }

  const groups = new Map<string, NonNullable<MenuProps['items']>[number][]>()

  for (const item of sidebarNav) {
    const groupKey = item.route.group
    const menuItem: NonNullable<MenuProps['items']>[number] = item.children?.length
      ? {
          key: item.route.id,
          icon: iconMap[item.route.icon],
          label: t(`route.${item.route.id}.title`, item.route.title),
          children: item.children.map((child) => ({
            key: child.id,
            label: t(`route.${child.id}.title`, child.title),
          })),
        }
      : {
          key: item.route.id,
          icon: iconMap[item.route.icon],
          label: t(`route.${item.route.id}.title`, item.route.title),
        }

    const current = groups.get(groupKey) ?? []
    current.push(menuItem)
    groups.set(groupKey, current)
  }

  return Array.from(groups.entries()).map(([groupKey, items]) => ({
    key: `group-${groupKey}`,
    type: 'group' as const,
    label: <span className="kc-nav-section-title">{groupLabels[groupKey] || t(`layout.group.${groupKey}`, groupKey)}</span>,
    children: items,
  }))
}

function buildItemKeyToPath(sidebarNav: SidebarNavItem[]): Record<string, string> {
  const map: Record<string, string> = {}
  for (const item of sidebarNav) {
    map[item.route.id] = item.route.redirectTo ?? item.route.path
    if (item.children) {
      for (const child of item.children) {
        map[child.id] = child.path
      }
    }
  }
  return map
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
  const menuItems = useMemo(() => buildMenuItems(sidebarNav, t), [sidebarNav, t])
  const itemKeyToPath = useMemo(() => buildItemKeyToPath(sidebarNav), [sidebarNav])
  const resolvedThemeMode = useMemo(() => resolveThemeMode(themeMode), [themeMode, systemThemeVersion])

  const currentMeta = getRouteMeta(location.pathname)
  const parentMeta = getParentRouteMeta(currentMeta)

  const selectedKeys = useMemo(() => {
    let primaryKey = currentMeta.id
    if (!currentMeta.navVisible && parentMeta?.navVisible && itemKeyToPath[parentMeta.id]) {
      primaryKey = parentMeta.id
    } else if (!currentMeta.navVisible && currentMeta.menuId && itemKeyToPath[currentMeta.menuId]) {
      primaryKey = currentMeta.menuId
    }
    return [primaryKey]
  }, [currentMeta, itemKeyToPath, parentMeta])

  const routeOpenKeys = useMemo(() => {
    if (currentMeta.parentId && itemKeyToPath[currentMeta.parentId]) {
      return [currentMeta.parentId]
    }
    return []
  }, [currentMeta, itemKeyToPath])

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
          <div className="kc-content-inner">
            <Outlet />
          </div>
        </Content>
      </Layout>
    </Layout>
  )
}
