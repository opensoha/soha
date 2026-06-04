import { lazy, Suspense, useDeferredValue, useMemo, useState } from 'react'
import { PlusOutlined, SearchOutlined } from '@ant-design/icons'
import { Button, Card, Descriptions, Input, Spin, Tabs, Tag, Tooltip, Typography, message } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementDetailHeader,
  ManagementDensityButton,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementState,
  ManagementRefreshButton,
  ManagementTableToolbar,
} from '@/components/management-list'
import { PlatformClusterScopeHint } from '@/components/platform-cluster-scope-hint'
import { ResourceEventsTimeline } from '@/components/resource-events-timeline'
import { ResourceMetricsPanel } from '@/components/resource-metrics-panel'
import { useResourceActions } from '@/components/resource-actions'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { hasAllowedAction } from '@/features/auth/permission-snapshot'
import { CreateResourceModal, ResourceMetaOverview, useResourceYAMLState } from '@/features/platform/configuration-detail-pages'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { downloadJSON } from '@/utils/download'
import { formatAgeSeconds } from '@/utils/time'
import type {
  ApiResponse,
  PersistentVolume,
  PersistentVolumeClaim,
  PersistentVolumeClaimDetail,
  PersistentVolumeDetail,
  ResourceMetrics,
  StorageClass,
  StorageClassDetail,
} from '@/types'
import type { TableColumnsType } from 'antd'
import './platform-pages.css'

const { Text } = Typography

function PlatformResourceState({
  description,
  kind,
  title,
}: {
  description: React.ReactNode
  kind?: 'empty' | 'error' | 'not-found' | 'select-scope'
  title?: React.ReactNode
}) {
  return <ManagementState bordered={false} compact description={description} kind={kind ?? 'empty'} title={title} />
}

function resourceEmptyKind(clusterId: string | null | undefined, description: string): 'empty' | 'select-scope' {
  return !clusterId || description.includes('请选择') || description.includes('Select a cluster') ? 'select-scope' : 'empty'
}

const K8sYamlEditor = lazy(async () => {
  const mod = await import('@/components/k8s-yaml-editor')
  return { default: mod.K8sYamlEditor }
})

const PVC_DEFAULT_TEMPLATE = `apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: example-pvc
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
`

const PV_DEFAULT_TEMPLATE = `apiVersion: v1
kind: PersistentVolume
metadata:
  name: example-pv
spec:
  capacity:
    storage: 10Gi
  accessModes:
    - ReadWriteOnce
  persistentVolumeReclaimPolicy: Retain
  hostPath:
    path: /data/example-pv
`

const STORAGE_CLASS_DEFAULT_TEMPLATE = `apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: example-storage-class
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
`

interface Service {
  name: string
  namespace: string
  type: string
  clusterIp: string
  ports: string[]
  selector?: Record<string, string>
  ageSeconds: number
  allowedActions?: string[]
}

interface ServiceBackendPod {
  name: string
  namespace: string
  phase: string
  readyContainers: string
  restarts: number
  nodeName?: string
  podIp?: string
  labels?: Record<string, string>
  ageSeconds: number
}

interface ServiceEvent {
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

interface Ingress {
  name: string
  namespace: string
  className?: string
  hosts: string[]
  address: string
  backendServices?: string[]
  ageSeconds: number
  allowedActions?: string[]
}

interface GatewayClass {
  name: string
  controllerName: string
  accepted?: string
  parametersRef?: string
  ageSeconds: number
  allowedActions?: string[]
}

interface Gateway {
  name: string
  namespace: string
  gatewayClass?: string
  addresses?: string[]
  listenerCount: number
  ageSeconds: number
  allowedActions?: string[]
}

interface EndpointSlice {
  name: string
  namespace: string
  addressType: string
  endpoints: number
  ports?: string[]
  ageSeconds: number
  allowedActions?: string[]
}

interface IngressClass {
  name: string
  controller: string
  isDefault: boolean
  parameters?: string
  ageSeconds: number
  allowedActions?: string[]
}

interface NetworkPolicy {
  name: string
  namespace: string
  policyTypes?: string[]
  ingressRules: number
  egressRules: number
  ageSeconds: number
  allowedActions?: string[]
}

function normalizeSearchKeyword(value: string) {
  return value.trim().toLowerCase()
}

function includesSearch(values: Array<string | undefined | null>, keyword: string) {
  if (!keyword) return true
  return values.some((value) => (value ?? '').toLowerCase().includes(keyword))
}

function StorageQueryPanel({
  localeCode,
  placeholder,
  searchKeyword,
  setSearchKeyword,
}: {
  localeCode: 'zh_CN' | 'en_US'
  placeholder: string
  searchKeyword: string
  setSearchKeyword: (value: string) => void
}) {
  return (
    <ManagementQueryPanel
      onFinish={() => undefined}
      actions={(
        <>
          <Button autoInsertSpace={false} disabled={!searchKeyword.trim()} htmlType="button" onClick={() => setSearchKeyword('')}>
            {localeCode === 'zh_CN' ? '重置' : 'Reset'}
          </Button>
          <Button autoInsertSpace={false} htmlType="submit" type="primary">
            {localeCode === 'zh_CN' ? '查询' : 'Search'}
          </Button>
        </>
      )}
    >
      <ManagementQueryField label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}>
        <Input
          allowClear
          className="soha-platform-compact-field soha-workload-search-input"
          prefix={<SearchOutlined />}
          size="small"
          value={searchKeyword}
          variant="filled"
          onChange={(event) => setSearchKeyword(event.target.value)}
          placeholder={placeholder}
        />
      </ManagementQueryField>
    </ManagementQueryPanel>
  )
}

function buildStorageNamespaceQuery(namespace: string | undefined | null) {
  if (!namespace) return ''
  return `?namespace=${encodeURIComponent(namespace)}`
}

function useResolvedNamespace() {
  const [searchParams] = useSearchParams()
  const { namespace } = usePlatformScopeStore()
  return (namespace && namespace !== '') ? namespace : (searchParams.get('namespace') || '')
}

function buildServiceDetailPath(name: string, selectedNamespace: string | null, rowNamespace: string) {
  const namespace = selectedNamespace && selectedNamespace !== '' ? selectedNamespace : rowNamespace
  const query = buildStorageNamespaceQuery(namespace)
  return `/network/services/${encodeURIComponent(name)}${query}`
}

