import { lazy, Suspense, useState, useEffect, useMemo } from 'react'
import {
  Tag, Button, Select, Tabs, TabPane, Card, Spin, Empty, Input,
  Descriptions, Typography, Space, Toast, Modal, InputNumber, Tooltip,
} from '@douyinfe/semi-ui'
import { IconDelete, IconEdit, IconRefresh, IconUndo } from '@douyinfe/semi-icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { hasAllowedAction } from '@/features/auth/permission-snapshot'
import { useI18n } from '@/i18n'
import { PageHeader } from '@/components/page-header'
import { PlatformScopeToolbar } from '@/components/platform-scope-toolbar'
import { ResourceEventsTimeline } from '@/components/resource-events-timeline'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { ResourceMetricsPanel } from '@/components/resource-metrics-panel'
import { ResourceProgressCell, formatBytesAsG, formatCpu } from '@/features/platform/node-resource-utils'
import { api } from '@/services/api-client'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { downloadJSON } from '@/utils/download'
import { formatAgeSeconds, formatDateTime, formatRelativeTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse, DeploymentRolloutStatus, PodDetail, PodMetrics, ResourceMetrics, ResourceQuantity, ResourceYAMLView, RolloutHistory, WorkloadCondition, WorkloadContainer } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'
import { StatGrid } from '@/components/stat-grid'

const { Text } = Typography

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

function resolveWorkloadNamespace(selectedNamespace: string | null, searchNamespace: string | null, rowNamespace?: string) {
  if (selectedNamespace && selectedNamespace !== '') return selectedNamespace
  if (searchNamespace) return searchNamespace
  return rowNamespace ?? ''
}

