import { useI18n } from '@/i18n'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { TableColumnsType } from 'antd'
import { AccessControlResourceListPage } from '../shared/list-page'
import { TableCellLink } from '@/components/table-cell-content'
import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import { useNavigate } from 'react-router-dom'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { ROLE_DEFAULT_TEMPLATE } from '../shared/templates'
import type { RoleResource } from './types'

export function PlatformAccessControlRolesPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId } = usePlatformScopeStore()
  const path = (record: RoleResource, edit = false) =>
    `/platform-access-control/roles/${encodeURIComponent(record.name)}?${new URLSearchParams({ clusterId: clusterId || '', namespace: record.namespace, ...(edit ? { tab: 'yaml', edit: 'true' } : {}) })}`
  const columns: TableColumnsType<RoleResource> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      width: 280,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (value: string, record) => (
        <TableCellLink label={value} onClick={() => navigate(path(record))} />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 200,
      sorter: (a, b) => a.namespace.localeCompare(b.namespace),
      render: (value: string) => <NamespaceScopeTag namespace={value} />,
    },
    {
      ...tableColumnPresets.datetime,
      title: 'Age',
      dataIndex: 'ageSeconds',
      sorter: (a, b) => a.ageSeconds - b.ageSeconds,
      width: 104,
      render: (value: number) => formatAgeSeconds(value),
    },
  ]
  return (
    <AccessControlResourceListPage
      autoRefresh
      permissionKey="platform.access-control.roles"
      onEdit={(record) => navigate(path(record, true))}
      columns={columns}
      defaultTemplate={ROLE_DEFAULT_TEMPLATE}
      emptyDescription={{
        zh_CN: '当前范围没有 Roles',
        en_US: 'No roles in the current scope',
      }}
      kind="roles"
      label="Roles"
      rowKey={(record) => `${record.namespace}/${record.name}`}
      searchValues={(record) => [record.name, record.namespace]}
    />
  )
}