function buildPodDetailPath(name: string, selectedNamespace: string | null, rowNamespace: string) {
  const namespace = selectedNamespace && selectedNamespace !== '' ? selectedNamespace : rowNamespace
  const query = buildStorageNamespaceQuery(namespace)
  return `/workloads/pods/${encodeURIComponent(name)}${query}`
}

function buildPvcDetailPath(name: string, namespace: string) {
  return `/storage/persistentvolumeclaims/${encodeURIComponent(name)}${buildStorageNamespaceQuery(namespace)}`
}

function selectorMatchesLabels(selector?: Record<string, string>, labels?: Record<string, string>) {
  const entries = Object.entries(selector ?? {})
  if (entries.length === 0) return false
  return entries.every(([key, value]) => (labels ?? {})[key] === value)
}

function useScopedQuery<T>(resource: 'services' | 'ingresses' | 'gateways' | 'persistentvolumeclaims') {
  const { clusterId, namespace } = usePlatformScopeStore()
  const resourcePathMap = {
    services: 'network/services',
    ingresses: 'network/ingresses',
    gateways: 'network/gateways',
    persistentvolumeclaims: 'storage/persistentvolumeclaims',
  } as const

  return useQuery({
    queryKey: [resource, clusterId, namespace],
    queryFn: () => api.get<ApiResponse<T[]>>(buildClusterScopedPath(clusterId!, resourcePathMap[resource], namespace)),
    enabled: !!clusterId,
  })
}

function usePlatformResourceQuery<T>(resourcePath: string, clusterScoped = false) {
  const { clusterId, namespace } = usePlatformScopeStore()
  return useQuery({
    queryKey: ['platform-resource-list', resourcePath, clusterId, clusterScoped ? '' : namespace],
    queryFn: () => api.get<ApiResponse<T[]>>(buildClusterScopedPath(clusterId!, resourcePath, clusterScoped ? undefined : namespace)),
    enabled: !!clusterId,
  })
}

function renderTextList(value?: string[], empty = '-') {
  if (!value || value.length === 0) return <Text type="secondary">{empty}</Text>
  return (
    <div className="soha-rbac-subject-list">
      {value.slice(0, 3).map((item) => <Tag key={item}>{item}</Tag>)}
      {value.length > 3 ? (
        <Tooltip title={value.slice(3).join(', ')}>
          <Tag>{`+${value.length - 3}`}</Tag>
        </Tooltip>
      ) : null}
    </div>
  )
}

function renderConditionStatus(value?: string) {
  if (!value) return <Text type="secondary">-</Text>
  return <StatusTag value={value} />
}

function buildNetworkErrorDescription(localeCode: 'zh_CN' | 'en_US', error: unknown) {
  if (error instanceof Error && error.message.trim()) {
    return localeCode === 'zh_CN'
      ? `网络资源请求失败：${error.message}`
      : `Failed to load network resources: ${error.message}`
  }
  return localeCode === 'zh_CN' ? '网络资源请求失败。' : 'Failed to load network resources.'
}

function NetworkResourceListPage<T extends Record<string, any>>({
  clusterScoped = false,
  columns,
  emptyDescription,
  resourcePath,
  rowKey,
  searchPlaceholder,
  searchValues,
  actionConfig,
}: {
  clusterScoped?: boolean
  columns: TableColumnsType<T>
  emptyDescription: string
  resourcePath: string
  rowKey: string | ((record: T) => string)
  searchPlaceholder: string
  searchValues: (record: T) => Array<string | undefined | null>
  actionConfig?: {
    resourceKind: string
    getName: (record: T) => string
    getNamespace?: (record: T) => string | undefined
  }
}) {
  const { localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)
  const query = usePlatformResourceQuery<T>(resourcePath, clusterScoped)
  const { column: actionColumn, modalNode } = useResourceActions<T>({
    resourcePath,
    resourceKind: actionConfig?.resourceKind ?? 'Resource',
    getName: actionConfig?.getName ?? (() => ''),
    getNamespace: actionConfig?.getNamespace,
    canDelete: (record) => hasAllowedAction(record.allowedActions, 'delete'),
    listInvalidationKey: ['platform-resource-list', resourcePath, clusterId, clusterScoped ? '' : namespace],
  })
  const rawItems = query.data?.data ?? []
  const filteredItems = useMemo(
    () => rawItems.filter((item) => includesSearch(searchValues(item), normalizedKeyword)),
    [normalizedKeyword, rawItems, searchValues],
  )
  const effectiveColumns = actionConfig ? [...columns, actionColumn] : columns
  const effectiveEmpty = !clusterId
    ? (localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster')
    : normalizedKeyword && rawItems.length > 0
      ? (localeCode === 'zh_CN' ? '没有匹配的资源' : 'No matching resources')
      : emptyDescription
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

  return (
    <div className="soha-page">
      {actionConfig ? modalNode : null}
      {query.isError ? (
        <ManagementState
          className="mb-3"
          description={buildNetworkErrorDescription(localeCode, query.error)}
          kind="error"
          title={localeCode === 'zh_CN' ? '网络资源暂时不可用' : 'Network resources unavailable'}
        />
      ) : null}
      <ManagementQueryPanel
        onFinish={() => undefined}
        actions={(
          <>
            <Button autoInsertSpace={false} disabled={!searchKeyword.trim()} htmlType="button" onClick={() => setSearchKeyword('')}>
              {localeCode === 'zh_CN' ? '重置' : 'Reset'}
            </Button>
            <Button autoInsertSpace={false} htmlType="submit" type="primary">
              {localeCode === 'zh_CN' ? '查询' : 'Search'}
            </Button>
          </>
        )}
      >
        <ManagementQueryField label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}>
          <Input
            allowClear
            className="soha-platform-compact-field soha-workload-search-input"
            prefix={<SearchOutlined />}
            size="small"
            value={searchKeyword}
            variant="filled"
            onChange={(event) => setSearchKeyword(event.target.value)}
            placeholder={searchPlaceholder}
          />
        </ManagementQueryField>
      </ManagementQueryPanel>
      <AdminTable
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={effectiveColumns}
        dataSource={clusterId ? filteredItems : []}
        rowKey={rowKey}
        loading={query.isLoading}
        paginationSummary={(
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN' ? `当前 ${filteredItems.length} / ${rawItems.length} 条` : `${filteredItems.length} / ${rawItems.length} items`}
          </Text>
        )}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        headerExtra={(
          <ManagementTableToolbar>
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
            />
            <ManagementRefreshButton
              aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              disabled={!clusterId}
              loading={query.isFetching}
              tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              onClick={() => {
                if (clusterId) {
                  void query.refetch()
                }
              }}
            />
          </ManagementTableToolbar>
        )}
        empty={<PlatformResourceState description={effectiveEmpty} kind={resourceEmptyKind(clusterId, effectiveEmpty)} />}
      />
    </div>
  )
}

