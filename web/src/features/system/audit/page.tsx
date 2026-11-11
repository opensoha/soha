import { useMemo, useState } from 'react'
import { Card, Descriptions, Drawer, Input, Select, Space, Tabs, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { EyeOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useLocation, useSearchParams } from 'react-router-dom'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementIconButton,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementQueryScope,
  ManagementRefreshButton,
  ManagementTableToolbar,
} from '@/components/management-list'
import { readableQueryData } from '@/utils/query-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { resolveSystemEndpointScope } from '../api'
import { systemQueries } from '../queries'
import {
  buildAuditResourceLabel,
  isTodayDate,
  prettifyAction,
  type AuditLog,
} from '../system-model'
import {
  UsageSnapshotDiffView,
  UsageSnapshotPanel,
  UsageSnapshotRawJson,
  UsageSnapshotSummary,
  usageSnapshotFilterParams,
} from '../usage-snapshot'
import '../shared/log-styles.css'

const { Paragraph, Text, Title } = Typography

function AuditLogDrawer({
  record,
  open,
  onClose,
}: {
  record: AuditLog | null
  open: boolean
  onClose: () => void
}) {
  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={record ? `审计记录 · ${prettifyAction(record.action)}` : '审计记录'}
      size={620}
      destroyOnHidden
    >
      {record ? (
        <Tabs
          items={[
            {
              key: 'overview',
              label: '概览',
              children: (
                <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                  <UsageSnapshotPanel metadata={record.metadata} />
                  <Descriptions
                    bordered
                    size="small"
                    column={1}
                    items={[
                      {
                        key: 'time',
                        label: '发生时间',
                        children: formatDateTime(record.createdAt),
                      },
                      {
                        key: 'actor',
                        label: '操作者',
                        children: record.actorName || record.actorId || '-',
                      },
                      {
                        key: 'action',
                        label: '动作',
                        children: (
                          <Space size={8} wrap>
                            <Text>{prettifyAction(record.action)}</Text>
                            <MetadataTag label={record.action} />
                          </Space>
                        ),
                      },
                      {
                        key: 'resource',
                        label: '资源',
                        children:
                          [record.resourceKind, record.resourceName].filter(Boolean).join(' / ') ||
                          '-',
                      },
                      {
                        key: 'result',
                        label: '结果',
                        children: <StatusTag value={record.result} />,
                      },
                      { key: 'summary', label: '摘要', children: record.summary || '-' },
                    ]}
                  />
                  <Card variant="outlined" className="soha-system-payload-card">
                    <Title level={5} style={{ marginTop: 0 }}>
                      访问上下文
                    </Title>
                    <Descriptions
                      size="small"
                      column={1}
                      items={[
                        {
                          key: 'roles',
                          label: '角色',
                          children: record.roles?.length ? record.roles.join(', ') : '-',
                        },
                        {
                          key: 'teams',
                          label: '团队',
                          children: record.teams?.length ? record.teams.join(', ') : '-',
                        },
                        {
                          key: 'requestRoute',
                          label: '请求路由',
                          children: record.requestPath ? (
                            <Space size={8} wrap>
                              {record.requestMethod ? (
                                <MetadataTag label={record.requestMethod} tone="blue" />
                              ) : null}
                              <Text code>{record.requestPath}</Text>
                            </Space>
                          ) : (
                            '-'
                          ),
                        },
                        { key: 'requestId', label: '请求 ID', children: record.requestId || '-' },
                        { key: 'sourceIp', label: '来源 IP', children: record.sourceIp || '-' },
                      ]}
                    />
                  </Card>
                </Space>
              ),
            },
            {
              key: 'diff',
              label: '结构化 diff',
              children: (
                <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                  <UsageSnapshotDiffView metadata={record.metadata} />
                  <Card variant="outlined" className="soha-system-payload-card">
                    <Typography.Title level={5} style={{ marginTop: 0 }}>
                      原始 JSON
                    </Typography.Title>
                    <UsageSnapshotRawJson metadata={record.metadata} />
                  </Card>
                </Space>
              ),
            },
          ]}
        />
      ) : null}
    </Drawer>
  )
}

export function AuditLogsPage() {
  const [params] = useSearchParams()
  const filters = usageSnapshotFilterParams(params)
  return (
    <AuditLogsPageContent
      key={JSON.stringify([filters.metadataKey, filters.metadataValue])}
      initialUsageFilters={filters}
    />
  )
}

