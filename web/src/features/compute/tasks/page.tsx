import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { useEffect, useMemo, useState } from 'react'
import {
  App,
  Alert,
  Descriptions,
  Drawer,
  Empty,
  Form,
  Input,
  Popconfirm,
  Progress,
  Select,
  Space,
  Spin,
  Typography,
} from 'antd'
import type { TableProps } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { FileTextOutlined, RedoOutlined, StopOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useLocation, useSearchParams } from 'react-router-dom'
import type {
  ComputeTaskCategory,
  ComputeTaskDomain,
  ComputeTaskStatus,
  ComputeTaskView,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementIconButton,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementQueryScope,
  ManagementRefreshButton,
  ManagementTableToolbar,
} from '@/components/management-list'
import { MetadataTag, StatusTag, type MetadataTagTone } from '@/components/status-tag'
import { useAIPageContext } from '@/features/copilot'
import { localeText, useI18n } from '@/i18n'
import { formatStatusLabel } from '@/i18n/status'
import { formatDateTime } from '@/utils/time'
import type { ComputeTaskFilters } from '../api'
import { computeMutations } from '../mutations'
import { computeQueries } from '../queries'
import { ComputeMutationFeedback } from '../feedback'
import { ComputeStreamStatus } from '../stream-status'
import { useComputeTaskStream } from './use-compute-task-stream'
import '../compute.css'

const { Text } = Typography
const DEFAULT_TASK_PAGE_SIZE = 15
const TASK_PAGE_SIZE_OPTIONS = [10, 15, 20, 50, 100]

const TASK_CATEGORY_LABELS: Record<ComputeTaskCategory, [string, string]> = {
  sync: ['同步', 'Sync'],
  build: ['构建', 'Build'],
  lifecycle: ['生命周期', 'Lifecycle'],
  operation: ['操作', 'Operation'],
}

const TASK_CATEGORY_TONES: Record<ComputeTaskCategory, MetadataTagTone> = {
  sync: 'cyan',
  build: 'purple',
  lifecycle: 'blue',
  operation: 'default',
}

const TASK_KIND_LABELS: Record<string, [string, string]> = {
  asset_sync: ['资产同步', 'Asset sync'],
  connection_test: ['连接检查', 'Connection check'],
  container_start: ['启动容器', 'Start container'],
  host_sync: ['主机同步', 'Host sync'],
  host_provision: ['主机创建', 'Host provision'],
  port_reserve: ['预留端口', 'Reserve port'],
  project_deploy: ['项目部署', 'Project deploy'],
  service_action: ['服务操作', 'Service action'],
  vm_action: ['虚拟机操作', 'VM action'],
  vm_create: ['创建虚拟机', 'Create VM'],
}

const VERIFICATION_SUMMARY_LABELS: Record<string, [string, string]> = {
  'resource verification is not available for this task': [
    '该任务暂不支持资源结果验证',
    'Resource verification is not available for this task',
  ],
  'target resource is observable after task completion': [
    '任务完成后目标资源可观测',
    'Target resource is observable after task completion',
  ],
  'task finished but the target resource could not be verified': [
    '任务已结束，但无法验证目标资源',
    'Task finished but the target resource could not be verified',
  ],
  'the source task did not complete successfully': [
    '源任务未成功完成',
    'The source task did not complete successfully',
  ],
  'this task has no verifiable target resource': [
    '该任务没有可验证的目标资源',
    'This task has no verifiable target resource',
  ],
  'waiting for the source task to finish': [
    '等待源任务结束',
    'Waiting for the source task to finish',
  ],
}

function taskKindLabel(kind: string, localeCode: 'zh_CN' | 'en_US') {
  return TASK_KIND_LABELS[kind]?.[localeCode === 'zh_CN' ? 0 : 1] ?? kind
}

function taskCategoryLabel(category: ComputeTaskCategory, localeCode: 'zh_CN' | 'en_US') {
  return TASK_CATEGORY_LABELS[category][localeCode === 'zh_CN' ? 0 : 1]
}

function verificationSummaryLabel(summary: string | undefined, localeCode: 'zh_CN' | 'en_US') {
  if (!summary) return '-'
  return VERIFICATION_SUMMARY_LABELS[summary]?.[localeCode === 'zh_CN' ? 0 : 1] ?? summary
}

export function computeTaskCategoryFromPath(pathname: string): ComputeTaskCategory | undefined {
  if (pathname.endsWith('/sync')) return 'sync'
  if (pathname.endsWith('/build')) return 'build'
  return undefined
}

