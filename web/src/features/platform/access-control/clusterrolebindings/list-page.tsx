import { useI18n } from '@/i18n'
import { TableCellLink, TableCellText } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { AccessControlResourceListPage } from '../shared/list-page'
import { splitAccessControlRoleRef } from '../shared/relationships'
import { CLUSTER_ROLE_BINDING_DEFAULT_TEMPLATE } from '../shared/templates'
import type { ClusterRoleBindingResource } from './types'

export function PlatformAccessControlClusterRoleBindingsPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId } = usePlatformScopeStore()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canViewClusterRoles = hasPermission(
    permissions,
    'platform.access-control.cluster-roles.view',
  )
  const path = (record: ClusterRoleBindingResource, edit = false) =>
    `/platform-access-control/clusterrolebindings/${encodeURIComponent(record.name)}?${new URLSearchParams({ clusterId: clusterId || '', ...(edit ? { tab: 'yaml', edit: 'true' } : {}) })}`
  const columns: TableColumnsType<ClusterRoleBindingResource> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      width: 310,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (value: string, record) => (
        <TableCellLink label={value} onClick={() => navigate(path(record))} />
      ),
    },
    {
      title: 'RoleRef',
      dataIndex: 'roleRef',
      width: 280,
      sorter: (a, b) => (a.roleRef ?? '').localeCompare(b.roleRef ?? ''),
      render: (value: string | undefined) => {
        const ref = splitAccessControlRoleRef(value)
        if (!canViewClusterRoles || ref?.kind !== 'ClusterRole' || !value || ref.name === value) {
          return <TableCellText value={value} />
        }
        return (
          <TableCellLink
            label={value}
            onClick={() =>
              navigate(
                `/platform-access-control/clusterroles/${encodeURIComponent(ref.name)}?${new URLSearchParams({ clusterId: clusterId || '' })}`,
              )
            }
          />
        )
      },
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
      permissionKey="platform.access-control.cluster-role-bindings"
      onEdit={(record) => navigate(path(record, true))}
      columns={columns}
      defaultTemplate={CLUSTER_ROLE_BINDING_DEFAULT_TEMPLATE}
      emptyDescription={{
        zh_CN: '当前集群没有 ClusterRoleBinding',
        en_US: 'No cluster role bindings in this cluster',
      }}
      kind="clusterrolebindings"
      label="ClusterRoleBindings"
      rowKey="name"
      searchValues={(record) => [record.name, record.roleRef]}
    />
  )
}
