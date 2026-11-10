import { AdminTable } from '@/components/admin-table'
import { ManagementIconButton, ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useAIPageContext } from '@/features/copilot'
import { ApiError } from '@/services/api-error'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { EyeOutlined, LineChartOutlined, ReloadOutlined, ShareAltOutlined } from '@ant-design/icons'
import type { ObservabilityService } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Alert, Descriptions, Drawer, Form, Space, Typography } from 'antd'
import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import type { ObservabilityServiceQuery } from './api'
import { signalSearchParams } from './model'
import { observabilitySignalQueries } from './queries'
import { queryTimes, SignalQueryForm, type SignalFilters } from './shared'

const { Text, Title } = Typography

function statusLabel(status: ObservabilityService['status']) {
  return { degraded: '降级', healthy: '健康', unknown: '未知', unsupported: '不支持' }[status]
}

function serviceRequest(
  values: Partial<SignalFilters>,
  clusterId: string | null,
  namespace: string | null,
  dataSourceId?: string | null,
): ObservabilityServiceQuery {
  return {
    ...queryTimes(values.rangeMinutes ?? 60, values.timeFrom, values.timeTo),
    clusterId: clusterId?.trim() || undefined,
    namespace: namespace?.trim() || undefined,
    dataSourceId: dataSourceId?.trim() || undefined,
    service: values.service?.trim() || undefined,
  }
}

