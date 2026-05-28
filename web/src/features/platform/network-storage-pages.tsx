import { lazy, Suspense, useDeferredValue, useMemo, useState } from 'react'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Descriptions, Empty, Input, Spin, Tabs, Tag, Tooltip, Typography, message } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
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

const { Text } = Typography

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

interface HTTPRoute {
  name: string
  namespace: string
  hostnames?: string[]
  parentRefs?: string[]
  backendServices?: string[]
  ageSeconds: number
  allowedActions?: string[]
}

interface BackendTLSPolicy {
  name: string
  namespace: string
  targetRefs?: string[]
  hostname?: string
  caCertificateRefs?: string[]
  wellKnownCACertificates?: string
  ageSeconds: number
  allowedActions?: string[]
}

interface GRPCRoute {
  name: string
  namespace: string
  hostnames?: string[]
  parentRefs?: string[]
  backendServices?: string[]
  ruleCount: number
  ageSeconds: number
  allowedActions?: string[]
}

interface ReferenceGrant {
  name: string
  namespace: string
  from?: string[]
  to?: string[]
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
    <div className="kc-rbac-subject-list">
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

function NetworkListTitle({ title, description, clusterScopedHint }: { title: string; description: string; clusterScopedHint?: string }) {
  return (
    <div className="kc-admin-table-title-block">
      <Text strong>{title}</Text>
      <Text type="secondary">{description}</Text>
      {clusterScopedHint ? <Text type="secondary">{clusterScopedHint}</Text> : null}
    </div>
  )
}

function NetworkResourceListPage<T extends Record<string, any>>({
  clusterScoped = false,
  columns,
  description,
  emptyDescription,
  resourcePath,
  rowKey,
  searchPlaceholder,
  searchValues,
  title,
  actionConfig,
}: {
  clusterScoped?: boolean
  columns: TableColumnsType<T>
  description: string
  emptyDescription: string
  resourcePath: string
  rowKey: string | ((record: T) => string)
  searchPlaceholder: string
  searchValues: (record: T) => Array<string | undefined | null>
  title: string
  actionConfig?: {
    resourceKind: string
    getName: (record: T) => string
    getNamespace?: (record: T) => string | undefined
  }
}) {
  const { localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const [searchKeyword, setSearchKeyword] = useState('')
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

  return (
    <div className="kc-page">
      {actionConfig ? modalNode : null}
      {query.isError ? (
        <Alert
          showIcon
          type="error"
          message={localeCode === 'zh_CN' ? '网络资源暂时不可用' : 'Network resources unavailable'}
          description={buildNetworkErrorDescription(localeCode, query.error)}
          style={{ marginBottom: 12 }}
        />
      ) : null}
      <AdminTable
        className="kc-platform-table"
        columns={effectiveColumns}
        dataSource={clusterId ? filteredItems : []}
        rowKey={rowKey}
        loading={query.isLoading}
        enableColumnSelection={false}
        scroll={{ x: 'max-content' }}
        title={(
          <NetworkListTitle
            title={title}
            description={description}
            clusterScopedHint={clusterScoped ? (localeCode === 'zh_CN' ? '集群级资源，忽略命名空间筛选。' : 'Cluster-scoped resource; namespace scope is ignored.') : undefined}
          />
        )}
        toolbar={(
          <div className="kc-workload-table-filters">
            <Input
              className="kc-platform-compact-field"
              size="small"
              value={searchKeyword}
              onChange={(event) => setSearchKeyword(event.target.value)}
              placeholder={searchPlaceholder}
              style={{ width: 300 }}
            />
          </div>
        )}
        toolbarExtra={(
          <div className="kc-page-toolbar">
            <Button size="small" icon={<ReloadOutlined />} variant="outlined" onClick={() => void query.refetch()}>
              {localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
            </Button>
          </div>
        )}
        empty={<Empty description={effectiveEmpty} />}
      />
    </div>
  )
}

function StorageListTitle({ title, description, clusterScopedHint }: { title: string; description: string; clusterScopedHint?: string }) {
  return (
    <div className="kc-admin-table-title-block">
      <Text strong>{title}</Text>
      <Text type="secondary">{description}</Text>
      {clusterScopedHint ? <Text type="secondary">{clusterScopedHint}</Text> : null}
    </div>
  )
}

function StorageListToolbar({ clusterScoped = false }: { clusterScoped?: boolean }) {
  void clusterScoped
  return null
}

function StorageDetailHeader({ title, description, actions }: { title: string; description: string; actions?: React.ReactNode }) {
  return (
    <div className="kc-admin-table-shell is-panel" style={{ marginBottom: 16 }}>
      <div className="kc-admin-table-header">
        <div className="kc-admin-table-header-main">
          <div className="kc-admin-table-title-block">
            <Text strong>{title}</Text>
            <Text type="secondary">{description}</Text>
          </div>
        </div>
        {actions ? <div className="kc-admin-table-header-extra">{actions}</div> : null}
      </div>
    </div>
  )
}

function StorageYamlTab({ state }: { state: ReturnType<typeof useResourceYAMLState> }) {
  return (
    <Suspense fallback={<Card className="kc-detail-card"><Spin size="large" /></Card>}>
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
  const { localeCode, t } = useI18n()
  const navigate = useNavigate()
  const { clusterId, namespace } = usePlatformScopeStore()
  const [searchKeyword, setSearchKeyword] = useState('')
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

  return (
    <div className="kc-page">
      {query.isError ? (
        <Alert
          showIcon
          type="error"
          message={localeCode === 'zh_CN' ? '网络资源暂时不可用' : 'Network resources unavailable'}
          description={buildNetworkErrorDescription(localeCode, query.error)}
          style={{ marginBottom: 12 }}
        />
      ) : null}
      <AdminTable
        className="kc-platform-table"
        columns={columns}
        dataSource={clusterId ? filteredItems : []}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={query.isLoading}
        enableColumnSelection={false}
        scroll={{ x: 'max-content' }}
        title={<NetworkListTitle title={t('page.network.services.title', 'Services')} description={t('page.network.services.desc', 'Inspect service exposure, access addresses, and ports by cluster and namespace.')} />}
        toolbar={(
          <div className="kc-workload-table-filters">
            <Input className="kc-platform-compact-field" size="small" value={searchKeyword} onChange={(event) => setSearchKeyword(event.target.value)} placeholder={localeCode === 'zh_CN' ? '搜索 Service / namespace / type / port' : 'Search service / namespace / type / port'} style={{ width: 300 }} />
          </div>
        )}
        toolbarExtra={<div className="kc-page-toolbar"><Button size="small" icon={<ReloadOutlined />} variant="outlined" onClick={() => void query.refetch()}>{localeCode === 'zh_CN' ? '刷新' : 'Refresh'}</Button></div>}
        empty={<Empty description={!clusterId ? (localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster') : (localeCode === 'zh_CN' ? '当前范围没有 Service' : 'No services in the current scope')} />}
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
    return <Empty description={localeCode === 'zh_CN' ? '请选择集群和命名空间' : 'Select a cluster and namespace'} />
  }
  if (servicesQuery.isLoading) return <Card loading className="kc-detail-card" />
  if (!service) return <Empty description={localeCode === 'zh_CN' ? '未找到服务' : 'Service not found'} />

  return (
    <div className="kc-page">
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
              <Card className="kc-detail-card">
                <Descriptions items={[
                  { key: 'name', label: localeCode === 'zh_CN' ? '名称' : 'Name', children: service.name },
                  { key: 'namespace', label: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', children: service.namespace },
                  { key: 'type', label: 'Type', children: service.type },
                  { key: 'clusterIp', label: 'Cluster IP', children: service.clusterIp || '-' },
                  { key: 'ports', label: 'Ports', children: service.ports?.join(', ') || '-' },
                  { key: 'age', label: 'Age', children: formatAgeSeconds(service.ageSeconds) },
                ]} />
              </Card>
              <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '后端 Pods' : 'Backend Pods'}>
                <AdminTable columns={backendPodColumns} dataSource={backendPodsQuery.data?.data ?? []} rowKey={(record) => `${record.namespace}/${record.name}`} loading={backendPodsQuery.isLoading} pageSize={10} enableColumnSelection={false} />
              </Card>
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
      title="Ingresses"
      description={localeCode === 'zh_CN' ? '按当前范围查看 Ingress 主机、地址与后端 Service。' : 'Inspect ingress hosts, addresses, and backend services in the current scope.'}
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
      title="GatewayClasses"
      description={localeCode === 'zh_CN' ? '查看 Gateway API 控制器类、接收状态与参数引用。' : 'Inspect Gateway API controller classes, acceptance status, and parameter references.'}
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
      title="Gateways"
      description={localeCode === 'zh_CN' ? '查看 Gateway API 网关实例、监听器与对外地址。' : 'Inspect Gateway API gateway instances, listeners, and addresses.'}
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

export function NetworkHTTPRoutesPage() {
  const { localeCode } = useI18n()
  const columns: TableColumnsType<HTTPRoute> = [
    { title: 'Name', dataIndex: 'name' },
    { title: 'Namespace', dataIndex: 'namespace' },
    { title: 'Hostnames', dataIndex: 'hostnames', render: (value?: string[]) => renderTextList(value) },
    { title: 'ParentRefs', dataIndex: 'parentRefs', render: (value?: string[]) => renderTextList(value) },
    { title: 'Backend Services', dataIndex: 'backendServices', render: (value?: string[]) => renderTextList(value) },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]
  return (
    <NetworkResourceListPage<HTTPRoute>
      title="HTTPRoutes"
      description={localeCode === 'zh_CN' ? '查看 HTTPRoute 主机、父级 Gateway 与后端 Service 绑定。' : 'Inspect HTTPRoute hosts, parent Gateways, and backend service bindings.'}
      resourcePath="network/httproutes"
      columns={columns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      searchPlaceholder={localeCode === 'zh_CN' ? '搜索 HTTPRoute / host / Gateway / service' : 'Search HTTPRoute / host / Gateway / service'}
      searchValues={(record) => [record.name, record.namespace, ...(record.hostnames ?? []), ...(record.parentRefs ?? []), ...(record.backendServices ?? [])]}
      emptyDescription={localeCode === 'zh_CN' ? '当前范围没有 HTTPRoute，或未安装 Gateway API CRD' : 'No HTTPRoutes in the current scope, or Gateway API CRDs are not installed'}
      actionConfig={{ resourceKind: 'HTTPRoute', getName: (record) => record.name, getNamespace: (record) => record.namespace }}
    />
  )
}

export function NetworkBackendTLSPoliciesPage() {
  const { localeCode } = useI18n()
  const columns: TableColumnsType<BackendTLSPolicy> = [
    { title: 'Name', dataIndex: 'name' },
    { title: 'Namespace', dataIndex: 'namespace' },
    { title: 'TargetRefs', dataIndex: 'targetRefs', render: (value?: string[]) => renderTextList(value) },
    { title: 'Hostname', dataIndex: 'hostname', render: (value?: string) => value || '-' },
    { title: 'CA CertificateRefs', dataIndex: 'caCertificateRefs', render: (value?: string[]) => renderTextList(value) },
    { title: 'Well Known CA', dataIndex: 'wellKnownCACertificates', render: (value?: string) => value || '-' },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]
  return (
    <NetworkResourceListPage<BackendTLSPolicy>
      title="BackendTLSPolicies"
      description={localeCode === 'zh_CN' ? '查看 Gateway API 后端 TLS 校验策略、目标引用与 CA 配置。' : 'Inspect Gateway API backend TLS validation policies, target refs, and CA settings.'}
      resourcePath="network/backendtlspolicies"
      columns={columns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      searchPlaceholder={localeCode === 'zh_CN' ? '搜索 BackendTLSPolicy / target / hostname' : 'Search BackendTLSPolicy / target / hostname'}
      searchValues={(record) => [record.name, record.namespace, record.hostname, record.wellKnownCACertificates, ...(record.targetRefs ?? []), ...(record.caCertificateRefs ?? [])]}
      emptyDescription={localeCode === 'zh_CN' ? '当前范围没有 BackendTLSPolicy，或未安装 Gateway API CRD' : 'No BackendTLSPolicies in the current scope, or Gateway API CRDs are not installed'}
      actionConfig={{ resourceKind: 'BackendTLSPolicy', getName: (record) => record.name, getNamespace: (record) => record.namespace }}
    />
  )
}

export function NetworkGRPCRoutesPage() {
  const { localeCode } = useI18n()
  const columns: TableColumnsType<GRPCRoute> = [
    { title: 'Name', dataIndex: 'name' },
    { title: 'Namespace', dataIndex: 'namespace' },
    { title: 'Hostnames', dataIndex: 'hostnames', render: (value?: string[]) => renderTextList(value) },
    { title: 'ParentRefs', dataIndex: 'parentRefs', render: (value?: string[]) => renderTextList(value) },
    { title: 'Backend Services', dataIndex: 'backendServices', render: (value?: string[]) => renderTextList(value) },
    { title: 'Rules', dataIndex: 'ruleCount' },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]
  return (
    <NetworkResourceListPage<GRPCRoute>
      title="GRPCRoutes"
      description={localeCode === 'zh_CN' ? '查看 gRPC 路由、父级 Gateway 与后端 Service 绑定。' : 'Inspect gRPC routes, parent Gateways, and backend service bindings.'}
      resourcePath="network/grpcroutes"
      columns={columns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      searchPlaceholder={localeCode === 'zh_CN' ? '搜索 GRPCRoute / host / Gateway / service' : 'Search GRPCRoute / host / Gateway / service'}
      searchValues={(record) => [record.name, record.namespace, ...(record.hostnames ?? []), ...(record.parentRefs ?? []), ...(record.backendServices ?? [])]}
      emptyDescription={localeCode === 'zh_CN' ? '当前范围没有 GRPCRoute，或未安装 Gateway API CRD' : 'No GRPCRoutes in the current scope, or Gateway API CRDs are not installed'}
      actionConfig={{ resourceKind: 'GRPCRoute', getName: (record) => record.name, getNamespace: (record) => record.namespace }}
    />
  )
}

export function NetworkReferenceGrantsPage() {
  const { localeCode } = useI18n()
  const columns: TableColumnsType<ReferenceGrant> = [
    { title: 'Name', dataIndex: 'name' },
    { title: 'Namespace', dataIndex: 'namespace' },
    { title: 'From', dataIndex: 'from', render: (value?: string[]) => renderTextList(value) },
    { title: 'To', dataIndex: 'to', render: (value?: string[]) => renderTextList(value) },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]
  return (
    <NetworkResourceListPage<ReferenceGrant>
      title="ReferenceGrants"
      description={localeCode === 'zh_CN' ? '查看跨 namespace 引用授权来源与目标资源。' : 'Inspect cross-namespace reference grants, sources, and target resources.'}
      resourcePath="network/referencegrants"
      columns={columns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      searchPlaceholder={localeCode === 'zh_CN' ? '搜索 ReferenceGrant / namespace / from / to' : 'Search ReferenceGrant / namespace / from / to'}
      searchValues={(record) => [record.name, record.namespace, ...(record.from ?? []), ...(record.to ?? [])]}
      emptyDescription={localeCode === 'zh_CN' ? '当前范围没有 ReferenceGrant，或未安装 Gateway API CRD' : 'No ReferenceGrants in the current scope, or Gateway API CRDs are not installed'}
      actionConfig={{ resourceKind: 'ReferenceGrant', getName: (record) => record.name, getNamespace: (record) => record.namespace }}
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
      title="EndpointSlices"
      description={localeCode === 'zh_CN' ? '查看 Service 后端地址切片、端口与 endpoint 数量。' : 'Inspect service backend endpoint slices, ports, and endpoint counts.'}
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
      title="IngressClasses"
      description={localeCode === 'zh_CN' ? '查看 Ingress 控制器类、默认标记与参数引用。' : 'Inspect ingress controller classes, default markers, and parameter references.'}
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
      title="NetworkPolicies"
      description={localeCode === 'zh_CN' ? '查看命名空间网络隔离策略、入站与出站规则数量。' : 'Inspect namespace network isolation policies and ingress or egress rule counts.'}
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
  return (
    <div className="kc-page">
      <CreateResourceModal visible={createVisible} onClose={() => setCreateVisible(false)} kind="PersistentVolumeClaim" resourcePath="storage/persistentvolumeclaims" defaultTemplate={PVC_DEFAULT_TEMPLATE} invalidationKeys={[['platform-resource', 'storage/persistentvolumeclaims']]} />
      <AdminTable
        columns={columns}
        dataSource={clusterId ? filteredItems : []}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        loading={query.isLoading}
        enableColumnSelection={false}
        scroll={{ x: 'max-content' }}
        title={<StorageListTitle title="PersistentVolumeClaims" description="" />}
        toolbar={<div className="kc-workload-table-filters"><StorageListToolbar /><Input className="kc-platform-compact-field" size="small" value={searchKeyword} onChange={(event) => setSearchKeyword(event.target.value)} placeholder={localeCode === 'zh_CN' ? '搜索 PVC / namespace / storageClass' : 'Search PVC / namespace / storageClass'} style={{ width: 280 }} /></div>}
        toolbarExtra={<div className="kc-page-toolbar"><Tooltip title={!clusterId ? (localeCode === 'zh_CN' ? '请先选择集群。' : 'Select a cluster first.') : ''}><span><Button size="small" type="primary" icon={<PlusOutlined />} disabled={!clusterId} onClick={() => setCreateVisible(true)}>{localeCode === 'zh_CN' ? '新增' : 'Create'}</Button></span></Tooltip><Button size="small" icon={<ReloadOutlined />} variant="outlined" onClick={() => void query.refetch()}>{localeCode === 'zh_CN' ? '刷新' : 'Refresh'}</Button></div>}
        empty={<Empty description={clusterId ? (localeCode === 'zh_CN' ? '当前范围没有 PVC' : 'No PVCs in the current scope') : (localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster')} />}
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
  return (
    <div className="kc-page">
      <CreateResourceModal visible={createVisible} onClose={() => setCreateVisible(false)} kind="PersistentVolume" resourcePath="storage/persistentvolumes" defaultTemplate={PV_DEFAULT_TEMPLATE} invalidationKeys={[['platform-resource', 'storage/persistentvolumes']]} namespaceScope="cluster" />
      <AdminTable
        columns={columns}
        dataSource={clusterId ? filteredItems : []}
        rowKey="name"
        loading={query.isLoading}
        enableColumnSelection={false}
        scroll={{ x: 'max-content' }}
        title={<StorageListTitle title="PersistentVolumes" description="" />}
        toolbar={<div className="kc-workload-table-filters"><StorageListToolbar clusterScoped /><Input className="kc-platform-compact-field" size="small" value={searchKeyword} onChange={(event) => setSearchKeyword(event.target.value)} placeholder={localeCode === 'zh_CN' ? '搜索 PV / claim / storageClass' : 'Search PV / claim / storageClass'} style={{ width: 280 }} /></div>}
        toolbarExtra={<div className="kc-page-toolbar"><Tooltip title={!clusterId ? (localeCode === 'zh_CN' ? '请先选择集群。' : 'Select a cluster first.') : ''}><span><Button size="small" type="primary" icon={<PlusOutlined />} disabled={!clusterId} onClick={() => setCreateVisible(true)}>{localeCode === 'zh_CN' ? '新增' : 'Create'}</Button></span></Tooltip><Button size="small" icon={<ReloadOutlined />} variant="outlined" onClick={() => void query.refetch()}>{localeCode === 'zh_CN' ? '刷新' : 'Refresh'}</Button></div>}
        empty={<Empty description={clusterId ? (localeCode === 'zh_CN' ? '当前集群没有 PV' : 'No PVs in this cluster') : (localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster')} />}
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
  return (
    <div className="kc-page">
      <CreateResourceModal visible={createVisible} onClose={() => setCreateVisible(false)} kind="StorageClass" resourcePath="storage/storageclasses" defaultTemplate={STORAGE_CLASS_DEFAULT_TEMPLATE} invalidationKeys={[['platform-resource', 'storage/storageclasses']]} namespaceScope="cluster" />
      <AdminTable
        columns={columns}
        dataSource={clusterId ? filteredItems : []}
        rowKey="name"
        loading={query.isLoading}
        enableColumnSelection={false}
        scroll={{ x: 'max-content' }}
        title={<StorageListTitle title="StorageClasses" description="" />}
        toolbar={<div className="kc-workload-table-filters"><StorageListToolbar clusterScoped /><Input className="kc-platform-compact-field" size="small" value={searchKeyword} onChange={(event) => setSearchKeyword(event.target.value)} placeholder={localeCode === 'zh_CN' ? '搜索 StorageClass / provisioner' : 'Search StorageClass / provisioner'} style={{ width: 280 }} /></div>}
        toolbarExtra={<div className="kc-page-toolbar"><Tooltip title={!clusterId ? (localeCode === 'zh_CN' ? '请先选择集群。' : 'Select a cluster first.') : ''}><span><Button size="small" type="primary" icon={<PlusOutlined />} disabled={!clusterId} onClick={() => setCreateVisible(true)}>{localeCode === 'zh_CN' ? '新增' : 'Create'}</Button></span></Tooltip><Button size="small" icon={<ReloadOutlined />} variant="outlined" onClick={() => void query.refetch()}>{localeCode === 'zh_CN' ? '刷新' : 'Refresh'}</Button></div>}
        empty={<Empty description={clusterId ? (localeCode === 'zh_CN' ? '当前集群没有 StorageClass' : 'No storage classes in this cluster') : (localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster')} />}
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
  if (!clusterId || !detailNamespace) return <Empty description={localeCode === 'zh_CN' ? '请选择集群和命名空间' : 'Select a cluster and namespace'} />
  if (detailQuery.isLoading) return <Card loading className="kc-detail-card" />
  if (!detail) return <Empty description={localeCode === 'zh_CN' ? 'PVC 未找到' : 'PVC not found'} />
  return (
    <div className="kc-page">
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
  if (!clusterId) return <Empty description={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'} />
  if (detailQuery.isLoading) return <Card loading className="kc-detail-card" />
  if (!detail) return <Empty description={localeCode === 'zh_CN' ? 'PV 未找到' : 'PV not found'} />
  return (
    <div className="kc-page">
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
  if (!clusterId) return <Empty description={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'} />
  if (detailQuery.isLoading) return <Card loading className="kc-detail-card" />
  if (!detail) return <Empty description={localeCode === 'zh_CN' ? 'StorageClass 未找到' : 'StorageClass not found'} />
  return (
    <div className="kc-page">
      <StorageDetailHeader title={`StorageClass: ${detail.name}`} description={localeCode === 'zh_CN' ? '查看 provisioner、绑定模式、参数与 YAML。' : 'Inspect provisioner, binding mode, parameters, and YAML.'} />
      <PlatformClusterScopeHint resourceLabel="StorageClass" />
      <Tabs items={[
        { key: 'overview', label: localeCode === 'zh_CN' ? '概览' : 'Overview', children: <><ResourceMetaOverview name={detail.name} namespace="-" createdAt={detail.createdAt} labels={detail.labels} annotations={detail.annotations} extra={[{ key: 'Provisioner', value: detail.provisioner }, { key: 'ReclaimPolicy', value: detail.reclaimPolicy || '-' }, { key: 'BindingMode', value: detail.volumeBindingMode || '-' }, { key: localeCode === 'zh_CN' ? '允许扩容' : 'Expansion', value: <BooleanTag value={detail.allowVolumeExpansion} trueLabel="Yes" falseLabel="No" /> }]} /><Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '参数' : 'Parameters'}>{detail.parameters && Object.keys(detail.parameters).length > 0 ? <Descriptions column={1} items={Object.entries(detail.parameters).map(([key, value]) => ({ key, label: key, children: value }))} /> : <Empty description={localeCode === 'zh_CN' ? '暂无参数' : 'No parameters'} />}</Card></> },
        { key: 'yaml', label: 'YAML', children: <StorageYamlTab state={yamlState} /> },
      ]} />
    </div>
  )
}
