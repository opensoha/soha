import { TableCellText } from '@/components/table-cell-content'
import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds, formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { Tooltip, type TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { ConfigurationNameLink, ConfigurationResourceListPage } from '../shared/list-page'
import { LEASE_TEMPLATE, type LeaseResource } from './types'

export function ConfigurationLeasesPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { clusterId } = usePlatformScopeStore(),
    navigate = useNavigate()
  const timestamp = (value?: string) =>
    value ? (
      <Tooltip title={value} trigger={['hover', 'focus', 'click']}>
        <span tabIndex={0}>
          {Number.isFinite(Date.parse(value)) ? formatDateTime(value) : value}
        </span>
      </Tooltip>
    ) : (
      '—'
    )
  const timeSort = (value?: string) =>
    value && Number.isFinite(Date.parse(value)) ? Date.parse(value) : -8640000000000000
  const columns: TableColumnsType<LeaseResource> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 320,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (name: string, row) => (
        <ConfigurationNameLink includeCluster kind="leases" name={name} namespace={row.namespace} />
      ),
    },
    {
      title: zh ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 220,
      sorter: (a, b) => a.namespace.localeCompare(b.namespace),
      render: (namespace: string) => <NamespaceScopeTag namespace={namespace} />,
    },
    {
      title: zh ? '持有者' : 'Holder',
      dataIndex: 'holderIdentity',
      width: 300,
      sorter: (a, b) => (a.holderIdentity || '').localeCompare(b.holderIdentity || ''),
      render: (value?: string) => (
        <TableCellText value={value || (zh ? '未声明持有者' : 'No holder declared')} />
      ),
    },
    {
      title: zh ? '租期' : 'Duration',
      dataIndex: 'leaseDurationSeconds',
      width: 120,
      sorter: (a, b) => (a.leaseDurationSeconds ?? -1) - (b.leaseDurationSeconds ?? -1),
      render: (value?: number) => (value == null ? '—' : `${value} s`),
    },
    {
      ...tableColumnPresets.datetime,
      title: zh ? '最近续租' : 'Last renewed',
      dataIndex: 'renewTime',
      width: 190,
      sorter: (a, b) => timeSort(a.renewTime) - timeSort(b.renewTime),
      render: timestamp,
    },
    {
      ...tableColumnPresets.datetime,
      title: zh ? '获取时间' : 'Acquired',
      dataIndex: 'acquireTime',
      width: 190,
      sorter: (a, b) => timeSort(a.acquireTime) - timeSort(b.acquireTime),
      render: timestamp,
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
      kind="leases"
      label="Leases"
      autoRefresh
      permissionKey="platform.configuration.leases"
      defaultTemplate={LEASE_TEMPLATE}
      singularLabel="Lease"
      emptyDescription={{ zh_CN: '当前范围没有 Lease', en_US: 'No leases in the current scope' }}
      onEdit={(row) =>
        navigate(
          '/configuration/leases/' +
            encodeURIComponent(row.name) +
            '?' +
            new URLSearchParams({
              clusterId: clusterId || '',
              namespace: row.namespace,
              tab: 'yaml',
            }),
        )
      }
    />
  )
}