function buildWorkloadDetailPath(resource: string, name: string, selectedNamespace: string | null, rowNamespace: string) {
  const params = new URLSearchParams()
  const resolvedNamespace = resolveWorkloadNamespace(selectedNamespace, null, rowNamespace)
  if (resolvedNamespace) {
    params.set('namespace', resolvedNamespace)
  }
  const query = params.toString()
  return query ? `/workloads/${resource}/${name}?${query}` : `/workloads/${resource}/${name}`
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

function normalizeSearchKeyword(value: string) {
  return value.trim().toLowerCase()
}

function includesSearch(values: Array<string | undefined | null>, keyword: string) {
  if (!keyword) return true
  return values.some((value) => (value ?? '').toLowerCase().includes(keyword))
}

interface WorkloadOverviewEvent {
  name: string
  namespace?: string
  type: string
  reason: string
  involvedKind?: string
  involvedName?: string
  message: string
  count: number
  ageSeconds: number
}

interface ApplicationEnvironment {
  id: string
  applicationId: string
  environmentId: string
  workflowTemplate?: {
    id: string
    name: string
    category?: string
  }
  targets?: Array<{
    id: string
    clusterId: string
    namespace: string
    workloadKind: string
    workloadName: string
    containerName?: string
    enabled: boolean
  }>
}

interface ApplicationSummary {
  id: string
  name: string
  businessLineId?: string
}

interface DeliveryEnvironment {
  id: string
  name: string
  key: string
}

interface BuildRecord {
  id: string
  applicationId: string
  status: string
  createdAt: string
}

interface WorkflowRecord {
  id: string
  applicationId: string
  clusterId: string
  namespace: string
  deploymentName: string
  status: string
  updatedAt: string
}

interface ReleaseRecord {
  id: string
  applicationId: string
  clusterId: string
  namespace: string
  deploymentName: string
  status: string
  createdAt: string
}

function targetMatchesDeployment(
  target: NonNullable<ApplicationEnvironment['targets']>[number] | undefined,
  clusterId: string,
  namespace: string,
  deploymentName: string,
) {
  if (!target) return false
  return target.clusterId === clusterId
    && target.namespace === namespace
    && target.workloadName === deploymentName
    && target.workloadKind.toLowerCase() === 'deployment'
    && target.enabled !== false
}

function selectorMatchesLabels(selector?: Record<string, string>, labels?: Record<string, string>) {
  const entries = Object.entries(selector ?? {})
  if (entries.length === 0) return false
  return entries.every(([key, value]) => (labels ?? {})[key] === value)
}

function conditionToTimelineEvent(condition: WorkloadCondition): WorkloadOverviewEvent {
  const timestamp = condition.lastTransitionTime ? new Date(condition.lastTransitionTime).getTime() : Date.now()
  const ageSeconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000))
  return {
    name: `${condition.type}:${condition.status}`,
    type: condition.status,
    reason: condition.reason || condition.type,
    involvedKind: 'Condition',
    involvedName: condition.type,
    message: condition.message || `${condition.type}: ${condition.status}`,
    count: 1,
    ageSeconds,
  }
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

  if (!clusterId) {
    return (
      <div className="kc-page">
        <PageHeader title={t('page.workloads.overview.title', 'Workload Overview')} description={t('page.workloads.overview.desc', 'Inspect workload counts and recent events under the current cluster and namespace scope.')} />
        <PlatformScopeToolbar />
        <Empty description={t('common.pleaseSelectClusterShort', 'Select a cluster')} />
      </div>
    )
  }

  const stats = [
    { label: 'Deployments', value: deploymentsQuery.data?.data?.length ?? 0 },
    { label: 'Pods', value: podsQuery.data?.data?.length ?? 0 },
    { label: 'StatefulSets', value: statefulSetsQuery.data?.data?.length ?? 0 },
    { label: 'DaemonSets', value: daemonSetsQuery.data?.data?.length ?? 0 },
    { label: 'Jobs', value: jobsQuery.data?.data?.length ?? 0 },
    { label: 'CronJobs', value: cronJobsQuery.data?.data?.length ?? 0 },
  ]

  const eventColumns: ColumnProps<WorkloadOverviewEvent>[] = [
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace', render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '类型' : 'Type', dataIndex: 'type', render: (value: string) => <StatusTag value={value} /> },
    { title: localeCode === 'zh_CN' ? '原因' : 'Reason', dataIndex: 'reason' },
    { title: localeCode === 'zh_CN' ? '对象' : 'Object', dataIndex: 'involvedName', render: (_: string, record: WorkloadOverviewEvent) => `${record.involvedKind || '-'} / ${record.involvedName || '-'}` },
    { title: localeCode === 'zh_CN' ? '消息' : 'Message', dataIndex: 'message', ellipsis: true },
    { title: localeCode === 'zh_CN' ? '次数' : 'Count', dataIndex: 'count' },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.workloads.overview.title', 'Workload Overview')} description={t('page.workloads.overview.desc', 'Inspect workload counts and recent events under the current cluster and namespace scope.')} />
      <PlatformScopeToolbar />
      <StatGrid items={stats} />
      <Card title={localeCode === 'zh_CN' ? '最近事件' : 'Recent Events'}>
        <AdminTable
          columns={eventColumns}
          dataSource={eventsQuery.data?.data ?? []}
          rowKey={(record) => `${record.namespace || ''}/${record.name}`}
          loading={eventsQuery.isLoading}
          pageSize={20}
        />
      </Card>
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

function WorkloadDetailShell({
  title,
  resource,
  paramKey,
  extraTabPanes,
  extraOverview,
  actions,
  activeTabKey,
  onTabChange,
  showScopeToolbar = true,
  yamlLast = false,
}: {
  title: string
  resource: string
  paramKey: string
  extraTabPanes?: React.ReactElement[]
  extraOverview?: React.ReactNode
  actions?: React.ReactNode
  activeTabKey?: string
  onTabChange?: (activeKey: string) => void
  showScopeToolbar?: boolean
  yamlLast?: boolean
}) {
  const { t, localeCode } = useI18n()
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
      Toast.success(t('yamlEditor.applySuccess', 'YAML applied'))
      yamlQuery.refetch()
      detailQuery.refetch()
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const detail = detailQuery.data?.data

  useEffect(() => {
    if (!yamlPath) return
    const draft = yamlDraftStorageKey ? window.localStorage.getItem(yamlDraftStorageKey) : null
    setYamlDraft(draft ?? yamlServerValue)
  }, [yamlDraftStorageKey, yamlPath, yamlServerValue])

  if (detailQuery.isLoading) return <div className="flex items-center justify-center h-64"><Spin size="large" /></div>
  if (!detail) return <Empty description={localeCode === 'zh_CN' ? `${title}未找到` : `${title} not found`} />

  return (
    <div className="kc-page">
      <PageHeader
        title={`${title}: ${name}`}
        description={localeCode === 'zh_CN' ? `查看 ${title} 的资源概览、标签、注解与 YAML 等详情信息。` : `Inspect ${title} overview, labels, annotations, and YAML details.`}
        actions={actions}
      />
      {showScopeToolbar ? <PlatformScopeToolbar /> : null}
      <Tabs
        type="line"
        {...(activeTabKey != null ? { activeKey: activeTabKey } : { defaultActiveKey: 'overview' })}
        onChange={onTabChange}
      >
        <TabPane tab={t('common.overview', 'Overview')} itemKey="overview">
          <Card className="kc-detail-card">
            <Descriptions
              data={[
                { key: t('common.name', 'Name'), value: detail.name },
                { key: t('common.namespace', 'Namespace'), value: detail.namespace },
                { key: t('common.createdAt', 'Created At'), value: detail.createdAt ? formatRelativeTime(detail.createdAt) : '-' },
              ]}
            />
            {detail.labels && Object.keys(detail.labels).length > 0 && (
              <div className="kc-detail-meta">
                <Text strong>{`${t('common.labels', 'Labels')}:`}</Text>
                <div className="kc-tag-list">
                  {Object.entries(detail.labels).map(([k, v]) => (
                    <Tag key={k} size="small">{k}={v}</Tag>
                  ))}
                </div>
              </div>
            )}
            {detail.annotations && Object.keys(detail.annotations).length > 0 && (
              <div className="kc-detail-meta">
                <Text strong>{`${localeCode === 'zh_CN' ? '注解' : 'Annotations'}:`}</Text>
                <pre className="kc-json-block">{JSON.stringify(detail.annotations, null, 2)}</pre>
              </div>
            )}
          </Card>
          {extraOverview}
        </TabPane>
        {yamlLast ? extraTabPanes?.map((tabPane) => tabPane) : null}
        <TabPane tab={t('common.yaml', 'YAML')} itemKey="yaml">
          <Suspense fallback={<Card className="kc-detail-card"><Spin size="large" /></Card>}>
            <K8sYamlEditor
              value={yamlDraft}
              onChange={setYamlDraft}
              onReset={() => {
                if (yamlDraftStorageKey) {
                  window.localStorage.removeItem(yamlDraftStorageKey)
                }
                setYamlDraft(yamlServerValue)
                Toast.success(t('yamlEditor.resetSuccess', 'YAML draft reset'))
              }}
              onSave={() => {
                if (!yamlDraftStorageKey) return
                window.localStorage.setItem(yamlDraftStorageKey, yamlDraft)
                Toast.success(t('yamlEditor.saveSuccess', 'YAML draft saved locally'))
              }}
              onApply={() => applyYamlMutation.mutate()}
              saveDisabled={!yamlDraftStorageKey}
              applyDisabled={!yamlPath || !yamlDraft.trim()}
              applying={applyYamlMutation.isPending}
            />
          </Suspense>
        </TabPane>
        {yamlLast ? null : extraTabPanes?.map((tabPane) => tabPane)}
      </Tabs>
    </div>
  )
}

/* ─── Deployments ─── */

interface Deployment {
  name: string
  namespace: string
  desiredReplicas: number
  readyReplicas: number
  updatedReplicas: number
  available: number
  ageSeconds: number
  allowedActions?: string[]
}

interface DeploymentDetailMeta {
  name: string
  namespace: string
  selector?: Record<string, string>
}

interface BatchRollbackDraft {
  key: string
  name: string
  namespace: string
  options: Array<{ value: string; label: string }>
  revision: string
}

function getDeploymentHealth(deployment: Deployment) {
  if (deployment.desiredReplicas === 0) return 'scaled-down'
  if (
    deployment.readyReplicas >= deployment.desiredReplicas
    && deployment.available >= deployment.desiredReplicas
    && deployment.updatedReplicas >= deployment.desiredReplicas
  ) {
    return 'healthy'
  }
  if (deployment.readyReplicas === 0 && deployment.available === 0) {
    return 'degraded'
  }
  return 'progressing'
}

function DeploymentPodsPanel({
  clusterId,
  deploymentName,
  namespace,
}: {
  clusterId?: string | null
  deploymentName: string
  namespace: string
}) {
  const { localeCode } = useI18n()
  const navigate = useNavigate()

  const deploymentDetailQuery = useQuery({
    queryKey: ['deployment-inline-detail', clusterId, namespace, deploymentName],
    queryFn: () => api.get<ApiResponse<DeploymentDetailMeta>>(
      `/clusters/${clusterId}/workloads/deployments/${deploymentName}/detail?namespace=${encodeURIComponent(namespace)}`,
    ),
    enabled: !!clusterId && !!namespace,
  })

  const podsQuery = useQuery({
    queryKey: ['deployment-inline-pods', clusterId, namespace, deploymentName],
    queryFn: async () => {
      const response = await api.get<ApiResponse<Pod[]>>(
        `/clusters/${clusterId}/workloads/pods?namespace=${encodeURIComponent(namespace)}`,
      )
      const selector = deploymentDetailQuery.data?.data?.selector
      return {
        data: (response.data ?? []).filter((item) => selectorMatchesLabels(selector, item.labels)),
      } as ApiResponse<Pod[]>
    },
    enabled: !!clusterId && !!namespace && !!deploymentDetailQuery.data?.data,
  })

  const columns: ColumnProps<Pod>[] = [
    {
      title: localeCode === 'zh_CN' ? 'Pod' : 'Pod',
      dataIndex: 'name',
      render: (value: string, record: Pod) => (
        <Button
          theme="borderless"
          type="primary"
          onClick={() => navigate(buildWorkloadDetailPath('pods', value, namespace, record.namespace))}
        >
          {value}
        </Button>
      ),
    },
    { title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'phase', render: (value: string) => <StatusTag value={value} /> },
    { title: 'Ready', dataIndex: 'readyContainers' },
    { title: localeCode === 'zh_CN' ? '重启次数' : 'Restarts', dataIndex: 'restarts' },
    { title: localeCode === 'zh_CN' ? '节点' : 'Node', dataIndex: 'nodeName', render: (value: string) => value || '-' },
  ]

  return (
    <div className="kc-page-section">
      <Text strong>{localeCode === 'zh_CN' ? '关联 Pods' : 'Related Pods'}</Text>
      <AdminTable
        columns={columns}
        dataSource={podsQuery.data?.data ?? []}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={deploymentDetailQuery.isLoading || podsQuery.isLoading}
        pageSize={10}
        enableColumnSelection={false}
      />
    </div>
  )
}

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

  const deployments = data?.data ?? []
  const normalizedKeyword = normalizeSearchKeyword(searchKeyword)

  const restartMutation = useMutation({
    mutationFn: ({ name, namespace: targetNamespace }: { name: string; namespace: string }) =>
      api.post(`/clusters/${clusterId}/workloads/deployments/restart`, { namespace: targetNamespace, name }),
    onSuccess: () => {
      Toast.success('已触发重启')
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const scaleMutation = useMutation({
    mutationFn: ({ name, namespace: targetNamespace, replicas }: { name: string; namespace: string; replicas: number }) =>
      api.post(`/clusters/${clusterId}/workloads/deployments/scale`, { namespace: targetNamespace, name, replicas }),
    onSuccess: () => {
      Toast.success('已触发扩缩容')
      setScaleTarget(null)
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const rollbackMutation = useMutation({
    mutationFn: ({ name, namespace: targetNamespace }: { name: string; namespace: string }) =>
      api.post(`/clusters/${clusterId}/workloads/deployments/rollback`, { namespace: targetNamespace, name }),
    onSuccess: () => {
      Toast.success('已触发回滚')
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    },
    onError: (err: Error) => Toast.error(err.message),
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
      Toast.success(failureCount > 0 ? `批量重启完成，成功 ${successCount}，失败 ${failureCount}` : `已批量重启 ${successCount} 个 Deployment`)
      setSelectedDeploymentKeys([])
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    },
    onError: (err: Error) => Toast.error(err.message),
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
      Toast.success(failureCount > 0 ? `批量扩缩完成，成功 ${successCount}，失败 ${failureCount}` : `已批量扩缩 ${successCount} 个 Deployment`)
      setBatchScaleVisible(false)
      setSelectedDeploymentKeys([])
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    },
    onError: (err: Error) => Toast.error(err.message),
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
      Toast.success(failureCount > 0 ? `批量回滚完成，成功 ${successCount}，失败 ${failureCount}` : `已批量回滚 ${successCount} 个 Deployment`)
      setBatchRollbackVisible(false)
      setSelectedDeploymentKeys([])
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    },
    onError: (err: Error) => Toast.error(err.message),
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
      Toast.error(err instanceof Error ? err.message : String(err))
      setBatchRollbackVisible(false)
    } finally {
      setBatchRollbackLoading(false)
    }
  }

  const columns: ColumnProps<Deployment>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (name: string, record: Deployment) => (
        <Button theme="borderless" type="primary" onClick={() => navigate(buildWorkloadDetailPath('deployments', name, namespace, record.namespace))}>
          {name}
        </Button>
      ),
    },
    { title: '命名空间', dataIndex: 'namespace' },
    {
      ...tableColumnPresets.status,
      title: localeCode === 'zh_CN' ? '健康度' : 'Health',
      dataIndex: 'name',
      render: (_: string, record: Deployment) => <StatusTag value={getDeploymentHealth(record)} />,
    },
    { title: 'Ready', dataIndex: 'readyReplicas', render: (_: number, record: Deployment) => `${record.readyReplicas}/${record.desiredReplicas}` },
    { title: 'Up-to-date', dataIndex: 'updatedReplicas' },
    { title: 'Available', dataIndex: 'available' },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
    {
      ...tableColumnPresets.action,
      title: t('common.actions', 'Actions'),
      dataIndex: 'name',
      render: (name: string, record: Deployment) => (
        <Space>
          {hasAllowedAction(record.allowedActions, 'restart') ? (
            <Tooltip content={localeCode === 'zh_CN' ? '重启' : 'Restart'}>
              <Button
                size="small"
                theme="borderless"
                icon={<IconRefresh />}
                aria-label={localeCode === 'zh_CN' ? '重启' : 'Restart'}
                onClick={() => restartMutation.mutate({ name, namespace: record.namespace })}
              />
            </Tooltip>
          ) : null}
          {hasAllowedAction(record.allowedActions, 'scale') ? (
            <Tooltip content={localeCode === 'zh_CN' ? '扩缩' : 'Scale'}>
              <Button
                size="small"
                theme="borderless"
                icon={<IconEdit />}
                aria-label={localeCode === 'zh_CN' ? '扩缩' : 'Scale'}
                onClick={() => setScaleTarget({ name, namespace: record.namespace, replicas: record.desiredReplicas })}
              />
            </Tooltip>
          ) : null}
          {hasAllowedAction(record.allowedActions, 'update') ? (
            <Tooltip content={localeCode === 'zh_CN' ? '回滚' : 'Rollback'}>
              <Button
                size="small"
                theme="borderless"
                icon={<IconUndo />}
                aria-label={localeCode === 'zh_CN' ? '回滚' : 'Rollback'}
                onClick={() => rollbackMutation.mutate({ name, namespace: record.namespace })}
              />
            </Tooltip>
          ) : null}
          {!hasAllowedAction(record.allowedActions, 'restart') && !hasAllowedAction(record.allowedActions, 'scale') && !hasAllowedAction(record.allowedActions, 'update') ? '-' : null}
        </Space>
      ),
    },
  ]

  const deploymentToolbar = (
    <div className="kc-workload-table-filters">
      <PlatformScopeToolbar embedded showLabel={false} clusterWidth={180} namespaceWidth={180} />
      <Input
        className="kc-platform-compact-field"
        size="small"
        value={searchKeyword}
        onChange={setSearchKeyword}
        placeholder={localeCode === 'zh_CN' ? '搜索 Deployment / Namespace' : 'Search deployment or namespace'}
        style={{ width: 260 }}
      />
      <Select
        className="kc-platform-compact-field"
        size="small"
        value={healthFilter}
        onChange={(value) => setHealthFilter(String(value))}
        style={{ width: 190 }}
        optionList={[
          { value: 'all', label: localeCode === 'zh_CN' ? '全部健康状态' : 'All health states' },
          { value: 'healthy', label: localeCode === 'zh_CN' ? '健康' : 'Healthy' },
          { value: 'progressing', label: localeCode === 'zh_CN' ? '进行中' : 'Progressing' },
          { value: 'degraded', label: localeCode === 'zh_CN' ? '异常' : 'Degraded' },
          { value: 'scaled-down', label: localeCode === 'zh_CN' ? '已缩容为 0' : 'Scaled down' },
        ]}
      />
      <Text className="kc-workload-table-summary" type="tertiary" size="small">
        {localeCode === 'zh_CN' ? `当前 ${filteredDeployments.length} / ${deployments.length} 条` : `${filteredDeployments.length} / ${deployments.length} items`}
      </Text>
    </div>
  )

  const deploymentToolbarExtra = (
    <div className="kc-page-toolbar">
      <Button
        theme="light"
        disabled={!canBatchRestart}
        loading={batchRestartMutation.isPending}
        onClick={() => batchRestartMutation.mutate(selectedDeployments)}
      >
        {localeCode === 'zh_CN' ? `批量重启 (${selectedDeployments.length})` : `Batch Restart (${selectedDeployments.length})`}
      </Button>
      <Button
        theme="light"
        disabled={!canBatchRollback}
        loading={batchRollbackLoading}
        onClick={openBatchRollbackModal}
      >
        {localeCode === 'zh_CN' ? `批量回滚 (${selectedDeployments.length})` : `Batch Rollback (${selectedDeployments.length})`}
      </Button>
      <Button
        theme="light"
        disabled={!canBatchScale}
        onClick={() => {
          setBatchScaleReplicas(selectedDeployments[0]?.desiredReplicas ?? 1)
          setBatchScaleVisible(true)
        }}
      >
        {localeCode === 'zh_CN' ? `批量扩缩 (${selectedDeployments.length})` : `Batch Scale (${selectedDeployments.length})`}
      </Button>
      <Button icon={<IconRefresh />} theme="light" onClick={() => queryClient.invalidateQueries({ queryKey: ['deployments', clusterId, namespace] })}>
        {t('common.refresh', 'Refresh')}
      </Button>
    </div>
  )

  return (
    <div className="kc-page">
      <AdminTable
        title={t('page.workloads.deployments.title', 'Deployments')}
        toolbar={deploymentToolbar}
        toolbarExtra={deploymentToolbarExtra}
        columns={columns}
        dataSource={filteredDeployments}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={isLoading}
        expandedRowRender={(record: Deployment) => (
          <DeploymentPodsPanel
            clusterId={clusterId}
            deploymentName={record.name}
            namespace={record.namespace}
          />
        )}
        rowSelection={{
          selectedRowKeys: selectedDeploymentKeys,
          onChange: (selectedRowKeys: string[]) => setSelectedDeploymentKeys(selectedRowKeys),
        }}
      />
      <Modal
        title={localeCode === 'zh_CN' ? '扩缩容' : 'Scale deployment'}
        visible={!!scaleTarget}
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
        visible={batchRollbackVisible}
        onOk={() => batchRollbackMutation.mutate(batchRollbackDrafts)}
        onCancel={() => setBatchRollbackVisible(false)}
        confirmLoading={batchRollbackMutation.isPending}
        width={900}
      >
        {batchRollbackLoading ? (
          <div className="flex items-center justify-center h-48"><Spin size="large" /></div>
        ) : (
          <AdminTable
            columns={[
              { title: localeCode === 'zh_CN' ? 'Deployment' : 'Deployment', dataIndex: 'name' },
              { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace' },
              {
                title: localeCode === 'zh_CN' ? '目标 Revision' : 'Target Revision',
                dataIndex: 'revision',
                render: (_: string, record: BatchRollbackDraft) => (
                  <Select
                    value={record.revision || undefined}
                    optionList={record.options}
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
        visible={batchScaleVisible}
        onOk={() => batchScaleMutation.mutate({ items: selectedDeployments, replicas: batchScaleReplicas })}
        onCancel={() => setBatchScaleVisible(false)}
        confirmLoading={batchScaleMutation.isPending}
      >
        <div className="flex flex-col gap-3">
          <Text type="tertiary">
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
  const environmentsQuery = useQuery({
    queryKey: ['delivery-environments'],
    queryFn: () => api.get<ApiResponse<DeliveryEnvironment[]>>('/delivery-environments'),
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
  const environmentMap = useMemo(
    () => Object.fromEntries((environmentsQuery.data?.data ?? []).map((item) => [item.id, item])),
    [environmentsQuery.data],
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
  const deploymentExportPayload = useMemo(() => ({
    exportedAt: new Date().toISOString(),
    clusterId,
    namespace: detailNamespace,
    deploymentName,
    detail: deploymentDetailQuery.data?.data ?? null,
    rolloutStatus: rolloutStatus ?? null,
    rolloutHistory,
    metrics: metricsQuery.data?.data ?? null,
    events: deploymentEventsQuery.data?.data ?? [],
    pods: deploymentPods,
    bindings: matchedBindings,
  }), [
    clusterId,
    deploymentDetailQuery.data,
    deploymentEventsQuery.data,
    deploymentName,
    deploymentPods,
    detailNamespace,
    matchedBindings,
    metricsQuery.data,
    rolloutHistory,
    rolloutStatus,
  ])

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
      Toast.success(localeCode === 'zh_CN' ? '已触发 Restart Deployment' : 'Restart Deployment triggered')
      queryClient.invalidateQueries({ queryKey: ['deployment-rollout-status', clusterId, detailNamespace, deploymentName] })
      queryClient.invalidateQueries({ queryKey: ['deployment-rollouts', clusterId, detailNamespace, deploymentName] })
      queryClient.invalidateQueries({ queryKey: ['deployments', clusterId, namespace] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const scaleDeploymentMutation = useMutation({
    mutationFn: async () =>
      api.post(`/clusters/${clusterId}/workloads/deployments/scale`, {
        namespace: detailNamespace,
        name: deploymentName,
        replicas: scaleReplicas,
      }),
    onSuccess: () => {
      Toast.success(localeCode === 'zh_CN' ? '已触发扩缩容' : 'Scale triggered')
      setScaleVisible(false)
      queryClient.invalidateQueries({ queryKey: ['deployment-rollout-status', clusterId, detailNamespace, deploymentName] })
      queryClient.invalidateQueries({ queryKey: ['deployment-rollouts', clusterId, detailNamespace, deploymentName] })
      queryClient.invalidateQueries({ queryKey: ['deployments', clusterId, namespace] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const rolloutColumns: ColumnProps<RolloutHistory>[] = [
    { title: localeCode === 'zh_CN' ? 'Revision' : 'Revision', dataIndex: 'revision' },
    { title: localeCode === 'zh_CN' ? '镜像' : 'Images', dataIndex: 'images', render: (value: string[]) => value?.join(', ') || '-' },
    { title: localeCode === 'zh_CN' ? '副本' : 'Replicas', dataIndex: 'replicas' },
    { title: localeCode === 'zh_CN' ? '就绪副本' : 'Ready', dataIndex: 'readyReplicas' },
    { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '创建时间' : 'Created', dataIndex: 'createdAt', render: (value: string) => value ? formatDateTime(value) : '-' },
  ]

  const deploymentPodColumns: ColumnProps<Pod>[] = [
    {
      title: localeCode === 'zh_CN' ? 'Pod' : 'Pod',
      dataIndex: 'name',
      render: (value: string, record: Pod) => (
        <Button
          theme="borderless"
          type="primary"
          onClick={() => navigate(buildWorkloadDetailPath('pods', value, detailNamespace, record.namespace))}
        >
          {value}
        </Button>
      ),
    },
    { title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'phase', render: (value: string) => <StatusTag value={value} /> },
    { title: 'Ready', dataIndex: 'readyContainers' },
    { title: localeCode === 'zh_CN' ? '重启次数' : 'Restarts', dataIndex: 'restarts' },
    { title: localeCode === 'zh_CN' ? '节点' : 'Node', dataIndex: 'nodeName', render: (value: string) => value || '-' },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]

  const linkageOverview = (
    <div className="kc-page-section">
      <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '滚动发布状态' : 'Rollout Status'}>
        {rolloutStatus ? (
          <Descriptions
            data={[
              { key: localeCode === 'zh_CN' ? 'Revision' : 'Revision', value: rolloutStatus.revision || '-' },
              { key: localeCode === 'zh_CN' ? '状态' : 'Status', value: <StatusTag value={rolloutStatus.status} /> },
              { key: localeCode === 'zh_CN' ? '消息' : 'Message', value: rolloutStatus.message || '-' },
              { key: localeCode === 'zh_CN' ? '副本' : 'Desired', value: rolloutStatus.desiredReplicas },
              { key: localeCode === 'zh_CN' ? '更新副本' : 'Updated', value: rolloutStatus.updatedReplicas },
              { key: localeCode === 'zh_CN' ? '就绪副本' : 'Ready', value: rolloutStatus.readyReplicas },
              { key: localeCode === 'zh_CN' ? '可用副本' : 'Available', value: rolloutStatus.availableReplicas },
            ]}
          />
        ) : (
          <Empty description={localeCode === 'zh_CN' ? '暂无滚动状态' : 'No rollout status'} />
        )}
      </Card>
      <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '滚动历史' : 'Rollout History'}>
        <AdminTable
          columns={rolloutColumns}
          dataSource={rolloutHistory}
          rowKey={(record) => record.revision}
          pageSize={10}
          enableColumnSelection={false}
        />
      </Card>
      <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '关联 Pods' : 'Related Pods'}>
        <AdminTable
          columns={deploymentPodColumns}
          dataSource={deploymentPods}
          rowKey={(record) => `${record.namespace}/${record.name}`}
          loading={deploymentPodsQuery.isLoading}
          pageSize={10}
          enableColumnSelection={false}
        />
      </Card>
      <Card className="kc-detail-card" title="交付联动">
        {matchedBindings.length === 0 ? (
          <Empty description="当前 Deployment 尚未绑定到任何应用环境" />
        ) : (
          <div className="kc-list-panel">
            {matchedBindings.map((binding) => {
              const application = applicationMap[binding.applicationId]
              const environment = environmentMap[binding.environmentId]
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
                <div key={binding.id} className="kc-list-row">
                  <div className="kc-list-row-meta">
                    <Text strong>{application?.name || binding.applicationId}</Text>
                    <Tag color="blue">{environment?.name || binding.environmentId}</Tag>
                    {binding.workflowTemplate?.name ? <Tag color="cyan">{binding.workflowTemplate.name}</Tag> : null}
                  </div>
                  <div className="kc-list-row-extra">
                    <StatusTag value={latestBuild?.status || 'unknown'} />
                    <StatusTag value={latestWorkflow?.status || 'unknown'} />
                    <StatusTag value={latestRelease?.status || 'unknown'} />
                    <Text type="tertiary" size="small">
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

  const metricsTab = (
    <TabPane tab={localeCode === 'zh_CN' ? '指标' : 'Metrics'} itemKey="metrics" key="metrics">
      <ResourceMetricsPanel
        title={localeCode === 'zh_CN' ? 'Deployment 指标' : 'Deployment Metrics'}
        data={metricsQuery.data?.data}
        loading={metricsQuery.isLoading}
      />
    </TabPane>
  )

  const eventsTab = (
    <TabPane tab={localeCode === 'zh_CN' ? '事件' : 'Events'} itemKey="events" key="events">
      <ResourceEventsTimeline
        title={localeCode === 'zh_CN' ? 'Deployment 事件时间线' : 'Deployment Event Timeline'}
        events={deploymentTimelineEvents}
        loading={deploymentEventsQuery.isLoading}
        emptyDescription={localeCode === 'zh_CN' ? '当前 Deployment 暂无事件和状态变化' : 'No deployment events or rollout condition transitions'}
      />
    </TabPane>
  )

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
            <Button theme="light" loading={restartDeploymentMutation.isPending} onClick={() => restartDeploymentMutation.mutate()}>
              Restart Deployment
            </Button>
            <Button theme="light" onClick={() => setScaleVisible(true)}>
              {localeCode === 'zh_CN' ? '扩缩容' : 'Scale'}
            </Button>
            <Button
              theme="light"
              onClick={() => downloadJSON(`deployment-diagnostics-${deploymentName}.json`, deploymentExportPayload)}
            >
              {localeCode === 'zh_CN' ? '导出诊断' : 'Export Diagnostics'}
            </Button>
          </Space>
        )}
      />
      <Modal
        title={localeCode === 'zh_CN' ? 'Deployment 扩缩容' : 'Scale deployment'}
        visible={scaleVisible}
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

interface Pod {
  name: string
  namespace: string
  phase: string
  readyContainers: string
  restarts: number
  nodeName: string
  podIp?: string
  cpu?: string
  memory?: string
  requests?: ResourceQuantity
  limits?: ResourceQuantity
  labels?: Record<string, string>
  persistentVolumeClaims?: string[]
  ageSeconds: number
}

function parseReadyContainers(value: string) {
  const [ready = '0', total = '0'] = value.split('/')
  return {
    ready: Number(ready) || 0,
    total: Number(total) || 0,
  }
}

function parseCpuValue(value?: string) {
  if (!value) return -1
  const normalized = value.trim().toLowerCase()
  if (!normalized) return -1
  if (normalized.endsWith('m')) {
    return Number.parseFloat(normalized.slice(0, -1)) / 1000
  }
  const parsed = Number.parseFloat(normalized)
  return Number.isNaN(parsed) ? -1 : parsed
}

function parseMemoryValue(value?: string) {
  if (!value) return -1
  const normalized = value.trim()
  const match = normalized.match(/^([\d.]+)\s*(Ki|Mi|Gi|Ti|Pi|Ei|B)?$/i)
  if (!match) return -1
  const amount = Number.parseFloat(match[1])
  if (Number.isNaN(amount)) return -1
  const unit = (match[2] || 'B').toUpperCase()
  const factors: Record<string, number> = {
    B: 1,
    KI: 1024,
    MI: 1024 ** 2,
    GI: 1024 ** 3,
    TI: 1024 ** 4,
    PI: 1024 ** 5,
    EI: 1024 ** 6,
  }
  return amount * (factors[unit] || 1)
}

function formatCpuDisplay(value?: string) {
  const formatted = formatCpu(value)
  return formatted === '-' ? value || '-' : formatted
}

function formatMemoryDisplay(value?: string) {
  if (!value) return '-'
  const formatted = formatBytesAsG(value.replace(/\s+/g, ''))
  return formatted === '-' ? value : formatted
}

function buildPodResourceSecondary(localeCode: string, requestDisplay: string, limitDisplay: string, hasBaseline: boolean) {
  if (hasBaseline) {
    return localeCode === 'zh_CN'
      ? `请求 ${requestDisplay} · 限制 ${limitDisplay}`
      : `Req ${requestDisplay} · Lim ${limitDisplay}`
  }
  return localeCode === 'zh_CN' ? '未配置 request/limit' : 'No request/limit set'
}

function renderPodResourceCell(record: Pod, resource: 'cpu' | 'memory', localeCode: string) {
  const usageRaw = resource === 'cpu' ? record.cpu : record.memory
  const requestRaw = resource === 'cpu' ? record.requests?.cpu : record.requests?.memory
  const limitRaw = resource === 'cpu' ? record.limits?.cpu : record.limits?.memory
  const parseValue = resource === 'cpu' ? parseCpuValue : parseMemoryValue
  const formatValue = resource === 'cpu' ? formatCpuDisplay : formatMemoryDisplay

  const usageDisplay = formatValue(usageRaw)
  const requestDisplay = formatValue(requestRaw)
  const limitDisplay = formatValue(limitRaw)
  const requestValue = parseValue(requestRaw)
  const limitValue = parseValue(limitRaw)
  const usageValue = parseValue(usageRaw)
  const baselineValue = requestValue > 0 ? requestValue : limitValue > 0 ? limitValue : null
  const baselineDisplay = requestValue > 0 ? requestDisplay : limitDisplay
  const secondary = buildPodResourceSecondary(localeCode, requestDisplay, limitDisplay, baselineValue != null)

  if (usageValue >= 0 && baselineValue != null) {
    return (
      <ResourceProgressCell
        primary={`${usageDisplay} / ${baselineDisplay}`}
        secondary={secondary}
        percent={(usageValue / baselineValue) * 100}
        ariaLabel={`${resource} usage for pod ${record.namespace}/${record.name}`}
        compact
      />
    )
  }

  if (usageDisplay === '-' && baselineValue == null) {
    return '-'
  }

  return (
    <div className="kc-resource-cell is-compact">
      <div className="kc-resource-cell-copy">
        <Text strong>{usageDisplay}</Text>
        <Text type="tertiary" size="small">{secondary}</Text>
      </div>
    </div>
  )
}

function compareStrings(left?: string, right?: string) {
  return (left || '').localeCompare(right || '')
}

function podSorter(compareFn: (left: Pod, right: Pod) => number) {
  return (left?: Pod, right?: Pod) => {
    if (!left && !right) return 0
    if (!left) return -1
    if (!right) return 1
    return compareFn(left, right)
  }
}

export function WorkloadsPodsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const { data, isLoading } = useScopedQuery<Pod>('pods')
  const [searchKeyword, setSearchKeyword] = useState('')
  const [phaseFilter, setPhaseFilter] = useState('all')
  const [restartFilter, setRestartFilter] = useState('all')
  const [pvcFilter, setPvcFilter] = useState('all')
  const [nodeFilter, setNodeFilter] = useState('all')

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

  const rebuildPodMutation = useMutation({
    mutationFn: async ({ name, namespace: targetNamespace }: { name: string; namespace: string }) =>
      api.delete(`/clusters/${clusterId}/workloads/pods/${encodeURIComponent(name)}?namespace=${encodeURIComponent(targetNamespace)}`),
    onSuccess: () => {
      Toast.success(localeCode === 'zh_CN' ? 'Pod 已删除，控制器将自动重建' : 'Pod deleted. The controller should recreate it automatically')
      queryClient.invalidateQueries({ queryKey: ['pods', clusterId, namespace] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const columns: ColumnProps<Pod>[] = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      width: 220,
      ellipsis: { showTitle: false },
      sorter: podSorter((left, right) => {
        const nameCompare = compareStrings(left.name, right.name)
        if (nameCompare !== 0) return nameCompare
        return compareStrings(left.namespace, right.namespace)
      }),
      defaultSortOrder: 'ascend',
      render: (name: string, record: Pod) => (
        <Tooltip content={name} position="topLeft">
          <Text
            link={{ onClick: () => navigate(buildWorkloadDetailPath('pods', name, namespace, record.namespace)) }}
            ellipsis={{ showTooltip: false }}
            style={{ maxWidth: '100%', display: 'block' }}
          >
            {name}
          </Text>
        </Tooltip>
      ),
    },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace', width: 136, ellipsis: { showTitle: true }, sorter: podSorter((left, right) => compareStrings(left.namespace, right.namespace)) },
    {
      ...tableColumnPresets.status,
      title: t('common.status', 'Status'),
      dataIndex: 'phase',
      width: 112,
      sorter: podSorter((left, right) => compareStrings(left.phase, right.phase)),
      render: (s: string) => <StatusTag value={s} />,
    },
    {
      title: 'Ready',
      dataIndex: 'readyContainers',
      width: 80,
      sorter: podSorter((left, right) => {
        const leftReady = parseReadyContainers(left.readyContainers)
        const rightReady = parseReadyContainers(right.readyContainers)
        if (leftReady.ready !== rightReady.ready) return leftReady.ready - rightReady.ready
        return leftReady.total - rightReady.total
      }),
    },
    { title: localeCode === 'zh_CN' ? '重启次数' : 'Restarts', dataIndex: 'restarts', width: 92, sorter: podSorter((left, right) => left.restarts - right.restarts) },
    { title: 'Pod IP', dataIndex: 'podIp', width: 128, render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '节点' : 'Node', dataIndex: 'nodeName', width: 152, ellipsis: { showTitle: true }, sorter: podSorter((left, right) => compareStrings(left.nodeName, right.nodeName)) },
    {
      title: 'CPU',
      dataIndex: 'cpu',
      width: 176,
      sorter: podSorter((left, right) => parseCpuValue(left.cpu) - parseCpuValue(right.cpu)),
      render: (_: string, record: Pod) => renderPodResourceCell(record, 'cpu', localeCode),
    },
    {
      title: localeCode === 'zh_CN' ? '内存' : 'Memory',
      dataIndex: 'memory',
      width: 176,
      sorter: podSorter((left, right) => parseMemoryValue(left.memory) - parseMemoryValue(right.memory)),
      render: (_: string, record: Pod) => renderPodResourceCell(record, 'memory', localeCode),
    },
    { ...tableColumnPresets.datetime, width: 84, title: 'Age', dataIndex: 'ageSeconds', sorter: podSorter((left, right) => left.ageSeconds - right.ageSeconds), render: (value: number) => formatAgeSeconds(value) },
    {
      width: 64,
      align: 'center' as const,
      fixed: 'right',
      title: '',
      dataIndex: 'name',
      render: (value: string, record: Pod) => (
        <Tooltip content={localeCode === 'zh_CN' ? '重建 Pod' : 'Rebuild Pod'}>
          <Button
            size="small"
            theme="borderless"
            type="danger"
            icon={<IconDelete />}
            aria-label={localeCode === 'zh_CN' ? '重建 Pod' : 'Rebuild Pod'}
            loading={rebuildPodMutation.isPending}
            onClick={() => {
              Modal.confirm({
                title: localeCode === 'zh_CN' ? `确认重建 Pod ${value}？` : `Rebuild pod ${value}?`,
                content: localeCode === 'zh_CN' ? '这会删除当前 Pod，由控制器自动重建。' : 'This deletes the current pod and lets the controller recreate it.',
                onOk: () => rebuildPodMutation.mutate({ name: value, namespace: record.namespace }),
              })
            }}
          />
        </Tooltip>
      ),
    },
  ]

  const podToolbar = (
    <div className="kc-workload-table-filters">
      <PlatformScopeToolbar embedded showLabel={false} clusterWidth={180} namespaceWidth={180} />
      <Input
        className="kc-platform-compact-field"
        size="small"
        value={searchKeyword}
        onChange={setSearchKeyword}
        placeholder={localeCode === 'zh_CN' ? '搜索 Pod / Namespace / Node / IP' : 'Search pod / namespace / node / IP'}
        style={{ width: 260 }}
      />
      <Select
        className="kc-platform-compact-field"
        size="small"
        value={phaseFilter}
        onChange={(value) => setPhaseFilter(String(value))}
        style={{ width: 180 }}
        optionList={[
          { value: 'all', label: localeCode === 'zh_CN' ? '全部状态' : 'All phases' },
          { value: 'Running', label: 'Running' },
          { value: 'Pending', label: 'Pending' },
          { value: 'Succeeded', label: 'Succeeded' },
          { value: 'Failed', label: 'Failed' },
          { value: 'Unknown', label: 'Unknown' },
        ]}
      />
      <Select
        className="kc-platform-compact-field"
        size="small"
        value={restartFilter}
        onChange={(value) => setRestartFilter(String(value))}
        style={{ width: 160 }}
        optionList={[
          { value: 'all', label: localeCode === 'zh_CN' ? '全部重启状态' : 'All restart states' },
          { value: 'restarting', label: localeCode === 'zh_CN' ? '仅有重启' : 'Restarted only' },
          { value: 'clean', label: localeCode === 'zh_CN' ? '仅无重启' : 'No restarts' },
        ]}
      />
      <Select
        className="kc-platform-compact-field"
        size="small"
        value={pvcFilter}
        onChange={(value) => setPvcFilter(String(value))}
        style={{ width: 160 }}
        optionList={[
          { value: 'all', label: localeCode === 'zh_CN' ? '全部存储状态' : 'All storage states' },
          { value: 'with-pvc', label: localeCode === 'zh_CN' ? '仅挂载 PVC' : 'With PVC only' },
          { value: 'without-pvc', label: localeCode === 'zh_CN' ? '仅无 PVC' : 'Without PVC' },
        ]}
      />
      <Select
        className="kc-platform-compact-field"
        size="small"
        value={nodeFilter}
        onChange={(value) => setNodeFilter(String(value))}
        style={{ width: 180 }}
        optionList={[
          { value: 'all', label: localeCode === 'zh_CN' ? '全部节点' : 'All nodes' },
          ...nodeOptions.map((item) => ({ value: item, label: item })),
        ]}
      />
      <Text className="kc-workload-table-summary" type="tertiary" size="small">
        {localeCode === 'zh_CN' ? `当前 ${filteredPods.length} / ${pods.length} 条` : `${filteredPods.length} / ${pods.length} items`}
      </Text>
    </div>
  )

  const podToolbarExtra = (
    <div className="kc-page-toolbar">
      <Button icon={<IconRefresh />} theme="light" onClick={() => queryClient.invalidateQueries({ queryKey: ['pods', clusterId, namespace] })}>
        {t('common.refresh', 'Refresh')}
      </Button>
    </div>
  )

  return (
    <div className="kc-page">
      <AdminTable
        className="kc-pods-table"
        title={t('page.workloads.pods.title', 'Pods')}
        toolbar={podToolbar}
        toolbarExtra={podToolbarExtra}
        columns={columns}
        dataSource={orderedPods}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={isLoading}
        pageSize={20}
        scroll={{ x: 1480 }}
      />
    </div>
  )
}

export function PodDetailPage() {
  const { localeCode } = useI18n()
  const params = useParams()
  const [searchParams] = useSearchParams()
  const podName = params.podName as string
  const { clusterId, namespace } = usePlatformScopeStore()
  const detailNamespace = resolveWorkloadNamespace(namespace, searchParams.get('namespace'))
  const [container, setContainer] = useState<string>('')
  const [terminalShell, setTerminalShell] = useState('/bin/sh')
  const [activeTabKey, setActiveTabKey] = useState('overview')
  const [terminalVisible, setTerminalVisible] = useState(false)
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
  const podExportPayload = useMemo(() => ({
    exportedAt: new Date().toISOString(),
    clusterId,
    namespace: detailNamespace,
    podName,
    container: container || null,
    detail: podDetail ?? null,
    metrics: podMetricsQuery.data?.data ?? null,
    events: podEventsQuery.data?.data ?? [],
  }), [clusterId, container, detailNamespace, podDetail, podEventsQuery.data, podMetricsQuery.data, podName])

  const containerColumns: ColumnProps<WorkloadContainer>[] = [
    { title: localeCode === 'zh_CN' ? '容器' : 'Container', dataIndex: 'name' },
    { title: localeCode === 'zh_CN' ? '镜像' : 'Image', dataIndex: 'image', ellipsis: true },
    { title: localeCode === 'zh_CN' ? '就绪' : 'Ready', dataIndex: 'ready', render: (value: boolean) => <BooleanTag value={value} trueLabel="Yes" falseLabel="No" /> },
    { title: localeCode === 'zh_CN' ? '状态' : 'State', dataIndex: 'state', render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '上次状态' : 'Last State', dataIndex: 'lastState', render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '重启次数' : 'Restarts', dataIndex: 'restartCount' },
  ]

  const conditionColumns: ColumnProps<WorkloadCondition>[] = [
    { title: localeCode === 'zh_CN' ? '条件' : 'Condition', dataIndex: 'type' },
    { title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
    { title: localeCode === 'zh_CN' ? '原因' : 'Reason', dataIndex: 'reason', render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '消息' : 'Message', dataIndex: 'message', ellipsis: true },
    { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '最近变化' : 'Last Transition', dataIndex: 'lastTransitionTime', render: (value: string) => value ? formatDateTime(value) : '-' },
  ]

  const runtimeOverview = podDetail ? (
    <>
      <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '运行时概览' : 'Runtime Overview'}>
        <Descriptions
          data={[
            { key: localeCode === 'zh_CN' ? '阶段' : 'Phase', value: <StatusTag value={podDetail.phase} /> },
            { key: 'Pod IP', value: podDetail.podIp || '-' },
            { key: 'Host IP', value: podDetail.hostIp || '-' },
            { key: localeCode === 'zh_CN' ? '节点' : 'Node', value: podDetail.nodeName || '-' },
            { key: localeCode === 'zh_CN' ? '服务账号' : 'ServiceAccount', value: podDetail.serviceAccountName || '-' },
            { key: 'QoS', value: podDetail.qosClass || '-' },
            { key: localeCode === 'zh_CN' ? '启动时间' : 'Started At', value: podDetail.startTime ? formatDateTime(podDetail.startTime) : '-' },
          ]}
        />
      </Card>
      <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '容器状态' : 'Containers'}>
        <AdminTable
          columns={containerColumns}
          dataSource={podDetail.containers ?? []}
          rowKey="name"
          pageSize={10}
          enableColumnSelection={false}
        />
      </Card>
      <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '条件' : 'Conditions'}>
        <AdminTable
          columns={conditionColumns}
          dataSource={podDetail.conditions ?? []}
          rowKey={(record) => `${record.type}:${record.lastTransitionTime || 'na'}`}
          pageSize={10}
          enableColumnSelection={false}
        />
      </Card>
    </>
  ) : null

  const metricsTab = (
    <TabPane tab={localeCode === 'zh_CN' ? '指标' : 'Metrics'} itemKey="metrics" key="metrics">
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
    </TabPane>
  )

  const eventsTab = (
    <TabPane tab={localeCode === 'zh_CN' ? '事件' : 'Events'} itemKey="events" key="events">
      <ResourceEventsTimeline
        title={localeCode === 'zh_CN' ? 'Pod 事件时间线' : 'Pod Event Timeline'}
        events={podTimelineEvents}
        loading={podEventsQuery.isLoading}
        emptyDescription={localeCode === 'zh_CN' ? '当前 Pod 暂无事件和条件变化' : 'No pod events or condition transitions'}
      />
    </TabPane>
  )

  const logsTab = (
    <TabPane tab={localeCode === 'zh_CN' ? '日志' : 'Logs'} itemKey="logs" key="logs">
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
    </TabPane>
  )

  return (
    <>
      <WorkloadDetailShell
        title="Pod"
        resource="pods"
        paramKey="podName"
        extraOverview={runtimeOverview}
        extraTabPanes={[logsTab, eventsTab, metricsTab]}
        activeTabKey={activeTabKey}
        onTabChange={setActiveTabKey}
        showScopeToolbar={false}
        yamlLast
        actions={(
          <Space>
            <Button theme="light" onClick={() => setTerminalVisible(true)}>
              {localeCode === 'zh_CN' ? '打开终端' : 'Open Terminal'}
            </Button>
            <Button
              theme="light"
              onClick={() => downloadJSON(`pod-diagnostics-${podName}.json`, podExportPayload)}
            >
              {localeCode === 'zh_CN' ? '导出诊断' : 'Export Diagnostics'}
            </Button>
          </Space>
        )}
      />
      <Modal
        title={`Terminal: ${podName}`}
        visible={terminalVisible}
        onCancel={() => setTerminalVisible(false)}
        footer={null}
        width={1080}
      >
        <div className="flex items-center gap-2 mb-2">
          <Text strong size="small">{localeCode === 'zh_CN' ? '容器:' : 'Container:'}</Text>
          <Select
            placeholder={localeCode === 'zh_CN' ? '选择容器' : 'Select container'}
            value={container}
            onChange={(value) => setContainer(String(value || ''))}
            style={{ width: 220 }}
            optionList={containerOptions}
            showClear
          />
          <Text strong size="small">{localeCode === 'zh_CN' ? 'Shell:' : 'Shell:'}</Text>
          <Select
            value={terminalShell}
            onChange={(value) => setTerminalShell(String(value))}
            style={{ width: 180 }}
            optionList={[
              { value: '/bin/sh', label: '/bin/sh' },
              { value: '/bin/bash', label: '/bin/bash' },
              { value: '/bin/ash', label: '/bin/ash' },
            ]}
          />
        </div>
        {terminalVisible ? (
          <Suspense fallback={<Card className="kc-detail-card"><Spin size="large" /></Card>}>
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

interface StatefulSet {
  name: string
  namespace: string
  serviceName?: string
  desiredReplicas: number
  readyReplicas: number
  currentReplicas: number
  ageSeconds: number
}

export function WorkloadsStatefulSetsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const { data, isLoading } = useScopedQuery<StatefulSet>('statefulsets')
  const [searchKeyword, setSearchKeyword] = useState('')

  const statefulSets = data?.data ?? []
  const filteredStatefulSets = useMemo(() => (
    statefulSets.filter((item) => includesSearch([item.name, item.namespace, item.serviceName], normalizeSearchKeyword(searchKeyword)))
  ), [searchKeyword, statefulSets])

  const columns: ColumnProps<StatefulSet>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (name: string, record: StatefulSet) => (
        <Button theme="borderless" type="primary" onClick={() => navigate(buildWorkloadDetailPath('statefulsets', name, namespace, record.namespace))}>{name}</Button>
      ),
    },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: 'Service', dataIndex: 'serviceName', render: (value: string) => value || '-' },
    { title: 'Ready', dataIndex: 'readyReplicas', render: (_: number, record: StatefulSet) => `${record.readyReplicas}/${record.desiredReplicas}` },
    { title: 'Current', dataIndex: 'currentReplicas' },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]

  return (
    <div className="kc-page">
      <AdminTable
        title={t('page.workloads.statefulsets.title', 'StatefulSets')}
        toolbar={(
          <div className="kc-workload-table-filters">
            <PlatformScopeToolbar embedded showLabel={false} clusterWidth={180} namespaceWidth={180} />
            <Input
              className="kc-platform-compact-field"
              size="small"
              value={searchKeyword}
              onChange={setSearchKeyword}
              placeholder={localeCode === 'zh_CN' ? '搜索 StatefulSet / Namespace / Service' : 'Search stateful set / namespace / service'}
              style={{ width: 280 }}
            />
            <Text className="kc-workload-table-summary" type="tertiary" size="small">
              {localeCode === 'zh_CN' ? `当前 ${filteredStatefulSets.length} / ${statefulSets.length} 条` : `${filteredStatefulSets.length} / ${statefulSets.length} items`}
            </Text>
          </div>
        )}
        toolbarExtra={(
          <div className="kc-page-toolbar">
            <Button icon={<IconRefresh />} theme="light" onClick={() => queryClient.invalidateQueries({ queryKey: ['statefulsets', clusterId, namespace] })}>
              {t('common.refresh', 'Refresh')}
            </Button>
          </div>
        )}
        columns={columns}
        dataSource={filteredStatefulSets}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={isLoading}
      />
    </div>
  )
}

export function StatefulSetDetailPage() {
  return <WorkloadDetailShell title="StatefulSet" resource="statefulsets" paramKey="statefulSetName" />
}

/* ─── DaemonSets ─── */

interface DaemonSet {
  name: string
  namespace: string
  desiredNumber: number
  currentNumber: number
  readyNumber: number
  availableNumber: number
  updatedNumber: number
  ageSeconds: number
}

export function WorkloadsDaemonSetsPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const { data, isLoading } = useScopedQuery<DaemonSet>('daemonsets')
  const [searchKeyword, setSearchKeyword] = useState('')

  const daemonSets = data?.data ?? []
  const filteredDaemonSets = useMemo(() => (
    daemonSets.filter((item) => includesSearch([item.name, item.namespace], normalizeSearchKeyword(searchKeyword)))
  ), [daemonSets, searchKeyword])

  const columns: ColumnProps<DaemonSet>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (name: string, record: DaemonSet) => (
        <Button theme="borderless" type="primary" onClick={() => navigate(buildWorkloadDetailPath('daemonsets', name, namespace, record.namespace))}>{name}</Button>
      ),
    },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: 'Desired', dataIndex: 'desiredNumber' },
    { title: 'Current', dataIndex: 'currentNumber' },
    { title: 'Ready', dataIndex: 'readyNumber' },
    { title: 'Available', dataIndex: 'availableNumber' },
    { title: 'Updated', dataIndex: 'updatedNumber' },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]

  return (
    <div className="kc-page">
      <AdminTable
        title="DaemonSets"
        toolbar={(
          <div className="kc-workload-table-filters">
            <PlatformScopeToolbar embedded showLabel={false} clusterWidth={180} namespaceWidth={180} />
            <Input
              className="kc-platform-compact-field"
              size="small"
              value={searchKeyword}
              onChange={setSearchKeyword}
              placeholder={localeCode === 'zh_CN' ? '搜索 DaemonSet / Namespace' : 'Search daemon set / namespace'}
              style={{ width: 260 }}
            />
            <Text className="kc-workload-table-summary" type="tertiary" size="small">
              {localeCode === 'zh_CN' ? `当前 ${filteredDaemonSets.length} / ${daemonSets.length} 条` : `${filteredDaemonSets.length} / ${daemonSets.length} items`}
            </Text>
          </div>
        )}
        toolbarExtra={(
          <div className="kc-page-toolbar">
            <Button icon={<IconRefresh />} theme="light" onClick={() => queryClient.invalidateQueries({ queryKey: ['daemonsets', clusterId, namespace] })}>
              {localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
            </Button>
          </div>
        )}
        columns={columns}
        dataSource={filteredDaemonSets}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={isLoading}
      />
    </div>
  )
}

export function DaemonSetDetailPage() {
  return <WorkloadDetailShell title="DaemonSet" resource="daemonsets" paramKey="daemonSetName" />
}

/* ─── Jobs ─── */

interface Job {
  name: string
  namespace: string
  completions: number
  succeeded: number
  failed: number
  active: number
  completionMode?: string
  ageSeconds: number
}

export function WorkloadsJobsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const { data, isLoading } = useScopedQuery<Job>('jobs')
  const [searchKeyword, setSearchKeyword] = useState('')

  const jobs = data?.data ?? []
  const filteredJobs = useMemo(() => (
    jobs.filter((item) => includesSearch([item.name, item.namespace, item.completionMode], normalizeSearchKeyword(searchKeyword)))
  ), [jobs, searchKeyword])

  const columns: ColumnProps<Job>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (name: string, record: Job) => (
        <Button theme="borderless" type="primary" onClick={() => navigate(buildWorkloadDetailPath('jobs', name, namespace, record.namespace))}>{name}</Button>
      ),
    },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: 'Completions', dataIndex: 'completions' },
    { title: 'Succeeded', dataIndex: 'succeeded' },
    { title: 'Failed', dataIndex: 'failed' },
    { title: 'Active', dataIndex: 'active' },
    { title: 'Mode', dataIndex: 'completionMode', render: (value: string) => value || '-' },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]

  return (
    <div className="kc-page">
      <AdminTable
        title={t('page.workloads.jobs.title', 'Jobs')}
        toolbar={(
          <div className="kc-workload-table-filters">
            <PlatformScopeToolbar embedded showLabel={false} clusterWidth={180} namespaceWidth={180} />
            <Input
              className="kc-platform-compact-field"
              size="small"
              value={searchKeyword}
              onChange={setSearchKeyword}
              placeholder={localeCode === 'zh_CN' ? '搜索 Job / Namespace / Mode' : 'Search job / namespace / mode'}
              style={{ width: 260 }}
            />
            <Text className="kc-workload-table-summary" type="tertiary" size="small">
              {localeCode === 'zh_CN' ? `当前 ${filteredJobs.length} / ${jobs.length} 条` : `${filteredJobs.length} / ${jobs.length} items`}
            </Text>
          </div>
        )}
        toolbarExtra={(
          <div className="kc-page-toolbar">
            <Button icon={<IconRefresh />} theme="light" onClick={() => queryClient.invalidateQueries({ queryKey: ['jobs', clusterId, namespace] })}>
              {t('common.refresh', 'Refresh')}
            </Button>
          </div>
        )}
        columns={columns}
        dataSource={filteredJobs}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={isLoading}
      />
    </div>
  )
}

export function JobDetailPage() {
  return <WorkloadDetailShell title="Job" resource="jobs" paramKey="jobName" />
}

/* ─── CronJobs ─── */

interface CronJob {
  name: string
  namespace: string
  schedule: string
  suspend: boolean
  activeJobs: number
  lastScheduleTime?: string
  ageSeconds: number
}

export function WorkloadsCronJobsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const { data, isLoading } = useScopedQuery<CronJob>('cronjobs')
  const [searchKeyword, setSearchKeyword] = useState('')

  const cronJobs = data?.data ?? []
  const filteredCronJobs = useMemo(() => (
    cronJobs.filter((item) => includesSearch([item.name, item.namespace, item.schedule], normalizeSearchKeyword(searchKeyword)))
  ), [cronJobs, searchKeyword])

  const columns: ColumnProps<CronJob>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (name: string, record: CronJob) => (
        <Button theme="borderless" type="primary" onClick={() => navigate(buildWorkloadDetailPath('cronjobs', name, namespace, record.namespace))}>{name}</Button>
      ),
    },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: 'Schedule', dataIndex: 'schedule' },
    {
      ...tableColumnPresets.status,
      title: localeCode === 'zh_CN' ? '暂停' : 'Suspend',
      dataIndex: 'suspend',
      render: (s: boolean) => <BooleanTag value={s} trueLabel="Yes" falseLabel="No" trueColor="orange" falseColor="green" />,
    },
    { title: 'Active', dataIndex: 'activeJobs' },
    { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '上次调度' : 'Last Schedule', dataIndex: 'lastScheduleTime', render: (t: string) => (t ? formatRelativeTime(t) : '-') },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]

  return (
    <div className="kc-page">
      <AdminTable
        title={t('page.workloads.cronjobs.title', 'CronJobs')}
        toolbar={(
          <div className="kc-workload-table-filters">
            <PlatformScopeToolbar embedded showLabel={false} clusterWidth={180} namespaceWidth={180} />
            <Input
              className="kc-platform-compact-field"
              size="small"
              value={searchKeyword}
              onChange={setSearchKeyword}
              placeholder={localeCode === 'zh_CN' ? '搜索 CronJob / Namespace / Schedule' : 'Search cron job / namespace / schedule'}
              style={{ width: 280 }}
            />
            <Text className="kc-workload-table-summary" type="tertiary" size="small">
              {localeCode === 'zh_CN' ? `当前 ${filteredCronJobs.length} / ${cronJobs.length} 条` : `${filteredCronJobs.length} / ${cronJobs.length} items`}
            </Text>
          </div>
        )}
        toolbarExtra={(
          <div className="kc-page-toolbar">
            <Button icon={<IconRefresh />} theme="light" onClick={() => queryClient.invalidateQueries({ queryKey: ['cronjobs', clusterId, namespace] })}>
              {t('common.refresh', 'Refresh')}
            </Button>
          </div>
        )}
        columns={columns}
        dataSource={filteredCronJobs}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={isLoading}
      />
    </div>
  )
}

export function CronJobDetailPage() {
  return <WorkloadDetailShell title="CronJob" resource="cronjobs" paramKey="cronJobName" />
}
