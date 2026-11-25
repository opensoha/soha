import type { ReactNode } from 'react'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { localeText, useI18n } from '@/i18n'
import { AppstoreOutlined, CodeOutlined, TeamOutlined } from '@ant-design/icons'
import { Badge } from 'antd'
import { Link } from 'react-router-dom'

export type PortalSectionKey = 'enterprise' | 'colleague-works' | 'my-apps'

export interface PortalViewNavProps {
  active: PortalSectionKey
  myAppsCount?: number
  extra?: ReactNode
}

export function PortalViewNav({ active, myAppsCount, extra }: PortalViewNavProps) {
  const { localeCode } = useI18n()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canAccessMyApps =
    hasPermission(permissionSnapshot, 'apphosting.apps.view') ||
    hasPermission(permissionSnapshot, 'apphosting.apps.publish')

  const items: Array<{
    key: PortalSectionKey
    to: string
    icon: ReactNode
    label: string
    badge?: number
  }> = [
    {
      key: 'enterprise',
      to: '/portal',
      icon: <AppstoreOutlined />,
      label: localeText(localeCode, '企业应用', 'Enterprise Apps'),
    },
    {
      key: 'colleague-works',
      to: '/portal/colleague-works',
      icon: <TeamOutlined />,
      label: localeText(localeCode, '同事作品', 'Colleague Works'),
    },
    ...(canAccessMyApps
      ? [
          {
            key: 'my-apps' as const,
            to: '/portal/my-apps',
            icon: <CodeOutlined />,
            label: localeText(localeCode, '我的作品', 'My Apps'),
            badge: typeof myAppsCount === 'number' && myAppsCount > 0 ? myAppsCount : undefined,
          },
        ]
      : []),
  ]

  return (
    <div className="soha-portal-view-nav-bar">
      <nav
        aria-label={localeText(localeCode, '门户视图切换', 'Portal view navigation')}
        className="soha-portal-view-nav"
      >
        {items.map((item) => {
          const isCurrent = item.key === active
          return (
            <Link
              key={item.key}
              to={item.to}
              aria-current={isCurrent ? 'page' : undefined}
              className={`soha-portal-view-nav-link${isCurrent ? ' is-active' : ''}`}
            >
              <span className="soha-portal-view-nav-icon">{item.icon}</span>
              <span>{item.label}</span>
              {item.badge !== undefined ? (
                <Badge count={item.badge} size="small" overflowCount={99} />
              ) : null}
            </Link>
          )
        })}
      </nav>
      {extra ? <div className="soha-portal-view-nav-extra">{extra}</div> : null}
    </div>
  )
}