function StorageDetailHeader({ title, description, actions }: { title: string; description: string; actions?: React.ReactNode }) {
  return (
    <ManagementDetailHeader
      actions={actions}
      description={description}
      title={title}
    />
  )
}

function StorageYamlTab({ state }: { state: ReturnType<typeof useResourceYAMLState> }) {
  return (
    <Suspense fallback={<Card className="soha-detail-card"><Spin size="large" /></Card>}>
      <div style={{ height: 620 }}>
        <K8sYamlEditor
          value={state.draft}
          onChange={state.setDraft}
          onReset={() => state.setDraft(state.serverValue)}
          onSave={() => void message.info('Local draft save disabled here')}
          onApply={() => state.applyMutation.mutate()}
          saveDisabled
          applyDisabled={!state.draft.trim()}
          applying={state.applyMutation.isPending}
        />
      </div>
    </Suspense>
  )
}

export function NetworkServicesPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId, namespace } = usePlatformScopeStore()
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)
  const query = usePlatformResourceQuery<Service>('network/services')
  const rawItems = query.data?.data ?? []
  const filteredItems = useMemo(
    () => rawItems.filter((item) => includesSearch([item.name, item.namespace, item.type, item.clusterIp, ...(item.ports ?? [])], normalizedKeyword)),
    [normalizedKeyword, rawItems],
  )
  const { column: actionColumn } = useResourceActions<Service>({
    resourcePath: 'network/services',
    resourceKind: 'Service',
    getName: (record) => record.name,
    getNamespace: (record) => record.namespace,
    canDelete: (record) => hasAllowedAction(record.allowedActions, 'delete'),
    listInvalidationKey: ['platform-resource-list', 'network/services', clusterId, namespace],
  })

  const columns: TableColumnsType<Service> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (value: string, record: Service) => (
        <Button type="text" onClick={() => navigate(buildServiceDetailPath(value, namespace, record.namespace))}>{value}</Button>
      ),
    },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: '类型', dataIndex: 'type' },
    { title: 'Cluster IP', dataIndex: 'clusterIp', render: (value: string) => value || '-' },
    { title: '端口', dataIndex: 'ports', render: (value: string[]) => value?.join(', ') || '-' },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
    actionColumn,
  ]
  const searchPlaceholder = localeCode === 'zh_CN' ? '搜索 Service / namespace / type / port' : 'Search service / namespace / type / port'
  const effectiveEmpty = !clusterId
    ? (localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster')
    : normalizedKeyword && rawItems.length > 0
      ? (localeCode === 'zh_CN' ? '没有匹配的 Service' : 'No matching services')
      : (localeCode === 'zh_CN' ? '当前范围没有 Service' : 'No services in the current scope')
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

  return (
    <div className="soha-page">
      {query.isError ? (
        <ManagementState
          className="mb-3"
          description={buildNetworkErrorDescription(localeCode, query.error)}
          kind="error"
          title={localeCode === 'zh_CN' ? '网络资源暂时不可用' : 'Network resources unavailable'}
        />
      ) : null}
      <ManagementQueryPanel
        onFinish={() => undefined}
        actions={(
          <>
            <Button autoInsertSpace={false} disabled={!searchKeyword.trim()} htmlType="button" onClick={() => setSearchKeyword('')}>
              {localeCode === 'zh_CN' ? '重置' : 'Reset'}
            </Button>
            <Button autoInsertSpace={false} htmlType="submit" type="primary">
              {localeCode === 'zh_CN' ? '查询' : 'Search'}
            </Button>
          </>
        )}
      >
        <ManagementQueryField label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}>
          <Input
            allowClear
            className="soha-platform-compact-field soha-workload-search-input"
            prefix={<SearchOutlined />}
            size="small"
            value={searchKeyword}
            variant="filled"
            onChange={(event) => setSearchKeyword(event.target.value)}
            placeholder={searchPlaceholder}
          />
        </ManagementQueryField>
      </ManagementQueryPanel>
      <AdminTable
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={columns}
        dataSource={clusterId ? filteredItems : []}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={query.isLoading}
        paginationSummary={(
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN' ? `当前 ${filteredItems.length} / ${rawItems.length} 条` : `${filteredItems.length} / ${rawItems.length} items`}
          </Text>
        )}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        headerExtra={(
          <ManagementTableToolbar>
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
            />
            <ManagementRefreshButton
              aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              disabled={!clusterId}
              loading={query.isFetching}
              tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              onClick={() => {
                if (clusterId) {
                  void query.refetch()
                }
              }}
            />
          </ManagementTableToolbar>
        )}
        empty={<PlatformResourceState description={effectiveEmpty} kind={resourceEmptyKind(clusterId, effectiveEmpty)} />}
      />
    </div>
  )
}

