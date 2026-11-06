import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, Card, Tooltip, type TableColumnsType } from 'antd'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { Link } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { ManagementTableToolbar, ManagementToolbarSearch } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { TableCellText } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { accessControlQueries } from '../shared/queries'
import { accessControlScopeFromSelection } from '../shared/scope'
import { splitAccessControlRoleRef } from '../shared/relationships'
import type { AccessControlBindingRecord } from '../shared/types'

function BindingSection({
  name,
  namespace,
  clusterId,
  kind,
}: {
  name: string
  namespace: string
  clusterId: string
  kind: 'rolebindings' | 'clusterrolebindings'
}) {
  const zh = useI18n().localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const cluster = kind === 'clusterrolebindings'
  const permission = cluster ? 'cluster-role-bindings' : 'role-bindings'
  const canRead = hasPermission(permissions, `platform.access-control.${permission}.view`)
  const options = accessControlQueries.list<AccessControlBindingRecord>(
    kind,
    accessControlScopeFromSelection(kind, clusterId, cluster ? null : namespace),
    { subjectKind: 'ServiceAccount', subjectName: name, subjectNamespace: namespace },
  )
  const result = useQuery({
    ...options,
    enabled: canRead && Boolean(name && namespace) && options.enabled,
  })
  const rows = canRead ? readableQueryData(result) : undefined
  const [search, setSearch] = useState('')
  useEffect(() => setSearch(''), [name, namespace, clusterId])
  const query = search.trim().toLocaleLowerCase()
  const shown = (rows ?? []).filter((row) =>
    [row.name, row.namespace, row.roleRef].some((value) =>
      value?.toLocaleLowerCase().includes(query),
    ),
  )
  const link = (
    resource: string,
    target: string,
    ns: string | undefined,
    allowed: boolean,
    label = target,
  ) =>
    allowed && clusterId && ((resource !== 'roles' && resource !== 'rolebindings') || ns) ? (
      <Link
        to={`/platform-access-control/${resource}/${encodeURIComponent(target)}?${new URLSearchParams({ clusterId, ...(ns ? { namespace: ns } : {}) })}`}
      >
        <TableCellText value={label} />
      </Link>
    ) : (
      <TableCellText value={label} />
    )
  const columns: TableColumnsType<AccessControlBindingRecord> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 260,
      render: (value: string, row) =>
        link(kind, value, cluster ? undefined : row.namespace, canRead),
    },
    ...(!cluster
      ? [
          {
            title: 'Namespace',
            dataIndex: 'namespace',
            width: 180,
            render: (value: string) => <TableCellText value={value} />,
          },
        ]
      : []),
    {
      title: 'RoleRef',
      dataIndex: 'roleRef',
      width: 280,
      render: (value: string, row) => {
        const ref = splitAccessControlRoleRef(value)
        if (!ref || !['Role', 'ClusterRole'].includes(ref.kind))
          return <TableCellText value={value} />
        const isRole = ref.kind === 'Role'
        return link(
          isRole ? 'roles' : 'clusterroles',
          ref.name,
          isRole ? row.namespace : undefined,
          hasPermission(
            permissions,
            `platform.access-control.${isRole ? 'roles' : 'cluster-roles'}.view`,
          ),
          value,
        )
      },
    },
  ]
  const label = cluster ? 'ClusterRoleBindings' : 'RoleBindings'
  return (
    <Card
      title={
        <span className="soha-sa-section-title">
          {label}
          <Tooltip
            trigger={['hover', 'focus', 'click']}
            title={
              zh
                ? '按 ServiceAccount 类型、名称和命名空间精确查询；RoleBinding 仅覆盖该账户所在命名空间，其他命名空间的引用不在当前接口返回范围内。只统计当前返回结果，权限过滤或读取失败可能限制范围；绑定不等于完整有效权限。'
                : 'Exact ServiceAccount kind, name and namespace filter; RoleBindings cover the account namespace only; cross-namespace references are outside this endpoint coverage. Counts reflect returned results only; permissions and read failures may limit coverage. Bindings do not establish complete effective access.'
            }
          >
            <Button
              type="text"
              size="small"
              icon={<QuestionCircleOutlined />}
              aria-label={zh ? '绑定范围说明' : 'Binding scope information'}
            />
          </Tooltip>
        </span>
      }
      extra={
        rows
          ? zh
            ? `显示 ${shown.length} / 当前返回 ${rows.length} 条`
            : `${shown.length} shown / ${rows.length} returned`
          : '—'
      }
    >
      <ReadFeedback canRead={canRead} query={result} label={label} />
      {canRead && (
        <>
          <div className="soha-sa-binding-toolbar">
            <ManagementTableToolbar>
              <ManagementToolbarSearch
                aria-label={zh ? '搜索当前返回的绑定' : 'Search returned bindings'}
                placeholder={
                  zh ? '搜索名称、Namespace、RoleRef' : 'Search name, namespace, roleRef'
                }
                value={search}
                onChange={setSearch}
              />
            </ManagementTableToolbar>
          </div>
          <AdminTable
            columns={columns}
            dataSource={shown}
            rowKey={(row) => `${row.namespace ?? ''}/${row.name}`}
            pagination={false}
            enableColumnSelection={false}
            tableSize="small"
            loading={result.isLoading}
            scroll={{ x: cluster ? 540 : 720 }}
            empty={
              rows === undefined
                ? zh
                  ? '尚无可用结果'
                  : 'No available results'
                : query
                  ? zh
                    ? '当前返回范围内无匹配绑定'
                    : 'No matches in returned scope'
                  : zh
                    ? '当前未返回绑定'
                    : 'No bindings returned'
            }
          />
        </>
      )}
    </Card>
  )
}
export function SaBindings(props: { name: string; namespace: string; clusterId: string }) {
  return (
    <div className="soha-sa-bindings">
      <BindingSection {...props} kind="rolebindings" />
      <BindingSection {...props} kind="clusterrolebindings" />
    </div>
  )
}
