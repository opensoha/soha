import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import type { CLISession } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Empty, Popconfirm, Space, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useState } from 'react'
import { useAuthStore } from '@/stores/auth-store'
import { cliAuthApi } from './cli-auth-api'
import { authKeys } from './keys'

// Cursor pages deliberately have no invented total or client-side global sorting.
export function CLISessionsPanel() {
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.user?.userId)
  const [pages, setPages] = useState<{ owner?: string; cursors: string[] }>({ cursors: [] })
  const cursors = pages.owner === userId ? pages.cursors : []
  const cursor = cursors[cursors.length - 1]
  const query = useQuery({
    queryKey: authKeys.cliSessionPage(userId ?? '', cursor ?? ''),
    queryFn: () => cliAuthApi.sessions(cursor),
    enabled: Boolean(userId),
  })
  const page = readableQueryData(query)
  const revoke = useOperationMutation(
    {
      mutationFn: cliAuthApi.revokeSession,
      onSuccess: () => client.invalidateQueries({ queryKey: authKeys.cliSessions() }),
    },
    {
      label: ['撤销 CLI 会话', 'Revoke CLI session'],
      target: (id) => id,
      receipt: () => ({ message: 'CLI 会话已撤销', evidence: '该会话的访问和刷新凭据均已失效。' }),
    },
  )
  const columns: TableColumnsType<CLISession> = [
    {
      title: '终端',
      dataIndex: 'deviceName',
      key: 'device',
      render: (value, row) => (
        <Space orientation="vertical" size={0}>
          <Typography.Text>{value}</Typography.Text>
          <Typography.Text type="secondary">{row.clientVersion}</Typography.Text>
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (value) => <StatusTag value={value} />,
    },
    {
      title: '最近活动',
      dataIndex: 'lastSeenAt',
      key: 'seen',
      render: (value) => formatDateTime(value),
    },
    {
      title: '空闲有效至',
      dataIndex: 'expiresAt',
      key: 'expires',
      render: (value) => formatDateTime(value),
    },
    {
      title: '最长有效至',
      dataIndex: 'absoluteExpiresAt',
      key: 'absolute',
      render: (value) => formatDateTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, row) => (
        <Popconfirm
          title="撤销此 CLI 会话？"
          description="对应终端将需要重新登录。"
          onConfirm={() => revoke.mutateAsync(row.id)}
          disabled={revoke.blocked || row.status !== 'active'}
        >
          <Button type="link" danger disabled={revoke.blocked || row.status !== 'active'}>
            撤销
          </Button>
        </Popconfirm>
      ),
    },
  ]
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Typography.Paragraph type="secondary">
        这里列出通过设备码或新版密码登录创建的 CLI 会话。Key 的撤销在访问凭据中管理。
      </Typography.Paragraph>
      <ReadFeedback label="CLI 会话" query={query} />
      {revoke.feedback}
      <AdminTable
        columns={columns}
        dataSource={page?.items ?? []}
        rowKey="id"
        pagination={false}
        localSorting={false}
        loading={query.isLoading}
        refreshing={query.isFetching}
        onRefresh={() => void query.refetch()}
        hasReadData={Boolean(page)}
        error={query.error}
        tableSize="small"
        scroll={{ x: 900 }}
        empty={<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无 CLI 会话" />}
      />
      <Space wrap>
        <Button
          disabled={cursors.length === 0 || query.isFetching}
          onClick={() => setPages({ owner: userId, cursors: cursors.slice(0, -1) })}
        >
          上一页
        </Button>
        <Typography.Text>第 {cursors.length + 1} 页</Typography.Text>
        <Button
          disabled={!page?.nextCursor || query.isFetching || query.isError}
          onClick={() => {
            const next = page?.nextCursor
            if (next) setPages({ owner: userId, cursors: [...cursors, next] })
          }}
        >
          下一页
        </Button>
      </Space>
    </Space>
  )
}
