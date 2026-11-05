import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, Card, Tooltip, type TableColumnsType } from 'antd'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { Link } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementState,
  ManagementTableToolbar,
  ManagementToolbarSearch,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { TableCellText } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { accessControlQueries } from '../shared/queries'
import { accessControlScopeFromSelection } from '../shared/scope'
import { splitAccessControlRoleRef } from '../shared/relationships'
import type { AccessControlBindingRecord } from '../shared/types'

export function RoleBindings({
  name,
  namespace,
  clusterId,
}: {
  name: string
  namespace: string
  clusterId: string
}) {
  const zh = useI18n().localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = hasPermission(permissions, 'platform.access-control.role-bindings.view')
  const options = accessControlQueries.list<AccessControlBindingRecord>(
    'rolebindings',
    accessControlScopeFromSelection('rolebindings', clusterId, namespace),
  )
  const result = useQuery({
    ...options,
    enabled: canRead && Boolean(name && namespace) && options.enabled,
  })
  const returnedRows = canRead ? readableQueryData(result) : undefined
  const rows = returnedRows?.filter((row) => {
    const ref = splitAccessControlRoleRef(row.roleRef)
    return ref?.kind === 'Role' && ref.name === name && row.namespace === namespace
  })
  const [search, setSearch] = useState('')
  useEffect(() => setSearch(''), [name, namespace, clusterId])
  const query = search.trim().toLocaleLowerCase()
  const shown = (rows ?? []).filter((row) =>
    [row.name, row.namespace, row.roleRef].some((value) =>
      value?.toLocaleLowerCase().includes(query),
    ),
  )
  const link = (
    resource: 'roles' | 'rolebindings',
    target: string,
    ns: string | undefined,
    allowed: boolean,
    label = target,
  ) =>
    allowed && clusterId && ns ? (
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
      render: (value: string, row) => link('rolebindings', value, row.namespace, canRead),
    },
    {
      title: 'Namespace',
      dataIndex: 'namespace',
      width: 180,
      render: (value: string) => <TableCellText value={value} />,
    },
    {
      title: 'RoleRef',
      dataIndex: 'roleRef',
      width: 280,
      render: (value: string) => {
        const ref = splitAccessControlRoleRef(value)
        if (!ref || ref.kind !== 'Role') return <TableCellText value={value} />
        return link(
          'roles',
          ref.name,
          namespace,
          hasPermission(permissions, 'platform.access-control.roles.view'),
          value,
        )
      },
    },
  ]
  const label = 'RoleBindings'
  return (
    <Card
      className="soha-detail-card"
      title={
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
          {label}
          <Tooltip
            trigger={['hover', 'focus', 'click']}
            title={
              zh
                ? '从当前命名空间有权读取的 RoleBindings 中按 Role 类型、名称及绑定命名空间精确匹配，不限定授权主体类型。ClusterRoleBinding 不能引用 Role。数量仅反映当前返回范围，权限过滤或读取失败可能限制覆盖范围，不能据此推断完整有效权限。'
                : 'Matches the exact Role kind, name and binding namespace in authorized RoleBindings returned for this namespace, for every subject type. ClusterRoleBindings cannot reference a Role. Counts reflect returned coverage only; permission filtering or read failures can limit coverage and do not establish complete effective access.'
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
            ? `显示 ${shown.length} / 当前返回范围内引用 ${rows.length} 条`
            : `${shown.length} shown / ${rows.length} references in returned scope`
          : zh
            ? '数量未知'
            : 'Count unknown'
      }
    >
      <ReadFeedback canRead={canRead} query={result} label={label} />
      {!canRead && <ManagementState bordered={false} compact kind="no-permission" />}
      {canRead && (
        <>
          <div style={{ marginBottom: 16 }}>
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
            scroll={{ x: 720 }}
            empty={
              rows === undefined
                ? zh
                  ? result.isError
                    ? '读取失败，结果未知'
                    : '尚无可用结果'
                  : result.isError
                    ? 'Read failed; results unknown'
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
