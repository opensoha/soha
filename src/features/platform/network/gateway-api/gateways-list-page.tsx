import { ReadFeedback } from '@/components/request-feedback'
import { TableCellLink } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import type { TableColumnsType } from 'antd'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { NetworkResourceListPage } from '../shared/list-page'
import { buildGatewayAPIRoutePath } from '../shared/paths'
import { networkResourcePath } from '../shared/resource-links'
import { GatewayAddresses } from './gateway-panels'
import { gatewayAPIQueries } from './queries'
import type { Gateway } from './types'
import './gateway.css'

export function NetworkGatewaysPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const navigate = useNavigate(),
    { clusterId, namespace } = usePlatformScopeStore()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const permitted = (action: string) =>
    hasPermission(permissions, `platform.network.gateways.${action}`)
  const canRead = permitted('view'),
    refresh = useWorkloadRefresh()
  const detailPath = (record: Gateway, tab?: string) =>
    buildGatewayAPIRoutePath('gateways', record.name, record.namespace, {
      clusterId: clusterId || '',
      tab,
    })
  const options = gatewayAPIQueries.list<Gateway>('gateways', toScopeKey(clusterId, namespace))
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const columns: TableColumnsType<Gateway> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 300,
      ellipsis: { showTitle: false },
      render: (value: string, record) => (
        <TableCellLink label={value} onClick={() => navigate(detailPath(record))} />
      ),
    },
    { title: zh ? '命名空间' : 'Namespace', dataIndex: 'namespace', width: 160 },
    {
      title: 'GatewayClass',
      dataIndex: 'gatewayClass',
      width: 190,
      render: (value?: string) => {
        const path = value
          ? networkResourcePath(permissions, {
              kind: 'GatewayClass',
              clusterId: clusterId || '',
              name: value,
            })
          : null
        return path ? (
          <Link className="soha-gateway-wrap" to={path}>
            {value}
          </Link>
        ) : (
          <span className="soha-gateway-wrap">{value || '—'}</span>
        )
      },
    },
    {
      title: zh ? '已分配地址' : 'Assigned addresses',
      dataIndex: 'addresses',
      width: 260,
      render: (value?: string[]) => (
        <GatewayAddresses addresses={value} empty={zh ? '未返回地址' : 'No address returned'} />
      ),
    },
    {
      title: zh ? '监听器' : 'Listeners',
      dataIndex: 'listenerCount',
      width: 110,
      render: (value: number, record) => <Link to={detailPath(record, 'listeners')}>{value}</Link>,
    },
    {
      title: zh ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 110,
      render: (value: number) => formatAgeSeconds(value),
    },
  ]
  return (
    <>
      <ReadFeedback canRead={canRead} query={query} label="Gateways" />
      {canRead && (
        <NetworkResourceListPage
          kind="gateways"
          query={query}
          columns={columns}
          rowKey={(item: Gateway) => `${item.namespace}/${item.name}`}
          deletable={permitted('delete')}
          onEdit={
            permitted('update') ? (record) => navigate(detailPath(record, 'yaml')) : undefined
          }
          buildAIPageContext={(items, searchKeyword) => ({
            sourceWorkbench: 'platform',
            sourceRoute: '/network/gateway-api/gateways',
            sourceTitle: 'Gateways',
            entityKind: 'kubernetes.gateway.list',
            entityName: 'Gateways',
            clusterId: clusterId ?? undefined,
            namespace: namespace ?? undefined,
            visibleFilters: { searchKeyword },
            pinnedData: { total: items.length },
          })}
          emptyDescription={{
            zh_CN: '当前未读取到 Gateway',
            en_US: 'No Gateways returned in the current scope',
          }}
          noMatchDescription={{ zh_CN: '没有匹配的 Gateway', en_US: 'No matching Gateways' }}
          searchPlaceholder={{
            zh_CN: '搜索网关 / 命名空间 / Class / 地址',
            en_US: 'Search gateway / namespace / Class / address',
          }}
          searchValues={(record) => [
            record.name,
            record.namespace,
            record.gatewayClass,
            ...(record.addresses || []),
          ]}
          refreshControl={
            <WorkloadRefresh
              compact
              disabled={!clusterId}
              settings={refresh}
              queries={[query]}
              resourceLabel="Gateways"
              onRefresh={() => query.refetch()}
            />
          }
        />
      )}
    </>
  )
}
