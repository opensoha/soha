import { ReadFeedback } from '@/components/request-feedback'
import { TableCellLink, TableCellText } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { NetworkResourceListPage } from '../shared/list-page'
import { buildGatewayAPIRoutePath } from '../shared/paths'
import { GatewayConditionTag } from './gateway-panels'
import { gatewayAPIQueries } from './queries'
import type { GatewayClass } from './types'

export function NetworkGatewayClassesPage() {
  const navigate = useNavigate()
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { clusterId } = usePlatformScopeStore()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const permitted = (action: string) =>
    hasPermission(permissions, `platform.network.gateway-classes.${action}`)
  const canRead = permitted('view'),
    refresh = useWorkloadRefresh()
  const options = gatewayAPIQueries.list<GatewayClass>(
    'gatewayclasses',
    toScopeKey(clusterId, null),
  )
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const path = (name: string, tab?: string) =>
    buildGatewayAPIRoutePath('gatewayclasses', name, '', { clusterId: clusterId || '', tab })
  const columns: TableColumnsType<GatewayClass> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 280,
      ellipsis: { showTitle: false },
      render: (name: string) => <TableCellLink label={name} onClick={() => navigate(path(name))} />,
    },
    {
      title: zh ? '控制器' : 'Controller',
      dataIndex: 'controllerName',
      width: 340,
      render: (value?: string) => <TableCellText value={value} />,
    },
    {
      title: 'Accepted',
      dataIndex: 'accepted',
      width: 150,
      render: (value?: string) => (
        <GatewayConditionTag compact condition={{ type: 'Accepted', status: value || 'Unknown' }} />
      ),
    },
    {
      title: zh ? '参数引用' : 'Parameters',
      dataIndex: 'parametersRef',
      width: 280,
      render: (value?: string) => (
        <TableCellText value={value || (zh ? '未配置' : 'Not configured')} />
      ),
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
      <ReadFeedback canRead={canRead} query={query} label="GatewayClasses" />
      {canRead && (
        <NetworkResourceListPage
          buildAIPageContext={(items, searchKeyword) => ({
            sourceWorkbench: 'platform',
            sourceRoute: '/network/gateway-api/gatewayclasses',
            sourceTitle: 'GatewayClasses',
            entityKind: 'kubernetes.gatewayclass.list',
            entityName: 'GatewayClasses',
            clusterId: clusterId ?? undefined,
            visibleFilters: { searchKeyword },
            pinnedData: { total: items.length },
          })}
          columns={columns}
          kind="gatewayclasses"
          query={query}
          rowKey="name"
          deletable={permitted('delete')}
          onEdit={permitted('update') ? (record) => navigate(path(record.name, 'yaml')) : undefined}
          emptyDescription={{
            zh_CN: '当前未读取到 GatewayClass',
            en_US: 'No GatewayClasses returned',
          }}
          noMatchDescription={{
            zh_CN: '没有匹配的 GatewayClass',
            en_US: 'No matching GatewayClasses',
          }}
          searchPlaceholder={{
            zh_CN: '搜索名称 / 控制器 / 参数 / Accepted',
            en_US: 'Search name / controller / parameters / Accepted',
          }}
          searchValues={(record) => [
            record.name,
            record.controllerName,
            record.accepted,
            record.parametersRef,
          ]}
          refreshControl={
            <WorkloadRefresh
              compact
              disabled={!clusterId}
              settings={refresh}
              queries={[query]}
              resourceLabel="GatewayClasses"
              onRefresh={() => query.refetch()}
            />
          }
        />
      )}
    </>
  )
}