function AuditLogsPageContent({
  initialUsageFilters,
}: {
  initialUsageFilters: ReturnType<typeof usageSnapshotFilterParams>
}) {
  const location = useLocation()
  const endpointScope = resolveSystemEndpointScope(location.pathname)
  const [actionFilter, setActionFilter] = useState<string>('')
  const [resultFilter, setResultFilter] = useState<string>('')
  const [requestMethodFilter, setRequestMethodFilter] = useState<string>('')
  const [requestPathFilter, setRequestPathFilter] = useState<string>('')
  const [metadataKeyFilter, setMetadataKeyFilter] = useState<string>(
    initialUsageFilters.metadataKey,
  )
  const [metadataValueFilter, setMetadataValueFilter] = useState<string>(
    initialUsageFilters.metadataValue,
  )
  const [viewMode, setViewMode] = useState<'all' | 'abnormal' | 'today'>('all')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const [activeRecord, setActiveRecord] = useState<AuditLog | null>(null)
  const query = useQuery(
    systemQueries.audit(endpointScope, {
      action: actionFilter,
      requestMethod: requestMethodFilter,
      requestPath: requestPathFilter,
      result: resultFilter,
      metadataKey: metadataKeyFilter,
      metadataValue: metadataValueFilter,
    }),
  )
  const { isFetching, isLoading, refetch } = query
  const rawLogs = readableQueryData(query)
  const filteredLogs = useMemo(() => {
    const logs = rawLogs ?? []
    if (viewMode === 'abnormal') {
      return logs.filter((item) => !['success', 'published'].includes(item.result))
    }
    if (viewMode === 'today') {
      return logs.filter((item) => isTodayDate(item.createdAt))
    }
    return logs
  }, [rawLogs, viewMode])

  const columns: TableColumnsType<AuditLog> = [
    {
      ...tableColumnPresets.datetime,
      title: '时间',
      dataIndex: 'createdAt',
      width: 150,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '操作者',
      dataIndex: 'actorName',
      width: 170,
      render: (_: string, record: AuditLog) => (
        <Space className="soha-log-actor-cell" orientation="vertical" size={0}>
          <Text strong>{record.actorName || record.actorId || '-'}</Text>
          {record.actorId && record.actorId !== record.actorName ? (
            <Text
              className="soha-log-actor-id"
              type="secondary"
              ellipsis={{ tooltip: record.actorId }}
            >
              {record.actorId}
            </Text>
          ) : null}
        </Space>
      ),
    },
    {
      title: '事件 / 资源',
      dataIndex: 'action',
      width: 220,
      render: (_: string, record: AuditLog) => {
        const resource = buildAuditResourceLabel(record.resourceKind, record.resourceName)
        return (
          <div className="soha-log-event-cell">
            <Space size={8} wrap>
              <Text strong>{prettifyAction(record.action)}</Text>
              <Text type="secondary">{record.action}</Text>
            </Space>
            <Space size={8} wrap>
              <Text>{resource.primary}</Text>
              {resource.secondary ? <Text type="secondary">{resource.secondary}</Text> : null}
            </Space>
          </div>
        )
      },
    },
    {
      title: '请求路由',
      dataIndex: 'requestPath',
      width: 280,
      render: (_: string, record: AuditLog) =>
        record.requestPath ? (
          <div className="soha-log-request-cell">
            {record.requestMethod ? <MetadataTag label={record.requestMethod} tone="blue" /> : null}
            <Text className="soha-log-request-path" ellipsis={{ tooltip: record.requestPath }}>
              {record.requestPath}
            </Text>
          </div>
        ) : (
          '-'
        ),
    },
    {
      ...tableColumnPresets.status,
      title: '结果',
      dataIndex: 'result',
      width: 90,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: '摘要',
      dataIndex: 'summary',
      width: 220,
      render: (value: string, record: AuditLog) => (
        <Space className="soha-log-summary-cell" orientation="vertical" size={4}>
          <Paragraph className="soha-log-summary" ellipsis={{ rows: 2, tooltip: value }}>
            {value || '-'}
          </Paragraph>
          <UsageSnapshotSummary metadata={record.metadata} />
        </Space>
      ),
    },
    {
      ...tableColumnPresets.action,
      title: '详情',
      dataIndex: 'id',
      width: 64,
      render: (_: string, record: AuditLog) => (
        <ManagementIconButton
          aria-label="查看审计详情"
          icon={<EyeOutlined />}
          size="small"
          tooltip="详情"
          onClick={() => setActiveRecord(record)}
        />
      ),
    },
  ]

  return (
    <ManagementDataPage
      query={{
        collapsible: true,
        onFinish: () => undefined,
        actions: (
          <ManagementQueryActions
            disabledReset={
              viewMode === 'all' &&
              !actionFilter &&
              !requestMethodFilter &&
              !requestPathFilter.trim() &&
              !resultFilter &&
              !metadataKeyFilter &&
              !metadataValueFilter.trim()
            }
            onReset={() => {
              setViewMode('all')
              setActionFilter('')
              setRequestMethodFilter('')
              setRequestPathFilter('')
              setResultFilter('')
              setMetadataKeyFilter('')
              setMetadataValueFilter('')
            }}
          />
        ),
        children: (
          <>
            <ManagementQueryScope
              label="快捷范围"
              value={viewMode}
              onChange={(value) => setViewMode(value as 'all' | 'abnormal' | 'today')}
              options={[
                { value: 'all', label: '全部' },
                { value: 'abnormal', label: '异常 / 拒绝' },
                { value: 'today', label: '今日' },
              ]}
            />
            <ManagementQueryField minWidth={140} width={160} label="动作">
              <Input
                allowClear
                placeholder="按动作过滤"
                value={actionFilter}
                onChange={(event) => setActionFilter(event.target.value)}
              />
            </ManagementQueryField>
            <ManagementQueryField minWidth={120} width={140} label="请求方法">
              <Select
                allowClear
                placeholder="全部方法"
                value={requestMethodFilter || undefined}
                onChange={(value) => setRequestMethodFilter(value || '')}
                options={['GET', 'POST', 'PUT', 'PATCH', 'DELETE'].map((value) => ({
                  value,
                  label: value,
                }))}
              />
            </ManagementQueryField>
            <ManagementQueryField grow minWidth={220} width={300} label="请求路由">
              <Input
                allowClear
                placeholder="完整路由，例如 /api/v1/pods"
                value={requestPathFilter}
                onChange={(event) => setRequestPathFilter(event.target.value)}
              />
            </ManagementQueryField>
            <ManagementQueryField minWidth={140} width={160} label="结果">
              <Select
                allowClear
                placeholder="全部结果"
                value={resultFilter || undefined}
                onChange={(value) => setResultFilter(value || '')}
                options={[
                  { value: 'success', label: 'success' },
                  { value: 'failure', label: 'failure' },
                  { value: 'deny', label: 'deny' },
                ]}
              />
            </ManagementQueryField>
            <ManagementQueryField minWidth={180} width={220} label="字段">
              <Select
                allowClear
                placeholder="元数据 字段"
                value={metadataKeyFilter || undefined}
                onChange={(value) => setMetadataKeyFilter(value || '')}
                options={[
                  { value: 'usageSnapshot.templateKind', label: 'templateKind' },
                  { value: 'usageSnapshot.templateId', label: 'templateId' },
                  { value: 'usageSnapshot.riskLevel', label: 'riskLevel' },
                  { value: 'usageSnapshot.before.templateId', label: 'before.templateId' },
                  { value: 'usageSnapshot.after.templateId', label: 'after.templateId' },
                  { value: 'usageSnapshot.after.riskLevel', label: 'after.riskLevel' },
                ]}
              />
            </ManagementQueryField>
            <ManagementQueryField grow minWidth={180} width={220} label="字段值">
              <Input
                allowClear
                placeholder="元数据 值"
                value={metadataValueFilter}
                onChange={(event) => setMetadataValueFilter(event.target.value)}
              />
            </ManagementQueryField>
          </>
        ),
      }}
      table={{
        columnSettingIconOnly: true,
        columnSettingPlacement: 'header',
        headerExtra: (
          <ManagementTableToolbar>
            <ManagementDensityButton
              aria-label="切换审计日志表格密度"
              size="small"
              tooltip={tableSize === 'small' ? '切换为宽松密度' : '切换为紧凑密度'}
              onClick={() => setTableSize((current) => (current === 'small' ? 'middle' : 'small'))}
            />
            <ManagementRefreshButton
              aria-label="刷新审计日志"
              loading={isFetching}
              size="small"
              tooltip="刷新"
              onClick={() => void refetch()}
            />
          </ManagementTableToolbar>
        ),
        columns,
        dataSource: filteredLogs,
        hasReadData: rawLogs !== undefined,
        error: query.error,
        onRetry: () => void refetch(),
        refreshing: isFetching,
        rowKey: 'id',
        loading: isLoading,
        pageSize: 50,
        scroll: { x: 'max-content' },
        tableSize,
        onRow: (record: AuditLog) => ({
          onClick: () => setActiveRecord(record),
          style: { cursor: 'pointer' },
        }),
      }}
      afterTable={
        <AuditLogDrawer
          record={rawLogs?.find((item) => item.id === activeRecord?.id) ?? null}
          open={!!rawLogs?.some((item) => item.id === activeRecord?.id)}
          onClose={() => setActiveRecord(null)}
        />
      }
    />
  )
}
