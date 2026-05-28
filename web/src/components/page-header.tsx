import type { ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { Typography } from 'antd'
import { ResourceWorkspaceScopeBar } from '@/components/platform-scope-toolbar'
import { getRouteMeta, getRouteScopeMode, getRouteWorkspace } from '@/routes/meta'

const { Title, Text } = Typography

interface PageHeaderProps {
  title: string
  description?: string
  actions?: ReactNode
  showResourceScope?: boolean
}

export function PageHeader({ title, description, actions, showResourceScope = true }: PageHeaderProps) {
  const location = useLocation()
  const currentRoute = getRouteMeta(location.pathname)
  const workspace = getRouteWorkspace(currentRoute)
  const scopeMode = getRouteScopeMode(currentRoute)
  const shouldShowResourceScope = showResourceScope && workspace === 'resource' && scopeMode !== 'hidden'

  return (
    <div className="soha-page-header-shell">
      <div className="soha-page-container-header">
        <div className="soha-page-container-header-main">
          <div className="soha-page-title-wrap">
            <Title className="soha-page-title" level={4}>
              {title}
            </Title>
            {description ? <Text className="soha-page-description">{description}</Text> : null}
          </div>
        </div>
        {actions ? <div className="soha-page-toolbar">{actions}</div> : null}
      </div>
      {shouldShowResourceScope ? <ResourceWorkspaceScopeBar scopeMode={scopeMode} /> : null}
    </div>
  )
}
