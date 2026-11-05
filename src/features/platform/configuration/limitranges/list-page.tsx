import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { ConfigurationNameLink, ConfigurationResourceListPage } from '../shared/list-page'
import { LIMIT_RANGE_TEMPLATE, type LimitRangeResource } from './types'

export function ConfigurationLimitRangesPage() {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const { clusterId } = usePlatformScopeStore()
  const navigate = useNavigate()
  const columns: TableColumnsType<LimitRangeResource> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 340,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (value: string, record) => (
        <ConfigurationNameLink
          includeCluster
          kind="limitranges"
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
      title: zh ? '规则数' : 'Rules',
      dataIndex: 'limits',
      width: 120,
      sorter: (a, b) => a.limits - b.limits,
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
      permissionKey="platform.configuration.limit-ranges"
      defaultTemplate={LIMIT_RANGE_TEMPLATE}
      singularLabel="LimitRange"
      emptyDescription={{
        zh_CN: '当前范围没有 LimitRange',
        en_US: 'No limit ranges in the current scope',
      }}
      kind="limitranges"
      label="LimitRanges"
      onEdit={(record) =>
        navigate(
          '/configuration/limitranges/' +
            encodeURIComponent(record.name) +
            '?' +
            new URLSearchParams({
              clusterId: clusterId || '',
              namespace: record.namespace,
              tab: 'yaml',
            }),
        )
      }
    />
  )
}
