import { ReadFeedback } from '@/components/request-feedback'
import { TableCellLink } from '@/components/table-cell-content'
import { encodeAIContextForElement } from '@/features/copilot'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { NetworkResourceListPage } from '../shared/list-page'
import { buildNetworkRoutePath } from '../shared/paths'
import { ServicePortDisplay, ServiceTypeTag } from './port-display'
import { serviceQueries } from './queries'
import { ServiceEditModal } from './service-edit-modal'
import type { Service } from './types'

export function NetworkServicesPage() {
  const { localeCode, t } = useI18n()
  const navigate = useNavigate()
  const { clusterId, namespace } = usePlatformScopeStore()
  const scope = toScopeKey(clusterId, namespace)
  const query = useQuery(serviceQueries.list(scope))
  const [editingService, setEditingService] = useState<Service | null>(null)
  const columns: TableColumnsType<Service> = [
    {
      title: t('common.name', '名称'),
      dataIndex: 'name',
      render: (value: string, record) => (
        <TableCellLink
          label={value}
          onClick={() => navigate(buildNetworkRoutePath('services', value, record.namespace))}
        />
      ),
    },
    { title: t('common.namespace', '命名空间'), dataIndex: 'namespace' },
    {
      title: t('common.type', '类型'),
      dataIndex: 'type',
      render: (value: string) => <ServiceTypeTag type={value} />,
    },
    {
      title: t('common.clusterIp', '集群 IP'),
      dataIndex: 'clusterIp',
      render: (value: string) => value || '-',
    },
    {
      title: t('common.ports', '端口'),
      key: 'ports',
      width: 360,
      render: (_, record) => (
        <ServicePortDisplay portMappings={record.portMappings} ports={record.ports} />
      ),
    },
    {
      title: t('common.age', '时长'),
      dataIndex: 'ageSeconds',
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
        onEdit={setEditingService}
        onRow={(record) => ({
          'data-ai-context': encodeAIContextForElement({
            sourceWorkbench: 'platform',
            sourceRoute: buildNetworkRoutePath('services', record.name, record.namespace),
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
        searchPlaceholder={{
          zh_CN: '搜索 Service / namespace / type / port',
          en_US: 'Search service / namespace / type / port',
        }}
        searchValues={(record) => [
          record.name,
          record.namespace,
          record.type,
          record.clusterIp,
          ...record.ports,
          ...(record.portMappings?.flatMap((mapping) => [
            mapping.name ?? '',
            mapping.protocol,
            mapping.targetPort,
            String(mapping.port),
            mapping.nodePort ? String(mapping.nodePort) : '',
          ]) ?? []),
        ]}
      />
      {editingService ? (
        <ServiceEditModal service={editingService} onClose={() => setEditingService(null)} />
      ) : null}
    </>
  )
}
