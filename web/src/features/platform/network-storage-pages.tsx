import { useMemo } from 'react'
import { Button, Card, Descriptions, Empty, Tabs, TabPane, Tag, Typography } from '@douyinfe/semi-ui'
import { useQuery } from '@tanstack/react-query'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { useI18n } from '@/i18n'
import { PageHeader } from '@/components/page-header'
import { PlatformClusterScopeHint } from '@/components/platform-cluster-scope-hint'
import { PlatformScopeToolbar } from '@/components/platform-scope-toolbar'
import { ResourceEventsTimeline } from '@/components/resource-events-timeline'
import { ResourceMetricsPanel } from '@/components/resource-metrics-panel'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { downloadJSON } from '@/utils/download'
import { formatAgeSeconds, formatRelativeTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse, ResourceMetrics } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'

const { Text } = Typography

function buildServiceDetailPath(name: string, selectedNamespace: string | null, rowNamespace: string) {
  const params = new URLSearchParams()
  const namespace = selectedNamespace && selectedNamespace !== '' ? selectedNamespace : rowNamespace
  if (namespace) {
    params.set('namespace', namespace)
  }
  const query = params.toString()
  return query ? `/network/services/${name}?${query}` : `/network/services/${name}`
}

function buildPodDetailPath(name: string, selectedNamespace: string | null, rowNamespace: string) {
  const params = new URLSearchParams()
  const namespace = selectedNamespace && selectedNamespace !== '' ? selectedNamespace : rowNamespace
  if (namespace) {
    params.set('namespace', namespace)
  }
  const query = params.toString()
  return query ? `/workloads/pods/${name}?${query}` : `/workloads/pods/${name}`
}

function selectorMatchesLabels(selector?: Record<string, string>, labels?: Record<string, string>) {
  const entries = Object.entries(selector ?? {})
  if (entries.length === 0) return false
  return entries.every(([key, value]) => (labels ?? {})[key] === value)
}

function useScopedQuery<T>(resource: string) {
  const { clusterId, namespace } = usePlatformScopeStore()
  if (!clusterId) {
    return useQuery({
      queryKey: [resource, clusterId, namespace],
      queryFn: () => Promise.resolve({ data: [] as T[] }),
      enabled: false,
    })
  }

  const resourcePathMap: Record<string, string> = {
    services: 'network/services',
    ingresses: 'network/ingresses',
    gateways: 'network/gateways',
    persistentvolumeclaims: 'storage/persistentvolumeclaims',
  }

  const scopedPath = resourcePathMap[resource]

  return useQuery({
    queryKey: [resource, clusterId, namespace],
    queryFn: () => api.get<ApiResponse<T[]>>(buildClusterScopedPath(clusterId, scopedPath, namespace)),
    enabled: !!clusterId && !!scopedPath,
  })
}

/* ─── Services ─── */

interface Service {
  name: string
  namespace: string
  type: string
  clusterIp: string
  ports: string[]
  selector?: Record<string, string>
  ageSeconds: number
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

export function NetworkServicesPage() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const { namespace } = usePlatformScopeStore()
  const { data, isLoading } = useScopedQuery<Service>('services')

  const columns: ColumnProps<Service>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (value: string, record: Service) => (
        <Button theme="borderless" type="primary" onClick={() => navigate(buildServiceDetailPath(value, namespace, record.namespace))}>
          {value}
        </Button>
      ),
    },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: '类型', dataIndex: 'type', render: (t: string) => <Tag>{t}</Tag> },
    { title: 'Cluster IP', dataIndex: 'clusterIp', render: (value: string) => value || '-' },
    { title: '端口', dataIndex: 'ports', render: (value: string[]) => value?.join(', ') || '-' },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.network.services.title', 'Services')} description={t('page.network.services.desc', 'Inspect service exposure, access addresses, and ports by cluster and namespace.')} />
      <PlatformScopeToolbar />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="name" loading={isLoading} />
    </div>
  )
}

