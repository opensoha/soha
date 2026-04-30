import type { ReactNode } from 'react'
import { PageContainer, ProCard } from '@ant-design/pro-components'
import { Button, Space, Tag, Typography } from 'antd'
import { useNavigate } from '@umijs/max'

const { Text, Title } = Typography

export interface PlatformWorkspaceTab {
  key: string
  label: string
  path: string
}

export interface PlatformWorkspaceMetric {
  label: string
  value: string
}

interface PlatformWorkspaceShellProps {
  title: string
  description: string
  currentPath: string
  tabs: PlatformWorkspaceTab[]
  metrics: PlatformWorkspaceMetric[]
  badge?: string
  actions?: ReactNode
  children: ReactNode
}

export function PlatformWorkspaceShell({
  title,
  description,
  currentPath,
  tabs,
  metrics,
  badge,
  actions,
  children,
}: PlatformWorkspaceShellProps) {
  const navigate = useNavigate()
  const activeTab = tabs.find((tab) => currentPath === tab.path || currentPath.startsWith(`${tab.path}/`))?.key ?? tabs[0]?.key

  return (
    <PageContainer
      ghost
      title={title}
      content={description}
      extra={actions}
      tabList={tabs.map((tab) => ({ key: tab.key, tab: tab.label }))}
      tabActiveKey={activeTab}
      onTabChange={(key) => {
        const nextTab = tabs.find((tab) => tab.key === key)
        if (nextTab) {
          navigate(nextTab.path)
        }
      }}
      childrenContentStyle={{ paddingBlock: 0, paddingInline: 0 }}
    >
      <ProCard
        ghost
        gutter={[16, 16]}
        wrap
        style={{ marginBottom: 16 }}
      >
        <ProCard colSpan={{ xs: 24, xl: 10 }} boxShadow>
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            <Space size={8} wrap>
              <Title level={5} style={{ margin: 0 }}>
                {title}
              </Title>
              {badge ? <Tag color="blue">{badge}</Tag> : null}
            </Space>
            <Text type="secondary">{description}</Text>
          </Space>
        </ProCard>
        <ProCard colSpan={{ xs: 24, xl: 14 }} ghost>
          <ProCard gutter={[12, 12]} wrap ghost>
            {metrics.map((metric) => (
              <ProCard key={metric.label} colSpan={{ xs: 24, sm: 12, xl: 8 }} boxShadow>
                <Space direction="vertical" size={4}>
                  <Text type="secondary">{metric.label}</Text>
                  <Text strong>{metric.value}</Text>
                </Space>
              </ProCard>
            ))}
          </ProCard>
        </ProCard>
      </ProCard>
      {children}
    </PageContainer>
  )
}

export function WorkspaceRefreshButton({ onClick }: { onClick: () => void }) {
  return (
    <Button onClick={onClick}>
      Refresh
    </Button>
  )
}
