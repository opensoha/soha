import { useCallback, useMemo } from 'react'
import { ResourceNameLink } from '@/features/platform/shared/resource-table-cells'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import { Typography, type TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { NetworkResourceListPage } from '../shared/list-page'
import { networkCoreQueries } from './queries'
import type { IngressClass } from './types'
import './ingressclass.css'

export function NetworkIngressClassesPage() {
  const navigate = useNavigate()
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { clusterId } = usePlatformScopeStore()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const permitted = (action: string) =>
    hasPermission(permissions, `platform.network.ingress-classes.${action}`)
  const canRead = permitted('view'),
    refresh = useWorkloadRefresh()
  const options = networkCoreQueries.list<IngressClass>(
    'ingressclasses',
    toScopeKey(clusterId, null),
  )
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const path = useCallback(
    (name: string, tab?: string) =>
      `/network/ingressclasses/${encodeURIComponent(name)}?${new URLSearchParams({ clusterId: clusterId || '', ...(tab ? { tab } : {}) })}`,
    [clusterId],
  )
  const columns = useMemo<TableColumnsType<IngressClass>>(
    () => [
      {
        title: zh ? '名称' : 'Name',
        dataIndex: 'name',
        width: 280,
        ellipsis: { showTitle: false },
        render: (value: string) => (
          <ResourceNameLink name={value} to={path(value)} onNavigate={navigate} />
        ),
      },
      {
        title: 'Controller',
        dataIndex: 'controller',
        width: 320,
        render: (value: string) => (
          <Typography.Text className="soha-ingressclass-wrap" copyable={Boolean(value)}>
            {value || '—'}
          </Typography.Text>
        ),
      },
      {
        title: zh ? '默认类' : 'Default class',
        dataIndex: 'isDefault',
        width: 120,
        render: (value: boolean) =>
          value ? <MetadataTag tone="blue" label={zh ? '默认类' : 'Default'} /> : '—',
      },
      {
        title: zh ? '参数引用' : 'Parameters',
        dataIndex: 'parameters',
        width: 240,
        render: (value?: string) => (
          <span className="soha-ingressclass-wrap">
            {value || (zh ? '未配置' : 'Not configured')}
          </span>
        ),
      },
      {
        title: zh ? '时长' : 'Age',
        dataIndex: 'ageSeconds',
        width: 104,
        render: (value: number) => formatAgeSeconds(value),
      },
    ],
    [zh, navigate, path],
  )
  const searchValues = useCallback(
    (record: IngressClass) => [record.name, record.controller, record.parameters],
    [],
  )
  const editRecord = useCallback(
    (record: IngressClass) => navigate(path(record.name, 'yaml')),
    [navigate, path],
  )

  return (
    <>
      <ReadFeedback canRead={canRead} query={query} label="IngressClasses" />
      {canRead && (
        <NetworkResourceListPage
          buildAIPageContext={(items, searchKeyword) => ({
            sourceWorkbench: 'platform',
            sourceRoute: '/network/ingressclasses',
            sourceTitle: 'IngressClasses',
            entityKind: 'kubernetes.ingressclass.list',
            entityName: 'IngressClasses',
            clusterId: clusterId ?? undefined,
            visibleFilters: { searchKeyword },
            pinnedData: { total: items.length },
          })}
          columns={columns}
          kind="ingressclasses"
          query={query}
          rowKey="name"
          deletable={permitted('delete')}
          onEdit={permitted('update') ? editRecord : undefined}
          emptyDescription={{
            zh_CN: '当前集群没有 IngressClass',
            en_US: 'No IngressClasses in this cluster',
          }}
          noMatchDescription={{
            zh_CN: '没有匹配的 IngressClass',
            en_US: 'No matching IngressClasses',
          }}
          searchPlaceholder={{
            zh_CN: '搜索名称 / Controller / 参数',
            en_US: 'Search name / Controller / parameters',
          }}
          searchValues={searchValues}
          refreshControl={
            <WorkloadRefresh
              compact
              disabled={!clusterId}
              settings={refresh}
              queries={[query]}
              resourceLabel="IngressClasses"
              onRefresh={() => query.refetch()}
            />
          }
        />
      )}
    </>
  )
}
