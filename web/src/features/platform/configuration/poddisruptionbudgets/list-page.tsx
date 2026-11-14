import { useState } from 'react'
import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { TableColumnsType } from 'antd'
import { ConfigurationNameLink, ConfigurationResourceListPage } from '../shared/list-page'
import { ConfigurationQuickEditModal } from '../shared/configuration-quick-edit-modal'
import { PDB_TEMPLATE, type PodDisruptionBudgetResource } from './types'
export function ConfigurationPodDisruptionBudgetsPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { clusterId, namespace } = usePlatformScopeStore()
  const [editing, setEditing] = useState<PodDisruptionBudgetResource | null>(null)
  const [editingScope, setEditingScope] = useState('')
  const scopeKey = JSON.stringify([clusterId, namespace])
  const columns: TableColumnsType<PodDisruptionBudgetResource> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 320,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (value: string, record) => (
        <ConfigurationNameLink
          includeCluster
          kind="poddisruptionbudgets"
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
      title: 'minAvailable',
      dataIndex: 'minAvailable',
      width: 145,
      render: (value?: string) => value || '—',
    },
    {
      title: 'maxUnavailable',
      dataIndex: 'maxUnavailable',
      width: 155,
      render: (value?: string) => value || '—',
    },
    ...(['currentHealthy', 'desiredHealthy', 'disruptionsAllowed'] as const).map((key, index) => ({
      title: (zh
        ? ['当前健康', '期望健康', '允许驱逐']
        : ['Current healthy', 'Desired healthy', 'Allowed disruptions'])[index],
      dataIndex: key,
      width: 125,
      sorter: (a: PodDisruptionBudgetResource, b: PodDisruptionBudgetResource) => a[key] - b[key],
      render: (value?: number) => value ?? '—',
    })),
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
        permissionKey="platform.configuration.pod-disruption-budgets"
        defaultTemplate={PDB_TEMPLATE}
        singularLabel="PodDisruptionBudget"
        emptyDescription={{
          zh_CN: '当前范围没有 PodDisruptionBudget',
          en_US: 'No pod disruption budgets in the current scope',
        }}
        kind="poddisruptionbudgets"
        label="PodDisruptionBudgets"
        onEdit={(record) => {
          setEditing(record)
          setEditingScope(scopeKey)
        }}
      />
      {editing && editingScope === scopeKey ? (
        <ConfigurationQuickEditModal
          key={JSON.stringify([clusterId, editing.namespace, editing.name])}
          kind="poddisruptionbudgets"
          name={editing.name}
          namespace={editing.namespace}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </>
  )
}