export function ServiceDetailPage() {
  const { t, localeCode } = useI18n()
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
      const response = await api.get<ApiResponse<ServiceBackendPod[]>>(
        `/clusters/${clusterId}/workloads/pods?namespace=${encodeURIComponent(detailNamespace)}`,
      )
      return {
        data: (response.data ?? []).filter((item) => selectorMatchesLabels(service?.selector, item.labels)),
      } as ApiResponse<ServiceBackendPod[]>
    },
    enabled: !!clusterId && !!detailNamespace && !!service,
  })

  const metricsQuery = useQuery({
    queryKey: ['service-metrics', clusterId, detailNamespace, serviceName],
    queryFn: () => api.get<ApiResponse<ResourceMetrics>>(
      `/clusters/${clusterId}/network/services/${serviceName}/metrics?namespace=${encodeURIComponent(detailNamespace)}`,
    ),
    enabled: !!clusterId && !!detailNamespace,
  })

  const eventsQuery = useQuery({
    queryKey: ['service-events', clusterId, detailNamespace, serviceName],
    queryFn: async () => {
      const response = await api.get<ApiResponse<ServiceEvent[]>>(
        buildClusterScopedPath(clusterId!, 'events', detailNamespace, { limit: 100 }),
      )
      return {
        data: (response.data ?? []).filter((item) =>
          item.involvedName === serviceName && (!item.involvedKind || item.involvedKind.toLowerCase() === 'service'),
        ),
      } as ApiResponse<ServiceEvent[]>
    },
    enabled: !!clusterId && !!detailNamespace,
  })

  const backendPodColumns: ColumnProps<ServiceBackendPod>[] = [
    {
      title: localeCode === 'zh_CN' ? 'Pod' : 'Pod',
      dataIndex: 'name',
      render: (value: string, record: ServiceBackendPod) => (
        <Button theme="borderless" type="primary" onClick={() => navigate(buildPodDetailPath(value, detailNamespace, record.namespace))}>
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
    return (
      <div className="kc-page">
        <PageHeader title={t('page.network.services.title', 'Services')} description={localeCode === 'zh_CN' ? '请选择集群与命名空间后查看服务工作台。' : 'Select a cluster and namespace before opening the service workspace.'} />
        <PlatformScopeToolbar />
        <Empty description={localeCode === 'zh_CN' ? '请选择集群和命名空间' : 'Select a cluster and namespace'} />
      </div>
    )
  }

  if (servicesQuery.isLoading) {
    return <div className="flex items-center justify-center h-64"><Card loading className="kc-detail-card" /></div>
  }

  if (!service) {
    return (
      <div className="kc-page">
        <PageHeader title={`${t('page.network.services.title', 'Services')}: ${serviceName}`} description={localeCode === 'zh_CN' ? '服务不存在或当前 scope 下不可见。' : 'The service does not exist or is not visible in the current scope.'} />
        <PlatformScopeToolbar />
        <Empty description={localeCode === 'zh_CN' ? '未找到服务' : 'Service not found'} />
      </div>
    )
  }

  return (
    <div className="kc-page">
      <PageHeader
        title={`Service: ${service.name}`}
        description={localeCode === 'zh_CN' ? '查看服务暴露信息、后端 Pod、事件与指标。' : 'Inspect service exposure, backend pods, events, and metrics.'}
        actions={(
          <Button theme="light" onClick={() => downloadJSON(`service-diagnostics-${service.name}.json`, exportPayload)}>
            {localeCode === 'zh_CN' ? '导出诊断' : 'Export Diagnostics'}
          </Button>
        )}
      />
      <PlatformScopeToolbar />
      <Tabs type="line">
        <TabPane tab={localeCode === 'zh_CN' ? '概览' : 'Overview'} itemKey="overview">
          <Card className="kc-detail-card">
            <Descriptions
              data={[
                { key: localeCode === 'zh_CN' ? '名称' : 'Name', value: service.name },
                { key: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', value: service.namespace },
                { key: localeCode === 'zh_CN' ? '类型' : 'Type', value: service.type },
                { key: 'Cluster IP', value: service.clusterIp || '-' },
                { key: 'Ports', value: service.ports?.join(', ') || '-' },
                { key: 'Age', value: formatAgeSeconds(service.ageSeconds) },
              ]}
            />
            {service.selector && Object.keys(service.selector).length > 0 ? (
              <div className="kc-detail-meta">
                <Text strong>{localeCode === 'zh_CN' ? 'Selector' : 'Selector'}</Text>
                <div className="kc-tag-list">
                  {Object.entries(service.selector).map(([key, value]) => (
                    <Tag key={key} size="small">{key}={String(value)}</Tag>
                  ))}
                </div>
              </div>
            ) : null}
          </Card>
          <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '后端 Pods' : 'Backend Pods'}>
            <AdminTable
              columns={backendPodColumns}
              dataSource={backendPodsQuery.data?.data ?? []}
              rowKey={(record) => `${record.namespace}/${record.name}`}
              loading={backendPodsQuery.isLoading}
              pageSize={10}
              enableColumnSelection={false}
            />
          </Card>
        </TabPane>
        <TabPane tab={localeCode === 'zh_CN' ? '指标' : 'Metrics'} itemKey="metrics">
          <ResourceMetricsPanel
            title={localeCode === 'zh_CN' ? 'Service 指标' : 'Service Metrics'}
            data={metricsQuery.data?.data}
            loading={metricsQuery.isLoading}
          />
        </TabPane>
        <TabPane tab={localeCode === 'zh_CN' ? '事件' : 'Events'} itemKey="events">
          <ResourceEventsTimeline
            title={localeCode === 'zh_CN' ? 'Service 事件时间线' : 'Service Event Timeline'}
            events={eventsQuery.data?.data ?? []}
            loading={eventsQuery.isLoading}
            emptyDescription={localeCode === 'zh_CN' ? '当前 Service 暂无事件' : 'No service events'}
          />
        </TabPane>
      </Tabs>
    </div>
  )
}

/* ─── Ingresses ─── */

interface Ingress {
  name: string
  namespace: string
  hosts: string[]
  address: string
  ports: string
  createdAt: string
}

export function NetworkIngressesPage() {
  const { t } = useI18n()
  const { data, isLoading } = useScopedQuery<Ingress>('ingresses')

  const columns: ColumnProps<Ingress>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: '命名空间', dataIndex: 'namespace' },
    {
      title: 'Hosts',
      dataIndex: 'hosts',
      render: (hosts: string[]) => hosts?.join(', ') ?? '-',
    },
    { title: 'Address', dataIndex: 'address' },
    { title: '端口', dataIndex: 'ports' },
    { title: 'Age', dataIndex: 'createdAt', render: (t: string) => formatRelativeTime(t) },
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.network.ingresses.title', 'Ingresses')} description={t('page.network.ingresses.desc', 'Inspect host rules, addresses, ports, and recent changes for ingress resources.')} />
      <PlatformScopeToolbar />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="name" loading={isLoading} />
    </div>
  )
}

