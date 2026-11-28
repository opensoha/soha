import { AdminTable } from '@/components/admin-table'
import type { TableColumnsType } from 'antd'
import { RedoOutlined, StopOutlined } from '@ant-design/icons'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { TableRowActionMenu } from '@/components/table-row-action-menu'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import type { DirectoryConflict } from './types'

export function DirectoryConflictCenter({
  canManagePeople,
  conflicts,
  loading,
  onResolve,
  unavailable = false,
}: {
  canManagePeople: boolean
  conflicts: DirectoryConflict[]
  loading: boolean
  onResolve: (id: string, resolution: 'ignore' | 'retry') => void
  unavailable?: boolean
}) {
  const columns: TableColumnsType<DirectoryConflict> = [
    { title: '对象', dataIndex: 'objectType', width: 100 },
    {
      title: '外部 ID',
      dataIndex: 'externalId',
      width: 200,
      render: (value?: string) => value || '-',
    },
    { title: '原因', dataIndex: 'reason' },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (value: string) => <StatusTag value={value} />,
    },
    { title: '发生时间', dataIndex: 'createdAt', width: 180, render: formatDateTime },
    {
      ...tableColumnPresets.action,
      title: '操作',
      key: 'actions',
      render: (_value, record) => (
        <TableRowActionMenu
          targetLabel={record.externalId || record.id}
          items={
            record.status === 'open' && canManagePeople
              ? [
                  {
                    key: 'retry',
                    label: '重试',
                    icon: <RedoOutlined />,
                    onClick: () => onResolve(record.id, 'retry'),
                  },
                  {
                    key: 'ignore',
                    label: '忽略',
                    icon: <StopOutlined />,
                    onClick: () => onResolve(record.id, 'ignore'),
                  },
                ]
              : []
          }
        />
      ),
    },
  ]

  if (unavailable) return null
  if (!loading && conflicts.length === 0) {
    return (
      <ManagementState
        compact
        kind="empty"
        title="没有待处理冲突"
        description="目录数据当前不需要人工处理。"
      />
    )
  }
  return (
    <AdminTable
      enableColumnSelection={false}
      pageSize={10}
      rowKey="id"
      tableSize="small"
      loading={loading}
      columns={columns}
      dataSource={conflicts}
      scroll={{ x: 900 }}
    />
  )
}
