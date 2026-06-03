import { lazy, Suspense, useState, useEffect, useMemo } from 'react'
import type { ReactNode } from 'react'
import {
  App, Tag, Button, Select, Tabs, Card, Spin, Input, Statistic, List,
  Descriptions, Typography, Space, Modal, Popconfirm, InputNumber, Switch, Tooltip, message,
} from 'antd'
import {
  AppstoreOutlined,
  CheckCircleOutlined,
  ClusterOutlined,
  ClockCircleOutlined,
  DeleteOutlined,
  DownOutlined,
  EditOutlined,
  ReloadOutlined,
  ScheduleOutlined,
  SearchOutlined,
  UndoOutlined,
  UpOutlined,
} from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementDensityButton,
  ManagementIconButton,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { useResourceActions } from '@/components/resource-actions'
import { TABLE_ACTIONS_COLUMN_CLASS_NAME } from '@/components/resource-actions'
import { hasAllowedAction } from '@/features/auth/permission-snapshot'
import { useI18n } from '@/i18n'
import { ResourceEventsTimeline } from '@/components/resource-events-timeline'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { ResourceMetricsPanel } from '@/components/resource-metrics-panel'
import { api } from '@/services/api-client'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds, formatDateTime, formatRelativeTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import {
  buildRelatedResourcePath,
  buildVolumeDetailPath,
  buildWorkloadDetailPath,
  compareStrings,
  conditionToTimelineEvent,
  formatContainerStateLabel,
  formatCpuDisplay,
  formatMemoryDisplay,
  formatRefreshTimestamp,
  formatVolumeTypeLabel,
  getDeploymentHealth,
  includesSearch,
  localizeRelatedRelation,
  localizeRelatedResourceKind,
  normalizeSearchKeyword,
  parseCpuValue,
  parseMemoryValue,
  parseReadyContainers,
  podSorter,
  resolveWorkloadNamespace,
  selectorMatchesLabels,
  targetMatchesDeployment,
} from './workloads-model'
import type {
  ApplicationEnvironment,
  ApplicationSummary,
  BatchRollbackDraft,
  BuildRecord,
  CronJob,
  DaemonSet,
  Deployment,
  DeploymentDetailMeta,
  Job,
  Pod,
  ReleaseRecord,
  StatefulSet,
  WorkflowRecord,
  WorkloadOverviewEvent,
} from './workloads-model'
import type { ApiResponse, DeploymentRolloutStatus, PodDetail, PodMetrics, PodRelatedResource, PodVolume, PodVolumeMount, ResourceMetrics, ResourceYAMLView, RolloutHistory, WorkloadCondition, WorkloadContainer } from '@/types'
import type { TableColumnsType, TabsProps } from 'antd'
import './platform-pages.css'

const { Link, Text } = Typography
const DEPLOYMENT_ACTIONS_COLUMN_CLASS_NAME = `${TABLE_ACTIONS_COLUMN_CLASS_NAME} soha-deployment-actions-column`

const K8sYamlEditor = lazy(async () => {
  const mod = await import('@/components/k8s-yaml-editor')
  return { default: mod.K8sYamlEditor }
})

const PodLogViewer = lazy(async () => {
  const mod = await import('@/components/pod-log-viewer')
  return { default: mod.PodLogViewer }
})

const PodTerminal = lazy(async () => {
  const mod = await import('@/components/pod-terminal')
  return { default: mod.PodTerminal }
})

/* ─── shared helpers ─── */



function renderDetailTagList(values: string[] | undefined, emptyLabel = '-') {
  if (!values || values.length === 0) {
    return <Text type="secondary">{emptyLabel}</Text>
  }
  return (
    <Space size={[6, 6]} wrap>
      {values.map((item) => (
        <Tag key={item}>{item}</Tag>
      ))}
    </Space>
  )
}

function renderVolumeMounts(mounts: PodVolumeMount[] | undefined) {
  if (!mounts || mounts.length === 0) {
    return <Text type="secondary">N/A</Text>
  }
  return (
    <div className="soha-volume-mount-list">
      {mounts.map((mount) => (
        <div key={`${mount.name}:${mount.mountPath}:${mount.subPath || '-'}`} className="soha-volume-mount-item">
          <Text className="soha-volume-mount-name">{mount.name}</Text>
          <Text type="secondary" className="soha-volume-mount-path">
            {mount.subPath ? `${mount.mountPath} (${mount.subPath})` : mount.mountPath}
          </Text>
          {mount.readOnly ? <Tag className="soha-volume-mount-badge">RO</Tag> : null}
        </div>
      ))}
    </div>
  )
}



function summarizeVolumeDetail(volume: PodVolume, localeCode: 'zh_CN' | 'en_US') {
  if (volume.sourceName && ['ConfigMap', 'Secret', 'PersistentVolumeClaim'].includes(volume.type)) {
    return volume.sourceName
  }
  const preferredDetail = (volume.details ?? []).find((item) =>
    item.startsWith('Sources:') || item.startsWith('Medium:') || item.startsWith('SizeLimit:'),
  ) ?? volume.details?.[0]
  return preferredDetail || (localeCode === 'zh_CN' ? '无' : 'N/A')
}

function renderVolumeDetail(
  volume: PodVolume,
  localeCode: 'zh_CN' | 'en_US',
  detailNamespace: string | null,
  navigate: ReturnType<typeof useNavigate>,
) {
  const targetPath = buildVolumeDetailPath(volume, detailNamespace)
  const summary = summarizeVolumeDetail(volume, localeCode)
  if (!targetPath) {
    return <Text type="secondary" className="soha-volume-detail-value">{summary}</Text>
  }
  return (
    <Link className="soha-volume-detail-link" onClick={() => navigate(targetPath)}>
      {summary}
    </Link>
  )
}



function useScopedQuery<T>(resource: string, extra?: string) {
  const { clusterId, namespace } = usePlatformScopeStore()
  if (!clusterId) {
    return useQuery({
      queryKey: [resource, clusterId, namespace, extra],
      queryFn: () => Promise.resolve({ data: [] as T[] }),
      enabled: false,
    })
  }

  return useQuery({
    queryKey: [resource, clusterId, namespace, extra],
    queryFn: () => api.get<ApiResponse<T[]>>(buildClusterScopedPath(clusterId, `workloads/${resource}${extra ?? ''}`, namespace)),
    enabled: !!clusterId,
  })
}



function WorkloadRefreshButton({
  disabled,
  label,
  loading,
  onRefresh,
}: {
  disabled?: boolean
  label: string
  loading?: boolean
  onRefresh: () => void
}) {
  return (
    <ManagementRefreshButton
      aria-label={label}
      disabled={disabled}
      loading={loading}
      tooltip={label}
      onClick={onRefresh}
    />
  )
}

function WorkloadSearchInput({
  onChange,
  placeholder,
  value,
  width = '100%',
}: {
  onChange: (value: string) => void
  placeholder: string
  value: string
  width?: number | string
}) {
  return (
    <Input
      allowClear
      className="soha-platform-compact-field soha-workload-search-input"
      prefix={<SearchOutlined />}
      size="small"
      value={value}
      variant="filled"
      onChange={(event) => onChange(event.target.value)}
      placeholder={placeholder}
      style={{ width }}
    />
  )
}

function WorkloadQueryPanel({
  children,
  expandable = false,
  expanded = false,
  hasActiveFilters,
  localeCode,
  onExpandedChange,
  onReset,
}: {
  children: ReactNode
  expandable?: boolean
  expanded?: boolean
  hasActiveFilters: boolean
  localeCode: 'zh_CN' | 'en_US'
  onExpandedChange?: (expanded: boolean) => void
  onReset: () => void
}) {
  return (
    <ManagementQueryPanel
      expanded={expanded}
      onFinish={() => undefined}
      actions={(
        <>
          <Button autoInsertSpace={false} disabled={!hasActiveFilters} htmlType="button" onClick={onReset}>
            {localeCode === 'zh_CN' ? '重置' : 'Reset'}
          </Button>
          <Button autoInsertSpace={false} htmlType="submit" type="primary">
            {localeCode === 'zh_CN' ? '查询' : 'Search'}
          </Button>
          {expandable ? (
            <Button
              autoInsertSpace={false}
              htmlType="button"
              icon={expanded ? <UpOutlined /> : <DownOutlined />}
              iconPlacement="end"
              type="link"
              onClick={() => onExpandedChange?.(!expanded)}
            >
              {expanded
                ? (localeCode === 'zh_CN' ? '收起' : 'Collapse')
                : (localeCode === 'zh_CN' ? '展开' : 'Expand')}
            </Button>
          ) : null}
        </>
      )}
    >
      {children}
    </ManagementQueryPanel>
  )
}

function WorkloadTableSummary({
  filteredCount,
  localeCode,
  totalCount,
}: {
  filteredCount: number
  localeCode: 'zh_CN' | 'en_US'
  totalCount: number
}) {
  return (
    <Text className="soha-workload-table-summary" type="secondary">
      {localeCode === 'zh_CN' ? `当前 ${filteredCount} / ${totalCount} 条` : `${filteredCount} / ${totalCount} items`}
    </Text>
  )
}

function WorkloadTableEmpty({
  clusterId,
  filteredCount,
  localeCode,
  resourceLabel,
  totalCount,
}: {
  clusterId?: string | null
  filteredCount: number
  localeCode: 'zh_CN' | 'en_US'
  resourceLabel: string
  totalCount: number
}) {
  const hasFilterMiss = totalCount > 0 && filteredCount === 0
  const title = !clusterId
    ? (localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster')
    : hasFilterMiss
      ? (localeCode === 'zh_CN' ? `没有匹配的 ${resourceLabel}` : `No matching ${resourceLabel}`)
      : (localeCode === 'zh_CN' ? `当前范围没有 ${resourceLabel}` : `No ${resourceLabel} in the current scope`)
  const description = !clusterId
    ? (localeCode === 'zh_CN' ? '在顶部作用域选择集群后查看工作负载资源。' : 'Select a cluster in the header scope controls to inspect workload resources.')
    : hasFilterMiss
      ? (localeCode === 'zh_CN' ? '调整搜索或筛选条件后重试。' : 'Adjust search or filters and try again.')
      : (localeCode === 'zh_CN' ? '当前集群和命名空间范围内没有可展示的记录。' : 'No records are available for the selected cluster and namespace scope.')

  return (
    <ManagementState
      bordered={false}
      compact
      description={description}
      kind={!clusterId ? 'select-scope' : 'empty'}
      title={title}
    />
  )
}

function useWorkloadTableDensity(localeCode: 'zh_CN' | 'en_US') {
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'
  const densityButton = (
    <ManagementDensityButton
      aria-label={densityLabel}
      title={densityLabel}
      tooltip={densityLabel}
      onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
    />
  )

  return { densityButton, tableSize }
}

function renderWorkloadNameLink(name: string, onClick: () => void) {
  return (
    <Tooltip title={name} placement="topLeft">
      <Link className="soha-workload-name-link" onClick={onClick}>
        {name}
      </Link>
    </Tooltip>
  )
}



export function WorkloadsOverviewPage() {
  const { t, localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const deploymentsQuery = useScopedQuery<Deployment>('deployments')
  const podsQuery = useScopedQuery<Pod>('pods')
  const statefulSetsQuery = useScopedQuery<StatefulSet>('statefulsets')
  const daemonSetsQuery = useScopedQuery<DaemonSet>('daemonsets')
  const jobsQuery = useScopedQuery<Job>('jobs')
  const cronJobsQuery = useScopedQuery<CronJob>('cronjobs')

  const eventsQuery = useQuery({
    queryKey: ['workload-overview-events', clusterId, namespace],
    queryFn: () =>
      api.get<ApiResponse<WorkloadOverviewEvent[]>>(
        buildClusterScopedPath(clusterId!, 'events', namespace, { limit: 200 }),
      ),
    enabled: !!clusterId,
  })
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)

  if (!clusterId) {
    return (
      <div className="soha-page soha-overview-page soha-workloads-overview-page">
        <ManagementState compact kind="select-scope" title={t('common.pleaseSelectClusterShort', 'Select a cluster')} />
      </div>
    )
  }

  const stats = [
    {
      key: 'deployments',
      label: 'Deployments',
      helper: localeCode === 'zh_CN' ? '无状态应用副本控制面' : 'Stateless application controllers',
      value: deploymentsQuery.data?.data?.length ?? 0,
      icon: <AppstoreOutlined />,
      tone: 'default',
    },
    {
      key: 'pods',
      label: 'Pods',
      helper: localeCode === 'zh_CN' ? '当前范围内运行实例' : 'Runtime instances in scope',
      value: podsQuery.data?.data?.length ?? 0,
      icon: <ClusterOutlined />,
      tone: 'success',
    },
    {
      key: 'statefulsets',
      label: 'StatefulSets',
      helper: localeCode === 'zh_CN' ? '有状态服务控制面' : 'Stateful service controllers',
      value: statefulSetsQuery.data?.data?.length ?? 0,
      icon: <CheckCircleOutlined />,
      tone: 'default',
    },
    {
      key: 'daemonsets',
      label: 'DaemonSets',
      helper: localeCode === 'zh_CN' ? '节点级守护进程' : 'Node-level daemon workloads',
      value: daemonSetsQuery.data?.data?.length ?? 0,
      icon: <ClusterOutlined />,
      tone: 'default',
    },
    {
      key: 'jobs',
      label: 'Jobs',
      helper: localeCode === 'zh_CN' ? '一次性任务资源' : 'One-off workload runs',
      value: jobsQuery.data?.data?.length ?? 0,
      icon: <ClockCircleOutlined />,
      tone: 'default',
    },
    {
      key: 'cronjobs',
      label: 'CronJobs',
      helper: localeCode === 'zh_CN' ? '周期调度任务' : 'Scheduled workload runs',
      value: cronJobsQuery.data?.data?.length ?? 0,
      icon: <ScheduleOutlined />,
      tone: 'default',
    },
  ]

  const eventColumns: TableColumnsType<WorkloadOverviewEvent> = [
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace', render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '类型' : 'Type', dataIndex: 'type', render: (value: string) => <StatusTag value={value} /> },
    { title: localeCode === 'zh_CN' ? '原因' : 'Reason', dataIndex: 'reason' },
    { title: localeCode === 'zh_CN' ? '对象' : 'Object', dataIndex: 'involvedName', render: (_: string, record: WorkloadOverviewEvent) => `${record.involvedKind || '-'} / ${record.involvedName || '-'}` },
    { title: localeCode === 'zh_CN' ? '消息' : 'Message', dataIndex: 'message', ellipsis: true },
    { title: localeCode === 'zh_CN' ? '次数' : 'Count', dataIndex: 'count' },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]
  const events = eventsQuery.data?.data ?? []

  return (
    <div className="soha-page soha-overview-page soha-workloads-overview-page">
      <div className="soha-overview-metric-grid soha-workload-overview-metric-grid">
        {stats.map((item) => (
          <Card key={item.key} size="small" variant="outlined" className={`soha-overview-metric-card is-${item.tone}`}>
            <div className="soha-overview-metric-card-head">
              <div className="soha-overview-metric-copy">
                <Text className="soha-overview-metric-label">{item.label}</Text>
                <Statistic value={item.value} />
              </div>
              <span className="soha-overview-metric-icon">{item.icon}</span>
            </div>
            <Text className="soha-overview-metric-helper">{item.helper}</Text>
          </Card>
        ))}
      </div>
      <AdminTable
        className="soha-workload-overview-events soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={(
          <ManagementTableToolbar>
            {densityButton}
            <WorkloadRefreshButton
              disabled={!clusterId}
              label={t('common.refresh', 'Refresh')}
              loading={eventsQuery.isFetching}
              onRefresh={() => void eventsQuery.refetch()}
            />
          </ManagementTableToolbar>
        )}
        columns={eventColumns}
        dataSource={events}
        rowKey={(record) => `${record.namespace || ''}/${record.name}`}
        loading={eventsQuery.isLoading}
        paginationSummary={<WorkloadTableSummary filteredCount={events.length} localeCode={localeCode} totalCount={events.length} />}
        empty={(
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={events.length}
            localeCode={localeCode}
            resourceLabel={localeCode === 'zh_CN' ? '事件记录' : 'event records'}
            totalCount={events.length}
          />
        )}
        pageSize={10}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
      />
    </div>
  )
}

