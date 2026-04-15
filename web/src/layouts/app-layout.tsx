import { useEffect, useMemo, useState } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import {
  Layout,
  Nav,
  Avatar,
  Dropdown,
  Breadcrumb,
  Button,
  Select,
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
} from '@douyinfe/semi-icons'
import type { ReactNode } from 'react'
import { Spin } from '@douyinfe/semi-ui'
import { useI18n } from '@/i18n'
import { getAccessibleSidebarNav, getRouteMeta, getParentRouteMeta } from '@/routes/meta'
import { usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { getSemiThemeLabel, semiThemeOptions, themeModeOptions } from '@/theme/semi-theme'
import type { SidebarNavItem } from '@/types'

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
  const themeId = usePreferencesStore((state) => state.themeId)
  const themeMode = usePreferencesStore((state) => state.themeMode)
  const setThemeId = usePreferencesStore((state) => state.setThemeId)
  const setThemeMode = usePreferencesStore((state) => state.setThemeMode)
  const localeCode = usePreferencesStore((state) => state.localeCode)
  const setLocaleCode = usePreferencesStore((state) => state.setLocaleCode)
  const { t } = useI18n()
  const [isNavCollapsed, setIsNavCollapsed] = useState(false)
  const [navOpenKeys, setNavOpenKeys] = useState<string[]>([])

  const sidebarNav = useMemo(
    () => getAccessibleSidebarNav(permissionSnapshotQuery.data?.data),
    [permissionSnapshotQuery.data?.data],
  )
  const navItems = useMemo(() => isNavCollapsed ? buildNavItems(sidebarNav, t).filter((item) => !String(item.itemKey).startsWith('section-')) : buildNavItems(sidebarNav, t), [isNavCollapsed, sidebarNav, t])
  const itemKeyToPath = useMemo(() => buildItemKeyToPath(sidebarNav), [sidebarNav])

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
    if (routeOpenKeys.length === 0) {
      return
    }
    setNavOpenKeys((current) => {
      const next = [...new Set([...current, ...routeOpenKeys])]
      return next
    })
  }, [isNavCollapsed, routeOpenKeys])

  const handleNavClick = (data: { itemKey: string }) => {
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
  const currentThemeLabel = getSemiThemeLabel(themeId)

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
          onOpenChange={(data) => setNavOpenKeys((data.openKeys ?? []) as string[])}
          onCollapseChange={(collapsed) => setIsNavCollapsed(Boolean(collapsed))}
          header={{
            logo: (
              <div className="kc-brand-mark">
                  KC
              </div>
            ),
            text: 'KubeCrux',
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
              theme="light"
              icon={<IconHelpCircle />}
              onClick={() => window.open('/docs/', '_blank', 'noopener,noreferrer')}
            >
              {t('layout.docs', 'Docs')}
            </Button>
            <span className="kc-header-pill">{currentThemeLabel}</span>
            <Select
              className="kc-theme-select"
              size="small"
              insetLabel={t('layout.language', 'Language')}
              value={localeCode}
              optionList={[
                { value: 'zh_CN', label: '简体中文' },
                { value: 'en_US', label: 'English' },
              ]}
              onChange={(value) => setLocaleCode(value as 'zh_CN' | 'en_US')}
            />
            <Select
              className="kc-theme-select"
              size="small"
              insetLabel={t('layout.theme', 'Theme')}
              value={themeId}
              optionList={semiThemeOptions.map((option) => ({
                value: option.id,
                label: option.label,
              }))}
              onChange={(value) => setThemeId(value as any)}
            />
            <Select
              className="kc-mode-select"
              size="small"
              insetLabel={t('layout.mode', 'Mode')}
              value={themeMode}
              optionList={themeModeOptions.map((option) => ({
                value: option.value,
                label: option.label,
              }))}
              onChange={(value) => setThemeMode(value as any)}
            />
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
                className="kc-user-trigger"
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