/* ─── Gateways ─── */

interface Gateway {
  name: string
  namespace: string
  gatewayClassName: string
  addresses: string
  programmed: string
  createdAt: string
}

export function NetworkGatewaysPage() {
  const { t } = useI18n()
  const { data, isLoading } = useScopedQuery<Gateway>('gateways')

  const columns: ColumnProps<Gateway>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: 'Gateway Class', dataIndex: 'gatewayClassName' },
    { title: 'Addresses', dataIndex: 'addresses' },
    {
      ...tableColumnPresets.status,
      title: 'Programmed',
      dataIndex: 'programmed',
      render: (s: string) => <StatusTag value={s} />,
    },
    { title: 'Age', dataIndex: 'createdAt', render: (t: string) => formatRelativeTime(t) },
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.network.gateways.title', 'Gateways')} description={t('page.network.gateways.desc', 'Review gateway entry points, gateway classes, and programmed status.')} />
      <PlatformScopeToolbar />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="name" loading={isLoading} />
    </div>
  )
}

/* ─── PVC ─── */

interface PVC {
  name: string
  namespace: string
  status: string
  volume: string
  capacity: string
  storageClass: string
  createdAt: string
}

export function StoragePvcPage() {
  const { t } = useI18n()
  const { data, isLoading } = useScopedQuery<PVC>('persistentvolumeclaims')

  const columns: ColumnProps<PVC>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: '命名空间', dataIndex: 'namespace' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
    { title: 'Volume', dataIndex: 'volume' },
    { title: '容量', dataIndex: 'capacity' },
    { title: 'StorageClass', dataIndex: 'storageClass' },
    { title: 'Age', dataIndex: 'createdAt', render: (t: string) => formatRelativeTime(t) },
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.storage.pvc.title', 'PersistentVolumeClaims')} description={t('page.storage.pvc.desc', 'Inspect requested capacity, access modes, and binding state in the current namespace.')} />
      <PlatformScopeToolbar />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="name" loading={isLoading} />
    </div>
  )
}

