import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { networkResourcePath } from '../shared/resource-links'
import { IngressEditModal } from './edit-modal'
import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import './styles.css'
import { ReadFeedback } from '@/components/request-feedback'
import { TableCellLink } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { NetworkResourceListPage } from '../shared/list-page'
import { buildNetworkRoutePath } from '../shared/paths'
import { renderNetworkTextList } from '../shared/renderers'
import { ingressQueries } from './queries'
import type { Ingress } from './types'

export function NetworkIngressesPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId, namespace } = usePlatformScopeStore()
  const scope = toScopeKey(clusterId, namespace)
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = hasPermission(permissions, 'platform.network.ingresses.view')
  const canUpdate = hasPermission(permissions, 'platform.network.ingresses.update')
  const canDelete = hasPermission(permissions, 'platform.network.ingresses.delete')
  const refresh = useWorkloadRefresh()
  const options = ingressQueries.list(scope)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const [editing, setEditing] = useState<{
    record: Ingress
    clusterId: string
    namespace: string | null
  }>()
  useEffect(() => setEditing(undefined), [clusterId, namespace])
  const editedRow = readableQueryData(query)?.find(
    (item) => item.name === editing?.record.name && item.namespace === editing?.record.namespace,
  )
  const editAllowed = canUpdate && hasAllowedAction(editedRow?.allowedActions, 'update')
  useEffect(() => {
    if (!editAllowed) setEditing(undefined)
  }, [editAllowed])
  const refLink = (kind: string, name: string, ns?: string) =>
    networkResourcePath(permissions, { clusterId: clusterId || '', namespace: ns, kind, name })
  const columns: TableColumnsType<Ingress> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      ellipsis: { showTitle: false },
      width: 320,
      render: (value: string, record) => (
        <TableCellLink
          label={value}
          onClick={() =>
            navigate(
              `${buildNetworkRoutePath('ingresses', value, record.namespace)}&clusterId=${encodeURIComponent(clusterId || '')}`,
            )
          }
        />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 160,
    },
    {
      title: 'IngressClass',
      dataIndex: 'className',
      width: 160,
      render: (value?: string) => {
        const path = value ? refLink('IngressClass', value) : null
        return path ? <Link to={path}>{value}</Link> : value || '—'
      },
    },
    {
      title: localeCode === 'zh_CN' ? '域名' : 'Hosts',
      width: 240,
      dataIndex: 'hosts',
      render: (value?: string[]) => renderNetworkTextList(value),
    },
    {
      title: localeCode === 'zh_CN' ? '地址' : 'Address',
      dataIndex: 'address',
      width: 210,
      render: (value?: string) => (
        <span className="soha-ingress-wrap">
          {value || (localeCode === 'zh_CN' ? '未分配' : 'Not assigned')}
        </span>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '后端 Service' : 'Backend Services',
      width: 240,
      dataIndex: 'backendServices',
      render: (value: string[] | undefined, record: Ingress) =>
        value?.length ? (
          <div className="soha-ingress-reference-list">
            {value.map((name) => {
              const path = refLink('Service', name, record.namespace)
              return path ? (
                <Link key={name} to={path}>
                  {name}
                </Link>
              ) : (
                <span key={name}>{name}</span>
              )
            })}
          </div>
        ) : (
          '—'
        ),
    },
    {
      title: localeCode === 'zh_CN' ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 104,
      render: (value: number) => formatAgeSeconds(value),
    },
  ]

  if (!canRead) return <ReadFeedback canRead={false} query={query} label="Ingresses" />
  return (
    <>
      <ReadFeedback canRead={query.isEnabled} query={query} label="资源记录" />
      <NetworkResourceListPage
        buildAIPageContext={(items, searchKeyword) => ({
          sourceWorkbench: 'platform',
          sourceRoute: '/network/ingresses',
          sourceTitle: localeCode === 'zh_CN' ? 'Ingresses 列表' : 'Ingresses',
          entityKind: 'kubernetes.ingress.list',
          entityName: 'Ingresses',
          clusterId: clusterId ?? undefined,
          namespace: namespace ?? undefined,
          timeRangeMinutes: 60,
          visibleFilters: { searchKeyword },
          pinnedData: { total: items.length },
          promptHint: '分析当前 Ingress 列表的域名、地址、IngressClass 和后端 Service 风险。',
        })}
        columns={columns}
        deletable={canDelete}
        onEdit={
          canUpdate
            ? (record) =>
                setEditing({ record, clusterId: clusterId || '', namespace: namespace || null })
            : undefined
        }
        refreshControl={
          <WorkloadRefresh
            settings={refresh}
            queries={[query]}
            onRefresh={() => query.refetch()}
            resourceLabel="Ingresses"
          />
        }
        emptyDescription={{
          zh_CN: '当前范围没有 Ingress',
          en_US: 'No ingresses in the current scope',
        }}
        kind="ingresses"
        noMatchDescription={{ zh_CN: '没有匹配的资源', en_US: 'No matching resources' }}
        query={query}
        searchPlaceholder={{
          zh_CN: '搜索 Ingress / namespace / host / service',
          en_US: 'Search ingress / namespace / host / service',
        }}
        searchValues={(record) => [
          record.name,
          record.namespace,
          record.className,
          record.address,
          ...(record.hosts ?? []),
          ...(record.backendServices ?? []),
        ]}
      />
      {editAllowed &&
        editing &&
        editing.clusterId === clusterId &&
        editing.namespace === (namespace || null) && (
          <IngressEditModal
            key={`${editing.clusterId}/${editing.record.namespace}/${editing.record.name}`}
            clusterId={editing.clusterId}
            ingress={editing.record}
            onClose={() => setEditing(undefined)}
          />
        )}
    </>
  )
}
