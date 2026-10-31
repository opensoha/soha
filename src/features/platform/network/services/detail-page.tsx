import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { ResourceEventsTimeline } from '@/components/resource-events-timeline'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType, TabsProps } from 'antd'
import { Button, Card, Spin } from 'antd'
import { lazy, Suspense } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { NetworkDetailShell } from '../shared/detail-shell'
import { resolveNetworkNamespace } from '../shared/scope'
import { ServiceDiagnosticsPanel } from './diagnostics-panel'
import { ServicePortDisplay, ServiceTypeTag } from './port-display'
import { serviceQueries } from './queries'
import type { ServiceBackendPod, ServiceEndpoint } from './types'

const ResourceMetricsPanel = lazy(async () => {
  const module = await import('@/components/resource-metrics-panel')
  return { default: module.ResourceMetricsPanel }
})

function buildPodDetailPath(name: string, namespace: string) {
  const query = new URLSearchParams({ namespace }).toString()
  return `/workloads/pods/${encodeURIComponent(name)}?${query}`
}

export function ServiceDetailPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const params = useParams()
  const [searchParams] = useSearchParams()
  const { clusterId, namespace } = usePlatformScopeStore()
  const serviceName = (params.serviceName as string | undefined) ?? ''
  const detailNamespace = resolveNetworkNamespace(namespace, searchParams.get('namespace'))
  const scope = toScopeKey(clusterId, detailNamespace)
  const [activeTabKey, setActiveTabKey] = useResourceDetailTab()
  const detailQuery = useQuery(serviceQueries.detail(scope, serviceName))
  const visibleDetailQueryData = readableQueryData(detailQuery)
  const service = visibleDetailQueryData

  const metricsOptions = serviceQueries.metrics(scope, serviceName)
  const metricsQuery = useQuery({
    ...metricsOptions,
    enabled: Boolean(metricsOptions.enabled) && activeTabKey === 'metrics',
  })
  const visibleMetricsQueryData =
    Boolean(metricsOptions.enabled) && activeTabKey === 'metrics'
      ? readableQueryData(metricsQuery)
      : undefined
  const eventsOptions = serviceQueries.events(scope, serviceName)
  const eventsQuery = useQuery({
    ...eventsOptions,
    enabled: Boolean(eventsOptions.enabled) && activeTabKey === 'events',
  })
  const visibleEventsQueryData =
    Boolean(eventsOptions.enabled) && activeTabKey === 'events'
      ? readableQueryData(eventsQuery)
      : undefined

  const backendPodColumns: TableColumnsType<ServiceBackendPod> = [
    {
      title: 'Pod',
      dataIndex: 'name',
      render: (value: string, record) => (
        <Button type="text" onClick={() => navigate(buildPodDetailPath(value, record.namespace))}>
          {value}
        </Button>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'phase',
      render: (value: string) => <StatusTag value={value} />,
    },
    { title: 'Ready', dataIndex: 'readyContainers' },
    { title: localeCode === 'zh_CN' ? '重启次数' : 'Restarts', dataIndex: 'restarts' },
    {
      title: localeCode === 'zh_CN' ? '节点' : 'Node',
      dataIndex: 'nodeName',
      render: (value?: string) => value || '-',
    },
    { title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]

  const endpointColumns: TableColumnsType<ServiceEndpoint> = [
    { title: localeCode === 'zh_CN' ? '地址' : 'Address', dataIndex: 'address' },
    {
      title: localeCode === 'zh_CN' ? '就绪' : 'Ready',
      dataIndex: 'ready',
      render: (value?: boolean) =>
        value === undefined ? '-' : <BooleanTag value={value} trueLabel="Yes" falseLabel="No" />,
    },
    {
      title: localeCode === 'zh_CN' ? '目标' : 'Target',
      dataIndex: 'targetRef',
      render: (value?: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '节点' : 'Node',
      dataIndex: 'nodeName',
      render: (value?: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '区域' : 'Zone',
      dataIndex: 'zone',
      render: (value?: string) => value || '-',
    },
  ]

  if (!clusterId || !detailNamespace) {
    return (
      <div className="soha-page">
        <ManagementState
          kind="select-scope"
          description={
            localeCode === 'zh_CN' ? '请选择集群和命名空间' : 'Select a cluster and namespace'
          }
        />
      </div>
    )
  }
  if (detailQuery.isLoading) return <Card loading className="soha-detail-card" />
  if (!service) {
    return (
      <div className="soha-page">
        <ManagementState
          kind="not-found"
          description={localeCode === 'zh_CN' ? '未找到服务' : 'Service not found'}
        />
      </div>
    )
  }

  const extraTabs: NonNullable<TabsProps['items']> = [
    {
      key: 'diagnostics',
      label: localeCode === 'zh_CN' ? '诊断' : 'Diagnostics',
      children:
        activeTabKey === 'diagnostics' ? (
          <ServiceDiagnosticsPanel
            clusterId={clusterId}
            namespace={detailNamespace}
            portMappings={service.portMappings}
            serviceName={service.name}
          />
        ) : null,
    },
    {
      key: 'metrics',
      label: localeCode === 'zh_CN' ? '指标' : 'Metrics',
      children:
        activeTabKey === 'metrics' ? (
          <Suspense
            fallback={
              <Card className="soha-detail-card">
                <Spin size="large" />
              </Card>
            }
          >
            <ResourceMetricsPanel data={visibleMetricsQueryData} loading={metricsQuery.isLoading} />
          </Suspense>
        ) : null,
    },
    {
      key: 'events',
      label: localeCode === 'zh_CN' ? '事件' : 'Events',
      children:
        activeTabKey === 'events' ? (
          <ResourceEventsTimeline
            events={visibleEventsQueryData ?? []}
            loading={eventsQuery.isLoading}
            emptyDescription={
              localeCode === 'zh_CN' ? '当前 Service 暂无事件' : 'No service events'
            }
          />
        ) : null,
    },
  ]

  return (
    <>
      <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源详情" />
      <ReadFeedback canRead={metricsQuery.isEnabled} query={metricsQuery} label="资源指标" />
      <ReadFeedback canRead={eventsQuery.isEnabled} query={eventsQuery} label="资源事件" />
      <NetworkDetailShell
        activeTabKey={activeTabKey}
        ageLabel="Age"
        aiContext={{
          timeRangeMinutes: visibleMetricsQueryData?.rangeMinutes ?? 60,
          pinnedData: {
            type: service.type,
            clusterIp: service.clusterIp,
            ports: service.ports,
            portMappings: service.portMappings,
            selector: service.selector,
            activeTab: activeTabKey,
          },
          promptHint:
            localeCode === 'zh_CN'
              ? `排查 Service ${service.name} 的访问异常、Endpoint/后端 Pod、事件、日志和指标。`
              : `Troubleshoot Service ${service.name} connectivity, endpoints, backend pods, events, logs, and metrics.`,
        }}
        detail={service}
        extraTabs={extraTabs}
        kind="services"
        label="Service"
        onTabChange={setActiveTabKey}
        overviewExtra={[
          { key: 'Type', value: <ServiceTypeTag type={service.type} /> },
          { key: 'Cluster IP', value: service.clusterIp || '-' },
          {
            key: 'Ports',
            value: <ServicePortDisplay portMappings={service.portMappings} ports={service.ports} />,
          },
        ]}
        overviewContent={
          <>
            <Card
              className="soha-detail-card"
              title={localeCode === 'zh_CN' ? 'Endpoints' : 'Endpoints'}
            >
              <AdminTable
                shellClassName="soha-management-table-shell"
                columns={endpointColumns}
                dataSource={service.endpoints ?? []}
                rowKey={(record) => `${record.address}/${record.targetRef ?? ''}`}
                pagination={false}
                scroll={{ x: 800, y: 520 }}
                tableSize="small"
                enableColumnSelection={false}
              />
            </Card>
            <Card
              className="soha-detail-card"
              title={localeCode === 'zh_CN' ? '关联 Pods' : 'Related Pods'}
            >
              <AdminTable
                shellClassName="soha-management-table-shell"
                columns={backendPodColumns}
                dataSource={service.backendPods ?? []}
                rowKey={(record) => `${record.namespace}/${record.name}`}
                pagination={false}
                scroll={{ x: 800, y: 520 }}
                tableSize="small"
                enableColumnSelection={false}
              />
            </Card>
          </>
        }
        target={{ scope, name: serviceName }}
      />
    </>
  )
}