export function computeTaskFiltersFromLocation(
  pathname: string,
  search: URLSearchParams,
): ComputeTaskFilters {
  return {
    domain: (search.get('domain') as ComputeTaskDomain | null) ?? undefined,
    providerKey: search.get('providerKey') || undefined,
    status: (search.get('status') as ComputeTaskStatus | null) ?? undefined,
    category:
      (search.get('category') as ComputeTaskCategory | null) ??
      computeTaskCategoryFromPath(pathname),
    resourceKind: search.get('resourceKind') || undefined,
    resourceId: search.get('resourceId') || undefined,
    sortBy: (search.get('sortBy') as ComputeTaskFilters['sortBy'] | null) ?? undefined,
    sortOrder: (search.get('sortOrder') as ComputeTaskFilters['sortOrder'] | null) ?? undefined,
    limit: DEFAULT_TASK_PAGE_SIZE,
  }
}

export function computeTaskPaginationTotal(
  currentPage: number,
  pageSize: number,
  itemCount: number,
  hasNextPage: boolean,
) {
  return (currentPage - 1) * pageSize + itemCount + (hasNextPage ? 1 : 0)
}

export function computeTaskCursorForPage(
  nextPage: number,
  cursorHistory: string[],
  currentPage: number,
) {
  if (nextPage < 1 || nextPage >= currentPage) return undefined
  return cursorHistory[nextPage - 1] || undefined
}

export function searchFromTaskFilters(
  filters: ComputeTaskFilters,
  drawer?: { domain: ComputeTaskDomain; taskId: string },
) {
  const params = new URLSearchParams()
  ;(
    [
      'category',
      'resourceKind',
      'resourceId',
      'domain',
      'providerKey',
      'status',
      'sortBy',
      'sortOrder',
    ] as const
  ).forEach((key) => {
    if (filters[key]) params.set(key, String(filters[key]))
  })
  if (drawer) {
    params.set('domain', drawer.domain)
    params.set('taskId', drawer.taskId)
    params.set('view', 'logs')
  }
  return params
}

