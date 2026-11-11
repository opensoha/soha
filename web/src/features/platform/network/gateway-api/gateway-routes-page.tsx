import { ReadFeedback } from '@/components/request-feedback'
import { TableCellLink } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import { Button, type TableColumnsType } from 'antd'
import { Link, useNavigate } from 'react-router-dom'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { NetworkResourceListPage } from '../shared/list-page'
import { buildGatewayAPIRoutePath } from '../shared/paths'
import { networkResourcePath } from '../shared/resource-links'
import { projectedGatewayReference } from './httproute-configuration'
import { HTTPRouteValues } from './httproute-panels'
import { gatewayAPIQueries } from './queries'
import type { HTTPRoute, GRPCRoute } from './types'
type GatewayRoute = HTTPRoute | GRPCRoute

export function GatewayRoutesPage({ kind }: { kind: 'httproutes' | 'grpcroutes' }) {
  const label = kind === 'grpcroutes' ? 'GRPCRoute' : 'HTTPRoute'
  const permissionSegment = kind === 'grpcroutes' ? 'grpc-routes' : 'http-routes'
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { clusterId, namespace } = usePlatformScopeStore(),
    navigate = useNavigate()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const permitted = (action: string) =>
    hasPermission(permissions, `platform.network.${permissionSegment}.${action}`)
  const canRead = permitted('view'),
    canGateways = hasPermission(permissions, 'platform.network.gateways.view'),
    canServices = hasPermission(permissions, 'platform.network.services.view')
  const refresh = useWorkloadRefresh(),
    options = gatewayAPIQueries.list<GatewayRoute>(kind, toScopeKey(clusterId, namespace))
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const path = (record: GatewayRoute, tab?: string) =>
    buildGatewayAPIRoutePath(kind, record.name, record.namespace, {
      clusterId: clusterId || '',
      tab,
    })
  const columns: TableColumnsType<GatewayRoute> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 280,
      ellipsis: { showTitle: false },
      render: (value: string, record) => (
        <TableCellLink label={value} onClick={() => navigate(path(record))} />
      ),
    },
    { title: zh ? '命名空间' : 'Namespace', dataIndex: 'namespace', width: 160 },
    {
      title: zh ? '域名' : 'Hostnames',
      dataIndex: 'hostnames',
      width: 260,
      render: (value?: string[]) => (
        <HTTPRouteValues compact values={value} empty={zh ? '未限定域名' : 'Any hostname'} />
      ),
    },
    {
      title: zh ? '父级引用' : 'Parents',
      dataIndex: 'parentRefs',
      width: 260,
      render: (values?: string[]) => (
        <div className="soha-gateway-values">
          {canGateways && values?.length
            ? values.map((value, index) => {
                const ref = projectedGatewayReference(value)
                const href =
                  ref && networkResourcePath(permissions, { ...ref, clusterId: clusterId || '' })
                return href ? (
                  <Link className="soha-gateway-wrap" key={`${value}/${index}`} to={href}>
                    {value}
                  </Link>
                ) : (
                  <span key={`${value}/${index}`} className="soha-gateway-wrap">
                    {value}
                  </span>
                )
              })
            : '—'}
        </div>
      ),
    },
    {
      title: zh ? '后端摘要' : 'Backend summary',
      dataIndex: 'backendServices',
      width: 240,
      render: (values: string[] | undefined, record) =>
        canServices && values?.length ? (
          <>
            <HTTPRouteValues compact values={values} />
            <Button
              type="link"
              className="soha-gateway-cell-button"
              onClick={() => navigate(path(record, 'rules'))}
            >
              {zh ? '查看规则' : 'View rules'}
            </Button>
          </>
        ) : (
          '—'
        ),
    },
    ...(kind === 'grpcroutes'
      ? [
          {
            title: zh ? '规则' : 'Rules',
            dataIndex: 'ruleCount',
            width: 100,
            render: (count: number, record: GatewayRoute) => (
              <Link to={path(record, 'rules')}>{count}</Link>
            ),
          },
        ]
      : []),
    {
      title: zh ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 104,
      render: (value: number) => formatAgeSeconds(value),
    },
  ]
  return (
    <>
      <ReadFeedback canRead={canRead} query={query} label={`${label}s`} />
      {canRead && (
        <NetworkResourceListPage
          kind={kind}
          query={query}
          columns={columns}
          buildAIPageContext={(items, searchKeyword) => ({
            sourceWorkbench: 'platform',
            sourceRoute: `/network/gateway-api/${kind}`,
            sourceTitle: `${label}s`,
            entityKind: `kubernetes.${kind === 'grpcroutes' ? 'grpcroute' : 'httproute'}.list`,
            entityName: `${label}s`,
            clusterId: clusterId ?? undefined,
            namespace: namespace ?? undefined,
            visibleFilters: { searchKeyword },
            pinnedData: { total: items.length },
          })}
          deletable={permitted('delete')}
          onEdit={permitted('update') ? (record) => navigate(path(record, 'yaml')) : undefined}
          emptyDescription={{ zh_CN: `当前未读取到 ${label}`, en_US: `No ${label}s returned` }}
          noMatchDescription={{ zh_CN: `没有匹配的 ${label}`, en_US: `No matching ${label}s` }}
          searchPlaceholder={{
            zh_CN: '搜索名称 / 命名空间 / 域名 / 父级 / 后端',
            en_US: 'Search name / namespace / hostname / parent / backend',
          }}
          searchValues={(record) => [
            record.name,
            record.namespace,
            ...(record.hostnames || []),
            ...(canGateways ? record.parentRefs || [] : []),
            ...(canServices ? record.backendServices || [] : []),
          ]}
          refreshControl={
            <WorkloadRefresh
              compact
              disabled={!clusterId}
              settings={refresh}
              queries={[query]}
              resourceLabel={`${label}s`}
              onRefresh={() => query.refetch()}
            />
          }
        />
      )}
    </>
  )
}
