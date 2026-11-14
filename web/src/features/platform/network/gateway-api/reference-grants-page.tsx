import { ReadFeedback } from '@/components/request-feedback'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { TableCellLink } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { NetworkResourceListPage } from '../shared/list-page'
import { buildGatewayAPIRoutePath } from '../shared/paths'
import { GrantSummary } from './reference-grant-panels'
import { gatewayAPIQueries } from './queries'
import type { ReferenceGrant } from './types'

export function NetworkReferenceGrantsPage() {
  const { localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const navigate = useNavigate()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const permitted = (action: string) =>
    hasPermission(permissions, `platform.network.reference-grants.${action}`)
  const canRead = permitted('view'),
    refresh = useWorkloadRefresh()
  const options = gatewayAPIQueries.list<ReferenceGrant>(
    'referencegrants',
    toScopeKey(clusterId, namespace),
  )
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const path = (record: ReferenceGrant, tab?: string) =>
    buildGatewayAPIRoutePath('referencegrants', record.name, record.namespace, {
      clusterId: clusterId || '',
      tab,
    })
  const columns: TableColumnsType<ReferenceGrant> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      width: 260,
      render: (value: string, record) => (
        <TableCellLink label={value} onClick={() => navigate(path(record))} />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 160,
    },
    {
      title: localeCode === 'zh_CN' ? '允许来源 · From' : 'Allowed sources · From',
      dataIndex: 'from',
      width: 320,
      minWidth: 240,
      render: (value?: string[]) => <GrantSummary labels={value} direction="from" />,
    },
    {
      title: localeCode === 'zh_CN' ? '允许目标 · To' : 'Allowed targets · To',
      dataIndex: 'to',
      width: 300,
      minWidth: 240,
      render: (value?: string[]) => <GrantSummary labels={value} direction="to" />,
    },
    {
      title: localeCode === 'zh_CN' ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 104,
      render: (value: number) => formatAgeSeconds(value),
    },
  ]
  return (
    <>
      <ReadFeedback canRead={canRead} query={query} label="ReferenceGrants" />
      {canRead && (
        <NetworkResourceListPage
          buildAIPageContext={(items, searchKeyword) => ({
            sourceWorkbench: 'platform',
            sourceRoute: '/network/gateway-api/referencegrants',
            sourceTitle: 'ReferenceGrants',
            entityKind: 'kubernetes.referencegrant.list',
            entityName: 'ReferenceGrants',
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
              resourceLabel="ReferenceGrants"
              onRefresh={() => query.refetch()}
            />
          }
          emptyDescription={{
            zh_CN: '当前未读取到 ReferenceGrant',
            en_US: 'No ReferenceGrants returned',
          }}
          kind="referencegrants"
          noMatchDescription={{ zh_CN: '没有匹配的资源', en_US: 'No matching resources' }}
          query={query}
          searchPlaceholder={{
            zh_CN: '搜索 ReferenceGrant / namespace / from / to',
            en_US: 'Search ReferenceGrant / namespace / from / to',
          }}
          searchValues={(record) => [
            record.name,
            record.namespace,
            ...(record.from ?? []),
            ...(record.to ?? []),
          ]}
        />
      )}
    </>
  )
}
