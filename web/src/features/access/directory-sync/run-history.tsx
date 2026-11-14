import { Button, Typography } from 'antd'
import { AdminTable } from '@/components/admin-table'
import type { TableColumnsType } from 'antd'
import { StopOutlined } from '@ant-design/icons'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { formatDateTime } from '@/utils/time'
import type { DirectorySyncRun } from './types'

const { Text } = Typography

export function DirectoryRunHistory({
  canSync,
  loading,
  onCancel,
  runs,
  unavailable = false,
}: {
  canSync: boolean
  loading: boolean
  onCancel: () => void
  runs: DirectorySyncRun[]
  unavailable?: boolean
}) {
  const columns: TableColumnsType<DirectorySyncRun> = [
    { title: '触发方式', dataIndex: 'trigger', width: 110 },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: '开始时间',
      dataIndex: 'startedAt',
      width: 180,
      render: (value?: string) => (value ? formatDateTime(value) : '-'),
    },
    {
      title: '结束时间',
      dataIndex: 'finishedAt',
      width: 180,
      render: (value?: string) => (value ? formatDateTime(value) : '-'),
    },
    {
      title: '错误',
      dataIndex: 'error',
      render: (value?: string) => (value ? <Text type="danger">{value}</Text> : '-'),
    },
  ]
  const hasRunning = runs.some((run) => run.status === 'queued' || run.status === 'running')

  if (unavailable) return null
  if (!loading && runs.length === 0) {
    return (
      <ManagementState
        compact
        kind="empty"
        title="暂无同步记录"
        description="首次同步后可在这里查看运行结果。"
      />
    )
  }

  return (
    <>
      {canSync && hasRunning ? (
        <Button danger size="small" icon={<StopOutlined />} onClick={onCancel}>
          取消当前同步
        </Button>
      ) : null}
      <AdminTable
        enableColumnSelection={false}
        rowKey="id"
        tableSize="small"
        loading={loading}
        columns={columns}
        dataSource={runs}
        pagination={false}
        scroll={{ x: 760 }}
      />
    </>
  )
}
