import { ReadFeedback } from '@/components/request-feedback'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { NetworkResourceListPage } from '../shared/list-page'
import { buildGatewayAPIRoutePath } from '../shared/paths'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { TableCellLink, TableCellText } from '@/components/table-cell-content'
import { gatewayAPIQueries } from './queries'
import type { BackendTLSPolicy } from './types'
import './backend-tls.css'

function ReferenceSummary({ values }: { values?: readonly string[] }) {
  if (!values?.length) return <span>—</span>
  return (
    <div className="soha-backend-tls-reference-summary">
      {values.slice(0, 2).map((value, index) => (
        <TableCellText key={`${value}/${index}`} value={value} />
      ))}
      {values.length > 2 && (
        <details>
          <summary>+{values.length - 2}</summary>
          {values.slice(2).map((value, index) => (
            <TableCellText key={`${value}/${index}`} value={value} />
          ))}
        </details>
      )}
    </div>
  )
}

export function NetworkBackendTLSPoliciesPage() {
  const { localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const navigate = useNavigate(),
    permissions = readableQueryData(usePermissionSnapshot())?.data
  const permitted = (action: string) =>
    hasPermission(permissions, `platform.network.backend-tls-policies.${action}`)
  const canRead = permitted('view'),
    refresh = useWorkloadRefresh()
  const options = gatewayAPIQueries.list<BackendTLSPolicy>(
    'backendtlspolicies',
    toScopeKey(clusterId, namespace),
  )
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const path = (record: BackendTLSPolicy, tab?: string) =>
    buildGatewayAPIRoutePath('backendtlspolicies', record.name, record.namespace, {
      clusterId: clusterId || '',
      tab,
    })
  const columns: TableColumnsType<BackendTLSPolicy> = [
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
      title: localeCode === 'zh_CN' ? '应用目标' : 'Targets',
      dataIndex: 'targetRefs',
      width: 280,
      minWidth: 240,
      render: (value?: string[]) => <ReferenceSummary values={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '校验域名' : 'Hostname',
      dataIndex: 'hostname',
      width: 240,
      minWidth: 180,
      render: (value?: string) => <TableCellText value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? 'CA 来源' : 'CA source',
      dataIndex: 'caCertificateRefs',
      width: 280,
      minWidth: 240,
      render: (value: string[] | undefined, record) =>
        record.wellKnownCACertificates || <ReferenceSummary values={value} />,
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
      <ReadFeedback canRead={canRead} query={query} label="资源记录" />
      {canRead && (
        <NetworkResourceListPage
          buildAIPageContext={(items, searchKeyword) => ({
            sourceWorkbench: 'platform',
            sourceRoute: '/network/gateway-api/backendtlspolicies',
            sourceTitle: 'BackendTLSPolicies',
            entityKind: 'kubernetes.backendtlspolicy.list',
            entityName: 'BackendTLSPolicies',
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
              resourceLabel="BackendTLSPolicies"
              onRefresh={() => query.refetch()}
            />
          }
          emptyDescription={{
            zh_CN: '当前未读取到 BackendTLSPolicy',
            en_US: 'No BackendTLSPolicies returned',
          }}
          kind="backendtlspolicies"
          noMatchDescription={{ zh_CN: '没有匹配的资源', en_US: 'No matching resources' }}
          query={query}
          searchPlaceholder={{
            zh_CN: '搜索 BackendTLSPolicy / namespace / target / hostname',
            en_US: 'Search BackendTLSPolicy / namespace / target / hostname',
          }}
          searchValues={(record) => [
            record.name,
            record.namespace,
            record.hostname,
            record.wellKnownCACertificates,
            ...(record.targetRefs ?? []),
            ...(record.caCertificateRefs ?? []),
          ]}
        />
      )}
    </>
  )
}
