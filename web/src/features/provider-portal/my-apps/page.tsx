import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import {
  appHostingQueries,
  hostedAppKindLabel,
  hostedAppStatusLabel,
  hostedAppStatusTagValue,
  hostedAppVisibilitySummary,
  resolveSafeHostedAppUrl,
  type HostedApp,
  type HostedAppStatus,
} from '@/features/app-hosting'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth-store'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import {
  CopyOutlined,
  ExportOutlined,
  ReloadOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { App, Button, Collapse, Empty, Input, Select, Space, Typography } from 'antd'
import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import '../provider-portal-pages.css'
import { PortalAccountMenu } from '../shared/account-menu'
import { formatPortalDateTime } from '../shared/formatters'
import { PortalViewNav } from '../shared/portal-view-nav'
import { HostedAppDetailDrawer } from './app-detail-drawer'

const { Paragraph, Text, Title } = Typography

type StatusFilterValue = 'all' | HostedAppStatus

export function PortalMyHostedAppsPage() {
  const { message } = App.useApp()
  const { localeCode } = useI18n()
  const currentUserId = useAuthStore((state) => state.user?.userId ?? '')
  const permissionSnapshot = usePermissionSnapshot().data?.data
  const canPublish = hasPermission(permissionSnapshot, 'apphosting.apps.publish')
  const canView = hasPermission(permissionSnapshot, 'apphosting.apps.view') || canPublish
  const canOpenHeya = hasPermission(permissionSnapshot, 'observe.ai.chat')

  const [keyword, setKeyword] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilterValue>('all')
  const [activeAppId, setActiveAppId] = useState<string | null>(null)

  const appsQuery = useQuery(
    appHostingQueries.myApps(
      {
        query: keyword.trim() || undefined,
        status: statusFilter === 'all' ? undefined : statusFilter,
        limit: 100,
      },
      canView,
    ),
  )

  const appsData = readableQueryData(appsQuery)
  const apps = useMemo(() => appsData?.items ?? [], [appsData])
  const denied = !canView || queryAccessDenied(appsQuery)
  const activeInitialApp = useMemo(
    () => apps.find((item) => item.id === activeAppId),
    [activeAppId, apps],
  )

  const columns = useMemo<TableColumnsType<HostedApp>>(
    () => [
      {
        key: 'name',
        dataIndex: 'name',
        title: '作品名称',
        width: 220,
        render: (_, record) => (
          <Space orientation="vertical" size={2}>
            <Text strong>{record.name}</Text>
            {record.description ? (
              <Text type="secondary" ellipsis={{ tooltip: record.description }} style={{ maxWidth: 200 }}>
                {record.description}
              </Text>
            ) : null}
          </Space>
        ),
      },
      {
        key: 'url',
        dataIndex: 'url',
        title: '固定访问链接',
        width: 300,
        render: (_, record) => {
          const safeUrl = resolveSafeHostedAppUrl(record.url)
          return (
            <Space size={6} wrap>
              <Text code ellipsis={{ tooltip: record.url }} style={{ maxWidth: 220 }}>
                {record.url}
              </Text>
              <Button
                size="small"
                type="text"
                icon={<CopyOutlined />}
                aria-label={`复制链接 ${record.name}`}
                onClick={async () => {
                  try {
                    await navigator.clipboard.writeText(record.url)
                    void message.success('已复制分享链接')
                  } catch {
                    void message.error('复制失败，请手动复制')
                  }
                }}
              />
              {safeUrl && record.status === 'online' ? (
                <Button
                  size="small"
                  type="text"
                  icon={<ExportOutlined />}
                  href={safeUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  aria-label={`打开应用 ${record.name}`}
                />
              ) : null}
            </Space>
          )
        },
      },
      {
        key: 'kind',
        dataIndex: 'kind',
        title: '类型',
        width: 110,
        render: (_, record) => (
          <MetadataTag
            tone={record.kind === 'dynamic' ? 'purple' : 'blue'}
            label={hostedAppKindLabel(record.kind)}
          />
        ),
      },
      {
        key: 'role',
        title: '我的角色',
        width: 110,
        render: (_, record) => {
          const isOwner = Boolean(currentUserId && record.ownerUserId === currentUserId)
          return (
            <MetadataTag tone={isOwner ? 'cyan' : 'default'} label={isOwner ? '作者' : '协作者'} />
          )
        },
      },
      {
        key: 'visibility',
        title: '可见范围',
        width: 190,
        render: (_, record) => (
          <Space size={6} wrap>
            <Text>{hostedAppVisibilitySummary(record.visibility)}</Text>
            {record.pendingVisibilityApproval?.status === 'pending' ? (
              <StatusTag value="pending" label="全公司待审批" />
            ) : null}
          </Space>
        ),
      },
      {
        key: 'status',
        dataIndex: 'status',
        title: '在线状态',
        width: 120,
        render: (_, record) => (
          <StatusTag
            value={hostedAppStatusTagValue(record.status)}
            label={hostedAppStatusLabel(record.status)}
          />
        ),
      },
      {
        key: 'updatedAt',
        dataIndex: 'updatedAt',
        title: '最近更新',
        width: 140,
        render: (_, record) => formatPortalDateTime(record.updatedAt, localeCode),
      },
      {
        key: 'actions',
        title: '操作',
        width: 120,
        render: (_, record) => (
          <Button size="small" type="link" onClick={() => setActiveAppId(record.id)}>
            管理 / 详情
          </Button>
        ),
      },
    ],
    [currentUserId, localeCode, message],
  )

  const hasFilters = Boolean(keyword.trim() || statusFilter !== 'all')

  return (
    <div className="soha-provider-portal soha-provider-portal-home soha-portal-my-apps">
      <main className="soha-portal-main">
        <PortalViewNav
          active="my-apps"
          myAppsCount={appsData?.total}
          extra={<PortalAccountMenu />}
        />
        <ReadFeedback query={appsQuery} label="我的作品" canRead={canView} />

        {!canView ? (
          <ManagementState
            kind="no-permission"
            title="暂无作品管理权限"
            description="需要具备托管应用查看（apphosting.apps.view）或发布（apphosting.apps.publish）权限。"
          />
        ) : !denied ? (
          <section className="soha-portal-hosted-surface">
            <header className="soha-portal-hosted-header">
              <div>
                <Title level={4} style={{ margin: 0 }}>
                  我的作品
                </Title>
                <Text type="secondary">
                  管理我创建的托管应用与参与协作维护的作品，无需系统安全后台权限
                </Text>
              </div>
              {canOpenHeya ? (
                <Link to="/heya">
                  <Button type="primary">前往 Heya 个人助手一键发布</Button>
                </Link>
              ) : null}
            </header>

            <Collapse
              size="small"
              items={[
                {
                  key: 'publish-guide',
                  label: '通过 CLI / AI 助手发布指引',
                  children: (
                    <Space orientation="vertical" size={6} style={{ width: '100%' }}>
                      <Paragraph style={{ marginBottom: 0 }}>
                        1. <Text strong>本地命令行或外部 AI 编码工具（Claude Code / Cursor / Codex）</Text>：在构建目录执行{' '}
                        <Text code copyable>
                          soha host deploy ./dist --name &quot;季度看板&quot;
                        </Text>
                        ，自动完成敏感扫描并分配固定 HTTPS 地址。
                      </Paragraph>
                      <Paragraph style={{ marginBottom: 0 }}>
                        2. <Text strong>内置个人助手直接发布</Text>：前往 Heya 个人助手工作空间切换至「发布应用」标签页，或在对话生成的静态成果卡片点击「发布为托管应用」。
                      </Paragraph>
                    </Space>
                  ),
                },
              ]}
            />

            <div className="soha-portal-my-apps-toolbar">
              <Input
                allowClear
                aria-label="搜索我的作品"
                className="soha-portal-colleague-search"
                placeholder="按作品名称或子域标识搜索"
                prefix={<SearchOutlined />}
                value={keyword}
                variant="filled"
                onChange={(event) => setKeyword(event.target.value)}
              />
              <Space wrap size={8}>
                <Select
                  aria-label="在线状态筛选"
                  options={[
                    { label: '全部状态', value: 'all' },
                    { label: '在线', value: 'online' },
                    { label: '已下线', value: 'offline' },
                    { label: '草稿', value: 'draft' },
                    { label: '管理员封停', value: 'suspended_by_admin' },
                  ]}
                  style={{ width: 148 }}
                  value={statusFilter}
                  onChange={(next: StatusFilterValue) => setStatusFilter(next)}
                />
                <Button
                  aria-label="刷新我的作品"
                  icon={<ReloadOutlined />}
                  loading={appsQuery.isFetching}
                  onClick={() => void appsQuery.refetch()}
                />
              </Space>
            </div>

            <AdminTable
              rowKey="id"
              columns={columns}
              dataSource={apps}
              error={appsQuery.error}
              hasReadData={Boolean(appsData)}
              loading={appsQuery.isLoading}
              refreshing={appsQuery.isFetching}
              onRefresh={() => appsQuery.refetch()}
              onRetry={() => appsQuery.refetch()}
              empty={
                hasFilters ? (
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    description="没有符合当前筛选条件的作品"
                  >
                    <Button
                      onClick={() => {
                        setKeyword('')
                        setStatusFilter('all')
                      }}
                    >
                      清空筛选条件
                    </Button>
                  </Empty>
                ) : (
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    description="您还没有创建或参与维护任何托管作品"
                  >
                    <Space orientation="vertical" size={8} align="center">
                      <Text type="secondary">
                        使用命令行一键发布构建目录：
                        <Text code copyable style={{ marginLeft: 4 }}>
                          soha host deploy ./dist --name &quot;季度看板&quot;
                        </Text>
                      </Text>
                      {canOpenHeya ? (
                        <Link to="/heya">
                          <Button type="primary">前往 Heya 个人助手一键发布</Button>
                        </Link>
                      ) : (
                        <Text type="secondary">或前往 Heya 个人助手一键发布工作区代码与成果</Text>
                      )}
                    </Space>
                  </Empty>
                )
              }
            />
          </section>
        ) : null}

        <HostedAppDetailDrawer
          appId={activeAppId}
          initialApp={activeInitialApp}
          currentUserId={currentUserId}
          canPublish={canPublish}
          onClose={() => setActiveAppId(null)}
          onDeleted={() => setActiveAppId(null)}
        />
      </main>
    </div>
  )
}
