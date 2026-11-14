import { useI18n } from '@/i18n'
import { TableCellLink, TableCellText } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { TableColumnsType } from 'antd'
import { useNavigate } from 'react-router-dom'
import { AccessControlResourceListPage } from '../shared/list-page'
import { splitAccessControlRoleRef } from '../shared/relationships'
import { ROLE_BINDING_DEFAULT_TEMPLATE } from '../shared/templates'
import type { RoleBindingResource } from './types'

export function PlatformAccessControlRoleBindingsPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId } = usePlatformScopeStore()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canViewRoles = hasPermission(permissions, 'platform.access-control.roles.view')
  const canViewClusterRoles = hasPermission(
    permissions,
    'platform.access-control.cluster-roles.view',
  )
  const path = (record: RoleBindingResource, edit = false) =>
    `/platform-access-control/rolebindings/${encodeURIComponent(record.name)}?${new URLSearchParams({ clusterId: clusterId || '', namespace: record.namespace, ...(edit ? { tab: 'yaml', edit: 'true' } : {}) })}`
  const columns: TableColumnsType<RoleBindingResource> = [
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
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 200,
      sorter: (a, b) => a.namespace.localeCompare(b.namespace),
      render: (value: string) => <NamespaceScopeTag namespace={value} />,
    },
    {
      title: 'RoleRef',
      dataIndex: 'roleRef',
      width: 310,
      sorter: (a, b) => (a.roleRef ?? '').localeCompare(b.roleRef ?? ''),
      render: (value: string | undefined, record) => {
        const ref = splitAccessControlRoleRef(value)
        const resourceKind =
          ref?.kind === 'Role' && canViewRoles && record.namespace
            ? 'roles'
            : ref?.kind === 'ClusterRole' && canViewClusterRoles
              ? 'clusterroles'
              : null
        if (!resourceKind || !ref || !value || ref.name === value) {
          return <TableCellText value={value} />
        }
        return (
          <TableCellLink
            label={value}
            onClick={() =>
              navigate(
                `/platform-access-control/${resourceKind}/${encodeURIComponent(ref.name)}?${new URLSearchParams({ clusterId: clusterId || '', ...(resourceKind === 'roles' ? { namespace: record.namespace } : {}) })}`,
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
      permissionKey="platform.access-control.role-bindings"
      onEdit={(record) => navigate(path(record, true))}
      columns={columns}
      defaultTemplate={ROLE_BINDING_DEFAULT_TEMPLATE}
      emptyDescription={{
        zh_CN: '当前范围没有 RoleBindings',
        en_US: 'No role bindings in the current scope',
      }}
      kind="rolebindings"
      label="RoleBindings"
      rowKey={(record) => `${record.namespace}/${record.name}`}
      searchValues={(record) => [record.name, record.namespace, record.roleRef]}
    />
  )
}
