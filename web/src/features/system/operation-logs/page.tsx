import { useMemo, useState } from 'react'
import { Card, Descriptions, Drawer, Input, Select, Space, Tabs, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { EyeOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
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
import { systemQueries } from '../queries'
import {
  buildTargetScopeLabel,
  compactText,
  prettifyOperationType,
  stringifyPayload,
  type OperationLog,
} from '../system-model'
import {
  UsageSnapshotDiffView,
  UsageSnapshotPanel,
  UsageSnapshotRawJson,
  UsageSnapshotSummary,
  usageSnapshotFilterParams,
} from '../usage-snapshot'
import '../shared/log-styles.css'

const { Paragraph, Text } = Typography

function OperationLogDrawer({
  record,
  open,
  onClose,
}: {
  record: OperationLog | null
  open: boolean
  onClose: () => void
}) {
  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={record ? prettifyOperationType(record.operationType).primary : '操作详情'}
      size={640}
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
                        key: 'operation',
                        label: '操作',
                        children: (
                          <Space orientation="vertical" size={0}>
                            <Text strong>
                              {prettifyOperationType(record.operationType).primary}
                            </Text>
                            <Text type="secondary">{record.operationType}</Text>
                          </Space>
                        ),
                      },
                      {
                        key: 'target',
                        label: '目标',
                        children: (
                          <Space orientation="vertical" size={0}>
                            <Text strong>
                              {buildTargetScopeLabel(record.targetScope || {}).primary}
                            </Text>
                            <Text type="secondary">
                              {buildTargetScopeLabel(record.targetScope || {}).secondary || '-'}
                            </Text>
                          </Space>
                        ),
                      },
                      {
                        key: 'result',
                        label: '结果',
                        children: <StatusTag value={record.result} />,
                      },
                      { key: 'summary', label: '摘要', children: record.summary || '-' },
                    ]}
                  />
                </Space>
              ),
            },
            {
              key: 'scope',
              label: '目标范围',
              children: (
                <pre className="soha-system-json-block">{stringifyPayload(record.targetScope)}</pre>
              ),
            },
            {
              key: 'request',
              label: '请求上下文',
              children: (
                <Descriptions
                  bordered
                  size="small"
                  column={1}
                  items={[
                    {
                      key: 'route',
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

export function OperationLogsPage() {
  const [params] = useSearchParams()
  const filters = usageSnapshotFilterParams(params)
  return (
    <OperationLogsPageContent
      key={JSON.stringify([filters.metadataKey, filters.metadataValue])}
      initialUsageFilters={filters}
    />
  )
}

function OperationLogsPageContent({
  initialUsageFilters,
}: {
  initialUsageFilters: ReturnType<typeof usageSnapshotFilterParams>
}) {
  const [operationTypeFilter, setOperationTypeFilter] = useState<string>('')
  const [resultFilter, setResultFilter] = useState<string>('')
  const [requestMethodFilter, setRequestMethodFilter] = useState<string>('')
  const [requestPathFilter, setRequestPathFilter] = useState<string>('')
  const [metadataKeyFilter, setMetadataKeyFilter] = useState<string>(
    initialUsageFilters.metadataKey,
  )
  const [metadataValueFilter, setMetadataValueFilter] = useState<string>(
    initialUsageFilters.metadataValue,
  )
  const [moduleView, setModuleView] = useState<
    'all' | 'system' | 'access' | 'platform' | 'virtualization' | 'delivery'
  >('all')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const [activeRecord, setActiveRecord] = useState<OperationLog | null>(null)
  const query = useQuery(
    systemQueries.operationLogs({
      operationType: operationTypeFilter,
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
    if (moduleView === 'all') return logs
    return logs.filter((item) => compactText(String(item.targetScope?.module || '')) === moduleView)
  }, [moduleView, rawLogs])

  const columns: TableColumnsType<OperationLog> = [
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
      render: (_: string, record: OperationLog) => (
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
      title: '操作 / 目标',
      dataIndex: 'operationType',
      width: 250,
      render: (value: string, record: OperationLog) => {
        const pretty = prettifyOperationType(value)
        const target = buildTargetScopeLabel(record.targetScope || {})
        return (
          <div className="soha-log-event-cell soha-log-operation-cell">
            <Space size={8} wrap>
              <Text strong>{pretty.primary}</Text>
              <Text type="secondary">{pretty.secondary}</Text>
            </Space>
            <Space size={8} wrap>
              <Text>{target.primary}</Text>
              {target.secondary ? <Text type="secondary">{target.secondary}</Text> : null}
            </Space>
          </div>
        )
      },
    },
    {
      title: '请求路由',
      dataIndex: 'requestPath',
      width: 280,
      render: (_: string, record: OperationLog) =>
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
      title: '状态',
      dataIndex: 'result',
      width: 90,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: '摘要',
      dataIndex: 'summary',
      width: 220,
      render: (value: string, record: OperationLog) => (
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
      render: (_: string, record: OperationLog) => (
        <ManagementIconButton
          aria-label="查看操作详情"
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
              moduleView === 'all' &&
              !operationTypeFilter.trim() &&
              !requestMethodFilter &&
              !requestPathFilter.trim() &&
              !resultFilter &&
              !metadataKeyFilter &&
              !metadataValueFilter.trim()
            }
            onReset={() => {
              setModuleView('all')
              setOperationTypeFilter('')
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
              label="业务域"
              value={moduleView}
              onChange={(value) =>
                setModuleView(
                  value as 'all' | 'system' | 'access' | 'platform' | 'virtualization' | 'delivery',
                )
              }
              options={[
                { value: 'all', label: '全部' },
                { value: 'system', label: '系统' },
                { value: 'access', label: '访问控制' },
                { value: 'platform', label: '平台' },
                { value: 'virtualization', label: '虚拟化' },
                { value: 'delivery', label: '交付' },
              ]}
            />
            <ManagementQueryField minWidth={180} width={220} label="操作类型">
              <Input
                allowClear
                placeholder="按操作类型过滤"
                value={operationTypeFilter}
                onChange={(event) => setOperationTypeFilter(event.target.value)}
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
                placeholder="完整路由，例如 /api/v1/resources"
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
              aria-label="切换操作日志表格密度"
              size="small"
              tooltip={tableSize === 'small' ? '切换为宽松密度' : '切换为紧凑密度'}
              onClick={() => setTableSize((current) => (current === 'small' ? 'middle' : 'small'))}
            />
            <ManagementRefreshButton
              aria-label="刷新操作日志"
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
        onRow: (record: OperationLog) => ({
          onClick: () => setActiveRecord(record),
          style: { cursor: 'pointer' },
        }),
      }}
      afterTable={
        <OperationLogDrawer
          record={rawLogs?.find((item) => item.id === activeRecord?.id) ?? null}
          open={!!rawLogs?.some((item) => item.id === activeRecord?.id)}
          onClose={() => setActiveRecord(null)}
        />
      }
    />
  )
}