export function ComputeTasksPage() {
  const { message } = App.useApp()
  const { localeCode } = useI18n()
  const location = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const initialFilters = useMemo(
    () => computeTaskFiltersFromLocation(location.pathname, searchParams),
    [location.pathname, searchParams],
  )
  const [filters, setFilters] = useState<ComputeTaskFilters>(initialFilters)
  const [cursorHistory, setCursorHistory] = useState<string[]>([])
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const [queryExpanded, setQueryExpanded] = useState(false)
  const [form] = Form.useForm<ComputeTaskFilters>()
  const queryClient = useQueryClient()
  const tasksQuery = useQuery(computeQueries.tasks(filters))
  const taskPage = readableQueryData(tasksQuery)
  const items = taskPage?.items ?? []
  const selectedTaskId = searchParams.get('view') === 'logs' ? searchParams.get('taskId') || '' : ''
  const selectedDomain =
    (searchParams.get('domain') as ComputeTaskDomain | null) ?? 'virtualization'
  const taskStream = useComputeTaskStream({
    domain: selectedDomain,
    taskId: selectedTaskId,
    enabled: Boolean(selectedTaskId),
  })
  const taskQuery = useQuery(computeQueries.task(selectedDomain, selectedTaskId))
  const logsQuery = useQuery(computeQueries.taskLogs(selectedDomain, selectedTaskId))
  const cancelMutation = useMutation(computeMutations.cancelTask(queryClient))
  const retryMutation = useMutation(computeMutations.retryTask(queryClient))
  const selectedTask = readableQueryData(taskQuery)
  const logs = readableQueryData(logsQuery)
  const pageSize = filters.limit ?? DEFAULT_TASK_PAGE_SIZE
  const currentPage = cursorHistory.length + 1
  const paginationTotal = computeTaskPaginationTotal(
    currentPage,
    pageSize,
    items.length,
    Boolean(taskPage?.nextCursor),
  )

  useEffect(() => {
    setFilters(initialFilters)
    setCursorHistory([])
    form.setFieldsValue(initialFilters)
  }, [form, initialFilters])

  useAIPageContext({
    sourceWorkbench: 'compute',
    sourceTitle: localeText(localeCode, '计算任务中心', 'Compute task center'),
    entityKind: 'compute.tasks',
    entityName: localeText(localeCode, '计算资源任务', 'Compute tasks'),
    visibleFilters: { ...filters },
    pinnedData: {
      taskCount: items.length,
      taskRefs: items.slice(0, 20).map((item) => ({
        domain: item.domain,
        id: item.id,
        status: item.normalizedStatus,
      })),
      toolRefs: ['compute.tasks.list', 'compute.tasks.get', 'compute.task_logs.list'],
    },
  })

  const updateFilters = (next: ComputeTaskFilters) => {
    const normalized = {
      ...next,
      cursor: undefined,
      limit: next.limit ?? filters.limit ?? DEFAULT_TASK_PAGE_SIZE,
    }
    setCursorHistory([])
    setFilters(normalized)
    setSearchParams(searchFromTaskFilters(normalized), { replace: true })
  }

  const changePage = (nextPage: number) => {
    if (nextPage === currentPage) return
    if (nextPage < currentPage) {
      const cursor = computeTaskCursorForPage(nextPage, cursorHistory, currentPage)
      setCursorHistory((current) => current.slice(0, nextPage - 1))
      setFilters((current) => ({ ...current, cursor }))
      return
    }
    if (nextPage === currentPage + 1 && taskPage?.nextCursor) {
      setCursorHistory((current) => [...current, filters.cursor ?? ''])
      setFilters((current) => ({ ...current, cursor: taskPage.nextCursor }))
    }
  }

  const changePageSize = (nextPageSize: number) => {
    setCursorHistory([])
    setFilters((current) => ({
      ...current,
      cursor: undefined,
      limit: nextPageSize,
    }))
  }

  const openLogs = (task: ComputeTaskView) => {
    setSearchParams(searchFromTaskFilters(filters, { domain: task.domain, taskId: task.id }))
  }

  const closeLogs = () => setSearchParams(searchFromTaskFilters(filters), { replace: true })

  const mutateTask = (action: 'cancel' | 'retry', task: ComputeTaskView) => {
    if (cancelMutation.isPending || retryMutation.isPending) return
    const mutation = action === 'cancel' ? cancelMutation : retryMutation
    mutation.mutate(
      { domain: task.domain, taskId: task.id },
      {
        onSuccess: () =>
          void message.success(
            action === 'cancel'
              ? localeText(localeCode, '取消请求已提交', 'Cancellation requested')
              : localeText(localeCode, '重试请求已提交', 'Retry requested'),
          ),
      },
    )
  }

  const activeSortBy = filters.sortBy ?? 'createdAt'
  const tableSortOrder = (filters.sortOrder ?? 'desc') === 'asc' ? 'ascend' : 'descend'
  const handleTableChange: NonNullable<TableProps<ComputeTaskView>['onChange']> = (
    _pagination,
    _tableFilters,
    sorter,
    extra,
  ) => {
    if (extra.action !== 'sort') return
    const selectedSorter = Array.isArray(sorter) ? sorter[0] : sorter
    if (!selectedSorter?.order || !selectedSorter.field) return
    updateFilters({
      ...filters,
      sortBy: String(selectedSorter.field) as ComputeTaskFilters['sortBy'],
      sortOrder: selectedSorter.order === 'ascend' ? 'asc' : 'desc',
    })
  }

  const columns: ColumnsType<ComputeTaskView> = [
    {
      title: localeText(localeCode, '任务', 'Task'),
      dataIndex: 'kind',
      key: 'kind',
      fixed: 'left',
      sorter: true,
      sortOrder: activeSortBy === 'kind' ? tableSortOrder : null,
      width: 250,
      render: (_value, record) => (
        <Space orientation="vertical" size={0}>
          <Text strong>{taskKindLabel(record.kind, localeCode)}</Text>
          <Text type="secondary">{record.id}</Text>
        </Space>
      ),
    },
    {
      title: localeText(localeCode, '领域', 'Domain'),
      dataIndex: 'domain',
      key: 'domain',
      sorter: true,
      sortOrder: activeSortBy === 'domain' ? tableSortOrder : null,
      width: 145,
      render: (value) => (
        <MetadataTag
          label={
            value === 'container_runtime'
              ? localeText(localeCode, '容器运行时', 'Container runtime')
              : localeText(localeCode, '虚拟化', 'Virtualization')
          }
          tone={value === 'container_runtime' ? 'cyan' : 'blue'}
        />
      ),
    },
    {
      title: localeText(localeCode, '类别', 'Category'),
      dataIndex: 'category',
      width: 110,
      render: (value: ComputeTaskCategory) => (
        <MetadataTag
          label={taskCategoryLabel(value, localeCode)}
          tone={TASK_CATEGORY_TONES[value]}
        />
      ),
    },
    {
      title: localeText(localeCode, '状态', 'Status'),
      dataIndex: 'normalizedStatus',
      key: 'status',
      sorter: true,
      sortOrder: activeSortBy === 'status' ? tableSortOrder : null,
      width: 120,
      render: (value, record) => (
        <Space orientation="vertical" size={2}>
          <StatusTag value={value} />
          {(record.progress ?? 0) > 0 && (record.progress ?? 0) < 1 ? (
            <Progress
              percent={Math.round((record.progress ?? 0) * 100)}
              showInfo={false}
              size={[64, 4]}
            />
          ) : null}
        </Space>
      ),
    },
    {
      title: localeText(localeCode, '关联资源', 'Related resources'),
      width: 240,
      render: (_value, record) =>
        record.resources.map((item) => item.displayName).join(' / ') || '-',
    },
    {
      title: localeText(localeCode, '创建时间', 'Created at'),
      dataIndex: 'createdAt',
      key: 'createdAt',
      sorter: true,
      sortOrder: activeSortBy === 'createdAt' ? tableSortOrder : null,
      width: 170,
      render: formatDateTime,
    },
    {
      title: localeText(localeCode, '摘要', 'Summary'),
      dataIndex: 'summary',
      width: 260,
      render: (value) => value || '-',
    },
    {
      title: localeText(localeCode, '操作', 'Actions'),
      key: 'actions',
      className: 'soha-compute-task-actions-column soha-table-actions-column',
      fixed: 'right',
      width: 96,
      render: (_value, record) => (
        <Space className="soha-row-action-icons" size={4}>
          {record.availableActions.includes('logs') ? (
            <ManagementIconButton
              aria-label={localeText(localeCode, '查看任务日志', 'View task logs')}
              icon={<FileTextOutlined />}
              size="small"
              tooltip={localeText(localeCode, '查看日志', 'View logs')}
              onClick={() => openLogs(record)}
            />
          ) : null}
          {record.availableActions.includes('cancel') ? (
            <Popconfirm
              title={localeText(localeCode, '确认取消任务？', 'Cancel this task?')}
              onConfirm={() => mutateTask('cancel', record)}
            >
              <ManagementIconButton
                aria-label={localeText(localeCode, '取消任务', 'Cancel task')}
                danger
                disabled={cancelMutation.isPending || retryMutation.isPending}
                icon={<StopOutlined />}
                size="small"
                loading={cancelMutation.isPending && cancelMutation.variables?.taskId === record.id}
                tooltip={localeText(localeCode, '取消', 'Cancel')}
              />
            </Popconfirm>
          ) : null}
          {record.availableActions.includes('retry') ? (
            <Popconfirm
              title={localeText(localeCode, '确认重试任务？', 'Retry this task?')}
              onConfirm={() => mutateTask('retry', record)}
            >
              <ManagementIconButton
                aria-label={localeText(localeCode, '重试任务', 'Retry task')}
                disabled={cancelMutation.isPending || retryMutation.isPending}
                icon={<RedoOutlined />}
                size="small"
                loading={retryMutation.isPending && retryMutation.variables?.taskId === record.id}
                tooltip={localeText(localeCode, '重试', 'Retry')}
              />
            </Popconfirm>
          ) : null}
        </Space>
      ),
    },
  ]

  return (
    <>
      <ManagementDataPage
        className="soha-compute-page"
        beforeQuery={
          <>
            <ReadFeedback
              query={tasksQuery}
              label={localeText(localeCode, '任务列表', 'Task list')}
            />
            <ComputeMutationFeedback
              operations={[
                {
                  label: localeText(localeCode, '取消任务', 'Cancel task'),
                  mutation: cancelMutation,
                  target: cancelMutation.variables?.taskId,
                  receipt: {
                    message: localeText(
                      localeCode,
                      '取消请求已提交，请以任务状态为准',
                      'Cancellation requested; check task status',
                    ),
                  },
                },
                {
                  label: localeText(localeCode, '重试任务', 'Retry task'),
                  mutation: retryMutation,
                  target: retryMutation.variables?.taskId,
                  receipt: {
                    message: localeText(
                      localeCode,
                      '重试请求已提交，请以任务状态为准',
                      'Retry requested; check task status',
                    ),
                  },
                },
              ]}
            />
            <ManagementQueryPanel
              form={form}
              initialValues={filters}
              expanded={queryExpanded}
              onExpandedChange={setQueryExpanded}
              onFinish={(values) => updateFilters({ ...filters, ...values })}
              actions={
                <ManagementQueryActions
                  onReset={() => {
                    setQueryExpanded(false)
                    form.resetFields()
                    form.setFieldsValue({
                      domain: undefined,
                      providerKey: undefined,
                      status: undefined,
                      category: undefined,
                    })
                    updateFilters({ limit: DEFAULT_TASK_PAGE_SIZE })
                  }}
                  submitLabel={localeText(localeCode, '筛选', 'Filter')}
                />
              }
            >
              <ManagementQueryScope
                label={localeText(localeCode, '任务领域', 'Task domain')}
                options={[
                  { label: localeText(localeCode, '全部', 'All'), value: 'all' },
                  {
                    label: localeText(localeCode, '虚拟化', 'Virtualization'),
                    value: 'virtualization',
                  },
                  {
                    label: localeText(localeCode, '容器运行时', 'Container runtime'),
                    value: 'container_runtime',
                  },
                ]}
                value={filters.domain ?? 'all'}
                onChange={(value) => {
                  const domain = value === 'all' ? undefined : (value as ComputeTaskDomain)
                  form.setFieldValue('domain', domain)
                  updateFilters({ ...filters, domain })
                }}
              />
              <Form.Item name="domain" hidden>
                <Input />
              </Form.Item>
              <ManagementQueryField label={localeText(localeCode, '状态', 'Status')} name="status">
                <Select
                  allowClear
                  placeholder={localeText(localeCode, '全部状态', 'All statuses')}
                  options={[
                    'queued',
                    'running',
                    'succeeded',
                    'failed',
                    'canceled',
                    'timeout',
                    'unknown',
                  ].map((value) => ({ value, label: formatStatusLabel(value, localeCode) }))}
                />
              </ManagementQueryField>
              <ManagementQueryField
                label={localeText(localeCode, '提供方', 'Provider')}
                name="providerKey"
              >
                <Input
                  allowClear
                  placeholder={localeText(localeCode, '提供方标识', 'Provider key')}
                />
              </ManagementQueryField>
              <ManagementQueryField
                label={localeText(localeCode, '类别', 'Category')}
                name="category"
              >
                <Select
                  allowClear
                  placeholder={localeText(localeCode, '全部类别', 'All categories')}
                  options={Object.keys(TASK_CATEGORY_LABELS).map((value) => ({
                    value,
                    label: taskCategoryLabel(value as ComputeTaskCategory, localeCode),
                  }))}
                />
              </ManagementQueryField>
            </ManagementQueryPanel>
          </>
        }
        table={{
          rowKey: (record: ComputeTaskView) => `${record.domain}:${record.id}`,
          columns,
          dataSource: items,
          loading: tasksQuery.isLoading,
          empty: tasksQuery.isError
            ? localeText(localeCode, '任务列表加载失败', 'Failed to load tasks')
            : localeText(localeCode, '暂无匹配任务', 'No matching tasks'),
          columnSettingIconOnly: true,
          columnSettingPlacement: 'header',
          headerExtra: (
            <ManagementTableToolbar>
              <ManagementDensityButton
                aria-label={localeText(localeCode, '切换表格密度', 'Toggle table density')}
                size="small"
                tooltip={
                  tableSize === 'small'
                    ? localeText(localeCode, '切换为宽松密度', 'Use relaxed density')
                    : localeText(localeCode, '切换为紧凑密度', 'Use compact density')
                }
                onClick={() =>
                  setTableSize((current) => (current === 'small' ? 'middle' : 'small'))
                }
              />
              {selectedTaskId ? (
                <ComputeStreamStatus
                  status={taskStream.status}
                  observedAt={taskStream.lastEventAt}
                  localeCode={localeCode}
                />
              ) : null}
              <ManagementRefreshButton
                aria-label={localeText(localeCode, '刷新任务列表', 'Refresh task list')}
                loading={tasksQuery.isFetching}
                size="small"
                tooltip={localeText(localeCode, '刷新', 'Refresh')}
                onClick={() => void tasksQuery.refetch()}
              />
            </ManagementTableToolbar>
          ),
          pageSize,
          pagination: {
            current: currentPage,
            currentPage,
            pageSize,
            pageSizeOptions: TASK_PAGE_SIZE_OPTIONS,
            total: paginationTotal,
            onPageChange: changePage,
            onPageSizeChange: changePageSize,
          },
          paginationSummary: (
            <Text type="secondary">
              {localeText(
                localeCode,
                `当前第 ${currentPage} 页，本页 ${items.length} 条${taskPage?.nextCursor ? '，还有更多' : ''}`,
                `Page ${currentPage}, ${items.length} items${taskPage?.nextCursor ? ', more available' : ''}`,
              )}
            </Text>
          ),
          onChange: handleTableChange,
          scroll: { x: 1391 },
          tableSize,
          viewportScroll: true,
        }}
      />
      <Drawer
        title={localeText(localeCode, '任务日志', 'Task logs')}
        size="large"
        open={Boolean(selectedTaskId)}
        destroyOnHidden
        styles={{ wrapper: { maxWidth: 'calc(100vw - 24px)' } }}
        extra={
          selectedTaskId ? (
            <ComputeStreamStatus
              status={taskStream.status}
              observedAt={taskStream.lastEventAt}
              localeCode={localeCode}
            />
          ) : null
        }
        onClose={closeLogs}
      >
        <ReadFeedback
          query={taskQuery}
          label={localeText(localeCode, '任务详情', 'Task details')}
        />
        <ReadFeedback query={logsQuery} label={localeText(localeCode, '任务日志', 'Task logs')} />
        {selectedTask ? (
          <Space orientation="vertical" size={16} style={{ width: '100%' }}>
            {selectedTask.failure ? (
              <Alert
                showIcon
                type="error"
                title={selectedTask.failure.message || selectedTask.failure.code}
              />
            ) : null}
            <Descriptions size="small" column={1} bordered>
              <Descriptions.Item label={localeText(localeCode, '任务', 'Task')}>
                {taskKindLabel(selectedTask.kind, localeCode)}
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '状态', 'Status')}>
                <StatusTag value={selectedTask.normalizedStatus} />
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '任务 ID', 'Task ID')}>
                <Text copyable>{selectedTask.id}</Text>
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '领域', 'Domain')}>
                {selectedTask.domain === 'container_runtime'
                  ? localeText(localeCode, '容器运行时', 'Container runtime')
                  : localeText(localeCode, '虚拟化', 'Virtualization')}
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '创建时间', 'Created at')}>
                {formatDateTime(selectedTask.createdAt)}
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '提供方', 'Provider')}>
                {[selectedTask.providerKey, selectedTask.providerSource]
                  .filter(Boolean)
                  .join(' / ') || '-'}
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '发起人', 'Requested by')}>
                {selectedTask.requestedBy || '-'}
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '尝试次数', 'Attempts')}>
                {selectedTask.attemptCount ?? '-'}
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '结束时间', 'Finished at')}>
                {formatDateTime(selectedTask.finishedAt)}
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '摘要', 'Summary')}>
                {selectedTask.summary || '-'}
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '心跳', 'Heartbeat')}>
                <StatusTag value={selectedTask.heartbeat?.status || 'unknown'} />
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '结果验证', 'Result verification')}>
                <StatusTag value={selectedTask.verification?.status || 'unknown'} />
              </Descriptions.Item>
              <Descriptions.Item label={localeText(localeCode, '验证说明', 'Verification details')}>
                {verificationSummaryLabel(selectedTask.verification?.summary, localeCode)}
              </Descriptions.Item>
            </Descriptions>
            {logsQuery.isLoading ? (
              <Spin
                description={localeText(localeCode, '正在加载任务日志...', 'Loading task logs...')}
              />
            ) : logs?.length ? (
              <div className="soha-compute-task-log-list">
                {logs.map((log) => (
                  <div className="soha-compute-task-log-row" key={log.id}>
                    <Space orientation="vertical" size={2} style={{ width: '100%' }}>
                      <Space wrap>
                        <StatusTag value={log.logLevel} />
                        <Text type="secondary">{formatDateTime(log.createdAt)}</Text>
                      </Space>
                      <Text>{log.message}</Text>
                      {log.payload ? <Text code>{log.payload}</Text> : null}
                    </Space>
                  </div>
                ))}
              </div>
            ) : (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={
                  logsQuery.isError
                    ? localeText(localeCode, '日志读取失败', 'Logs unavailable')
                    : localeText(localeCode, '暂无日志', 'No logs')
                }
              />
            )}
          </Space>
        ) : taskQuery.isLoading ? (
          <Spin />
        ) : taskQuery.isError ? null : (
          <Empty
            description={localeText(
              localeCode,
              '任务不存在或不可访问',
              'Task not found or unavailable',
            )}
          />
        )}
      </Drawer>
    </>
  )
}
