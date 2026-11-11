import { BooleanTag, MetadataTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { ConfigurationNameLink, ConfigurationResourceListPage } from '../shared/list-page'
import { PRIORITY_CLASS_TEMPLATE, type PriorityClassResource } from './types'
export function ConfigurationPriorityClassesPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { clusterId } = usePlatformScopeStore(),
    navigate = useNavigate()
  const columns: TableColumnsType<PriorityClassResource> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 320,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (name: string) => (
        <ConfigurationNameLink includeCluster kind="priorityclasses" name={name} />
      ),
    },
    {
      title: zh ? '优先级' : 'Priority',
      dataIndex: 'value',
      width: 150,
      sorter: (a, b) => a.value - b.value,
      defaultSortOrder: 'descend',
      render: (value: number) => new Intl.NumberFormat('en-US').format(value),
    },
    {
      title: zh ? '全局默认' : 'Global default',
      dataIndex: 'globalDefault',
      width: 135,
      sorter: (a, b) => Number(a.globalDefault) - Number(b.globalDefault),
      render: (value: boolean) => (
        <BooleanTag value={value} trueLabel={zh ? '是' : 'Yes'} falseLabel={zh ? '否' : 'No'} />
      ),
    },
    {
      title: zh ? '抢占策略' : 'Preemption',
      dataIndex: 'preemptionPolicy',
      width: 190,
      render: (value?: string) =>
        value ? (
          <MetadataTag label={value} />
        ) : (
          <TableCellText
            value={zh ? '默认 · PreemptLowerPriority' : 'Default · PreemptLowerPriority'}
          />
        ),
    },
    {
      title: zh ? '说明' : 'Description',
      dataIndex: 'description',
      width: 380,
      render: (value?: string) => <TableCellText value={value} />,
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
      permissionKey="platform.configuration.priority-classes"
      defaultTemplate={PRIORITY_CLASS_TEMPLATE}
      singularLabel="PriorityClass"
      emptyDescription={{
        zh_CN: '当前集群没有 PriorityClass',
        en_US: 'No priority classes in this cluster',
      }}
      kind="priorityclasses"
      label="PriorityClasses"
      scopeMode="cluster"
      onEdit={(record) =>
        navigate(
          '/configuration/priorityclasses/' +
            encodeURIComponent(record.name) +
            '?' +
            new URLSearchParams({ clusterId: clusterId || '', tab: 'yaml' }),
        )
      }
    />
  )
}
