import { ReadFeedback } from '@/components/request-feedback'
import { TableCellLink } from '@/components/table-cell-content'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { encodeAIContextForElement } from '@/features/copilot'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { useEffect, useState } from 'react'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { useNavigate } from 'react-router-dom'
import { NetworkResourceListPage } from '../shared/list-page'
import { buildNetworkRoutePath } from '../shared/paths'
import { ServicePortDisplay, ServiceTypeTag } from './port-display'
import { serviceQueries } from './queries'
import { ServiceEditModal } from './service-edit-modal'
import { ServiceSelectorDisplay } from './selector-display'
import type { Service } from './types'

export function NetworkServicesPage() {
  const { localeCode, t } = useI18n()
  const navigate = useNavigate()
  const { clusterId, namespace } = usePlatformScopeStore()
  const scope = toScopeKey(clusterId, namespace)
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canUpdate = hasPermission(permissions, 'platform.network.services.update')
  const refresh = useWorkloadRefresh()
  const query = useQuery({ ...serviceQueries.list(scope), refetchInterval: refresh.interval })
  const [editing, setEditing] = useState<{
    record: Service
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
  const columns: TableColumnsType<Service> = [
    {
      title: t('common.name', '名称'),
      dataIndex: 'name',
      width: 300,
      fixed: 'left',
      render: (value: string, record) => (
        <TableCellLink
          label={value}
          onClick={() =>
            navigate(
              buildNetworkRoutePath('services', value, record.namespace, {
                clusterId: clusterId || undefined,
              }),
            )
          }
        />
      ),
    },
    { title: t('common.namespace', '命名空间'), dataIndex: 'namespace', width: 180 },
    {
      title: t('common.type', '类型'),
      dataIndex: 'type',
      width: 140,
      render: (value: string) => <ServiceTypeTag type={value} />,
    },
    {
      title: t('common.clusterIp', '集群 IP'),
      dataIndex: 'clusterIp',
      width: 160,
      render: (value: string) => value || '-',
    },
    {
      title: t('common.ports', '端口'),
      key: 'ports',
      width: 290,
      render: (_, record) => (
        <ServicePortDisplay portMappings={record.portMappings} ports={record.ports} />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '选择器' : 'Selector',
      key: 'selector',
      width: 260,
      render: (_, record) => (
        <ServiceSelectorDisplay
          compact
          selector={record.selector}
          backendLabel={
            localeCode === 'zh_CN'
              ? `查看 ${record.name} 的后端`
              : `View backends for ${record.name}`
          }
          backendPath={buildNetworkRoutePath('services', record.name, record.namespace, {
            tab: 'backends',
            clusterId: clusterId || undefined,
          })}
        />
      ),
    },
    {
      title: t('common.age', '时长'),
      dataIndex: 'ageSeconds',
      width: 104,
      render: (value: number) => formatAgeSeconds(value),
    },
  ]

  return (
    <>
      <ReadFeedback canRead={query.isEnabled} query={query} label="资源记录" />

      <NetworkResourceListPage
        buildAIPageContext={(items, searchKeyword) => ({
          sourceWorkbench: 'platform',
          sourceRoute: '/network/services',
          sourceTitle: localeCode === 'zh_CN' ? 'Services 列表' : 'Services',
          entityKind: 'kubernetes.service.list',
          entityName: 'Services',
          clusterId: clusterId ?? undefined,
          namespace: namespace ?? undefined,
          timeRangeMinutes: 60,
          visibleFilters: { searchKeyword },
          pinnedData: { total: items.length },
          promptHint:
            localeCode === 'zh_CN'
              ? '分析当前 Service 列表的类型、端口、ClusterIP、后端 Pod 和事件风险。'
              : 'Analyze service types, ports, ClusterIP, backend pods, and event risks.',
        })}
        columns={columns}
        emptyDescription={{
          zh_CN: '当前范围没有 Service',
          en_US: 'No services in the current scope',
        }}
        kind="services"
        noMatchDescription={{ zh_CN: '没有匹配的 Service', en_US: 'No matching services' }}
        onEdit={
          canUpdate
            ? (record) => clusterId && setEditing({ record, clusterId, namespace })
            : undefined
        }
        onRow={(record) => ({
          'data-ai-context': encodeAIContextForElement({
            sourceWorkbench: 'platform',
            sourceRoute: buildNetworkRoutePath('services', record.name, record.namespace, {
              clusterId: clusterId || undefined,
            }),
            sourceTitle: `Service ${record.name}`,
            entityKind: 'kubernetes.service',
            entityName: record.name,
            clusterId: clusterId ?? undefined,
            namespace: record.namespace,
            service: record.name,
            timeRangeMinutes: 60,
          }),
        })}
        query={query}
        refreshControl={
          <WorkloadRefresh
            settings={refresh}
            queries={[query]}
            onRefresh={() => query.refetch()}
            resourceLabel="Services"
            compact
            disabled={!clusterId}
          />
        }
        searchPlaceholder={{
          zh_CN: '搜索 Service / namespace / type / port / selector',
          en_US: 'Search service / namespace / type / port / selector',
        }}
        searchValues={(record) => [
          record.name,
          record.namespace,
          record.type,
          record.clusterIp,
          ...record.ports,
          ...Object.entries(record.selector || {}).map(([key, value]) => `${key}=${value}`),
          ...(record.portMappings?.flatMap((mapping) => [
            mapping.name ?? '',
            mapping.protocol,
            mapping.targetPort,
            String(mapping.port),
            mapping.nodePort ? String(mapping.nodePort) : '',
          ]) ?? []),
        ]}
      />
      {editAllowed &&
        editing &&
        editing.clusterId === clusterId &&
        editing.namespace === namespace && (
          <ServiceEditModal
            key={JSON.stringify([editing.clusterId, editing.record.namespace, editing.record.name])}
            clusterId={editing.clusterId}
            service={editing.record}
            onClose={() => setEditing(undefined)}
          />
        )}
    </>
  )
}