/* ─── PV ─── */

interface PV {
  name: string
  capacity: string
  accessModes: string[]
  reclaimPolicy: string
  status: string
  storageClass: string
  createdAt: string
}

export function StoragePvPage() {
  const { t } = useI18n()
  const { clusterId } = usePlatformScopeStore()

  const { data, isLoading } = useQuery({
    queryKey: ['persistentvolumes', clusterId],
    queryFn: () => api.get<ApiResponse<PV[]>>(buildClusterScopedPath(clusterId!, 'storage/persistentvolumes')),
    enabled: !!clusterId,
  })

  const columns: ColumnProps<PV>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: '容量', dataIndex: 'capacity' },
    {
      title: 'Access Modes',
      dataIndex: 'accessModes',
      render: (modes: string[]) => modes?.join(', ') ?? '-',
    },
    { title: 'Reclaim Policy', dataIndex: 'reclaimPolicy' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
    { title: 'StorageClass', dataIndex: 'storageClass' },
    { title: 'Age', dataIndex: 'createdAt', render: (t: string) => formatRelativeTime(t) },
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.storage.pv.title', 'PersistentVolumes')} description={t('page.storage.pv.desc', 'Inspect cluster-scoped persistent volumes, capacity supply, and reclaim policy.')} />
      <PlatformScopeToolbar />
      <PlatformClusterScopeHint resourceLabel="PersistentVolumes" />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="name" loading={isLoading} />
    </div>
  )
}

/* ─── StorageClasses ─── */

interface StorageClass {
  name: string
  provisioner: string
  reclaimPolicy: string
  volumeBindingMode: string
  allowVolumeExpansion: boolean
  createdAt: string
}

export function StorageClassesPage() {
  const { t } = useI18n()
  const { clusterId } = usePlatformScopeStore()

  const { data, isLoading } = useQuery({
    queryKey: ['storageclasses', clusterId],
    queryFn: () => api.get<ApiResponse<StorageClass[]>>(buildClusterScopedPath(clusterId!, 'storage/storageclasses')),
    enabled: !!clusterId,
  })

  const columns: ColumnProps<StorageClass>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: 'Provisioner', dataIndex: 'provisioner' },
    { title: 'Reclaim Policy', dataIndex: 'reclaimPolicy' },
    { title: 'Binding Mode', dataIndex: 'volumeBindingMode' },
    {
      ...tableColumnPresets.status,
      title: '允许扩容',
      dataIndex: 'allowVolumeExpansion',
      render: (v: boolean) => <BooleanTag value={v} trueLabel="Yes" falseLabel="No" />,
    },
    { title: 'Age', dataIndex: 'createdAt', render: (t: string) => formatRelativeTime(t) },
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.storage.classes.title', 'StorageClasses')} description={t('page.storage.classes.desc', 'Inspect provisioners, binding mode, and expansion capability.')} />
      <PlatformScopeToolbar />
      <PlatformClusterScopeHint resourceLabel="StorageClasses" />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="name" loading={isLoading} />
    </div>
  )
}