/* ─── generic workload detail ─── */

interface WorkloadMeta {
  name: string
  namespace: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  createdAt: string
  yaml?: string
  [key: string]: unknown
}

function WorkloadMetadataSection({
  items,
  title,
}: {
  items?: Record<string, string>
  title: ReactNode
}) {
  const entries = Object.entries(items ?? {}).filter(([key]) => key.trim())
  if (entries.length === 0) return null

  return (
    <div className="soha-workload-metadata-section">
      <Text strong className="soha-workload-metadata-title">{title}</Text>
      <div className="soha-workload-kv-grid">
        {entries.map(([key, value]) => {
          const displayValue = value || '-'
          return (
            <Tooltip
              key={key}
              title={(
                <div className="soha-workload-kv-tooltip">
                  <div>{key}</div>
                  <div>{displayValue}</div>
                </div>
              )}
            >
              <div className="soha-workload-kv-item" title={`${key}: ${displayValue}`}>
                <span className="soha-workload-kv-key">{`${key}:`}</span>
                <span className="soha-workload-kv-value">{displayValue}</span>
              </div>
            </Tooltip>
          )
        })}
      </div>
    </div>
  )
}

function WorkloadDetailShell({
  title,
  resource,
  paramKey,
  extraTabPanes,
  extraOverview,
  actions,
  activeTabKey,
  onTabChange,
  yamlLast = false,
}: {
  title: string
  resource: string
  paramKey: string
  extraTabPanes?: NonNullable<TabsProps['items']>
  extraOverview?: React.ReactNode
  actions?: React.ReactNode
  activeTabKey?: string
  onTabChange?: (activeKey: string) => void
  yamlLast?: boolean
}) {
  const { t, localeCode } = useI18n()
  const { message } = App.useApp()
  const params = useParams()
  const [searchParams] = useSearchParams()
  const name = params[paramKey] as string
  const { clusterId, namespace } = usePlatformScopeStore()
  const detailNamespace = resolveWorkloadNamespace(namespace, searchParams.get('namespace'))

  const detailPath = clusterId
    ? `/clusters/${clusterId}/workloads/${resource}/${name}/detail${detailNamespace ? `?namespace=${encodeURIComponent(detailNamespace)}` : ''}`
    : null

  const yamlPath = clusterId
    ? `/clusters/${clusterId}/workloads/${resource}/${name}/yaml${detailNamespace ? `?namespace=${encodeURIComponent(detailNamespace)}` : ''}`
    : null

  const detailQuery = useQuery({
    queryKey: [resource, 'detail', clusterId, detailNamespace, name],
    queryFn: () => api.get<ApiResponse<WorkloadMeta>>(detailPath!),
    enabled: !!detailPath,
  })

  const yamlQuery = useQuery({
    queryKey: [resource, 'yaml', clusterId, detailNamespace, name],
    queryFn: () => api.get<ApiResponse<ResourceYAMLView>>(yamlPath!),
    enabled: !!yamlPath,
  })
  const yamlServerValue = yamlQuery.data?.data?.content ?? ''
  const yamlDraftStorageKey = useMemo(
    () => (clusterId ? `kc:yaml-draft:${clusterId}:${resource}:${detailNamespace || 'default'}:${name}` : ''),
    [clusterId, detailNamespace, name, resource],
  )
  const [yamlDraft, setYamlDraft] = useState('')

  const applyYamlMutation = useMutation({
    mutationFn: () => api.put<ApiResponse<ResourceYAMLView>>(yamlPath!, { content: yamlDraft }),
    onSuccess: (response) => {
      if (yamlDraftStorageKey) {
        window.localStorage.removeItem(yamlDraftStorageKey)
      }
      setYamlDraft(response.data?.content ?? yamlDraft)
      void message.success(t('yamlEditor.applySuccess', 'YAML applied'))
      yamlQuery.refetch()
      detailQuery.refetch()
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const detail = detailQuery.data?.data

  useEffect(() => {
    if (!yamlPath) return
    const draft = yamlDraftStorageKey ? window.localStorage.getItem(yamlDraftStorageKey) : null
    setYamlDraft(draft ?? yamlServerValue)
  }, [yamlDraftStorageKey, yamlPath, yamlServerValue])

  if (detailQuery.isLoading) return <div className="flex items-center justify-center h-64"><Spin size="large" /></div>
  if (!detail) return <ManagementState compact kind="not-found" title={localeCode === 'zh_CN' ? `${title}未找到` : `${title} not found`} />

  return (
    <div className="soha-page soha-workload-detail-page">
      <div className="soha-workload-detail-heading">
        <div className="soha-workload-detail-heading-main">
          <Text type="secondary" className="soha-workload-detail-kind">{title}</Text>
          <Text strong className="soha-workload-detail-name">{name}</Text>
        </div>
        {actions ? <div className="soha-workload-detail-actions">{actions}</div> : null}
      </div>
      <Tabs
        {...(activeTabKey != null ? { activeKey: activeTabKey } : { defaultActiveKey: 'overview' })}
        className="soha-workload-detail-tabs"
        onChange={onTabChange}
        indicator={{ size: (origin) => Math.max(16, origin - 16), align: 'center' }}
        size="small"
        tabBarGutter={18}
        items={[
          {
            key: 'overview',
            label: t('common.overview', 'Overview'),
            children: (
              <>
                <Card className="soha-detail-card">
                  <Descriptions
                    items={[
                      { key: t('common.name', 'Name'), label: t('common.name', 'Name'), children: detail.name },
                      { key: t('common.namespace', 'Namespace'), label: t('common.namespace', 'Namespace'), children: detail.namespace },
                      { key: t('common.createdAt', 'Created At'), label: t('common.createdAt', 'Created At'), children: detail.createdAt ? formatRelativeTime(detail.createdAt) : '-' },
                    ]}
                  />
                  <div className="soha-workload-metadata-stack">
                    <WorkloadMetadataSection items={detail.labels} title={t('common.labels', 'Labels')} />
                    <WorkloadMetadataSection items={detail.annotations} title={localeCode === 'zh_CN' ? '注解' : 'Annotations'} />
                  </div>
                </Card>
                {extraOverview}
              </>
            ),
          },
          ...(yamlLast ? extraTabPanes ?? [] : []),
          {
            key: 'yaml',
            label: t('common.yaml', 'YAML'),
            children: (
              <Suspense fallback={<Card className="soha-detail-card"><Spin size="large" /></Card>}>
                <K8sYamlEditor
                  value={yamlDraft}
                  onChange={setYamlDraft}
                  onReset={() => {
                    if (yamlDraftStorageKey) {
                      window.localStorage.removeItem(yamlDraftStorageKey)
                    }
                    setYamlDraft(yamlServerValue)
                    void message.success(t('yamlEditor.resetSuccess', 'YAML draft reset'))
                  }}
                  onSave={() => {
                    if (!yamlDraftStorageKey) return
                    window.localStorage.setItem(yamlDraftStorageKey, yamlDraft)
                    void message.success(t('yamlEditor.saveSuccess', 'YAML draft saved locally'))
                  }}
                  onApply={() => applyYamlMutation.mutate()}
                  saveDisabled={!yamlDraftStorageKey}
                  applyDisabled={!yamlPath || !yamlDraft.trim()}
                  applying={applyYamlMutation.isPending}
                />
              </Suspense>
            ),
          },
          ...(yamlLast ? [] : extraTabPanes ?? []),
        ]}
      />
    </div>
  )
}

/* ─── Deployments ─── */



export function WorkloadsDeploymentsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const { data, isLoading } = useScopedQuery<Deployment>('deployments')
  const [scaleTarget, setScaleTarget] = useState<{ name: string; namespace: string; replicas: number } | null>(null)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [healthFilter, setHealthFilter] = useState('all')
  const [selectedDeploymentKeys, setSelectedDeploymentKeys] = useState<string[]>([])
  const [batchScaleVisible, setBatchScaleVisible] = useState(false)
  const [batchScaleReplicas, setBatchScaleReplicas] = useState(1)
  const [batchRollbackVisible, setBatchRollbackVisible] = useState(false)
  const [batchRollbackLoading, setBatchRollbackLoading] = useState(false)
  const [batchRollbackDrafts, setBatchRollbackDrafts] = useState<BatchRollbackDraft[]>([])
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)

  const deployments = data?.data ?? []
  const normalizedKeyword = normalizeSearchKeyword(searchKeyword)

  const restartMutation = useMutation({
    mutationFn: ({ name, namespace: targetNamespace }: { name: string; namespace: string }) =>
      api.post(`/clusters/${clusterId}/workloads/deployments/restart`, { namespace: targetNamespace, name }),
    onSuccess: () => {
      void message.success('已触发重启')
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const scaleMutation = useMutation({
    mutationFn: ({ name, namespace: targetNamespace, replicas }: { name: string; namespace: string; replicas: number }) =>
      api.post(`/clusters/${clusterId}/workloads/deployments/scale`, { namespace: targetNamespace, name, replicas }),
    onSuccess: () => {
      void message.success('已触发扩缩容')
      setScaleTarget(null)
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const rollbackMutation = useMutation({
    mutationFn: ({ name, namespace: targetNamespace }: { name: string; namespace: string }) =>
      api.post(`/clusters/${clusterId}/workloads/deployments/rollback`, { namespace: targetNamespace, name }),
    onSuccess: () => {
      void message.success('已触发回滚')
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: ({ name, namespace: targetNamespace }: { name: string; namespace: string }) =>
      api.delete(`/clusters/${clusterId}/workloads/deployments/${encodeURIComponent(name)}?namespace=${encodeURIComponent(targetNamespace)}`),
    onSuccess: () => {
      void message.success(localeCode === 'zh_CN' ? '已删除' : 'Deleted')
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const batchRestartMutation = useMutation({
    mutationFn: async (items: Deployment[]) =>
      Promise.allSettled(
        items.map((item) =>
          api.post(`/clusters/${clusterId}/workloads/deployments/restart`, { namespace: item.namespace, name: item.name }),
        ),
      ),
    onSuccess: (results) => {
      const successCount = results.filter((item) => item.status === 'fulfilled').length
      const failureCount = results.length - successCount
      void message.success(failureCount > 0 ? `批量重启完成，成功 ${successCount}，失败 ${failureCount}` : `已批量重启 ${successCount} 个 Deployment`)
      setSelectedDeploymentKeys([])
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const batchScaleMutation = useMutation({
    mutationFn: async ({ items, replicas }: { items: Deployment[]; replicas: number }) =>
      Promise.allSettled(
        items.map((item) =>
          api.post(`/clusters/${clusterId}/workloads/deployments/scale`, { namespace: item.namespace, name: item.name, replicas }),
        ),
      ),
    onSuccess: (results) => {
      const successCount = results.filter((item) => item.status === 'fulfilled').length
      const failureCount = results.length - successCount
      void message.success(failureCount > 0 ? `批量扩缩完成，成功 ${successCount}，失败 ${failureCount}` : `已批量扩缩 ${successCount} 个 Deployment`)
      setBatchScaleVisible(false)
      setSelectedDeploymentKeys([])
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const batchRollbackMutation = useMutation({
    mutationFn: async (items: BatchRollbackDraft[]) => {
      const validItems = items.filter((item) => item.revision)
      if (validItems.length === 0) {
        throw new Error(localeCode === 'zh_CN' ? '请至少为一个 Deployment 选择回滚 Revision' : 'Select at least one rollback revision')
      }
      return Promise.allSettled(
        validItems.map((item) =>
          api.post(`/clusters/${clusterId}/workloads/deployments/rollback`, {
            namespace: item.namespace,
            name: item.name,
            revision: item.revision,
          }),
        ),
      )
    },
    onSuccess: (results) => {
      const successCount = results.filter((item) => item.status === 'fulfilled').length
      const failureCount = results.length - successCount
      void message.success(failureCount > 0 ? `批量回滚完成，成功 ${successCount}，失败 ${failureCount}` : `已批量回滚 ${successCount} 个 Deployment`)
      setBatchRollbackVisible(false)
      setSelectedDeploymentKeys([])
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const filteredDeployments = useMemo(() => (
    deployments.filter((item) => {
      const health = getDeploymentHealth(item)
      if (healthFilter !== 'all' && health !== healthFilter) return false
      return includesSearch([item.name, item.namespace], normalizedKeyword)
    })
  ), [deployments, healthFilter, normalizedKeyword])

  const selectedDeployments = useMemo(
    () => deployments.filter((item) => selectedDeploymentKeys.includes(`${item.namespace}/${item.name}`)),
    [deployments, selectedDeploymentKeys],
  )
  const canBatchRestart = selectedDeployments.length > 0 && selectedDeployments.every((item) => hasAllowedAction(item.allowedActions, 'restart'))
  const canBatchScale = selectedDeployments.length > 0 && selectedDeployments.every((item) => hasAllowedAction(item.allowedActions, 'scale'))
  const canBatchRollback = selectedDeployments.length > 0 && selectedDeployments.every((item) => hasAllowedAction(item.allowedActions, 'update'))

  const openBatchRollbackModal = async () => {
    if (!clusterId || selectedDeployments.length === 0) return
    setBatchRollbackLoading(true)
    setBatchRollbackVisible(true)
    try {
      const drafts = await Promise.all(
        selectedDeployments.map(async (item) => {
          const response = await api.get<ApiResponse<RolloutHistory[]>>(
            `/clusters/${clusterId}/workloads/deployments/${item.name}/rollouts?namespace=${encodeURIComponent(item.namespace)}`,
          )
          const options = (response.data ?? [])
            .filter((history) => history.revision)
            .map((history) => ({
              value: history.revision,
              label: `${history.revision}${history.createdAt ? ` · ${formatDateTime(history.createdAt)}` : ''}`,
            }))
          return {
            key: `${item.namespace}/${item.name}`,
            name: item.name,
            namespace: item.namespace,
            options,
            revision: options[1]?.value ?? options[0]?.value ?? '',
          } satisfies BatchRollbackDraft
        }),
      )
      setBatchRollbackDrafts(drafts)
    } catch (err) {
      void message.error(err instanceof Error ? err.message : String(err))
      setBatchRollbackVisible(false)
    } finally {
      setBatchRollbackLoading(false)
    }
  }

  const columns: TableColumnsType<Deployment> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      render: (name: string, record: Deployment) => (
        renderWorkloadNameLink(name, () => navigate(buildWorkloadDetailPath('deployments', name, namespace, record.namespace)))
      ),
    },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace' },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'health',
      key: 'health',
      width: 120,
      render: (_: unknown, record: Deployment) => <StatusTag value={getDeploymentHealth(record)} />,
    },
    { title: 'Ready', dataIndex: 'readyReplicas', width: 88, render: (_: number, record: Deployment) => `${record.readyReplicas}/${record.desiredReplicas}` },
    { title: localeCode === 'zh_CN' ? '已更新' : 'Up-to-date', dataIndex: 'updatedReplicas', width: 96 },
    { title: localeCode === 'zh_CN' ? '可用' : 'Available', dataIndex: 'available', width: 88 },
    { title: 'Age', dataIndex: 'ageSeconds', width: 104, render: (value: number) => formatAgeSeconds(value) },
    {
      fixed: 'right',
      title: '',
      dataIndex: 'name',
      key: 'actions',
      width: 148,
      align: 'center',
      onHeaderCell: () => ({ className: DEPLOYMENT_ACTIONS_COLUMN_CLASS_NAME }),
      onCell: () => ({ className: DEPLOYMENT_ACTIONS_COLUMN_CLASS_NAME }),
      render: (name: string, record: Deployment) => {
        const canRestart = hasAllowedAction(record.allowedActions, 'restart')
        const canScale = hasAllowedAction(record.allowedActions, 'scale')
        const canRollback = hasAllowedAction(record.allowedActions, 'update')
        const canDelete = hasAllowedAction(record.allowedActions, 'delete')
        if (!canRestart && !canScale && !canRollback && !canDelete) return '-'
        return (
          <Space size={4} className="soha-deployment-action-cell">
            {canRestart ? (
              <ManagementIconButton
                icon={<ReloadOutlined />}
                aria-label={localeCode === 'zh_CN' ? '重启' : 'Restart'}
                tooltip={localeCode === 'zh_CN' ? '重启' : 'Restart'}
                onClick={() => restartMutation.mutate({ name, namespace: record.namespace })}
              />
            ) : null}
            {canScale ? (
              <ManagementIconButton
                icon={<EditOutlined />}
                aria-label={localeCode === 'zh_CN' ? '扩缩' : 'Scale'}
                tooltip={localeCode === 'zh_CN' ? '扩缩' : 'Scale'}
                onClick={() => setScaleTarget({ name, namespace: record.namespace, replicas: record.desiredReplicas })}
              />
            ) : null}
            {canRollback ? (
              <ManagementIconButton
                icon={<UndoOutlined />}
                aria-label={localeCode === 'zh_CN' ? '回滚' : 'Rollback'}
                tooltip={localeCode === 'zh_CN' ? '回滚' : 'Rollback'}
                onClick={() => rollbackMutation.mutate({ name, namespace: record.namespace })}
              />
            ) : null}
            {canDelete ? (
              <Popconfirm
                title={localeCode === 'zh_CN' ? `确认删除 ${name}？` : `Delete ${name}?`}
                description={localeCode === 'zh_CN' ? '此操作不可恢复，删除后集群资源立即消失。' : 'This deletes the resource immediately and cannot be undone.'}
                okText={localeCode === 'zh_CN' ? '删除' : 'Delete'}
                cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
                okButtonProps={{ danger: true, loading: deleteMutation.isPending && deleteMutation.variables?.name === name && deleteMutation.variables?.namespace === record.namespace }}
                placement="topRight"
                onConfirm={() => deleteMutation.mutate({ name, namespace: record.namespace })}
              >
                <ManagementIconButton
                  danger
                  icon={<DeleteOutlined />}
                  aria-label={localeCode === 'zh_CN' ? '删除' : 'Delete'}
                  loading={deleteMutation.isPending && deleteMutation.variables?.name === name && deleteMutation.variables?.namespace === record.namespace}
                  tooltip={localeCode === 'zh_CN' ? '删除' : 'Delete'}
                />
              </Popconfirm>
            ) : null}
          </Space>
        )
      },
    },
  ]

  const deploymentQueryPanel = (
    <WorkloadQueryPanel
      hasActiveFilters={Boolean(searchKeyword.trim()) || healthFilter !== 'all'}
      localeCode={localeCode}
      onReset={() => {
        setSearchKeyword('')
        setHealthFilter('all')
      }}
    >
      <ManagementQueryField label={localeCode === 'zh_CN' ? '名称' : 'Name'}>
        <WorkloadSearchInput
          value={searchKeyword}
          onChange={setSearchKeyword}
          placeholder={localeCode === 'zh_CN' ? '搜索 Deployment 名称' : 'Search deployment name'}
        />
      </ManagementQueryField>
      <ManagementQueryField label={localeCode === 'zh_CN' ? '健康状态' : 'Health'}>
        <Select
          className="soha-platform-compact-field"
          size="small"
          value={healthFilter}
          variant="filled"
          onChange={(value) => setHealthFilter(String(value || 'all'))}
          options={[
            { value: 'all', label: localeCode === 'zh_CN' ? '全部健康状态' : 'All health states' },
            { value: 'healthy', label: localeCode === 'zh_CN' ? '健康' : 'Healthy' },
            { value: 'progressing', label: localeCode === 'zh_CN' ? '进行中' : 'Progressing' },
            { value: 'degraded', label: localeCode === 'zh_CN' ? '异常' : 'Degraded' },
            { value: 'scaled-down', label: localeCode === 'zh_CN' ? '已缩容为 0' : 'Scaled down' },
          ]}
        />
      </ManagementQueryField>
    </WorkloadQueryPanel>
  )

  const deploymentToolbarExtra = (
    <ManagementTableToolbar>
      <Button
        autoInsertSpace={false}
        size="small"
        variant="outlined"
        disabled={!canBatchRestart}
        loading={batchRestartMutation.isPending}
        onClick={() => batchRestartMutation.mutate(selectedDeployments)}
      >
        {localeCode === 'zh_CN' ? `批量重启 (${selectedDeployments.length})` : `Batch Restart (${selectedDeployments.length})`}
      </Button>
      <Button
        autoInsertSpace={false}
        size="small"
        variant="outlined"
        disabled={!canBatchRollback}
        loading={batchRollbackLoading}
        onClick={openBatchRollbackModal}
      >
        {localeCode === 'zh_CN' ? `批量回滚 (${selectedDeployments.length})` : `Batch Rollback (${selectedDeployments.length})`}
      </Button>
      <Button
        autoInsertSpace={false}
        size="small"
        variant="outlined"
        disabled={!canBatchScale}
        onClick={() => {
          setBatchScaleReplicas(selectedDeployments[0]?.desiredReplicas ?? 1)
          setBatchScaleVisible(true)
        }}
      >
        {localeCode === 'zh_CN' ? `批量扩缩 (${selectedDeployments.length})` : `Batch Scale (${selectedDeployments.length})`}
      </Button>
      {densityButton}
      <WorkloadRefreshButton
        disabled={!clusterId}
        label={t('common.refresh', 'Refresh')}
        onRefresh={() => queryClient.invalidateQueries({ queryKey: ['deployments', clusterId, namespace] })}
      />
    </ManagementTableToolbar>
  )

  return (
    <div className="soha-page">
      {deploymentQueryPanel}
      <AdminTable
        className="soha-deployments-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={deploymentToolbarExtra}
        columns={columns}
        dataSource={filteredDeployments}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={isLoading}
        paginationSummary={<WorkloadTableSummary filteredCount={filteredDeployments.length} localeCode={localeCode} totalCount={deployments.length} />}
        empty={(
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={filteredDeployments.length}
            localeCode={localeCode}
            resourceLabel="Deployments"
            totalCount={deployments.length}
          />
        )}
        pageSize={10}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        selectCurrentPageOnly
        rowSelection={{
          selectedRowKeys: selectedDeploymentKeys,
          onChange: (selectedRowKeys: string[]) => setSelectedDeploymentKeys(selectedRowKeys),
        }}
      />
      <Modal
        title={localeCode === 'zh_CN' ? '扩缩容' : 'Scale deployment'}
        open={!!scaleTarget}
        onOk={() => {
          if (scaleTarget) {
            scaleMutation.mutate(scaleTarget)
          }
        }}
        onCancel={() => setScaleTarget(null)}
        confirmLoading={scaleMutation.isPending}
      >
        <div className="flex items-center gap-2">
          <Text>{localeCode === 'zh_CN' ? '副本数:' : 'Replicas:'}</Text>
          <InputNumber
            value={scaleTarget?.replicas ?? 1}
            min={0}
            onChange={(v) => scaleTarget && setScaleTarget({ ...scaleTarget, replicas: v as number })}
          />
        </div>
      </Modal>
      <Modal
        title={localeCode === 'zh_CN' ? '批量回滚' : 'Batch rollback deployments'}
        open={batchRollbackVisible}
        onOk={() => batchRollbackMutation.mutate(batchRollbackDrafts)}
        onCancel={() => setBatchRollbackVisible(false)}
        confirmLoading={batchRollbackMutation.isPending}
        width={900}
      >
        {batchRollbackLoading ? (
          <div className="flex items-center justify-center h-48"><Spin size="large" /></div>
        ) : (
          <AdminTable
            shellClassName="soha-management-table-shell"
            columns={[
              { title: localeCode === 'zh_CN' ? 'Deployment' : 'Deployment', dataIndex: 'name' },
              { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace' },
              {
                title: localeCode === 'zh_CN' ? '目标 Revision' : 'Target Revision',
                dataIndex: 'revision',
                render: (_: string, record: BatchRollbackDraft) => (
                  <Select
                    value={record.revision || undefined}
                    options={record.options}
                    style={{ width: 260 }}
                    placeholder={localeCode === 'zh_CN' ? '选择回滚版本' : 'Select revision'}
                    onChange={(value) =>
                      setBatchRollbackDrafts((current) =>
                        current.map((item) => item.key === record.key ? { ...item, revision: String(value || '') } : item),
                      )
                    }
                  />
                ),
              },
            ]}
            dataSource={batchRollbackDrafts}
            rowKey="key"
            pageSize={10}
            enableColumnSelection={false}
          />
        )}
      </Modal>
      <Modal
        title={localeCode === 'zh_CN' ? '批量扩缩容' : 'Batch scale deployments'}
        open={batchScaleVisible}
        onOk={() => batchScaleMutation.mutate({ items: selectedDeployments, replicas: batchScaleReplicas })}
        onCancel={() => setBatchScaleVisible(false)}
        confirmLoading={batchScaleMutation.isPending}
      >
        <div className="flex flex-col gap-3">
          <Text type="secondary">
            {localeCode === 'zh_CN'
              ? `将对 ${selectedDeployments.length} 个 Deployment 应用相同副本数`
              : `Apply the same replica count to ${selectedDeployments.length} deployments`}
          </Text>
          <div className="flex items-center gap-2">
            <Text>{localeCode === 'zh_CN' ? '副本数:' : 'Replicas:'}</Text>
            <InputNumber value={batchScaleReplicas} min={0} onChange={(value) => setBatchScaleReplicas(Number(value) || 0)} />
          </div>
        </div>
      </Modal>
    </div>
  )
}

export function DeploymentDetailPage() {
  const { localeCode } = useI18n()
  const params = useParams()
  const [searchParams] = useSearchParams()
  const deploymentName = params.deploymentName as string
  const { clusterId, namespace } = usePlatformScopeStore()
  const detailNamespace = resolveWorkloadNamespace(namespace, searchParams.get('namespace'))
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [scaleVisible, setScaleVisible] = useState(false)
  const [scaleReplicas, setScaleReplicas] = useState(1)

  const deploymentDetailQuery = useQuery({
    queryKey: ['deployment-detail-meta', clusterId, detailNamespace, deploymentName],
    queryFn: () => api.get<ApiResponse<DeploymentDetailMeta>>(
      `/clusters/${clusterId}/workloads/deployments/${deploymentName}/detail?namespace=${encodeURIComponent(detailNamespace!)}`,
    ),
    enabled: !!clusterId && !!detailNamespace,
  })

  const bindingsQuery = useQuery({
    queryKey: ['application-environments'],
    queryFn: () => api.get<ApiResponse<ApplicationEnvironment[]>>('/application-environments'),
  })
  const applicationsQuery = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<ApplicationSummary[]>>('/applications'),
  })
  const buildsQuery = useQuery({
    queryKey: ['builds'],
    queryFn: () => api.get<ApiResponse<BuildRecord[]>>('/builds'),
  })
  const workflowsQuery = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.get<ApiResponse<WorkflowRecord[]>>('/workflows'),
  })
  const releasesQuery = useQuery({
    queryKey: ['releases'],
    queryFn: () => api.get<ApiResponse<ReleaseRecord[]>>('/releases'),
  })
  const metricsQuery = useQuery({
    queryKey: ['deployment-metrics', clusterId, detailNamespace, deploymentName],
    queryFn: () => api.get<ApiResponse<ResourceMetrics>>(
      `/clusters/${clusterId}/workloads/deployments/${deploymentName}/metrics?namespace=${encodeURIComponent(detailNamespace!)}`,
    ),
    enabled: !!clusterId && !!detailNamespace,
  })
  const rolloutStatusQuery = useQuery({
    queryKey: ['deployment-rollout-status', clusterId, detailNamespace, deploymentName],
    queryFn: () => api.get<ApiResponse<DeploymentRolloutStatus>>(
      `/clusters/${clusterId}/workloads/deployments/${deploymentName}/rollout-status?namespace=${encodeURIComponent(detailNamespace!)}`,
    ),
    enabled: !!clusterId && !!detailNamespace,
  })
  const rolloutHistoryQuery = useQuery({
    queryKey: ['deployment-rollouts', clusterId, detailNamespace, deploymentName],
    queryFn: () => api.get<ApiResponse<RolloutHistory[]>>(
      `/clusters/${clusterId}/workloads/deployments/${deploymentName}/rollouts?namespace=${encodeURIComponent(detailNamespace!)}`,
    ),
    enabled: !!clusterId && !!detailNamespace,
  })
  const deploymentEventsQuery = useQuery({
    queryKey: ['deployment-events', clusterId, detailNamespace, deploymentName],
    queryFn: async () => {
      const response = await api.get<ApiResponse<WorkloadOverviewEvent[]>>(
        buildClusterScopedPath(clusterId!, 'events', detailNamespace, { limit: 100 }),
      )
      return {
        data: (response.data ?? []).filter((item) =>
          item.involvedName === deploymentName && (!item.involvedKind || item.involvedKind.toLowerCase() === 'deployment'),
        ),
      } as ApiResponse<WorkloadOverviewEvent[]>
    },
    enabled: !!clusterId && !!detailNamespace,
  })
  const deploymentPodsQuery = useQuery({
    queryKey: ['deployment-pods', clusterId, detailNamespace, deploymentName],
    queryFn: async () => {
      const response = await api.get<ApiResponse<Pod[]>>(
        `/clusters/${clusterId}/workloads/pods?namespace=${encodeURIComponent(detailNamespace!)}`,
      )
      const selector = deploymentDetailQuery.data?.data?.selector
      return {
        data: (response.data ?? []).filter((item) => selectorMatchesLabels(selector, item.labels)),
      } as ApiResponse<Pod[]>
    },
    enabled: !!clusterId && !!detailNamespace && !!deploymentDetailQuery.data?.data,
  })

  const matchedBindings = useMemo<ApplicationEnvironment[]>(() => {
    if (!clusterId || !detailNamespace) return []
    return (bindingsQuery.data?.data ?? []).filter((binding) =>
      (binding.targets ?? []).some((target) =>
        targetMatchesDeployment(target, clusterId, detailNamespace, deploymentName),
      ),
    )
  }, [bindingsQuery.data, clusterId, detailNamespace, deploymentName])

  const applicationMap = useMemo(
    () => Object.fromEntries((applicationsQuery.data?.data ?? []).map((item) => [item.id, item])),
    [applicationsQuery.data],
  )
  const latestBuildByApplication = useMemo(
    () => Object.fromEntries((buildsQuery.data?.data ?? []).map((item) => [item.applicationId, item])),
    [buildsQuery.data],
  )

  const rolloutStatus = rolloutStatusQuery.data?.data
  const rolloutHistory = rolloutHistoryQuery.data?.data ?? []
  const deploymentPods = deploymentPodsQuery.data?.data ?? []
  const deploymentTimelineEvents = useMemo(
    () => (deploymentEventsQuery.data?.data?.length
      ? deploymentEventsQuery.data.data
      : (rolloutStatus?.conditions ?? []).map(conditionToTimelineEvent)),
    [deploymentEventsQuery.data, rolloutStatus],
  )
  useEffect(() => {
    if (rolloutStatus?.desiredReplicas != null) {
      setScaleReplicas(rolloutStatus.desiredReplicas)
    }
  }, [rolloutStatus])

  const restartDeploymentMutation = useMutation({
    mutationFn: async () =>
      api.post(`/clusters/${clusterId}/workloads/deployments/restart`, {
        namespace: detailNamespace,
        name: deploymentName,
      }),
    onSuccess: () => {
      void message.success(localeCode === 'zh_CN' ? '已触发重启' : 'Restart triggered')
      queryClient.invalidateQueries({ queryKey: ['deployment-rollout-status', clusterId, detailNamespace, deploymentName] })
      queryClient.invalidateQueries({ queryKey: ['deployment-rollouts', clusterId, detailNamespace, deploymentName] })
      queryClient.invalidateQueries({ queryKey: ['deployments', clusterId, namespace] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const scaleDeploymentMutation = useMutation({
    mutationFn: async () =>
      api.post(`/clusters/${clusterId}/workloads/deployments/scale`, {
        namespace: detailNamespace,
        name: deploymentName,
        replicas: scaleReplicas,
      }),
    onSuccess: () => {
      void message.success(localeCode === 'zh_CN' ? '已触发扩缩容' : 'Scale triggered')
      setScaleVisible(false)
      queryClient.invalidateQueries({ queryKey: ['deployment-rollout-status', clusterId, detailNamespace, deploymentName] })
      queryClient.invalidateQueries({ queryKey: ['deployment-rollouts', clusterId, detailNamespace, deploymentName] })
      queryClient.invalidateQueries({ queryKey: ['deployments', clusterId, namespace] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const linkageOverview = (
    <div className="soha-detail-stack">
      <Card className="soha-detail-card soha-related-pod-card" title={localeCode === 'zh_CN' ? '关联 Pods' : 'Related Pods'}>
        <List
          className="soha-related-pod-list"
          dataSource={deploymentPods}
          loading={deploymentPodsQuery.isLoading}
          rowKey={(record) => `${record.namespace}/${record.name}`}
          locale={{
            emptyText: (
              <ManagementState
                bordered={false}
                compact
                title={localeCode === 'zh_CN' ? '暂无关联 Pods' : 'No related Pods'}
              />
            ),
          }}
          renderItem={(pod: Pod) => (
            <List.Item className="soha-related-pod-item">
              <div className="soha-related-pod-line">
                <Tooltip title={pod.name}>
                  <Button
                    type="link"
                    className="soha-related-pod-name"
                    onClick={() => navigate(buildWorkloadDetailPath('pods', pod.name, detailNamespace, pod.namespace))}
                  >
                    {pod.name}
                  </Button>
                </Tooltip>
                <StatusTag value={pod.phase} />
                <Tag color="blue" className="soha-related-pod-tag">
                  {pod.namespace || detailNamespace || '-'}
                </Tag>
                <Tag color="cyan" className="soha-related-pod-tag">
                  {pod.podIp || '-'}
                </Tag>
                <Tag color="success" className="soha-related-pod-tag">
                  {`Ready ${pod.readyContainers || '-'}`}
                </Tag>
                <Tag color={(pod.restarts ?? 0) > 0 ? 'warning' : 'default'} className="soha-related-pod-tag">
                  {`${localeCode === 'zh_CN' ? '重启' : 'Restarts'} ${pod.restarts ?? 0}`}
                </Tag>
                <Tooltip title={pod.nodeName || '-'}>
                  <Tag color="purple" className="soha-related-pod-tag soha-related-pod-tag-node">
                    {pod.nodeName || '-'}
                  </Tag>
                </Tooltip>
                <Tag color="geekblue" className="soha-related-pod-tag">
                  {formatAgeSeconds(pod.ageSeconds)}
                </Tag>
              </div>
            </List.Item>
          )}
        />
      </Card>
      <Card className="soha-detail-card soha-rollout-card" title={localeCode === 'zh_CN' ? '滚动发布' : 'Rollout'}>
        <div className="soha-rollout-status-section">
          {rolloutStatus ? (
            <div className="soha-rollout-status-compact">
              <span className="soha-rollout-status-chip">
                <Text type="secondary">Revision</Text>
                <Text strong>{rolloutStatus.revision || '-'}</Text>
              </span>
              <span className="soha-rollout-status-chip">
                <Text type="secondary">{localeCode === 'zh_CN' ? '状态' : 'Status'}</Text>
                <StatusTag value={rolloutStatus.status} />
              </span>
              <Tooltip title={rolloutStatus.message || '-'}>
                <span className="soha-rollout-status-chip soha-rollout-status-chip-message">
                  <Text type="secondary">{localeCode === 'zh_CN' ? '消息' : 'Message'}</Text>
                  <Text className="soha-rollout-status-message">{rolloutStatus.message || '-'}</Text>
                </span>
              </Tooltip>
              <span className="soha-rollout-status-chip">
                <Text type="secondary">{localeCode === 'zh_CN' ? '副本' : 'Desired'}</Text>
                <Text>{rolloutStatus.desiredReplicas}</Text>
              </span>
              <span className="soha-rollout-status-chip">
                <Text type="secondary">{localeCode === 'zh_CN' ? '更新' : 'Updated'}</Text>
                <Text>{rolloutStatus.updatedReplicas}</Text>
              </span>
              <span className="soha-rollout-status-chip">
                <Text type="secondary">{localeCode === 'zh_CN' ? '就绪' : 'Ready'}</Text>
                <Text>{rolloutStatus.readyReplicas}</Text>
              </span>
              <span className="soha-rollout-status-chip">
                <Text type="secondary">{localeCode === 'zh_CN' ? '可用' : 'Available'}</Text>
                <Text>{rolloutStatus.availableReplicas}</Text>
              </span>
            </div>
          ) : (
            <ManagementState bordered={false} compact title={localeCode === 'zh_CN' ? '暂无滚动状态' : 'No rollout status'} />
          )}
        </div>
        <div className="soha-rollout-history-section">
          {rolloutHistory.length === 0 ? (
            <ManagementState bordered={false} compact title={localeCode === 'zh_CN' ? '暂无滚动历史' : 'No rollout history'} />
          ) : (
            <div className="soha-rollout-history-list">
              {rolloutHistory.map((record) => (
                <div key={record.revision} className="soha-rollout-history-row">
                  <Text type="secondary" className="soha-rollout-history-time">
                    {record.createdAt ? formatDateTime(record.createdAt) : '-'}
                  </Text>
                  <Text strong className="soha-rollout-history-revision">{`Revision ${record.revision || '-'}`}</Text>
                  <Tag className="soha-rollout-history-tag">{`${localeCode === 'zh_CN' ? '副本' : 'Replicas'} ${record.replicas ?? '-'}`}</Tag>
                  <Tag className="soha-rollout-history-tag">{`${localeCode === 'zh_CN' ? '就绪' : 'Ready'} ${record.readyReplicas ?? '-'}`}</Tag>
                  <Text type="secondary" className="soha-rollout-history-image">
                    {record.images?.length ? record.images.join(', ') : (localeCode === 'zh_CN' ? '未记录镜像' : 'No image recorded')}
                  </Text>
                </div>
              ))}
            </div>
          )}
        </div>
      </Card>
      <Card className="soha-detail-card" title="交付联动">
        {matchedBindings.length === 0 ? (
          <ManagementState bordered={false} compact title="当前 Deployment 尚未绑定到任何应用环境" />
        ) : (
          <div className="soha-list-panel">
            {matchedBindings.map((binding) => {
              const application = applicationMap[binding.applicationId]
              const latestBuild = latestBuildByApplication[binding.applicationId]
              const latestWorkflow = (workflowsQuery.data?.data ?? []).find((item) =>
                item.applicationId === binding.applicationId
                  && item.clusterId === clusterId
                  && item.namespace === detailNamespace
                  && item.deploymentName === deploymentName,
              )
              const latestRelease = (releasesQuery.data?.data ?? []).find((item) =>
                item.applicationId === binding.applicationId
                  && item.clusterId === clusterId
                  && item.namespace === detailNamespace
                  && item.deploymentName === deploymentName,
              )

              return (
                <div key={binding.id} className="soha-list-row">
                  <div className="soha-list-row-meta">
                    <Text strong>{application?.name || binding.applicationId}</Text>
                    <Tag color="blue">{binding.environmentKey || binding.environmentId}</Tag>
                    {binding.workflowTemplate?.name ? <Tag color="cyan">{binding.workflowTemplate.name}</Tag> : null}
                  </div>
                  <div className="soha-list-row-extra">
                    <StatusTag value={latestBuild?.status || 'unknown'} />
                    <StatusTag value={latestWorkflow?.status || 'unknown'} />
                    <StatusTag value={latestRelease?.status || 'unknown'} />
                    <Text type="secondary" className="text-xs">
                      {latestRelease?.createdAt
                        ? `最近发布: ${formatDateTime(latestRelease.createdAt)}`
                        : latestWorkflow?.updatedAt
                          ? `最近工作流: ${formatDateTime(latestWorkflow.updatedAt)}`
                          : latestBuild?.createdAt
                            ? `最近构建: ${formatDateTime(latestBuild.createdAt)}`
                            : '暂无执行记录'}
                    </Text>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </Card>
    </div>
  )

  const metricsTab: NonNullable<TabsProps['items']>[number] = {
    key: 'metrics',
    label: localeCode === 'zh_CN' ? '指标' : 'Metrics',
    children: (
      <ResourceMetricsPanel
        title={localeCode === 'zh_CN' ? 'Deployment 指标' : 'Deployment Metrics'}
        data={metricsQuery.data?.data}
        loading={metricsQuery.isLoading}
      />
    ),
  }

  const eventsTab: NonNullable<TabsProps['items']>[number] = {
    key: 'events',
    label: localeCode === 'zh_CN' ? '事件' : 'Events',
    children: (
      <ResourceEventsTimeline
        title={localeCode === 'zh_CN' ? 'Deployment 事件时间线' : 'Deployment Event Timeline'}
        events={deploymentTimelineEvents}
        loading={deploymentEventsQuery.isLoading}
        emptyDescription={localeCode === 'zh_CN' ? '当前 Deployment 暂无事件和状态变化' : 'No deployment events or rollout condition transitions'}
      />
    ),
  }

  return (
    <>
      <WorkloadDetailShell
        title="Deployment"
        resource="deployments"
        paramKey="deploymentName"
        extraOverview={linkageOverview}
        extraTabPanes={[metricsTab, eventsTab]}
        yamlLast
        actions={(
          <Space>
            <Button autoInsertSpace={false} variant="outlined" loading={restartDeploymentMutation.isPending} onClick={() => restartDeploymentMutation.mutate()}>
              {localeCode === 'zh_CN' ? '重启' : 'Restart'}
            </Button>
            <Button variant="outlined" onClick={() => setScaleVisible(true)}>
              {localeCode === 'zh_CN' ? '扩缩容' : 'Scale'}
            </Button>
          </Space>
        )}
      />
      <Modal
        title={localeCode === 'zh_CN' ? 'Deployment 扩缩容' : 'Scale deployment'}
        open={scaleVisible}
        onOk={() => scaleDeploymentMutation.mutate()}
        onCancel={() => setScaleVisible(false)}
        confirmLoading={scaleDeploymentMutation.isPending}
      >
        <div className="flex items-center gap-2">
          <Text>{localeCode === 'zh_CN' ? '副本数:' : 'Replicas:'}</Text>
          <InputNumber value={scaleReplicas} min={0} onChange={(value) => setScaleReplicas(Number(value) || 0)} />
        </div>
      </Modal>
    </>
  )
}

/* ─── Pods ─── */


function renderPodRuntimeCell(record: Pod) {
  const ready = parseReadyContainers(record.readyContainers)
  const readyHealthy = ready.total > 0 && ready.ready >= ready.total

  return (
    <Space size={6} wrap={false} className="soha-pod-table-runtime">
      <StatusTag value={record.phase} />
      <Tag bordered={false} color={readyHealthy ? 'success' : 'warning'}>
        {`Ready ${record.readyContainers || '-'}`}
      </Tag>
    </Space>
  )
}

function renderPodResourceSummaryCell(record: Pod, localeCode: 'zh_CN' | 'en_US') {
  return (
    <Space size={6} wrap={false} className="soha-pod-table-resource-summary">
      <Tag bordered={false} color="processing">
        {`CPU ${formatCpuDisplay(record.cpu)}`}
      </Tag>
      <Tag bordered={false} color="purple">
        {`${localeCode === 'zh_CN' ? '内存' : 'MEM'} ${formatMemoryDisplay(record.memory)}`}
      </Tag>
    </Space>
  )
}

function renderPodNameCell(record: Pod, onClick: () => void, localeCode: 'zh_CN' | 'en_US') {
  const podIp = record.podIp || '-'
  const nodeName = record.nodeName || '-'
  const age = formatAgeSeconds(record.ageSeconds)

  return (
    <div className="soha-pod-table-name-cell">
      <Tooltip title={record.name} placement="topLeft">
        <Link className="soha-pod-table-name-link" onClick={onClick}>
          {record.name}
        </Link>
      </Tooltip>
      <Space size={4} wrap={false} className="soha-pod-table-meta">
        <Tag bordered={false} color="blue">{record.namespace || '-'}</Tag>
        <Tag bordered={false} color="cyan">{`IP ${podIp}`}</Tag>
        <Tooltip title={`${localeCode === 'zh_CN' ? '节点' : 'Node'}: ${nodeName}`} placement="topLeft">
          <Tag bordered={false} color="geekblue" className="soha-pod-table-node-tag">
            {nodeName}
          </Tag>
        </Tooltip>
        <Tag bordered={false} className="soha-pod-table-age-tag">
          {age}
        </Tag>
      </Space>
    </div>
  )
}



export function WorkloadsPodsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId, namespace } = usePlatformScopeStore()
  const [autoRefreshEnabled, setAutoRefreshEnabled] = useState(true)
  const [autoRefreshIntervalSeconds, setAutoRefreshIntervalSeconds] = useState(15)
  const [manualRefreshPending, setManualRefreshPending] = useState(false)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [phaseFilter, setPhaseFilter] = useState('all')
  const [restartFilter, setRestartFilter] = useState('all')
  const [pvcFilter, setPvcFilter] = useState('all')
  const [nodeFilter, setNodeFilter] = useState('all')
  const [queryExpanded, setQueryExpanded] = useState(false)
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)

  const podsQuery = useQuery({
    queryKey: ['pods', clusterId, namespace, undefined],
    queryFn: () => api.get<ApiResponse<Pod[]>>(buildClusterScopedPath(clusterId!, 'workloads/pods', namespace)),
    enabled: !!clusterId,
    refetchInterval: autoRefreshEnabled && clusterId ? autoRefreshIntervalSeconds * 1000 : false,
  })

  const data = podsQuery.data
  const isLoading = podsQuery.isLoading
  const isBackgroundRefreshing = podsQuery.isFetching && !podsQuery.isLoading && !manualRefreshPending

  const pods = data?.data ?? []
  const normalizedKeyword = normalizeSearchKeyword(searchKeyword)
  const nodeOptions = useMemo(() => (
    Array.from(new Set(pods.map((item) => item.nodeName).filter(Boolean))).sort()
  ), [pods])

  const filteredPods = useMemo(() => (
    pods.filter((item) => {
      if (phaseFilter !== 'all' && item.phase !== phaseFilter) return false
      if (restartFilter === 'restarting' && item.restarts <= 0) return false
      if (restartFilter === 'clean' && item.restarts > 0) return false
      if (pvcFilter === 'with-pvc' && (item.persistentVolumeClaims?.length ?? 0) === 0) return false
      if (pvcFilter === 'without-pvc' && (item.persistentVolumeClaims?.length ?? 0) > 0) return false
      if (nodeFilter !== 'all' && item.nodeName !== nodeFilter) return false
      return includesSearch([item.name, item.namespace, item.nodeName, item.podIp], normalizedKeyword)
    })
  ), [nodeFilter, normalizedKeyword, phaseFilter, pods, pvcFilter, restartFilter])

  const orderedPods = useMemo(() => (
    [...filteredPods].sort((left, right) => {
      const nameCompare = compareStrings(left.name, right.name)
      if (nameCompare !== 0) return nameCompare
      return compareStrings(left.namespace, right.namespace)
    })
  ), [filteredPods])

  const refreshStatusLabel = manualRefreshPending
    ? (localeCode === 'zh_CN' ? '手动刷新中…' : 'Manual refresh in progress…')
    : isBackgroundRefreshing
      ? (localeCode === 'zh_CN' ? '自动刷新中…' : 'Auto refresh in progress…')
      : clusterId
        ? (localeCode === 'zh_CN'
          ? `更新于 ${formatRefreshTimestamp(podsQuery.dataUpdatedAt, localeCode)}`
          : `Updated at ${formatRefreshTimestamp(podsQuery.dataUpdatedAt, localeCode)}`)
        : (localeCode === 'zh_CN' ? '选择集群后开始刷新' : 'Select a cluster to start refreshing')

  const rebuildPodMutation = useMutation({
    mutationFn: async ({ name, namespace: targetNamespace }: { name: string; namespace: string }) =>
      api.delete(`/clusters/${clusterId}/workloads/pods/${encodeURIComponent(name)}?namespace=${encodeURIComponent(targetNamespace)}`),
    onSuccess: () => {
      void message.success(localeCode === 'zh_CN' ? 'Pod 已删除，控制器将自动重建' : 'Pod deleted. The controller should recreate it automatically')
      void podsQuery.refetch()
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const handleRefresh = async () => {
    if (!clusterId || manualRefreshPending) {
      return
    }
    setManualRefreshPending(true)
    try {
      await podsQuery.refetch()
    } finally {
      setManualRefreshPending(false)
    }
  }

  const columns: TableColumnsType<Pod> = [
    {
      title: 'Pod',
      dataIndex: 'name',
      width: 320,
      ellipsis: { showTitle: false },
      sorter: podSorter((left, right) => {
        const nameCompare = compareStrings(left.name, right.name)
        if (nameCompare !== 0) return nameCompare
        return compareStrings(left.namespace, right.namespace)
      }),
      defaultSortOrder: 'ascend',
      render: (_name: string, record: Pod) => renderPodNameCell(
        record,
        () => navigate(buildWorkloadDetailPath('pods', record.name, namespace, record.namespace)),
        localeCode,
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '运行状态' : 'Runtime',
      dataIndex: 'phase',
      width: 150,
      sorter: podSorter((left, right) => {
        const phaseCompare = compareStrings(left.phase, right.phase)
        if (phaseCompare !== 0) return phaseCompare
        const leftReady = parseReadyContainers(left.readyContainers)
        const rightReady = parseReadyContainers(right.readyContainers)
        if (leftReady.total !== rightReady.total) return leftReady.total - rightReady.total
        return leftReady.ready - rightReady.ready
      }),
      render: (_phase: string, record: Pod) => renderPodRuntimeCell(record),
    },
    {
      title: localeCode === 'zh_CN' ? '重启' : 'Restarts',
      dataIndex: 'restarts',
      width: 64,
      sorter: podSorter((left, right) => left.restarts - right.restarts),
      render: (value: number) => (
        <Tag bordered={false} color={value > 0 ? 'warning' : 'default'}>{value}</Tag>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '资源' : 'Resources',
      key: 'resources',
      width: 132,
      sorter: podSorter((left, right) => {
        const cpuCompare = parseCpuValue(left.cpu) - parseCpuValue(right.cpu)
        if (cpuCompare !== 0) return cpuCompare
        return parseMemoryValue(left.memory) - parseMemoryValue(right.memory)
      }),
      render: (_: unknown, record: Pod) => renderPodResourceSummaryCell(record, localeCode),
    },
    {
      fixed: 'right',
      title: localeCode === 'zh_CN' ? '操作' : 'Actions',
      dataIndex: 'name',
      key: 'actions',
      width: 64,
      align: 'center',
      className: `${TABLE_ACTIONS_COLUMN_CLASS_NAME} soha-pod-actions-column`,
      onHeaderCell: () => ({ className: `${TABLE_ACTIONS_COLUMN_CLASS_NAME} soha-pod-actions-column` }),
      onCell: () => ({ className: `${TABLE_ACTIONS_COLUMN_CLASS_NAME} soha-pod-actions-column` }),
      render: (value: string, record: Pod) => (
        <Space size={4} className="soha-deployment-action-cell">
          <Popconfirm
            title={localeCode === 'zh_CN' ? `确认重建 Pod ${value}？` : `Rebuild pod ${value}?`}
            description={localeCode === 'zh_CN' ? '这会删除当前 Pod，由控制器自动重建。' : 'This deletes the current pod and lets the controller recreate it.'}
            okText={localeCode === 'zh_CN' ? '重建' : 'Rebuild'}
            cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
            okButtonProps={{ danger: true, loading: rebuildPodMutation.isPending }}
            placement="topRight"
            onConfirm={() => rebuildPodMutation.mutate({ name: value, namespace: record.namespace })}
          >
            <ManagementIconButton
              danger
              icon={<DeleteOutlined />}
              aria-label={localeCode === 'zh_CN' ? '重建 Pod' : 'Rebuild Pod'}
              loading={rebuildPodMutation.isPending}
              tooltip={localeCode === 'zh_CN' ? '重建 Pod' : 'Rebuild Pod'}
            />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const podQueryPanel = (
    <WorkloadQueryPanel
      expandable
      expanded={queryExpanded}
      hasActiveFilters={Boolean(searchKeyword.trim()) || phaseFilter !== 'all' || restartFilter !== 'all' || pvcFilter !== 'all' || nodeFilter !== 'all'}
      localeCode={localeCode}
      onExpandedChange={setQueryExpanded}
      onReset={() => {
        setSearchKeyword('')
        setPhaseFilter('all')
        setRestartFilter('all')
        setPvcFilter('all')
        setNodeFilter('all')
        setQueryExpanded(false)
      }}
    >
      <ManagementQueryField label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}>
        <WorkloadSearchInput
          value={searchKeyword}
          onChange={setSearchKeyword}
          placeholder={localeCode === 'zh_CN' ? '搜索 Pod / Node / IP' : 'Search pod / node / IP'}
        />
      </ManagementQueryField>
      <ManagementQueryField label={localeCode === 'zh_CN' ? '状态' : 'Phase'}>
        <Select
          className="soha-platform-compact-field"
          size="small"
          value={phaseFilter}
          variant="filled"
          onChange={(value) => setPhaseFilter(String(value || 'all'))}
          options={[
            { value: 'all', label: localeCode === 'zh_CN' ? '全部状态' : 'All phases' },
            { value: 'Running', label: 'Running' },
            { value: 'Pending', label: 'Pending' },
            { value: 'Succeeded', label: 'Succeeded' },
            { value: 'Failed', label: 'Failed' },
            { value: 'Unknown', label: 'Unknown' },
          ]}
        />
      </ManagementQueryField>
      {queryExpanded ? (
        <>
          <ManagementQueryField label={localeCode === 'zh_CN' ? '重启' : 'Restarts'}>
            <Select
              className="soha-platform-compact-field"
              size="small"
              value={restartFilter}
              variant="filled"
              onChange={(value) => setRestartFilter(String(value || 'all'))}
              options={[
                { value: 'all', label: localeCode === 'zh_CN' ? '全部重启状态' : 'All restart states' },
                { value: 'restarting', label: localeCode === 'zh_CN' ? '仅有重启' : 'Restarted only' },
                { value: 'clean', label: localeCode === 'zh_CN' ? '仅无重启' : 'No restarts' },
              ]}
            />
          </ManagementQueryField>
          <ManagementQueryField label={localeCode === 'zh_CN' ? '存储' : 'Storage'}>
            <Select
              className="soha-platform-compact-field"
              size="small"
              value={pvcFilter}
              variant="filled"
              onChange={(value) => setPvcFilter(String(value || 'all'))}
              options={[
                { value: 'all', label: localeCode === 'zh_CN' ? '全部存储状态' : 'All storage states' },
                { value: 'with-pvc', label: localeCode === 'zh_CN' ? '仅挂载 PVC' : 'With PVC only' },
                { value: 'without-pvc', label: localeCode === 'zh_CN' ? '仅无 PVC' : 'Without PVC' },
              ]}
            />
          </ManagementQueryField>
          <ManagementQueryField label={localeCode === 'zh_CN' ? '节点' : 'Node'}>
            <Select
              className="soha-platform-compact-field"
              size="small"
              value={nodeFilter}
              variant="filled"
              onChange={(value) => setNodeFilter(String(value || 'all'))}
              options={[
                { value: 'all', label: localeCode === 'zh_CN' ? '全部节点' : 'All nodes' },
                ...nodeOptions.map((item) => ({ value: item, label: item })),
              ]}
            />
          </ManagementQueryField>
        </>
      ) : null}
    </WorkloadQueryPanel>
  )

  const podToolbarExtra = (
    <ManagementTableToolbar>
      <Text className="soha-refresh-meta" type="secondary">
        {refreshStatusLabel}
      </Text>
      <div className="soha-refresh-controls">
        <Text className="soha-refresh-meta" type="secondary">
          {localeCode === 'zh_CN' ? '自动刷新' : 'Auto refresh'}
        </Text>
        <Switch
          size="small"
          checked={autoRefreshEnabled}
          onChange={setAutoRefreshEnabled}
          disabled={!clusterId}
        />
        <Select
          className="soha-platform-compact-field"
          size="small"
          value={autoRefreshIntervalSeconds}
          onChange={(value) => setAutoRefreshIntervalSeconds(Number(value))}
          disabled={!clusterId || !autoRefreshEnabled}
          style={{ width: 96 }}
          options={[
            { value: 5, label: localeCode === 'zh_CN' ? '5 秒' : '5s' },
            { value: 15, label: localeCode === 'zh_CN' ? '15 秒' : '15s' },
            { value: 30, label: localeCode === 'zh_CN' ? '30 秒' : '30s' },
          ]}
        />
      </div>
      {densityButton}
      <WorkloadRefreshButton
        label={t('common.refresh', 'Refresh')}
        loading={manualRefreshPending}
        disabled={!clusterId || manualRefreshPending}
        onRefresh={() => void handleRefresh()}
      />
    </ManagementTableToolbar>
  )

  return (
    <div className="soha-page">
      {podQueryPanel}
      <AdminTable
        className="soha-pods-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={podToolbarExtra}
        columns={columns}
        dataSource={orderedPods}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={isLoading}
        paginationSummary={<WorkloadTableSummary filteredCount={orderedPods.length} localeCode={localeCode} totalCount={pods.length} />}
        empty={(
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={orderedPods.length}
            localeCode={localeCode}
            resourceLabel="Pods"
            totalCount={pods.length}
          />
        )}
        pageSize={10}
        tableSize={tableSize}
        scroll={{ x: 730 }}
      />
    </div>
  )
}

export function PodDetailPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const params = useParams()
  const [searchParams] = useSearchParams()
  const podName = params.podName as string
  const { clusterId, namespace } = usePlatformScopeStore()
  const detailNamespace = resolveWorkloadNamespace(namespace, searchParams.get('namespace'))
  const [container, setContainer] = useState<string>('')
  const [terminalShell, setTerminalShell] = useState('/bin/sh')
  const [activeTabKey, setActiveTabKey] = useState('overview')
  const [terminalVisible, setTerminalVisible] = useState(false)
  const [terminalMounted, setTerminalMounted] = useState(false)
  const [metricsRangeMinutes, setMetricsRangeMinutes] = useState(60)

  const podDetailPath = clusterId && detailNamespace
    ? `/clusters/${clusterId}/workloads/pods/${podName}/detail?namespace=${encodeURIComponent(detailNamespace)}`
    : null

  const podDetailQuery = useQuery({
    queryKey: ['pod-detail-meta', clusterId, detailNamespace, podName],
    queryFn: () => api.get<ApiResponse<PodDetail>>(podDetailPath!),
    enabled: !!podDetailPath,
  })

  const podMetricsPath = clusterId && detailNamespace
    ? `/clusters/${clusterId}/workloads/pods/${podName}/metrics?namespace=${encodeURIComponent(detailNamespace)}`
    : null

  const podMetricsQuery = useQuery({
    queryKey: ['pod-metrics', clusterId, detailNamespace, podName, metricsRangeMinutes],
    queryFn: () => api.get<ApiResponse<PodMetrics>>(`${podMetricsPath!}&rangeMinutes=${metricsRangeMinutes}`),
    enabled: !!podMetricsPath && activeTabKey === 'metrics',
  })

  const podEventsQuery = useQuery({
    queryKey: ['pod-events', clusterId, detailNamespace, podName],
    queryFn: async () => {
      const response = await api.get<ApiResponse<WorkloadOverviewEvent[]>>(
        buildClusterScopedPath(clusterId!, 'events', detailNamespace, { limit: 100 }),
      )
      return {
        data: (response.data ?? []).filter((item) =>
          item.involvedName === podName && (!item.involvedKind || item.involvedKind.toLowerCase() === 'pod'),
        ),
      } as ApiResponse<WorkloadOverviewEvent[]>
    },
    enabled: !!clusterId && !!detailNamespace && activeTabKey === 'events',
  })

  const containerOptions = (podDetailQuery.data?.data?.containers ?? []).map((item) => ({
    value: item.name,
    label: item.name,
  }))

  useEffect(() => {
    if (container) return
    if (containerOptions.length > 0) {
      setContainer(String(containerOptions[0].value))
    }
  }, [container, containerOptions])

  const podDetail = podDetailQuery.data?.data
  const podTimelineEvents = useMemo(
    () => (podEventsQuery.data?.data?.length
      ? podEventsQuery.data.data
      : (podDetail?.conditions ?? []).map(conditionToTimelineEvent)),
    [podDetail, podEventsQuery.data],
  )
  const podVolumes = podDetail?.volumes ?? []
  const podRelatedResources = podDetail?.relatedResources ?? []
  const containerSummary = useMemo(() => {
    const containers = podDetail?.containers ?? []
    return {
      total: containers.length,
      ready: containers.filter((item) => item.ready).length,
      restarts: containers.reduce((sum, item) => sum + (item.restartCount || 0), 0),
    }
  }, [podDetail])

  const containerColumns: TableColumnsType<WorkloadContainer> = [
    { title: localeCode === 'zh_CN' ? '容器' : 'Container', dataIndex: 'name' },
    { title: localeCode === 'zh_CN' ? '镜像' : 'Image', dataIndex: 'image', ellipsis: true },
    { title: localeCode === 'zh_CN' ? '就绪' : 'Ready', dataIndex: 'ready', render: (value: boolean) => <BooleanTag value={value} trueLabel="Yes" falseLabel="No" /> },
    { title: localeCode === 'zh_CN' ? '状态' : 'State', dataIndex: 'state', render: (value: string) => formatContainerStateLabel(value) },
    { title: localeCode === 'zh_CN' ? '原因' : 'Reason', dataIndex: 'reason', render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '上次状态' : 'Last State', dataIndex: 'lastState', render: (value: string) => formatContainerStateLabel(value) },
    { title: localeCode === 'zh_CN' ? '重启次数' : 'Restarts', dataIndex: 'restartCount' },
    { title: localeCode === 'zh_CN' ? '启动时间' : 'Started At', dataIndex: 'startedAt', render: (value?: string) => value ? formatDateTime(value) : '-' },
    { title: 'Container ID', dataIndex: 'containerId', ellipsis: true, render: (value?: string) => value || '-' },
  ]

  const volumeColumns: TableColumnsType<PodVolume> = [
    {
      title: localeCode === 'zh_CN' ? '卷名' : 'Volume',
      dataIndex: 'name',
      width: 220,
      render: (value: string, record: PodVolume) => {
        const targetPath = buildVolumeDetailPath(record, detailNamespace)
        if (!targetPath) {
          return <Text className="soha-volume-name-text">{value}</Text>
        }
        return (
          <Link className="soha-volume-name-link" onClick={() => navigate(targetPath)}>
            {value}
          </Link>
        )
      },
    },
    {
      title: 'Type',
      dataIndex: 'type',
      width: 130,
      render: (value: string) => <Tag className="soha-volume-type-tag">{formatVolumeTypeLabel(value)}</Tag>,
    },
    {
      title: localeCode === 'zh_CN' ? '详情' : 'Details',
      width: 300,
      render: (_: unknown, record: PodVolume) => renderVolumeDetail(record, localeCode, detailNamespace, navigate),
    },
    {
      title: localeCode === 'zh_CN' ? 'Volume Mounts' : 'Volume Mounts',
      dataIndex: 'volumeMounts',
      render: (value?: PodVolumeMount[]) => renderVolumeMounts(value),
    },
  ]

  const relatedResourceColumns: TableColumnsType<PodRelatedResource> = [
    {
      title: localeCode === 'zh_CN' ? '资源类型' : 'Kind',
      dataIndex: 'kind',
      width: 150,
      render: (value: string) => <Tag>{localizeRelatedResourceKind(value, localeCode)}</Tag>,
    },
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      render: (value: string, record: PodRelatedResource) => {
        const targetPath = buildRelatedResourcePath(record, detailNamespace)
        if (!targetPath) {
          return value
        }
        return (
          <Link onClick={() => navigate(targetPath)}>
            {value}
          </Link>
        )
      },
    },
    {
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 160,
      render: (value?: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '关联关系' : 'Relations',
      dataIndex: 'relations',
      render: (value?: string[]) => (
        <Space size={[6, 6]} wrap>
          {(value ?? []).map((item) => (
            <Tag key={item}>{localizeRelatedRelation(item, localeCode)}</Tag>
          ))}
        </Space>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '详情' : 'Details',
      dataIndex: 'details',
      render: (value?: string[]) => renderDetailTagList(value, localeCode === 'zh_CN' ? '无' : 'None'),
    },
  ]

  const conditionColumns: TableColumnsType<WorkloadCondition> = [
    { title: localeCode === 'zh_CN' ? '条件' : 'Condition', dataIndex: 'type' },
    { title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
    { title: localeCode === 'zh_CN' ? '原因' : 'Reason', dataIndex: 'reason', render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '消息' : 'Message', dataIndex: 'message', ellipsis: true },
    { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '最近变化' : 'Last Transition', dataIndex: 'lastTransitionTime', render: (value: string) => value ? formatDateTime(value) : '-' },
  ]

  const runtimeOverview = podDetail ? (
    <div className="soha-pod-overview-stack">
      <Card
        className="soha-detail-card soha-pod-overview-card soha-pod-overview-status"
        title={localeCode === 'zh_CN' ? '容器状态' : 'Containers'}
        extra={(
          <Space size={6}>
            <Tag color="blue">{localeCode === 'zh_CN' ? '容器' : 'Containers'} {containerSummary.total}</Tag>
            <Tag color="green">{localeCode === 'zh_CN' ? '就绪' : 'Ready'} {containerSummary.ready}</Tag>
            <Tag color="orange">{localeCode === 'zh_CN' ? '重启' : 'Restarts'} {containerSummary.restarts}</Tag>
          </Space>
        )}
      >
        <AdminTable
          shellClassName="soha-management-table-shell"
          columns={containerColumns}
          dataSource={podDetail.containers ?? []}
          rowKey="name"
          pageSize={10}
          enableColumnSelection={false}
        />
      </Card>
      <Card className="soha-detail-card soha-pod-overview-card soha-pod-overview-summary" title={localeCode === 'zh_CN' ? '运行时概览' : 'Runtime Overview'}>
        <Descriptions
          items={[
            { key: localeCode === 'zh_CN' ? '阶段' : 'Phase', label: localeCode === 'zh_CN' ? '阶段' : 'Phase', children: <StatusTag value={podDetail.phase} /> },
            { key: 'Pod IP', label: 'Pod IP', children: podDetail.podIp || '-' },
            { key: 'Host IP', label: 'Host IP', children: podDetail.hostIp || '-' },
            { key: localeCode === 'zh_CN' ? '节点' : 'Node', label: localeCode === 'zh_CN' ? '节点' : 'Node', children: podDetail.nodeName || '-' },
            { key: localeCode === 'zh_CN' ? '服务账号' : 'ServiceAccount', label: localeCode === 'zh_CN' ? '服务账号' : 'ServiceAccount', children: podDetail.serviceAccountName || '-' },
            { key: 'QoS', label: 'QoS', children: podDetail.qosClass || '-' },
            { key: localeCode === 'zh_CN' ? '启动时间' : 'Started At', label: localeCode === 'zh_CN' ? '启动时间' : 'Started At', children: podDetail.startTime ? formatDateTime(podDetail.startTime) : '-' },
          ]}
        />
      </Card>
      <Card className="soha-detail-card soha-pod-overview-card soha-pod-overview-conditions" title={localeCode === 'zh_CN' ? '条件' : 'Conditions'}>
        <AdminTable
          shellClassName="soha-management-table-shell"
          columns={conditionColumns}
          dataSource={podDetail.conditions ?? []}
          rowKey={(record) => `${record.type}:${record.lastTransitionTime || 'na'}`}
          pageSize={10}
          enableColumnSelection={false}
        />
      </Card>
    </div>
  ) : null

  const metricsTab: NonNullable<TabsProps['items']>[number] = {
    key: 'metrics',
    label: localeCode === 'zh_CN' ? '指标' : 'Metrics',
    children: (
      <ResourceMetricsPanel
        title={localeCode === 'zh_CN' ? 'Pod 指标' : 'Pod Metrics'}
        data={podMetricsQuery.data?.data}
        loading={podMetricsQuery.isLoading}
        rangeMinutes={metricsRangeMinutes}
        onRangeChange={setMetricsRangeMinutes}
        errorMessage={podMetricsQuery.error instanceof Error ? podMetricsQuery.error.message : undefined}
        resourceRequests={podDetail?.requests}
        resourceLimits={podDetail?.limits}
        compact
      />
    ),
  }

  const eventsTab: NonNullable<TabsProps['items']>[number] = {
    key: 'events',
    label: localeCode === 'zh_CN' ? '事件' : 'Events',
    children: (
      <ResourceEventsTimeline
        title={localeCode === 'zh_CN' ? 'Pod 事件时间线' : 'Pod Event Timeline'}
        events={podTimelineEvents}
        loading={podEventsQuery.isLoading}
        emptyDescription={localeCode === 'zh_CN' ? '当前 Pod 暂无事件和条件变化' : 'No pod events or condition transitions'}
      />
    ),
  }

  const containersTab: NonNullable<TabsProps['items']>[number] = {
    key: 'containers',
    label: localeCode === 'zh_CN' ? '容器' : 'Containers',
    children: (
      <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '容器状态' : 'Container Status'}>
        <AdminTable
          shellClassName="soha-management-table-shell"
          columns={containerColumns}
          dataSource={podDetail?.containers ?? []}
          rowKey={(record) => record.name}
          pageSize={10}
          enableColumnSelection={false}
        />
      </Card>
    ),
  }

  const volumesTab: NonNullable<TabsProps['items']>[number] = {
    key: 'volumes',
    label: localeCode === 'zh_CN' ? '卷' : 'Volumes',
    children: (
      <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? 'Pod 卷与挂载' : 'Pod Volumes & Mounts'}>
        <AdminTable
          className="soha-pod-volumes-table"
          shellClassName="soha-management-table-shell"
          columns={volumeColumns}
          dataSource={podVolumes}
          rowKey={(record) => record.name}
          pageSize={10}
          enableColumnSelection={false}
          empty={<ManagementState bordered={false} compact title={localeCode === 'zh_CN' ? '当前 Pod 没有关联卷' : 'No pod volumes found'} />}
        />
      </Card>
    ),
  }

  const relatedResourcesTab: NonNullable<TabsProps['items']>[number] = {
    key: 'related-resources',
    label: localeCode === 'zh_CN' ? '相关资源' : 'Related Resources',
    children: (
      <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? 'Pod 关联资源' : 'Pod Related Resources'}>
        <AdminTable
          shellClassName="soha-management-table-shell"
          columns={relatedResourceColumns}
          dataSource={podRelatedResources}
          rowKey={(record) => `${record.kind}:${record.namespace || 'cluster'}:${record.name}`}
          pageSize={10}
          enableColumnSelection={false}
          empty={<ManagementState bordered={false} compact title={localeCode === 'zh_CN' ? '当前 Pod 暂无可识别的关联资源' : 'No related resources detected for this pod'} />}
        />
      </Card>
    ),
  }

  const logsTab: NonNullable<TabsProps['items']>[number] = {
    key: 'logs',
    label: localeCode === 'zh_CN' ? '日志' : 'Logs',
    children: (
      <Suspense fallback={<Spin size="large" />}>
        <PodLogViewer
          clusterId={clusterId}
          namespace={detailNamespace}
          podName={podName}
          container={container || undefined}
          active={activeTabKey === 'logs'}
          containerOptions={containerOptions}
          onContainerChange={setContainer}
        />
      </Suspense>
    ),
  }

  return (
    <>
      <WorkloadDetailShell
        title="Pod"
        resource="pods"
        paramKey="podName"
        extraOverview={runtimeOverview}
        extraTabPanes={[containersTab, logsTab, eventsTab, volumesTab, relatedResourcesTab, metricsTab]}
        activeTabKey={activeTabKey}
        onTabChange={setActiveTabKey}
        yamlLast
        actions={(
          <Space>
            <Button variant="outlined" onClick={() => setTerminalVisible(true)}>
              {localeCode === 'zh_CN' ? '打开终端' : 'Open Terminal'}
            </Button>
          </Space>
        )}
      />
      <Modal
        title={`Terminal: ${podName}`}
        open={terminalVisible}
        onCancel={() => setTerminalVisible(false)}
        afterOpenChange={setTerminalMounted}
        footer={null}
        width={1080}
      >
        <div className="soha-terminal-controls">
          <div className="soha-terminal-control-group">
            <Text strong className="text-xs">{localeCode === 'zh_CN' ? '容器:' : 'Container:'}</Text>
            <Select
              placeholder={localeCode === 'zh_CN' ? '选择容器' : 'Select container'}
              value={container}
              onChange={(value) => setContainer(String(value || ''))}
              style={{ width: 220 }}
              options={containerOptions}
              allowClear
            />
          </div>
          <div className="soha-terminal-control-group">
            <Text strong className="text-xs">{localeCode === 'zh_CN' ? 'Shell:' : 'Shell:'}</Text>
            <Select
              value={terminalShell}
              onChange={(value) => setTerminalShell(String(value))}
              style={{ width: 180 }}
              options={[
                { value: '/bin/sh', label: '/bin/sh' },
                { value: '/bin/bash', label: '/bin/bash' },
                { value: '/bin/ash', label: '/bin/ash' },
              ]}
            />
          </div>
        </div>
        {terminalMounted ? (
          <Suspense fallback={<Card className="soha-detail-card"><Spin size="large" /></Card>}>
            <PodTerminal
              clusterId={clusterId}
              namespace={detailNamespace}
              podName={podName}
              container={container || undefined}
              shell={terminalShell}
            />
          </Suspense>
        ) : null}
      </Modal>
    </>
  )
}

/* ─── StatefulSets ─── */



export function WorkloadsStatefulSetsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const { data, isLoading } = useScopedQuery<StatefulSet>('statefulsets')
  const [searchKeyword, setSearchKeyword] = useState('')
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)

  const statefulSets = data?.data ?? []
  const filteredStatefulSets = useMemo(() => (
    statefulSets.filter((item) => includesSearch([item.name, item.namespace, item.serviceName], normalizeSearchKeyword(searchKeyword)))
  ), [searchKeyword, statefulSets])

  const columns: TableColumnsType<StatefulSet> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      width: 260,
      ellipsis: { showTitle: false },
      render: (name: string, record: StatefulSet) => (
        renderWorkloadNameLink(name, () => navigate(buildWorkloadDetailPath('statefulsets', name, namespace, record.namespace)))
      ),
    },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace', width: 160 },
    { title: 'Service', dataIndex: 'serviceName', width: 180, ellipsis: { showTitle: true }, render: (value: string) => value || '-' },
    { title: 'Ready', dataIndex: 'readyReplicas', width: 96, render: (_: number, record: StatefulSet) => `${record.readyReplicas}/${record.desiredReplicas}` },
    { title: 'Current', dataIndex: 'currentReplicas', width: 96 },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', width: 104, render: (value: number) => formatAgeSeconds(value) },
  ]

  const { column: statefulSetActions, modalNode: statefulSetYamlModal } = useResourceActions<StatefulSet>({
    resourcePath: 'workloads/statefulsets',
    resourceKind: 'StatefulSet',
    getName: (record) => record.name,
    getNamespace: (record) => record.namespace,
    listInvalidationKey: ['statefulsets'],
  })
  columns.push(statefulSetActions)

  const statefulSetQueryPanel = (
    <WorkloadQueryPanel
      hasActiveFilters={Boolean(searchKeyword.trim())}
      localeCode={localeCode}
      onReset={() => setSearchKeyword('')}
    >
      <ManagementQueryField label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}>
        <WorkloadSearchInput
          value={searchKeyword}
          onChange={setSearchKeyword}
          placeholder={localeCode === 'zh_CN' ? '搜索 StatefulSet / Namespace / Service' : 'Search stateful set / namespace / service'}
        />
      </ManagementQueryField>
    </WorkloadQueryPanel>
  )

  return (
    <div className="soha-page">
      {statefulSetYamlModal}
      {statefulSetQueryPanel}
      <AdminTable
        className="soha-statefulsets-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={(
          <ManagementTableToolbar>
            {densityButton}
            <WorkloadRefreshButton
              disabled={!clusterId}
              label={t('common.refresh', 'Refresh')}
              onRefresh={() => queryClient.invalidateQueries({ queryKey: ['statefulsets', clusterId, namespace] })}
            />
          </ManagementTableToolbar>
        )}
        columns={columns}
        dataSource={filteredStatefulSets}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={isLoading}
        paginationSummary={<WorkloadTableSummary filteredCount={filteredStatefulSets.length} localeCode={localeCode} totalCount={statefulSets.length} />}
        empty={(
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={filteredStatefulSets.length}
            localeCode={localeCode}
            resourceLabel="StatefulSets"
            totalCount={statefulSets.length}
          />
        )}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
      />
    </div>
  )
}

export function StatefulSetDetailPage() {
  return <WorkloadDetailShell title="StatefulSet" resource="statefulsets" paramKey="statefulSetName" />
}

/* ─── DaemonSets ─── */



export function WorkloadsDaemonSetsPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const { data, isLoading } = useScopedQuery<DaemonSet>('daemonsets')
  const [searchKeyword, setSearchKeyword] = useState('')
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)

  const daemonSets = data?.data ?? []
  const filteredDaemonSets = useMemo(() => (
    daemonSets.filter((item) => includesSearch([item.name, item.namespace], normalizeSearchKeyword(searchKeyword)))
  ), [daemonSets, searchKeyword])

  const columns: TableColumnsType<DaemonSet> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      width: 260,
      ellipsis: { showTitle: false },
      render: (name: string, record: DaemonSet) => (
        renderWorkloadNameLink(name, () => navigate(buildWorkloadDetailPath('daemonsets', name, namespace, record.namespace)))
      ),
    },
    { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace', width: 160 },
    { title: 'Desired', dataIndex: 'desiredNumber', width: 96 },
    { title: 'Current', dataIndex: 'currentNumber', width: 96 },
    { title: 'Ready', dataIndex: 'readyNumber', width: 96 },
    { title: 'Available', dataIndex: 'availableNumber', width: 110 },
    { title: 'Updated', dataIndex: 'updatedNumber', width: 96 },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', width: 104, render: (value: number) => formatAgeSeconds(value) },
  ]

  const { column: daemonSetActions, modalNode: daemonSetYamlModal } = useResourceActions<DaemonSet>({
    resourcePath: 'workloads/daemonsets',
    resourceKind: 'DaemonSet',
    getName: (record) => record.name,
    getNamespace: (record) => record.namespace,
    listInvalidationKey: ['daemonsets'],
  })
  columns.push(daemonSetActions)

  const daemonSetQueryPanel = (
    <WorkloadQueryPanel
      hasActiveFilters={Boolean(searchKeyword.trim())}
      localeCode={localeCode}
      onReset={() => setSearchKeyword('')}
    >
      <ManagementQueryField label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}>
        <WorkloadSearchInput
          value={searchKeyword}
          onChange={setSearchKeyword}
          placeholder={localeCode === 'zh_CN' ? '搜索 DaemonSet / Namespace' : 'Search daemon set / namespace'}
        />
      </ManagementQueryField>
    </WorkloadQueryPanel>
  )

  return (
    <>
      {daemonSetYamlModal}
    <div className="soha-page">
      {daemonSetQueryPanel}
      <AdminTable
        className="soha-daemonsets-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={(
          <ManagementTableToolbar>
            {densityButton}
            <WorkloadRefreshButton
              disabled={!clusterId}
              label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              onRefresh={() => queryClient.invalidateQueries({ queryKey: ['daemonsets', clusterId, namespace] })}
            />
          </ManagementTableToolbar>
        )}
        columns={columns}
        dataSource={filteredDaemonSets}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={isLoading}
        paginationSummary={<WorkloadTableSummary filteredCount={filteredDaemonSets.length} localeCode={localeCode} totalCount={daemonSets.length} />}
        empty={(
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={filteredDaemonSets.length}
            localeCode={localeCode}
            resourceLabel="DaemonSets"
            totalCount={daemonSets.length}
          />
        )}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
      />
    </div>
    </>
  )
}

export function DaemonSetDetailPage() {
  return <WorkloadDetailShell title="DaemonSet" resource="daemonsets" paramKey="daemonSetName" />
}

/* ─── Jobs ─── */



export function WorkloadsJobsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const { data, isLoading } = useScopedQuery<Job>('jobs')
  const [searchKeyword, setSearchKeyword] = useState('')
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)

  const jobs = data?.data ?? []
  const filteredJobs = useMemo(() => (
    jobs.filter((item) => includesSearch([item.name, item.namespace, item.completionMode], normalizeSearchKeyword(searchKeyword)))
  ), [jobs, searchKeyword])

  const columns: TableColumnsType<Job> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      width: 260,
      ellipsis: { showTitle: false },
      render: (name: string, record: Job) => (
        renderWorkloadNameLink(name, () => navigate(buildWorkloadDetailPath('jobs', name, namespace, record.namespace)))
      ),
    },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace', width: 160 },
    { title: 'Completions', dataIndex: 'completions', width: 120 },
    { title: 'Succeeded', dataIndex: 'succeeded', width: 104 },
    { title: 'Failed', dataIndex: 'failed', width: 88 },
    { title: 'Active', dataIndex: 'active', width: 88 },
    { title: 'Mode', dataIndex: 'completionMode', width: 140, render: (value: string) => value || '-' },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', width: 104, render: (value: number) => formatAgeSeconds(value) },
  ]

  const { column: jobActions, modalNode: jobYamlModal } = useResourceActions<Job>({
    resourcePath: 'workloads/jobs',
    resourceKind: 'Job',
    getName: (record) => record.name,
    getNamespace: (record) => record.namespace,
    listInvalidationKey: ['jobs'],
  })
  columns.push(jobActions)

  const jobQueryPanel = (
    <WorkloadQueryPanel
      hasActiveFilters={Boolean(searchKeyword.trim())}
      localeCode={localeCode}
      onReset={() => setSearchKeyword('')}
    >
      <ManagementQueryField label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}>
        <WorkloadSearchInput
          value={searchKeyword}
          onChange={setSearchKeyword}
          placeholder={localeCode === 'zh_CN' ? '搜索 Job / Namespace / Mode' : 'Search job / namespace / mode'}
        />
      </ManagementQueryField>
    </WorkloadQueryPanel>
  )

  return (
    <>
    {jobYamlModal}
    <div className="soha-page">
      {jobQueryPanel}
      <AdminTable
        className="soha-jobs-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={(
          <ManagementTableToolbar>
            {densityButton}
            <WorkloadRefreshButton
              disabled={!clusterId}
              label={t('common.refresh', 'Refresh')}
              onRefresh={() => queryClient.invalidateQueries({ queryKey: ['jobs', clusterId, namespace] })}
            />
          </ManagementTableToolbar>
        )}
        columns={columns}
        dataSource={filteredJobs}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={isLoading}
        paginationSummary={<WorkloadTableSummary filteredCount={filteredJobs.length} localeCode={localeCode} totalCount={jobs.length} />}
        empty={(
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={filteredJobs.length}
            localeCode={localeCode}
            resourceLabel="Jobs"
            totalCount={jobs.length}
          />
        )}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
      />
    </div>
    </>
  )
}

export function JobDetailPage() {
  return <WorkloadDetailShell title="Job" resource="jobs" paramKey="jobName" />
}

/* ─── CronJobs ─── */



export function WorkloadsCronJobsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const { data, isLoading } = useScopedQuery<CronJob>('cronjobs')
  const [searchKeyword, setSearchKeyword] = useState('')
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)

  const cronJobs = data?.data ?? []
  const filteredCronJobs = useMemo(() => (
    cronJobs.filter((item) => includesSearch([item.name, item.namespace, item.schedule], normalizeSearchKeyword(searchKeyword)))
  ), [cronJobs, searchKeyword])

  const columns: TableColumnsType<CronJob> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      width: 260,
      ellipsis: { showTitle: false },
      render: (name: string, record: CronJob) => (
        renderWorkloadNameLink(name, () => navigate(buildWorkloadDetailPath('cronjobs', name, namespace, record.namespace)))
      ),
    },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace', width: 160 },
    { title: 'Schedule', dataIndex: 'schedule', width: 180 },
    {
      ...tableColumnPresets.status,
      title: localeCode === 'zh_CN' ? '暂停' : 'Suspend',
      dataIndex: 'suspend',
      width: 96,
      render: (s: boolean) => <BooleanTag value={s} trueLabel="Yes" falseLabel="No" trueColor="orange" falseColor="green" />,
    },
    { title: 'Active', dataIndex: 'activeJobs', width: 88 },
    { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '上次调度' : 'Last Schedule', dataIndex: 'lastScheduleTime', width: 140, render: (t: string) => (t ? formatRelativeTime(t) : '-') },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', width: 104, render: (value: number) => formatAgeSeconds(value) },
  ]

  const { column: cronJobActions, modalNode: cronJobYamlModal } = useResourceActions<CronJob>({
    resourcePath: 'workloads/cronjobs',
    resourceKind: 'CronJob',
    getName: (record) => record.name,
    getNamespace: (record) => record.namespace,
    listInvalidationKey: ['cronjobs'],
  })
  columns.push(cronJobActions)

  const cronJobQueryPanel = (
    <WorkloadQueryPanel
      hasActiveFilters={Boolean(searchKeyword.trim())}
      localeCode={localeCode}
      onReset={() => setSearchKeyword('')}
    >
      <ManagementQueryField label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}>
        <WorkloadSearchInput
          value={searchKeyword}
          onChange={setSearchKeyword}
          placeholder={localeCode === 'zh_CN' ? '搜索 CronJob / Namespace / Schedule' : 'Search cron job / namespace / schedule'}
        />
      </ManagementQueryField>
    </WorkloadQueryPanel>
  )

  return (
    <>
    {cronJobYamlModal}
    <div className="soha-page">
      {cronJobQueryPanel}
      <AdminTable
        className="soha-cronjobs-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={(
          <ManagementTableToolbar>
            {densityButton}
            <WorkloadRefreshButton
              disabled={!clusterId}
              label={t('common.refresh', 'Refresh')}
              onRefresh={() => queryClient.invalidateQueries({ queryKey: ['cronjobs', clusterId, namespace] })}
            />
          </ManagementTableToolbar>
        )}
        columns={columns}
        dataSource={filteredCronJobs}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={isLoading}
        paginationSummary={<WorkloadTableSummary filteredCount={filteredCronJobs.length} localeCode={localeCode} totalCount={cronJobs.length} />}
        empty={(
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={filteredCronJobs.length}
            localeCode={localeCode}
            resourceLabel="CronJobs"
            totalCount={cronJobs.length}
          />
        )}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
      />
    </div>
    </>
  )
}

export function CronJobDetailPage() {
  return <WorkloadDetailShell title="CronJob" resource="cronjobs" paramKey="cronJobName" />
}
