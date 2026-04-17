import { useEffect, useMemo, useState } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import {
  Layout,
  Nav,
  Avatar,
  Dropdown,
  Breadcrumb,
  Button,
} from '@douyinfe/semi-ui'
import {
  IconDesktop,
  IconGlobe,
  IconGridView,
  IconServer,
  IconPuzzle,
  IconAppCenter,
  IconSend,
  IconInbox,
  IconPulse,
  IconAlertTriangle,
  IconBell,
  IconUserCircle,
  IconComment,
  IconShield,
  IconUser,
  IconUserGroup,
  IconSetting,
  IconMenu,
  IconFile,
  IconList,
  IconLock,
  IconHelpCircle,
  IconLanguage,
  IconMoon,
  IconSun,
} from '@douyinfe/semi-icons'
import type { ReactNode } from 'react'
import { Spin } from '@douyinfe/semi-ui'
import { HeaderPreferenceButton } from '@/components/header-preference-button'
import { useI18n } from '@/i18n'
import { useBrandingSettings } from '@/features/settings/use-branding-settings'
import { getAccessibleSidebarNav, getRouteMeta, getParentRouteMeta } from '@/routes/meta'
import { usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { resolveThemeMode } from '@/theme/semi-theme'
import type { SidebarNavItem } from '@/types'
import { getNormalizedBranding } from '@/features/settings/use-branding-settings'

const { Sider, Header, Content } = Layout

const iconMap: Record<string, ReactNode> = {
  IconDesktop: <IconDesktop />,
  IconGlobe: <IconGlobe />,
  IconGridView: <IconGridView />,
  IconConnection: <IconServer />,
  IconServer: <IconServer />,
  IconPuzzle: <IconPuzzle />,
  IconAppCenter: <IconAppCenter />,
  IconFlow: <IconSend />,
  IconSend: <IconSend />,
  IconInbox: <IconInbox />,
  IconPulse: <IconPulse />,
  IconAlertTriangle: <IconAlertTriangle />,
  IconBell: <IconBell />,
  IconUserCircle: <IconUserCircle />,
  IconComment: <IconComment />,
  IconShield: <IconShield />,
  IconUser: <IconUser />,
  IconUserGroup: <IconUserGroup />,
  IconSetting: <IconSetting />,
  IconMenu: <IconMenu />,
  IconFile: <IconFile />,
  IconList: <IconList />,
  IconBookOpen: <IconFile />,
  IconLock: <IconLock />,
}

function buildNavItems(sidebarNav: SidebarNavItem[], t: (key: string, fallback?: string) => string) {
  const groupLabels: Record<string, string> = {
    overview: '概览',
    platform: '平台管理',
    delivery: '应用交付',
    observe: '运维智能',
    access: '访问控制',
    system: '系统管理',
    settings: '设置中心',
  }

  const items: any[] = []
  let previousGroup = ''

  for (const item of sidebarNav) {
    if (item.route.group !== previousGroup) {
      previousGroup = item.route.group
      items.push({
        itemKey: `section-${item.route.group}`,
        text: <span className="kc-nav-section-title">{groupLabels[item.route.group] || t(`layout.group.${item.route.group}`, item.route.group)}</span>,
        disabled: true,
      })
    }
    if (item.children && item.children.length > 0) {
      items.push({
        itemKey: item.route.id,
        text: t(`route.${item.route.id}.title`, item.route.title),
        icon: iconMap[item.route.icon],
        items: item.children.map((child) => ({
          itemKey: child.id,
          text: t(`route.${child.id}.title`, child.title),
        })),
      })
      continue
    }
    items.push({
      itemKey: item.route.id,
      text: t(`route.${item.route.id}.title`, item.route.title),
      icon: iconMap[item.route.icon],
    })
  }

  return items
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
  const { t } = useI18n()
  const [isNavCollapsed, setIsNavCollapsed] = useState(false)
  const [navOpenKeys, setNavOpenKeys] = useState<string[]>([])
  const [resolvedThemeMode, setResolvedThemeMode] = useState<'light' | 'dark'>(() => resolveThemeMode(themeMode))

  const sidebarNav = useMemo(
    () => getAccessibleSidebarNav(permissionSnapshotQuery.data?.data),
    [permissionSnapshotQuery.data?.data],
  )
  const navItems = useMemo(() => isNavCollapsed ? buildNavItems(sidebarNav, t).filter((item) => !String(item.itemKey).startsWith('section-')) : buildNavItems(sidebarNav, t), [isNavCollapsed, sidebarNav, t])
  const itemKeyToPath = useMemo(() => buildItemKeyToPath(sidebarNav), [sidebarNav])
  const parentItemKeys = useMemo(
    () => new Set(sidebarNav.filter((item) => item.children?.length).map((item) => item.route.id)),
    [sidebarNav],
  )

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
    if (currentMeta.navVisible && sidebarNav.some((item) => item.route.id === currentMeta.id && item.children?.length)) {
      return [currentMeta.id]
    }
    return []
  }, [currentMeta, itemKeyToPath, sidebarNav])

  useEffect(() => {
    if (isNavCollapsed) {
      setNavOpenKeys([])
      return
    }
    setNavOpenKeys(routeOpenKeys)
  }, [isNavCollapsed, location.pathname, routeOpenKeys])

  useEffect(() => {
    const updateResolvedThemeMode = () => setResolvedThemeMode(resolveThemeMode(themeMode))
    updateResolvedThemeMode()

    if (themeMode !== 'system' || typeof window === 'undefined') {
      return undefined
    }

    const media = window.matchMedia('(prefers-color-scheme: dark)')
    media.addEventListener('change', updateResolvedThemeMode)
    return () => media.removeEventListener('change', updateResolvedThemeMode)
  }, [themeMode])

  const handleNavClick = (data: { itemKey: string }) => {
    if (parentItemKeys.has(data.itemKey)) {
      setNavOpenKeys((current) => (
        current.includes(data.itemKey)
          ? current.filter((key) => key !== data.itemKey)
          : [...current, data.itemKey]
      ))
      return
    }
    const path = itemKeyToPath[data.itemKey]
    if (path) {
      navigate(path)
    }
  }

  const handleLogout = () => {
    clearAuth()
    navigate('/login')
  }

  const breadcrumbRoutes = useMemo(() => {
    const routes: { name: string; path?: string }[] = []
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
  const activeLogo = isNavCollapsed ? (collapsedLogo || expandedLogo) : (expandedLogo || collapsedLogo)
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
      <Sider className="kc-sider" style={{ backgroundColor: 'transparent' }}>
        <Nav
          className="kc-nav"
          style={{ height: '100%' }}
          items={navItems}
          selectedKeys={selectedKeys}
          openKeys={navOpenKeys}
          onClick={handleNavClick as any}
          onOpenChange={(data) => {
            const nextOpenKeys = Array.isArray(data)
              ? data
              : ((data?.openKeys ?? []) as string[])
            setNavOpenKeys(nextOpenKeys)
          }}
          onCollapseChange={(collapsed) => setIsNavCollapsed(Boolean(collapsed))}
          header={{
            logo: activeLogo ? (
              <img className="kc-brand-logo" src={activeLogo} alt={branding.sidebarTitle} />
            ) : (
              <div className="kc-brand-mark">
                KC
              </div>
            ),
            text: branding.sidebarTitle,
          }}
          footer={{
            collapseButton: true,
          }}
        />
      </Sider>

      <Layout className="kc-main">
        <Header className="kc-header">
          <div className="kc-header-main">
            <Breadcrumb>
              {breadcrumbRoutes.map((route, index) => (
                <Breadcrumb.Item
                  key={index}
                  href={route.path}
                  onClick={(e) => {
                    if (route.path) {
                      e?.preventDefault()
                      navigate(route.path)
                    }
                  }}
                >
                  {route.name}
                </Breadcrumb.Item>
              ))}
            </Breadcrumb>
          </div>

          <div className="kc-header-right">
            <Button
              className="kc-header-action"
              size="small"
              theme="borderless"
              icon={<IconHelpCircle />}
              onClick={() => window.open('/docs/', '_blank', 'noopener,noreferrer')}
            >
              {t('layout.docs', 'Docs')}
            </Button>
            <div className="kc-header-preferences">
              <HeaderPreferenceButton
                ariaLabel={languageSwitchTitle}
                title={languageSwitchTitle}
                inset
                icon={<IconLanguage />}
                label={languageSwitchLabel}
                onClick={() => setLocaleCode(localeCode === 'zh_CN' ? 'en_US' : 'zh_CN')}
              />
              <HeaderPreferenceButton
                ariaLabel={themeSwitchTitle}
                title={themeSwitchTitle}
                icon={resolvedThemeMode === 'dark' ? <IconSun /> : <IconMoon />}
                onClick={() => setThemeMode(resolvedThemeMode === 'dark' ? 'light' : 'dark')}
                pressed={resolvedThemeMode === 'dark'}
              />
            </div>
            <Dropdown
              position="bottomRight"
              render={
                <Dropdown.Menu>
                  <Dropdown.Item disabled>
                    {userDisplayName}
                  </Dropdown.Item>
                  <Dropdown.Divider />
                  <Dropdown.Item onClick={handleLogout}>
                    {t('layout.logout', 'Sign out')}
                  </Dropdown.Item>
                </Dropdown.Menu>
              }
            >
              <Button
                className="kc-header-action kc-user-trigger"
                size="small"
                theme="borderless"
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