export function ObservabilityServicesPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const { clusterId, namespace } = usePlatformScopeStore()
  const requestedClusterId = searchParams.get('cluster') || clusterId
  const requestedNamespace = searchParams.get('namespace') || namespace
  const [form] = Form.useForm<SignalFilters>()
  const [request, setRequest] = useState<ObservabilityServiceQuery>(() =>
    serviceRequest(
      {
        rangeMinutes: 60,
        service: searchParams.get('service') ?? undefined,
        timeFrom: searchParams.get('from') ?? undefined,
        timeTo: searchParams.get('to') ?? undefined,
      },
      requestedClusterId,
      requestedNamespace,
      searchParams.get('dataSourceId'),
    ),
  )
  const [selectedServiceId, setSelectedServiceId] = useState('')
  const services = useQuery(observabilitySignalQueries.services(request))
  const service = useQuery(
    observabilitySignalQueries.service(selectedServiceId, selectedServiceId ? request : undefined),
  )
  const topology = useQuery(
    observabilitySignalQueries.topology(selectedServiceId, selectedServiceId ? request : undefined),
  )
  const serviceData = readableQueryData(service)
  const servicesData = readableQueryData(services)
  const topologyData = readableQueryData(topology)
  useAIPageContext({
    sourceWorkbench: 'monitoring',
    sourceTitle: '服务调查',
    entityKind: 'monitoring.signal.services',
    entityName:
      serviceData?.data.displayName || serviceData?.data.name || request.service || '服务',
    clusterId: request.clusterId,
    namespace: request.namespace,
    service: serviceData?.data.name || request.service,
    visibleFilters: {
      dataSourceId: request.dataSourceId,
      service: request.service,
      timeFrom: searchParams.get('from') || request.timeFrom,
      timeTo: searchParams.get('to') || request.timeTo,
    },
    pinnedData: servicesData
      ? {
          serviceCount: servicesData.items.length,
          selectedServiceId: selectedServiceId || undefined,
          topologyEdgeCount: topologyData?.data.edges.length,
        }
      : undefined,
    promptHint: '分析当前服务范围的 RED 指标、依赖关系、异常影响面，并给出关联证据。',
  })

  function signalPath(signal: 'metrics' | 'traces', serviceName: string) {
    const params = signalSearchParams(new URLSearchParams(), {
      signal,
      dataSourceId: request.dataSourceId,
      cluster: request.clusterId,
      namespace: request.namespace,
      service: serviceName,
      from: request.timeFrom,
      to: request.timeTo,
    })
    return `/monitoring-workbench/${signal}?${params.toString()}`
  }

  const columns: TableColumnsType<ObservabilityService> = [
    {
      title: '服务',
      dataIndex: 'displayName',
      key: 'name',
      render: (_, item) => item.displayName || item.name,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (value: ObservabilityService['status']) => (
        <StatusTag label={statusLabel(value)} value={value} />
      ),
    },
    { title: '实例', dataIndex: ['instances', 'length'], key: 'instances', width: 90 },
    { title: '端点', dataIndex: ['endpoints', 'length'], key: 'endpoints', width: 90 },
    {
      title: '请求率',
      dataIndex: 'requestRate',
      key: 'requestRate',
      width: 110,
      render: (value?: number) => (value === undefined ? '-' : `${value.toFixed(2)}/s`),
    },
    {
      title: '错误率',
      dataIndex: 'errorRate',
      key: 'errorRate',
      width: 100,
      render: (value?: number) => (value === undefined ? '-' : `${(value * 100).toFixed(2)}%`),
    },
    {
      title: 'P95',
      dataIndex: 'latencyP95Ms',
      key: 'latencyP95Ms',
      width: 100,
      render: (value?: number) => (value === undefined ? '-' : `${value.toFixed(1)} ms`),
    },
    {
      key: 'actions',
      width: 132,
      render: (_, item) => (
        <Space size={0}>
          <ManagementIconButton
            aria-label="查看服务详情"
            icon={<EyeOutlined />}
            tooltip="查看服务详情"
            onClick={() => setSelectedServiceId(item.id)}
          />
          <ManagementIconButton
            aria-label="查看服务指标"
            icon={<LineChartOutlined />}
            tooltip="查看服务指标"
            onClick={() => navigate(signalPath('metrics', item.name))}
          />
          <ManagementIconButton
            aria-label="查看服务链路"
            icon={<ShareAltOutlined />}
            tooltip="查看服务链路"
            onClick={() => navigate(signalPath('traces', item.name))}
          />
        </Space>
      ),
    },
  ]

  return (
    <div className="soha-page soha-signal-page">
      <SignalQueryForm
        form={form}
        initialValues={{
          dataSourceId: searchParams.get('dataSourceId') ?? undefined,
          rangeMinutes: 60,
          service: searchParams.get('service') ?? undefined,
          timeFrom: searchParams.get('from') ?? undefined,
          timeTo: searchParams.get('to') ?? undefined,
        }}
        loading={services.isFetching}
        showWorkload={false}
        submitLabel="查询服务"
        onFinish={(values) => {
          const next = serviceRequest(
            values,
            requestedClusterId,
            requestedNamespace,
            searchParams.get('dataSourceId'),
          )
          setRequest(next)
          setSearchParams(
            signalSearchParams(searchParams, {
              dataSourceId: next.dataSourceId,
              cluster: next.clusterId,
              namespace: next.namespace,
              service: next.service,
              from: next.timeFrom,
              to: next.timeTo,
            }),
            { replace: true },
          )
        }}
      />
      {servicesData?.meta.warnings?.map((warning) => (
        <Alert key={warning} title={warning} showIcon type="warning" />
      ))}
      {services.error instanceof ApiError && services.error.status === 404 ? (
        <Text type="secondary">请确认服务查询 Provider 和链接指定的数据源仍可用。</Text>
      ) : null}
      <AdminTable
        enableDensity
        error={services.error}
        hasReadData={services.data !== undefined}
        refreshing={services.isFetching}
        onRefresh={() => void services.refetch()}
        loading={services.isLoading}
        columnSettingIconOnly
        columnSettingPlacement="header"
        columns={columns}
        dataSource={servicesData?.items ?? []}
        empty={<ManagementState bordered={false} compact description="当前范围没有发现服务" />}
        pagination={false}
        rowKey="id"
        shellClassName="soha-management-table-shell"
        scroll={{ x: 'max-content' }}
      />

      <Drawer
        destroyOnHidden
        open={Boolean(selectedServiceId)}
        size="large"
        extra={
          <ManagementIconButton
            aria-label="刷新服务详情"
            tooltip="刷新服务详情"
            icon={<ReloadOutlined spin={service.isFetching || topology.isFetching} />}
            onClick={() => {
              void service.refetch()
              void topology.refetch()
            }}
          />
        }
        title={serviceData?.data.displayName || serviceData?.data.name || '服务详情'}
        onClose={() => setSelectedServiceId('')}
      >
        <ReadFeedback label="服务详情" query={service} />
        {service.isLoading ? (
          <ManagementState bordered={false} compact kind="loading" title="正在加载服务" />
        ) : serviceData ? (
          <>
            <Descriptions column={2} size="small">
              <Descriptions.Item label="状态">
                <StatusTag
                  label={statusLabel(serviceData.data.status)}
                  value={serviceData.data.status}
                />
              </Descriptions.Item>
              <Descriptions.Item label="命名空间">
                {serviceData.data.namespace || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="实例">
                {serviceData.data.instances.length}
              </Descriptions.Item>
              <Descriptions.Item label="端点">
                {serviceData.data.endpoints.length}
              </Descriptions.Item>
            </Descriptions>
            <div className="soha-service-drawer-section">
              <Title level={5}>端点</Title>
              {serviceData.data.endpoints.length ? (
                <ul className="soha-list-panel">
                  {serviceData.data.endpoints.map((endpoint) => (
                    <li key={endpoint.name} className="soha-list-row">
                      <Text>{endpoint.name}</Text>
                      <div className="soha-list-row-extra">
                        <StatusTag label={statusLabel(endpoint.status)} value={endpoint.status} />
                      </div>
                    </li>
                  ))}
                </ul>
              ) : (
                <ManagementState bordered={false} compact description="暂无端点" />
              )}
            </div>
            <div className="soha-service-drawer-section">
              <Space>
                <Title level={5}>依赖拓扑</Title>
                <MetadataTag label="Beta" tone="gold" />
              </Space>
              <ReadFeedback label="服务依赖" query={topology} />
              {topology.isLoading ? (
                <ManagementState bordered={false} compact kind="loading" title="正在加载拓扑" />
              ) : !topologyData ? null : topologyData.data.edges.length ? (
                <div className="soha-signal-data-table-wrap">
                  <table aria-label="服务依赖数据表">
                    <thead>
                      <tr>
                        <th scope="col">来源</th>
                        <th scope="col">目标</th>
                        <th scope="col">状态</th>
                        <th scope="col">请求率</th>
                        <th scope="col">错误率</th>
                        <th scope="col">P95</th>
                      </tr>
                    </thead>
                    <tbody>
                      {(topologyData?.data.edges ?? []).map((edge) => (
                        <tr key={`${edge.sourceServiceId}:${edge.targetServiceId}`}>
                          <td>
                            <Text code>{edge.sourceServiceId}</Text>
                          </td>
                          <td>
                            <Text code>{edge.targetServiceId}</Text>
                          </td>
                          <td>
                            <StatusTag label={statusLabel(edge.status)} value={edge.status} />
                          </td>
                          <td>
                            {edge.requestRate === undefined
                              ? '-'
                              : `${edge.requestRate.toFixed(2)}/s`}
                          </td>
                          <td>
                            {edge.errorRate === undefined
                              ? '-'
                              : `${(edge.errorRate * 100).toFixed(2)}%`}
                          </td>
                          <td>
                            {edge.latencyP95Ms === undefined
                              ? '-'
                              : `${edge.latencyP95Ms.toFixed(1)} ms`}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <ManagementState bordered={false} compact description="当前范围暂无服务依赖" />
              )}
            </div>
          </>
        ) : null}
      </Drawer>
    </div>
  )
}
