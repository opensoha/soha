import type { ReactNode } from 'react'
import { useMemo } from 'react'
import { useLocation, useNavigate } from '@umijs/max'
import { PageContainer } from '@ant-design/pro-components'
import { Button, Card, Col, Empty, Row, Space, Tag, Typography } from 'antd'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { getRouteMeta, routeMeta } from '@/routes/meta'

const { Paragraph, Text, Title } = Typography

interface WorkspaceItem {
  key: string
  path: string
  label: string
  description: string
  permissionKey?: string
}

interface WorkspaceStat {
  label: string
  value: string
  hint: string
}

interface WorkspaceHighlight {
  title: string
  description: string
}

interface WorkspaceAction {
  label: string
  description: string
}

interface NonPlatformWorkspaceProps {
  rootPath: string
  title: string
  description: string
  workspaceLabel: string
  workspaceSummary: string
  items: WorkspaceItem[]
  stats?: WorkspaceStat[]
  highlights?: WorkspaceHighlight[]
  actions?: WorkspaceAction[]
  children: ReactNode
}

function canViewItem(permissionKeys: string[] | undefined, item: WorkspaceItem) {
  if (!item.permissionKey) return true
  return hasPermission(permissionKeys ? { permissionKeys, visibleMenuIds: [], visibleMenus: [] } : undefined, item.permissionKey)
}

export function NonPlatformWorkspace({
  rootPath,
  title,
  description,
  workspaceLabel,
  workspaceSummary,
  items,
  stats = [],
  highlights = [],
  actions = [],
  children,
}: NonPlatformWorkspaceProps) {
  const location = useLocation()
  const navigate = useNavigate()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const currentRoute = getRouteMeta(location.pathname)

  const visibleItems = useMemo(
    () =>
      items.filter((item) => {
        if (!item.permissionKey) return true
        return hasPermission(snapshot, item.permissionKey)
      }),
    [items, snapshot],
  )

  const activeItem = visibleItems.find((item) => location.pathname === item.path || location.pathname.startsWith(`${item.path}/`))
    ?? visibleItems[0]

  const menuTag = useMemo(() => {
    const route = routeMeta.find((item) => item.path === activeItem?.path)
    if (!route?.menuId) return null
    return route.menuId
  }, [activeItem?.path])

  return (
    <PageContainer
      title={title}
      subTitle={description}
      tags={
        <Space size={[8, 8]} wrap>
          <Tag color="blue">{workspaceLabel}</Tag>
          <Tag>{currentRoute.title}</Tag>
          {menuTag ? <Tag bordered={false}>{menuTag}</Tag> : null}
        </Space>
      }
      content={
        <Row gutter={[16, 16]}>
          <Col xs={24} xl={14}>
            <Paragraph style={{ marginBottom: 0 }}>
              {workspaceSummary}
            </Paragraph>
          </Col>
          <Col xs={24} xl={10}>
            <Card size="small">
              <Text type="secondary">当前入口</Text>
              <Title level={5} style={{ marginTop: 8, marginBottom: 4 }}>
                {activeItem?.label ?? currentRoute.title}
              </Title>
              <Paragraph type="secondary" style={{ marginBottom: 0 }}>
                {activeItem?.description ?? currentRoute.description}
              </Paragraph>
            </Card>
          </Col>
        </Row>
      }
      extra={
        <Space wrap>
          {visibleItems.map((item) => (
            <Button
              key={item.key}
              type={activeItem?.key === item.key ? 'primary' : 'default'}
              onClick={() => navigate(item.path)}
            >
              {item.label}
            </Button>
          ))}
        </Space>
      }
    >
      {stats.length > 0 ? (
        <Row gutter={[16, 16]} style={{ marginBottom: 16 }}>
          {stats.map((item) => (
            <Col key={item.label} xs={24} sm={12} xl={6}>
              <Card size="small">
                <Text type="secondary">{item.label}</Text>
                <Title level={4} style={{ marginTop: 8, marginBottom: 4 }}>
                  {item.value}
                </Title>
                <Paragraph type="secondary" style={{ marginBottom: 0 }}>
                  {item.hint}
                </Paragraph>
              </Card>
            </Col>
          ))}
        </Row>
      ) : null}

      {(highlights.length > 0 || actions.length > 0) ? (
        <Row gutter={[16, 16]} style={{ marginBottom: 16 }}>
          {highlights.length > 0 ? (
            <Col xs={24} xl={actions.length > 0 ? 14 : 24}>
              <Card size="small" title="工作区重点">
                <Space direction="vertical" size={12} style={{ width: '100%' }}>
                  {highlights.map((item) => (
                    <div key={item.title}>
                      <Text strong>{item.title}</Text>
                      <Paragraph type="secondary" style={{ marginTop: 4, marginBottom: 0 }}>
                        {item.description}
                      </Paragraph>
                    </div>
                  ))}
                </Space>
              </Card>
            </Col>
          ) : null}
          {actions.length > 0 ? (
            <Col xs={24} xl={highlights.length > 0 ? 10 : 24}>
              <Card size="small" title="当前动作焦点">
                <Space direction="vertical" size={12} style={{ width: '100%' }}>
                  {actions.map((item) => (
                    <div key={item.label}>
                      <Text strong>{item.label}</Text>
                      <Paragraph type="secondary" style={{ marginTop: 4, marginBottom: 0 }}>
                        {item.description}
                      </Paragraph>
                    </div>
                  ))}
                </Space>
              </Card>
            </Col>
          ) : null}
        </Row>
      ) : null}

      {visibleItems.length === 0 ? (
        <Card>
          <Empty description={`${rootPath} 没有可访问的工作区`} />
        </Card>
      ) : (
        children
      )}
    </PageContainer>
  )
}
