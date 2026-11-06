import { useI18n } from '@/i18n'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { TableColumnsType } from 'antd'
import { AccessControlResourceListPage } from '../shared/list-page'
import { TableCellLink } from '@/components/table-cell-content'
import { useNavigate } from 'react-router-dom'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { CLUSTER_ROLE_DEFAULT_TEMPLATE } from '../shared/templates'
import type { ClusterRoleResource } from './types'

export function PlatformAccessControlClusterRolesPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId } = usePlatformScopeStore()
  const path = (record: ClusterRoleResource, edit = false) =>
    `/platform-access-control/clusterroles/${encodeURIComponent(record.name)}?${new URLSearchParams({ clusterId: clusterId || '', ...(edit ? { tab: 'yaml', edit: 'true' } : {}) })}`
  const columns: TableColumnsType<ClusterRoleResource> = [
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
      permissionKey="platform.access-control.cluster-roles"
      onEdit={(record) => navigate(path(record, true))}
      columns={columns}
      defaultTemplate={CLUSTER_ROLE_DEFAULT_TEMPLATE}
      emptyDescription={{
        zh_CN: '当前集群没有 ClusterRole',
        en_US: 'No cluster roles in this cluster',
      }}
      kind="clusterroles"
      label="ClusterRoles"
      rowKey="name"
      searchValues={(record) => [record.name]}
    />
  )
}
