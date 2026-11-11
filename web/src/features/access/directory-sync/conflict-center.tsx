import { Button, Space } from 'antd'
import { AdminTable } from '@/components/admin-table'
import type { TableColumnsType } from 'antd'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
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
      title: '操作',
      key: 'actions',
      width: 140,
      render: (_value, record) =>
        record.status === 'open' && canManagePeople ? (
          <Space size={4}>
            <Button size="small" onClick={() => onResolve(record.id, 'retry')}>
              重试
            </Button>
            <Button size="small" onClick={() => onResolve(record.id, 'ignore')}>
              忽略
            </Button>
          </Space>
        ) : (
          '-'
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
