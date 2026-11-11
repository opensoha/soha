import { useEffect, useState } from 'react'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { ManagementTableToolbar, ManagementToolbarSearch } from '@/components/management-list'
import { Alert, Button, Card, Tooltip, type TableColumnsType } from 'antd'
import { Link } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import type { PersistentVolume, PersistentVolumeClaim, StorageClassDetail } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'

export function ScRelations({
  detail,
  clusterId,
  kind,
}: {
  detail: StorageClassDetail
  clusterId: string
  kind: 'volumes' | 'claims'
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canVolume = hasPermission(permissions, 'platform.storage.persistent-volumes.view')
  const canClaim = hasPermission(permissions, 'platform.storage.persistent-volume-claims.view')
  const volume = (name?: string) =>
    name && canVolume ? (
      <Link
        to={`/storage/persistentvolumes/${encodeURIComponent(name)}?${new URLSearchParams({ clusterId })}`}
      >
        <TableCellText value={name} />
      </Link>
    ) : (
      <TableCellText value={name} />
    )
  const claim = (name?: string, namespace?: string, label?: string) =>
    name && namespace && canClaim ? (
      <Link
        to={`/storage/persistentvolumeclaims/${encodeURIComponent(name)}?${new URLSearchParams({ clusterId, namespace })}`}
      >
        <TableCellText value={label || name} />
      </Link>
    ) : (
      <TableCellText value={label || name} />
    )
  const status = {
    title: zh ? '状态' : 'Status',
    dataIndex: 'status',
    width: 120,
    render: (value: string) => (value ? <StatusTag value={value} /> : '—'),
  }
  const volumeColumns: TableColumnsType<PersistentVolume> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 240,
      render: (name: string) => volume(name),
    },
    status,
    {
      title: zh ? '声明容量' : 'Declared capacity',
      dataIndex: 'capacity',
      width: 160,
      render: (value?: string) => <TableCellText value={value} />,
    },
    {
      title: 'ClaimRef',
      key: 'claimRef',
      width: 260,
      render: (_, row) =>
        claim(
          row.claimName,
          row.claimNamespace,
          row.claimRef ||
            (row.claimName && row.claimNamespace
              ? `${row.claimNamespace}/${row.claimName}`
              : undefined),
        ),
    },
  ]
  const claimColumns: TableColumnsType<PersistentVolumeClaim> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 240,
      render: (name: string, row) => claim(name, row.namespace),
    },
    {
      title: 'Namespace',
      dataIndex: 'namespace',
      width: 180,
      render: (value: string) => (
        <MetadataTag label={<TableCellText value={value} />} tone="blue" />
      ),
    },
    status,
    {
      title: zh ? '请求容量' : 'Requested capacity',
      dataIndex: 'requested',
      width: 160,
      render: (value?: string) => <TableCellText value={value} />,
    },
    { title: 'PV', dataIndex: 'volumeName', width: 240, render: (name?: string) => volume(name) },
  ]
  const [search, setSearch] = useState('')
  useEffect(() => setSearch(''), [kind, detail.name, clusterId])
  const query = search.trim().toLocaleLowerCase()
  const volumes = (detail.volumes ?? []).filter((row) =>
    [row.name, row.status, row.claimRef, row.claimName, row.claimNamespace].some((value) =>
      value?.toLocaleLowerCase().includes(query),
    ),
  )
  const claims = (detail.claims ?? []).filter((row) =>
    [row.name, row.namespace, row.status, row.volumeName].some((value) =>
      value?.toLocaleLowerCase().includes(query),
    ),
  )
  const rows = detail[kind]
  const shown = kind === 'volumes' ? volumes.length : claims.length
  const truncated = detail[kind === 'volumes' ? 'volumesTruncated' : 'claimsTruncated']
  const empty =
    rows === undefined
      ? zh
        ? '未提供关系清单'
        : 'Relationship list not provided'
      : query
        ? zh
          ? '当前返回范围内没有匹配关系'
          : 'No matching relationships in the returned scope'
        : zh
          ? '当前未返回关系；不代表全局未使用'
          : 'No relationships returned; global non-use is not established'
  return (
    <Card
      className="soha-detail-card"
      title={
        <span className="soha-sc-section-title">
          {kind === 'volumes' ? 'PVs' : 'PVCs'}
          <Tooltip
            trigger={['hover', 'focus', 'click']}
            title={
              zh
                ? '仅展示当前返回的资源。权限过滤、读取失败或截断可能影响范围；资源引用不代表挂载成功或存储后端健康。'
                : 'Only currently returned resources are shown. Permissions, read failures or truncation may affect coverage; references do not establish successful mounts or backend health.'
            }
          >
            <Button
              type="text"
              size="small"
              icon={<QuestionCircleOutlined />}
              aria-label={zh ? '资源范围说明' : 'Resource coverage information'}
            />
          </Tooltip>
        </span>
      }
      extra={
        <span className="soha-sc-muted">
          {rows === undefined
            ? zh
              ? '未提供'
              : 'Not provided'
            : zh
              ? `显示 ${shown} / 当前返回 ${rows.length} 条`
              : `${shown} shown / ${rows.length} currently returned`}
        </span>
      }
    >
      <ManagementTableToolbar>
        <ManagementToolbarSearch
          aria-label={zh ? '搜索当前返回的关系' : 'Search returned relationships'}
          placeholder={
            kind === 'volumes'
              ? zh
                ? '搜索名称、状态、ClaimRef'
                : 'Search name, status, claim'
              : zh
                ? '搜索名称、Namespace、状态、PV'
                : 'Search name, namespace, status, PV'
          }
          value={search}
          onChange={setSearch}
        />
      </ManagementTableToolbar>
      {truncated ? (
        <Alert
          className="soha-sc-truncation"
          showIcon
          type="warning"
          title={zh ? '结果已截断，当前清单不完整' : 'Results truncated; this list is incomplete'}
        />
      ) : null}
      {kind === 'volumes' ? (
        <AdminTable
          columns={volumeColumns}
          dataSource={volumes}
          pagination={false}
          enableColumnSelection={false}
          tableSize="small"
          rowKey="name"
          scroll={{ x: 780 }}
          empty={empty}
        />
      ) : (
        <AdminTable
          columns={claimColumns}
          dataSource={claims}
          pagination={false}
          enableColumnSelection={false}
          tableSize="small"
          rowKey={(row) => `${row.namespace}/${row.name}`}
          scroll={{ x: 940 }}
          empty={empty}
        />
      )}
    </Card>
  )
}
