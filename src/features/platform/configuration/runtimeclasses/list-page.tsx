import { TableCellText } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { ConfigurationNameLink, ConfigurationResourceListPage } from '../shared/list-page'
import { RUNTIME_CLASS_TEMPLATE, type RuntimeClassResource } from './types'
export function ConfigurationRuntimeClassesPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { clusterId } = usePlatformScopeStore(),
    navigate = useNavigate()
  const columns: TableColumnsType<RuntimeClassResource> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 320,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (name: string) => (
        <ConfigurationNameLink includeCluster kind="runtimeclasses" name={name} />
      ),
    },
    {
      title: 'Handler',
      dataIndex: 'handler',
      width: 320,
      sorter: (a, b) => a.handler.localeCompare(b.handler),
      render: (value: string) => <TableCellText value={value} />,
    },
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
    <ConfigurationResourceListPage
      columns={columns}
      autoRefresh
      permissionKey="platform.configuration.runtime-classes"
      defaultTemplate={RUNTIME_CLASS_TEMPLATE}
      singularLabel="RuntimeClass"
      emptyDescription={{
        zh_CN: '当前集群没有 RuntimeClass',
        en_US: 'No runtime classes in this cluster',
      }}
      kind="runtimeclasses"
      label="RuntimeClasses"
      scopeMode="cluster"
      onEdit={(record) =>
        navigate(
          '/configuration/runtimeclasses/' +
            encodeURIComponent(record.name) +
            '?' +
            new URLSearchParams({ clusterId: clusterId || '', tab: 'yaml' }),
        )
      }
    />
  )
}