export function ServiceDetailPage() {
  const { localeCode } = useI18n()
  const params = useParams()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const serviceName = params.serviceName as string
  const { clusterId, namespace } = usePlatformScopeStore()
  const detailNamespace = (namespace && namespace !== '' ? namespace : searchParams.get('namespace')) || ''

  const servicesQuery = useQuery({
    queryKey: ['service-detail-source', clusterId, detailNamespace],
    queryFn: () => api.get<ApiResponse<Service[]>>(buildClusterScopedPath(clusterId!, 'network/services', detailNamespace)),
    enabled: !!clusterId && !!detailNamespace,
  })

  const service = useMemo(
    () => (servicesQuery.data?.data ?? []).find((item) => item.name === serviceName) ?? null,
    [serviceName, servicesQuery.data],
  )

  const backendPodsQuery = useQuery({
    queryKey: ['service-backend-pods', clusterId, detailNamespace, serviceName],
    queryFn: async () => {
      const response = await api.get<ApiResponse<ServiceBackendPod[]>>(`/clusters/${clusterId}/workloads/pods?namespace=${encodeURIComponent(detailNamespace)}`)
      return { data: (response.data ?? []).filter((item) => selectorMatchesLabels(service?.selector, item.labels)) } as ApiResponse<ServiceBackendPod[]>
    },
    enabled: !!clusterId && !!detailNamespace && !!service,
  })

  const metricsQuery = useQuery({
    queryKey: ['service-metrics', clusterId, detailNamespace, serviceName],
    queryFn: () => api.get<ApiResponse<ResourceMetrics>>(`/clusters/${clusterId}/network/services/${serviceName}/metrics?namespace=${encodeURIComponent(detailNamespace)}`),
    enabled: !!clusterId && !!detailNamespace,
  })

  const eventsQuery = useQuery({
    queryKey: ['service-events', clusterId, detailNamespace, serviceName],
    queryFn: async () => {
      const response = await api.get<ApiResponse<ServiceEvent[]>>(buildClusterScopedPath(clusterId!, 'events', detailNamespace, { limit: 100 }))
      return {
        data: (response.data ?? []).filter((item) => item.involvedName === serviceName && (!item.involvedKind || item.involvedKind.toLowerCase() === 'service')),
      } as ApiResponse<ServiceEvent[]>
    },
    enabled: !!clusterId && !!detailNamespace,
  })

  const backendPodColumns: TableColumnsType<ServiceBackendPod> = [
    {
      title: 'Pod',
      dataIndex: 'name',
      render: (value: string, record: ServiceBackendPod) => (
        <Button type="text" onClick={() => navigate(buildPodDetailPath(value, detailNamespace, record.namespace))}>{value}</Button>
      ),
    },
    { title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'phase', render: (value: string) => <StatusTag value={value} /> },
    { title: 'Ready', dataIndex: 'readyContainers' },
    { title: localeCode === 'zh_CN' ? '重启次数' : 'Restarts', dataIndex: 'restarts' },
    { title: localeCode === 'zh_CN' ? '节点' : 'Node', dataIndex: 'nodeName', render: (value?: string) => value || '-' },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]

  const exportPayload = useMemo(() => ({
    exportedAt: new Date().toISOString(),
    clusterId,
    namespace: detailNamespace,
    service: service ?? null,
    backendPods: backendPodsQuery.data?.data ?? [],
    metrics: metricsQuery.data?.data ?? null,
    events: eventsQuery.data?.data ?? [],
  }), [backendPodsQuery.data, clusterId, detailNamespace, eventsQuery.data, metricsQuery.data, service])

  if (!clusterId || !detailNamespace) {
    return <div className="soha-page"><ManagementState kind="select-scope" description={localeCode === 'zh_CN' ? '请选择集群和命名空间' : 'Select a cluster and namespace'} /></div>
  }
  if (servicesQuery.isLoading) return <Card loading className="soha-detail-card" />
  if (!service) return <div className="soha-page"><ManagementState kind="not-found" description={localeCode === 'zh_CN' ? '未找到服务' : 'Service not found'} /></div>

  return (
    <div className="soha-page">
      <StorageDetailHeader
        title={`Service: ${service.name}`}
        description={localeCode === 'zh_CN' ? '查看服务暴露信息、后端 Pod、事件与指标。' : 'Inspect service exposure, backend pods, events, and metrics.'}
        actions={<Button variant="outlined" onClick={() => downloadJSON(`service-diagnostics-${service.name}.json`, exportPayload)}>{localeCode === 'zh_CN' ? '导出诊断' : 'Export Diagnostics'}</Button>}
      />
      <Tabs items={[
        {
          key: 'overview',
          label: localeCode === 'zh_CN' ? '概览' : 'Overview',
          children: (
            <>
              <Card className="soha-detail-card">
                <Descriptions items={[
                  { key: 'name', label: localeCode === 'zh_CN' ? '名称' : 'Name', children: service.name },
                  { key: 'namespace', label: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', children: service.namespace },
                  { key: 'type', label: 'Type', children: service.type },
                  { key: 'clusterIp', label: 'Cluster IP', children: service.clusterIp || '-' },
                  { key: 'ports', label: 'Ports', children: service.ports?.join(', ') || '-' },
                  { key: 'age', label: 'Age', children: formatAgeSeconds(service.ageSeconds) },
                ]} />
              </Card>
              <AdminTable
                className="soha-platform-table"
                columnSettingIconOnly
                columnSettingPlacement="header"
                shellClassName="soha-management-table-shell"
                title={localeCode === 'zh_CN' ? '后端 Pods' : 'Backend Pods'}
                columns={backendPodColumns}
                dataSource={backendPodsQuery.data?.data ?? []}
                rowKey={(record) => `${record.namespace}/${record.name}`}
                loading={backendPodsQuery.isLoading}
                pageSize={10}
                tableSize="small"
                scroll={{ x: 'max-content' }}
              />
            </>
          ),
        },
        { key: 'metrics', label: localeCode === 'zh_CN' ? '指标' : 'Metrics', children: <ResourceMetricsPanel title="Service Metrics" data={metricsQuery.data?.data} loading={metricsQuery.isLoading} /> },
        { key: 'events', label: localeCode === 'zh_CN' ? '事件' : 'Events', children: <ResourceEventsTimeline title="Service Event Timeline" events={eventsQuery.data?.data ?? []} loading={eventsQuery.isLoading} emptyDescription={localeCode === 'zh_CN' ? '当前 Service 暂无事件' : 'No service events'} /> },
      ]} />
    </div>
  )
}

export function NetworkIngressesPage() {
  const { localeCode } = useI18n()
  const columns: TableColumnsType<Ingress> = [
    { title: '名称', dataIndex: 'name' },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: 'IngressClass', dataIndex: 'className', render: (value?: string) => value || '-' },
    { title: 'Hosts', dataIndex: 'hosts', render: (value?: string[]) => renderTextList(value) },
    { title: 'Address', dataIndex: 'address', render: (value?: string) => value || '-' },
    { title: 'Backend Services', dataIndex: 'backendServices', render: (value?: string[]) => renderTextList(value) },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]
  return (
    <NetworkResourceListPage<Ingress>
      resourcePath="network/ingresses"
      columns={columns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      searchPlaceholder={localeCode === 'zh_CN' ? '搜索 Ingress / namespace / host / service' : 'Search ingress / namespace / host / service'}
      searchValues={(record) => [record.name, record.namespace, record.className, record.address, ...(record.hosts ?? []), ...(record.backendServices ?? [])]}
      emptyDescription={localeCode === 'zh_CN' ? '当前范围没有 Ingress' : 'No ingresses in the current scope'}
      actionConfig={{ resourceKind: 'Ingress', getName: (record) => record.name, getNamespace: (record) => record.namespace }}
    />
  )
}

export function NetworkGatewayClassesPage() {
  const { localeCode } = useI18n()
  const columns: TableColumnsType<GatewayClass> = [
    { title: 'Name', dataIndex: 'name' },
    { title: 'Controller', dataIndex: 'controllerName', render: (value?: string) => value || '-' },
    { title: 'Accepted', dataIndex: 'accepted', render: (value?: string) => renderConditionStatus(value) },
    { title: 'Parameters', dataIndex: 'parametersRef', render: (value?: string) => value || '-' },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]
  return (
    <NetworkResourceListPage<GatewayClass>
      clusterScoped
      resourcePath="network/gatewayclasses"
      columns={columns}
      rowKey="name"
      searchPlaceholder={localeCode === 'zh_CN' ? '搜索 GatewayClass / controller' : 'Search GatewayClass / controller'}
      searchValues={(record) => [record.name, record.controllerName, record.accepted, record.parametersRef]}
      emptyDescription={localeCode === 'zh_CN' ? '当前集群没有 GatewayClass，或未安装 Gateway API CRD' : 'No GatewayClasses in this cluster, or Gateway API CRDs are not installed'}
      actionConfig={{ resourceKind: 'GatewayClass', getName: (record) => record.name }}
    />
  )
}

export function NetworkGatewaysPage() {
  const { localeCode } = useI18n()
  const columns: TableColumnsType<Gateway> = [
    { title: '名称', dataIndex: 'name' },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: 'GatewayClass', dataIndex: 'gatewayClass', render: (value?: string) => value || '-' },
    { title: 'Addresses', dataIndex: 'addresses', render: (value?: string[]) => renderTextList(value) },
    { title: 'Listeners', dataIndex: 'listenerCount' },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]
  return (
    <NetworkResourceListPage<Gateway>
      resourcePath="network/gateways"
      columns={columns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      searchPlaceholder={localeCode === 'zh_CN' ? '搜索 Gateway / namespace / class / address' : 'Search gateway / namespace / class / address'}
      searchValues={(record) => [record.name, record.namespace, record.gatewayClass, ...(record.addresses ?? [])]}
      emptyDescription={localeCode === 'zh_CN' ? '当前范围没有 Gateway，或未安装 Gateway API CRD' : 'No Gateways in the current scope, or Gateway API CRDs are not installed'}
      actionConfig={{ resourceKind: 'Gateway', getName: (record) => record.name, getNamespace: (record) => record.namespace }}
    />
  )
}

export function NetworkEndpointSlicesPage() {
  const { localeCode } = useI18n()
  const columns: TableColumnsType<EndpointSlice> = [
    { title: 'Name', dataIndex: 'name' },
    { title: 'Namespace', dataIndex: 'namespace' },
    { title: 'Address Type', dataIndex: 'addressType' },
    { title: 'Endpoints', dataIndex: 'endpoints' },
    { title: 'Ports', dataIndex: 'ports', render: (value?: string[]) => value?.join(', ') || '-' },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]
  return (
    <NetworkResourceListPage<EndpointSlice>
      resourcePath="network/endpointslices"
      columns={columns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      searchPlaceholder={localeCode === 'zh_CN' ? '搜索 EndpointSlice / namespace / address type / port' : 'Search EndpointSlice / namespace / address type / port'}
      searchValues={(record) => [record.name, record.namespace, record.addressType, ...(record.ports ?? [])]}
      emptyDescription={localeCode === 'zh_CN' ? '当前范围没有 EndpointSlice' : 'No EndpointSlices in the current scope'}
      actionConfig={{ resourceKind: 'EndpointSlice', getName: (record) => record.name, getNamespace: (record) => record.namespace }}
    />
  )
}

export function NetworkIngressClassesPage() {
  const { localeCode } = useI18n()
  const columns: TableColumnsType<IngressClass> = [
    { title: 'Name', dataIndex: 'name' },
    { title: 'Controller', dataIndex: 'controller' },
    { title: 'Default', dataIndex: 'isDefault', render: (value: boolean) => <BooleanTag value={value} trueLabel="Yes" falseLabel="No" /> },
    { title: 'Parameters', dataIndex: 'parameters', render: (value?: string) => value || '-' },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]
  return (
    <NetworkResourceListPage<IngressClass>
      clusterScoped
      resourcePath="network/ingressclasses"
      columns={columns}
      rowKey="name"
      searchPlaceholder={localeCode === 'zh_CN' ? '搜索 IngressClass / controller' : 'Search IngressClass / controller'}
      searchValues={(record) => [record.name, record.controller, record.parameters]}
      emptyDescription={localeCode === 'zh_CN' ? '当前集群没有 IngressClass' : 'No IngressClasses in this cluster'}
      actionConfig={{ resourceKind: 'IngressClass', getName: (record) => record.name }}
    />
  )
}

export function NetworkPoliciesPage() {
  const { localeCode } = useI18n()
  const columns: TableColumnsType<NetworkPolicy> = [
    { title: 'Name', dataIndex: 'name' },
    { title: 'Namespace', dataIndex: 'namespace' },
    { title: 'Policy Types', dataIndex: 'policyTypes', render: (value?: string[]) => renderTextList(value) },
    { title: 'Ingress Rules', dataIndex: 'ingressRules' },
    { title: 'Egress Rules', dataIndex: 'egressRules' },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]
  return (
    <NetworkResourceListPage<NetworkPolicy>
      resourcePath="network/networkpolicies"
      columns={columns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      searchPlaceholder={localeCode === 'zh_CN' ? '搜索 NetworkPolicy / namespace / type' : 'Search NetworkPolicy / namespace / type'}
      searchValues={(record) => [record.name, record.namespace, ...(record.policyTypes ?? [])]}
      emptyDescription={localeCode === 'zh_CN' ? '当前范围没有 NetworkPolicy' : 'No NetworkPolicies in the current scope'}
      actionConfig={{ resourceKind: 'NetworkPolicy', getName: (record) => record.name, getNamespace: (record) => record.namespace }}
    />
  )
}

export function StoragePvcPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId } = usePlatformScopeStore()
  const [createVisible, setCreateVisible] = useState(false)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)
  const query = useScopedQuery<PersistentVolumeClaim>('persistentvolumeclaims')
  const rawItems = query.data?.data ?? []
  const filteredItems = useMemo(() => rawItems.filter((item) => includesSearch([item.name, item.namespace, item.status, item.storageClass, item.volumeName], normalizedKeyword)), [normalizedKeyword, rawItems])
  const { column: actionColumn } = useResourceActions<PersistentVolumeClaim>({
    resourcePath: 'storage/persistentvolumeclaims',
    resourceKind: 'PersistentVolumeClaim',
    getName: (record) => record.name,
    getNamespace: (record) => record.namespace,
    canDelete: (record) => hasAllowedAction(record.allowedActions, 'delete'),
  })
  const columns: TableColumnsType<PersistentVolumeClaim> = [
    { title: localeCode === 'zh_CN' ? '名称' : 'Name', dataIndex: 'name', render: (value: string, record: PersistentVolumeClaim) => <Button type="text" onClick={() => navigate(buildPvcDetailPath(value, record.namespace))}>{value}</Button> },
    { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace' },
    { title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
    { title: 'Volume', dataIndex: 'volumeName', render: (value?: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '申请容量' : 'Requested', dataIndex: 'requested', render: (value?: string) => value || '-' },
    { title: 'StorageClass', dataIndex: 'storageClass', render: (value?: string) => value || '-' },
    { title: 'Access Modes', dataIndex: 'accessModes', render: (value?: string[]) => value?.join(', ') || '-' },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
    actionColumn,
  ]
  const searchPlaceholder = localeCode === 'zh_CN' ? '搜索 PVC / namespace / storageClass' : 'Search PVC / namespace / storageClass'
  const effectiveEmpty = !clusterId
    ? (localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster')
    : normalizedKeyword && rawItems.length > 0
      ? (localeCode === 'zh_CN' ? '没有匹配的 PVC' : 'No matching PVCs')
      : (localeCode === 'zh_CN' ? '当前范围没有 PVC' : 'No PVCs in the current scope')
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

  return (
    <div className="soha-page">
      <CreateResourceModal visible={createVisible} onClose={() => setCreateVisible(false)} kind="PersistentVolumeClaim" resourcePath="storage/persistentvolumeclaims" defaultTemplate={PVC_DEFAULT_TEMPLATE} invalidationKeys={[['platform-resource', 'storage/persistentvolumeclaims']]} />
      <StorageQueryPanel
        localeCode={localeCode}
        placeholder={searchPlaceholder}
        searchKeyword={searchKeyword}
        setSearchKeyword={setSearchKeyword}
      />
      <AdminTable
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={columns}
        dataSource={clusterId ? filteredItems : []}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={query.isLoading}
        paginationSummary={(
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN' ? `当前 ${filteredItems.length} / ${rawItems.length} 条` : `${filteredItems.length} / ${rawItems.length} items`}
          </Text>
        )}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        headerExtra={(
          <ManagementTableToolbar>
            <Tooltip title={!clusterId ? (localeCode === 'zh_CN' ? '请先选择集群。' : 'Select a cluster first.') : ''}>
              <span>
                <Button autoInsertSpace={false} size="small" type="primary" icon={<PlusOutlined />} disabled={!clusterId} onClick={() => setCreateVisible(true)}>
                  {localeCode === 'zh_CN' ? '新增' : 'Create'}
                </Button>
              </span>
            </Tooltip>
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
            />
            <ManagementRefreshButton
              aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              disabled={!clusterId}
              loading={query.isFetching}
              tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              onClick={() => {
                if (clusterId) {
                  void query.refetch()
                }
              }}
            />
          </ManagementTableToolbar>
        )}
        empty={<PlatformResourceState description={effectiveEmpty} kind={clusterId ? 'empty' : 'select-scope'} />}
      />
    </div>
  )
}

export function StoragePvPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId } = usePlatformScopeStore()
  const [createVisible, setCreateVisible] = useState(false)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)
  const query = useQuery({ queryKey: ['platform-resource', 'storage/persistentvolumes', clusterId], queryFn: () => api.get<ApiResponse<PersistentVolume[]>>(buildClusterScopedPath(clusterId!, 'storage/persistentvolumes')), enabled: !!clusterId })
  const rawItems = query.data?.data ?? []
  const filteredItems = useMemo(() => rawItems.filter((item) => includesSearch([item.name, item.status, item.storageClass, item.claimRef, item.reclaimPolicy], normalizedKeyword)), [normalizedKeyword, rawItems])
  const { column: actionColumn } = useResourceActions<PersistentVolume>({ resourcePath: 'storage/persistentvolumes', resourceKind: 'PersistentVolume', getName: (record) => record.name, canDelete: (record) => hasAllowedAction(record.allowedActions, 'delete') })
  const columns: TableColumnsType<PersistentVolume> = [
    { title: localeCode === 'zh_CN' ? '名称' : 'Name', dataIndex: 'name', render: (value: string) => <Button type="text" onClick={() => navigate(`/storage/persistentvolumes/${encodeURIComponent(value)}`)}>{value}</Button> },
    { title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
    { title: localeCode === 'zh_CN' ? '容量' : 'Capacity', dataIndex: 'capacity', render: (value?: string) => value || '-' },
    { title: 'StorageClass', dataIndex: 'storageClass', render: (value?: string) => value || '-' },
    { title: 'Claim', dataIndex: 'claimRef', render: (value?: string) => value || '-' },
    { title: 'Access Modes', dataIndex: 'accessModes', render: (value?: string[]) => value?.join(', ') || '-' },
    { title: 'Reclaim Policy', dataIndex: 'reclaimPolicy', render: (value?: string) => value || '-' },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
    actionColumn,
  ]
  const searchPlaceholder = localeCode === 'zh_CN' ? '搜索 PV / claim / storageClass' : 'Search PV / claim / storageClass'
  const effectiveEmpty = !clusterId
    ? (localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster')
    : normalizedKeyword && rawItems.length > 0
      ? (localeCode === 'zh_CN' ? '没有匹配的 PV' : 'No matching PVs')
      : (localeCode === 'zh_CN' ? '当前集群没有 PV' : 'No PVs in this cluster')
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

  return (
    <div className="soha-page">
      <CreateResourceModal visible={createVisible} onClose={() => setCreateVisible(false)} kind="PersistentVolume" resourcePath="storage/persistentvolumes" defaultTemplate={PV_DEFAULT_TEMPLATE} invalidationKeys={[['platform-resource', 'storage/persistentvolumes']]} namespaceScope="cluster" />
      <StorageQueryPanel
        localeCode={localeCode}
        placeholder={searchPlaceholder}
        searchKeyword={searchKeyword}
        setSearchKeyword={setSearchKeyword}
      />
      <AdminTable
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={columns}
        dataSource={clusterId ? filteredItems : []}
        rowKey="name"
        loading={query.isLoading}
        paginationSummary={(
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN' ? `当前 ${filteredItems.length} / ${rawItems.length} 条` : `${filteredItems.length} / ${rawItems.length} items`}
          </Text>
        )}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        headerExtra={(
          <ManagementTableToolbar>
            <Tooltip title={!clusterId ? (localeCode === 'zh_CN' ? '请先选择集群。' : 'Select a cluster first.') : ''}>
              <span>
                <Button autoInsertSpace={false} size="small" type="primary" icon={<PlusOutlined />} disabled={!clusterId} onClick={() => setCreateVisible(true)}>
                  {localeCode === 'zh_CN' ? '新增' : 'Create'}
                </Button>
              </span>
            </Tooltip>
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
            />
            <ManagementRefreshButton
              aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              disabled={!clusterId}
              loading={query.isFetching}
              tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              onClick={() => {
                if (clusterId) {
                  void query.refetch()
                }
              }}
            />
          </ManagementTableToolbar>
        )}
        empty={<PlatformResourceState description={effectiveEmpty} kind={clusterId ? 'empty' : 'select-scope'} />}
      />
    </div>
  )
}

export function StorageClassesPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId } = usePlatformScopeStore()
  const [createVisible, setCreateVisible] = useState(false)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)
  const query = useQuery({ queryKey: ['platform-resource', 'storage/storageclasses', clusterId], queryFn: () => api.get<ApiResponse<StorageClass[]>>(buildClusterScopedPath(clusterId!, 'storage/storageclasses')), enabled: !!clusterId })
  const rawItems = query.data?.data ?? []
  const filteredItems = useMemo(() => rawItems.filter((item) => includesSearch([item.name, item.provisioner, item.reclaimPolicy, item.volumeBindingMode], normalizedKeyword)), [normalizedKeyword, rawItems])
  const { column: actionColumn } = useResourceActions<StorageClass>({ resourcePath: 'storage/storageclasses', resourceKind: 'StorageClass', getName: (record) => record.name, canDelete: (record) => hasAllowedAction(record.allowedActions, 'delete') })
  const columns: TableColumnsType<StorageClass> = [
    { title: localeCode === 'zh_CN' ? '名称' : 'Name', dataIndex: 'name', render: (value: string) => <Button type="text" onClick={() => navigate(`/storage/storageclasses/${encodeURIComponent(value)}`)}>{value}</Button> },
    { title: 'Provisioner', dataIndex: 'provisioner' },
    { title: 'Reclaim Policy', dataIndex: 'reclaimPolicy', render: (value?: string) => value || '-' },
    { title: 'Binding Mode', dataIndex: 'volumeBindingMode', render: (value?: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '允许扩容' : 'Expansion', dataIndex: 'allowVolumeExpansion', render: (value: boolean) => <BooleanTag value={value} trueLabel="Yes" falseLabel="No" /> },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
    actionColumn,
  ]
  const searchPlaceholder = localeCode === 'zh_CN' ? '搜索 StorageClass / provisioner' : 'Search StorageClass / provisioner'
  const effectiveEmpty = !clusterId
    ? (localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster')
    : normalizedKeyword && rawItems.length > 0
      ? (localeCode === 'zh_CN' ? '没有匹配的 StorageClass' : 'No matching storage classes')
      : (localeCode === 'zh_CN' ? '当前集群没有 StorageClass' : 'No storage classes in this cluster')
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

  return (
    <div className="soha-page">
      <CreateResourceModal visible={createVisible} onClose={() => setCreateVisible(false)} kind="StorageClass" resourcePath="storage/storageclasses" defaultTemplate={STORAGE_CLASS_DEFAULT_TEMPLATE} invalidationKeys={[['platform-resource', 'storage/storageclasses']]} namespaceScope="cluster" />
      <StorageQueryPanel
        localeCode={localeCode}
        placeholder={searchPlaceholder}
        searchKeyword={searchKeyword}
        setSearchKeyword={setSearchKeyword}
      />
      <AdminTable
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={columns}
        dataSource={clusterId ? filteredItems : []}
        rowKey="name"
        loading={query.isLoading}
        paginationSummary={(
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN' ? `当前 ${filteredItems.length} / ${rawItems.length} 条` : `${filteredItems.length} / ${rawItems.length} items`}
          </Text>
        )}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        headerExtra={(
          <ManagementTableToolbar>
            <Tooltip title={!clusterId ? (localeCode === 'zh_CN' ? '请先选择集群。' : 'Select a cluster first.') : ''}>
              <span>
                <Button autoInsertSpace={false} size="small" type="primary" icon={<PlusOutlined />} disabled={!clusterId} onClick={() => setCreateVisible(true)}>
                  {localeCode === 'zh_CN' ? '新增' : 'Create'}
                </Button>
              </span>
            </Tooltip>
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
            />
            <ManagementRefreshButton
              aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              disabled={!clusterId}
              loading={query.isFetching}
              tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              onClick={() => {
                if (clusterId) {
                  void query.refetch()
                }
              }}
            />
          </ManagementTableToolbar>
        )}
        empty={<PlatformResourceState description={effectiveEmpty} kind={clusterId ? 'empty' : 'select-scope'} />}
      />
    </div>
  )
}

export function StoragePvcDetailPage() {
  const { localeCode } = useI18n()
  const params = useParams()
  const name = params.name as string
  const detailNamespace = useResolvedNamespace()
  const { clusterId } = usePlatformScopeStore()
  const detailPath = clusterId && detailNamespace ? `/clusters/${clusterId}/storage/persistentvolumeclaims/${encodeURIComponent(name)}/detail?namespace=${encodeURIComponent(detailNamespace)}` : null
  const yamlPath = clusterId && detailNamespace ? `/clusters/${clusterId}/storage/persistentvolumeclaims/${encodeURIComponent(name)}/yaml?namespace=${encodeURIComponent(detailNamespace)}` : null
  const detailQuery = useQuery({ queryKey: ['storage-pvc', 'detail', name, detailNamespace], queryFn: () => api.get<ApiResponse<PersistentVolumeClaimDetail>>(detailPath!), enabled: !!detailPath })
  const yamlState = useResourceYAMLState(yamlPath, 'storage-pvc', name, detailNamespace)
  const detail = detailQuery.data?.data
  if (!clusterId || !detailNamespace) return <div className="soha-page"><ManagementState kind="select-scope" description={localeCode === 'zh_CN' ? '请选择集群和命名空间' : 'Select a cluster and namespace'} /></div>
  if (detailQuery.isLoading) return <Card loading className="soha-detail-card" />
  if (!detail) return <div className="soha-page"><ManagementState kind="not-found" description={localeCode === 'zh_CN' ? 'PVC 未找到' : 'PVC not found'} /></div>
  return (
    <div className="soha-page">
      <StorageDetailHeader title={`PVC: ${detail.name}`} description={localeCode === 'zh_CN' ? '查看 PVC 绑定状态、容量与 YAML。' : 'Inspect PVC binding status, capacity, and YAML.'} />
      <Tabs items={[
        { key: 'overview', label: localeCode === 'zh_CN' ? '概览' : 'Overview', children: <ResourceMetaOverview name={detail.name} namespace={detail.namespace} createdAt={detail.createdAt} labels={detail.labels} annotations={detail.annotations} extra={[{ key: localeCode === 'zh_CN' ? '状态' : 'Status', value: <StatusTag value={detail.status} /> }, { key: 'Volume', value: detail.volumeName || '-' }, { key: 'StorageClass', value: detail.storageClass || '-' }, { key: localeCode === 'zh_CN' ? '申请容量' : 'Requested', value: detail.requested || '-' }, { key: localeCode === 'zh_CN' ? '已分配容量' : 'Capacity', value: detail.capacity || '-' }, { key: 'VolumeMode', value: detail.volumeMode || '-' }, { key: 'AccessModes', value: detail.accessModes?.join(', ') || '-' }]} /> },
        { key: 'yaml', label: 'YAML', children: <StorageYamlTab state={yamlState} /> },
      ]} />
    </div>
  )
}

export function StoragePvDetailPage() {
  const { localeCode } = useI18n()
  const params = useParams()
  const name = params.name as string
  const { clusterId } = usePlatformScopeStore()
  const detailPath = clusterId ? `/clusters/${clusterId}/storage/persistentvolumes/${encodeURIComponent(name)}/detail` : null
  const yamlPath = clusterId ? `/clusters/${clusterId}/storage/persistentvolumes/${encodeURIComponent(name)}/yaml` : null
  const detailQuery = useQuery({ queryKey: ['storage-pv', 'detail', name, clusterId], queryFn: () => api.get<ApiResponse<PersistentVolumeDetail>>(detailPath!), enabled: !!detailPath })
  const yamlState = useResourceYAMLState(yamlPath, 'storage-pv', name, '')
  const detail = detailQuery.data?.data
  if (!clusterId) return <div className="soha-page"><ManagementState kind="select-scope" description={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'} /></div>
  if (detailQuery.isLoading) return <Card loading className="soha-detail-card" />
  if (!detail) return <div className="soha-page"><ManagementState kind="not-found" description={localeCode === 'zh_CN' ? 'PV 未找到' : 'PV not found'} /></div>
  return (
    <div className="soha-page">
      <StorageDetailHeader title={`PV: ${detail.name}`} description={localeCode === 'zh_CN' ? '查看 PV 容量、绑定关系、回收策略与 YAML。' : 'Inspect PV capacity, claim linkage, reclaim policy, and YAML.'} />
      <PlatformClusterScopeHint resourceLabel="PersistentVolume" />
      <Tabs items={[
        { key: 'overview', label: localeCode === 'zh_CN' ? '概览' : 'Overview', children: <ResourceMetaOverview name={detail.name} namespace="-" createdAt={detail.createdAt} labels={detail.labels} annotations={detail.annotations} extra={[{ key: localeCode === 'zh_CN' ? '状态' : 'Status', value: <StatusTag value={detail.status} /> }, { key: localeCode === 'zh_CN' ? '容量' : 'Capacity', value: detail.capacity || '-' }, { key: 'StorageClass', value: detail.storageClass || '-' }, { key: 'Claim', value: detail.claimRef || '-' }, { key: 'AccessModes', value: detail.accessModes?.join(', ') || '-' }, { key: 'ReclaimPolicy', value: detail.reclaimPolicy || '-' }, { key: 'VolumeMode', value: detail.volumeMode || '-' }]} /> },
        { key: 'yaml', label: 'YAML', children: <StorageYamlTab state={yamlState} /> },
      ]} />
    </div>
  )
}

export function StorageClassDetailPage() {
  const { localeCode } = useI18n()
  const params = useParams()
  const name = params.name as string
  const { clusterId } = usePlatformScopeStore()
  const detailPath = clusterId ? `/clusters/${clusterId}/storage/storageclasses/${encodeURIComponent(name)}/detail` : null
  const yamlPath = clusterId ? `/clusters/${clusterId}/storage/storageclasses/${encodeURIComponent(name)}/yaml` : null
  const detailQuery = useQuery({ queryKey: ['storageclass', 'detail', name, clusterId], queryFn: () => api.get<ApiResponse<StorageClassDetail>>(detailPath!), enabled: !!detailPath })
  const yamlState = useResourceYAMLState(yamlPath, 'storageclass', name, '')
  const detail = detailQuery.data?.data
  if (!clusterId) return <div className="soha-page"><ManagementState kind="select-scope" description={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'} /></div>
  if (detailQuery.isLoading) return <Card loading className="soha-detail-card" />
  if (!detail) return <div className="soha-page"><ManagementState kind="not-found" description={localeCode === 'zh_CN' ? 'StorageClass 未找到' : 'StorageClass not found'} /></div>
  return (
    <div className="soha-page">
      <StorageDetailHeader title={`StorageClass: ${detail.name}`} description={localeCode === 'zh_CN' ? '查看 provisioner、绑定模式、参数与 YAML。' : 'Inspect provisioner, binding mode, parameters, and YAML.'} />
      <PlatformClusterScopeHint resourceLabel="StorageClass" />
      <Tabs items={[
        { key: 'overview', label: localeCode === 'zh_CN' ? '概览' : 'Overview', children: <><ResourceMetaOverview name={detail.name} namespace="-" createdAt={detail.createdAt} labels={detail.labels} annotations={detail.annotations} extra={[{ key: 'Provisioner', value: detail.provisioner }, { key: 'ReclaimPolicy', value: detail.reclaimPolicy || '-' }, { key: 'BindingMode', value: detail.volumeBindingMode || '-' }, { key: localeCode === 'zh_CN' ? '允许扩容' : 'Expansion', value: <BooleanTag value={detail.allowVolumeExpansion} trueLabel="Yes" falseLabel="No" /> }]} /><Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '参数' : 'Parameters'}>{detail.parameters && Object.keys(detail.parameters).length > 0 ? <Descriptions column={1} items={Object.entries(detail.parameters).map(([key, value]) => ({ key, label: key, children: value }))} /> : <ManagementState bordered={false} compact description={localeCode === 'zh_CN' ? '暂无参数' : 'No parameters'} />}</Card></> },
        { key: 'yaml', label: 'YAML', children: <StorageYamlTab state={yamlState} /> },
      ]} />
    </div>
  )
}
