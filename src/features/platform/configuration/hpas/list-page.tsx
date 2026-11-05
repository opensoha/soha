import { useState } from 'react'
import { TableCellText } from '@/components/table-cell-content'
import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { TableColumnsType } from 'antd'
import { ConfigurationNameLink, ConfigurationResourceListPage } from '../shared/list-page'
import { ConfigurationQuickEditModal } from '../shared/configuration-quick-edit-modal'
import { HPA_TEMPLATE, type HorizontalPodAutoscalerResource } from './types'
export function ConfigurationHPAPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { clusterId, namespace } = usePlatformScopeStore()
  const [editing, setEditing] = useState<HorizontalPodAutoscalerResource | null>(null)
  const [editingScope, setEditingScope] = useState('')
  const scopeKey = JSON.stringify([clusterId, namespace])
  const columns: TableColumnsType<HorizontalPodAutoscalerResource> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 320,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (value: string, record) => (
        <ConfigurationNameLink
          includeCluster
          kind="hpas"
          name={value}
          namespace={record.namespace}
        />
      ),
    },
    {
      title: zh ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 220,
      sorter: (a, b) => a.namespace.localeCompare(b.namespace),
      render: (value: string) => <NamespaceScopeTag namespace={value} />,
    },
    {
      title: zh ? '扩缩目标' : 'Target',
      dataIndex: 'targetRef',
      width: 280,
      sorter: (a, b) => a.targetRef.localeCompare(b.targetRef),
      render: (value: string) => <TableCellText value={value} />,
    },
    ...(['currentReplicas', 'desiredReplicas', 'minReplicas', 'maxReplicas'] as const).map(
      (key, index) => ({
        title: (zh ? ['当前', '期望', '最小', '最大'] : ['Current', 'Desired', 'Min', 'Max'])[
          index
        ],
        dataIndex: key,
        width: 100,
        sorter: (a: HorizontalPodAutoscalerResource, b: HorizontalPodAutoscalerResource) =>
          a[key] - b[key],
        render: (value?: number) => value ?? '—',
      }),
    ),
    {
      ...tableColumnPresets.datetime,
      title: zh ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 120,
      sorter: (a, b) => a.ageSeconds - b.ageSeconds,
      render: (value: number) => formatAgeSeconds(value),
    },
  ]
  return (
    <>
      <ConfigurationResourceListPage
        columns={columns}
        autoRefresh
        permissionKey="platform.configuration.horizontal-pod-autoscalers"
        defaultTemplate={HPA_TEMPLATE}
        singularLabel="HorizontalPodAutoscaler"
        emptyDescription={{
          zh_CN: '当前范围没有 HPA',
          en_US: 'No HPA resources in the current scope',
        }}
        kind="hpas"
        label="HorizontalPodAutoscalers"
        onEdit={(record) => {
          setEditing(record)
          setEditingScope(scopeKey)
        }}
      />
      {editing && editingScope === scopeKey ? (
        <ConfigurationQuickEditModal
          key={JSON.stringify([clusterId, editing.namespace, editing.name])}
          kind="hpas"
          name={editing.name}
          namespace={editing.namespace}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </>
  )
}
