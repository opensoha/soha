import type { ReactNode } from 'react'
import { Typography } from 'antd'

const { Title, Text } = Typography

interface PageHeaderProps {
  title: string
  description?: string
  actions?: ReactNode
}

export function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <div className="kc-page-header">
      <div className="kc-page-title-wrap">
        <Title className="kc-page-title" level={4}>
          {title}
        </Title>
        {description ? <Text className="kc-page-description">{description}</Text> : null}
      </div>
      {actions ? <div className="kc-page-toolbar">{actions}</div> : null}
    </div>
  )
}
