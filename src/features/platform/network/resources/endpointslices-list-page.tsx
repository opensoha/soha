import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { SlicePorts } from './endpointslice-panels'
import { ReadFeedback } from '@/components/request-feedback'
import { TableCellLink } from '@/components/table-cell-content'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { NetworkResourceListPage } from '../shared/list-page'
import { networkCoreQueries } from './queries'
import type { EndpointSlice } from './types'

export function NetworkEndpointSlicesPage() {
  const navigate = useNavigate()
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const permitted = (action: string) =>
    hasPermission(permissions, `platform.network.endpoint-slices.${action}`)
  const canRead = permitted('view'),
    refresh = useWorkloadRefresh()
  const { clusterId, namespace } = usePlatformScopeStore()
  const options = networkCoreQueries.list<EndpointSlice>(
    'endpointslices',
    toScopeKey(clusterId, namespace),
  )
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const path = (record: EndpointSlice, tab?: string) => {
    const params = new URLSearchParams({ clusterId: clusterId || '', namespace: record.namespace })
    if (tab) params.set('tab', tab)
    return `/network/endpointslices/${encodeURIComponent(record.name)}?${params}`
  }
  const columns: TableColumnsType<EndpointSlice> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      ellipsis: { showTitle: false },
      width: 280,
      render: (value: string, record) => (
        <TableCellLink label={value} onClick={() => navigate(path(record))} />
      ),
    },
    { title: zh ? '命名空间' : 'Namespace', dataIndex: 'namespace', width: 160 },
    { title: zh ? '地址族' : 'Address type', dataIndex: 'addressType', width: 130 },
    { title: zh ? '端点数' : 'Endpoints', dataIndex: 'endpoints', width: 110 },
    {
      title: zh ? '端口' : 'Ports',
      dataIndex: 'ports',
      width: 280,
      minWidth: 220,
      render: (value?: string[]) => <SlicePorts ports={value} />,
    },
    {
      title: zh ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 104,
      render: (value: number) => formatAgeSeconds(value),
    },
  ]
  return (
    <>
      <ReadFeedback canRead={canRead} query={query} label="资源记录" />
      {canRead && (
        <NetworkResourceListPage
          buildAIPageContext={(items, searchKeyword) => ({
            sourceWorkbench: 'platform',
            sourceRoute: '/network/endpointslices',
            sourceTitle: 'EndpointSlices',
            entityKind: 'kubernetes.endpointslice.list',
            entityName: 'EndpointSlices',
            clusterId: clusterId ?? undefined,
            namespace: namespace ?? undefined,
            visibleFilters: { searchKeyword },
            pinnedData: { total: items.length },
          })}
          columns={columns}
          deletable={permitted('delete')}
          onEdit={permitted('update') ? (record) => navigate(path(record, 'yaml')) : undefined}
          refreshControl={
            <WorkloadRefresh
              compact
              disabled={!clusterId}
              settings={refresh}
              queries={[query]}
              resourceLabel="EndpointSlices"
              onRefresh={() => query.refetch()}
            />
          }
          emptyDescription={{
            zh_CN: '当前范围没有 EndpointSlice',
            en_US: 'No EndpointSlices in the current scope',
          }}
          kind="endpointslices"
          noMatchDescription={{ zh_CN: '没有匹配的资源', en_US: 'No matching resources' }}
          query={query}
          searchPlaceholder={{
            zh_CN: '搜索 EndpointSlice / namespace / address type / port',
            en_US: 'Search EndpointSlice / namespace / address type / port',
          }}
          searchValues={(record) => [
            record.name,
            record.namespace,
            record.addressType,
            ...(record.ports ?? []),
          ]}
        />
      )}
    </>
  )
}
