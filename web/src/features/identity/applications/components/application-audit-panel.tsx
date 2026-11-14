import { Link } from 'react-router-dom'
import { Button, Space, Typography } from 'antd'
import { useQueries } from '@tanstack/react-query'
import { ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { systemQueries } from '@/features/system'
import { isApiError } from '@/services/api-error'

export function ApplicationAuditPanel({ id, providerId }: { id: string; providerId?: string }) {
  const snapshot = usePermissionSnapshot().data?.data
  const canView =
    hasPermission(snapshot, 'identity.audit.view') || hasPermission(snapshot, 'system.audit.view')
  const filters = [
    { metadataKey: 'applicationId', metadataValue: id },
    { metadataKey: 'event.applicationId', metadataValue: id },
    ...(providerId ? [{ metadataKey: 'providerId', metadataValue: providerId }] : []),
  ]
  const queries = useQueries({
    queries: filters.map((filter) => ({
      ...systemQueries.audit('identity', { ...filter, limit: 20 }),
      enabled: canView,
    })),
  })
  if (!canView) return <ManagementState kind="no-permission" title="没有查看接入审计的权限" />
  const error = queries.find((query) => query.isError)?.error
  if (error)
    return (
      <ManagementState
        kind={isApiError(error) && error.status === 403 ? 'no-permission' : 'error'}
        title="接入记录加载失败"
        description={error.message}
        actions={
          <Button onClick={() => queries.forEach((query) => void query.refetch())}>重试</Button>
        }
      />
    )
  if (queries.some((query) => query.isPending))
    return <ManagementState kind="loading" title="正在读取接入记录" />
  const records = [
    ...new Map(
      queries.flatMap((query) => query.data ?? []).map((item) => [item.id, item]),
    ).values(),
  ]
    .sort((a, b) => b.createdAt.localeCompare(a.createdAt))
    .slice(0, 20)
  return (
    <>
      <Typography.Paragraph type="secondary">
        最近 20 条关联审计。保存配置不计为登录验证。
      </Typography.Paragraph>
      {!records.length ? (
        <ManagementState kind="empty" title="暂无关联接入记录" />
      ) : (
        <ul>
          {records.map((record) => (
            <li key={record.id} style={{ marginBottom: 16, overflowWrap: 'anywhere' }}>
              <Space wrap>
                <Typography.Text strong>{record.action}</Typography.Text>
                <MetadataTag label={record.result} />
              </Space>
              <div>
                <time dateTime={record.createdAt}>{record.createdAt}</time> ·{' '}
                {record.actorName || record.actorId || '系统'}
              </div>
              <Typography.Text type="secondary">{record.summary}</Typography.Text>
              {record.requestId && (
                <div>
                  <Typography.Text copyable>{record.requestId}</Typography.Text>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      <Link to="/identity/audit">打开审计</Link>
    </>
  )
}
