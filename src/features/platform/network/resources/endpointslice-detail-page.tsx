import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { BooleanTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Card } from 'antd'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { NetworkDetailShell } from '../shared/detail-shell'
import { resolveNetworkNamespace } from '../shared/scope'
import { networkCoreQueries } from './queries'
import type { EndpointSliceDetail, EndpointSliceEndpoint } from './types'

export function EndpointSliceDetailPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId, namespace } = usePlatformScopeStore()
  const name = (useParams().name as string | undefined) ?? ''
  const [searchParams] = useSearchParams()
  const detailNamespace = resolveNetworkNamespace(namespace, searchParams.get('namespace'))
  const scope = toScopeKey(clusterId, detailNamespace)
  const [activeTabKey, setActiveTabKey] = useResourceDetailTab()
  const query = useQuery(
    networkCoreQueries.detail<EndpointSliceDetail>('endpointslices', scope, name),
  )
  const visibleQueryData = readableQueryData(query)
  const detail = visibleQueryData
  if (!clusterId || !detailNamespace)
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
  if (query.isLoading) return <Card loading className="soha-detail-card" />
  if (!detail)
    return (
      <div className="soha-page">
        <ManagementState
          kind="not-found"
          description={localeCode === 'zh_CN' ? 'EndpointSlice 未找到' : 'EndpointSlice not found'}
        />
      </div>
    )
  const endpointColumns: TableColumnsType<EndpointSliceEndpoint> = [
    { title: localeCode === 'zh_CN' ? '地址' : 'Address', dataIndex: 'address' },
    {
      title: localeCode === 'zh_CN' ? '就绪' : 'Ready',
      dataIndex: 'ready',
      render: (value?: boolean) =>
        value === undefined ? '-' : <BooleanTag value={value} trueLabel="Yes" falseLabel="No" />,
    },
    {
      title: 'Serving',
      dataIndex: 'serving',
      render: (value?: boolean) =>
        value === undefined ? '-' : <BooleanTag value={value} trueLabel="Yes" falseLabel="No" />,
    },
    {
      title: 'Terminating',
      dataIndex: 'terminating',
      render: (value?: boolean) =>
        value === undefined ? '-' : <BooleanTag value={value} trueLabel="Yes" falseLabel="No" />,
    },
    {
      title: localeCode === 'zh_CN' ? '目标' : 'Target',
      dataIndex: 'targetRef',
      render: (value?: string) =>
        value?.startsWith('Pod/') ? (
          <Button
            type="text"
            onClick={() =>
              navigate(
                `/workloads/pods/${encodeURIComponent(value.slice(4))}?${new URLSearchParams({ namespace: detail.namespace })}`,
              )
            }
          >
            {value}
          </Button>
        ) : (
          value || '-'
        ),
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
  return (
    <>
      <ReadFeedback canRead={query.isEnabled} query={query} label="资源记录" />
      <NetworkDetailShell
        activeTabKey={activeTabKey}
        detail={detail}
        kind="endpointslices"
        label="EndpointSlice"
        onTabChange={setActiveTabKey}
        overviewExtra={[
          { key: 'Address Type', value: detail.addressType || '-' },
          {
            key: 'Service',
            value: detail.serviceName ? (
              <Button
                type="text"
                onClick={() =>
                  navigate(
                    `/network/services/${encodeURIComponent(detail.serviceName!)}?${new URLSearchParams({ namespace: detail.namespace })}`,
                  )
                }
              >
                {detail.serviceName}
              </Button>
            ) : (
              '-'
            ),
          },
          { key: 'Endpoints', value: detail.endpoints?.length ?? 0 },
          { key: 'Ports', value: detail.ports?.join(', ') || '-' },
        ]}
        overviewContent={
          <Card className="soha-detail-card" title="Endpoints">
            <AdminTable
              columns={endpointColumns}
              dataSource={detail.endpoints ?? []}
              rowKey={(record) => `${record.address}/${record.targetRef ?? ''}`}
              pagination={false}
              scroll={{ x: 800, y: 520 }}
              tableSize="small"
              enableColumnSelection={false}
            />
          </Card>
        }
        target={{ scope, name }}
      />
    </>
  )
}
