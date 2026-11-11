import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAuthStore } from '@/stores/auth-store'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Flex, Popconfirm, Result, Space, Typography } from 'antd'
import { useLocation, useNavigate } from 'react-router-dom'
import { workbenchApi } from '../api'
import { workbenchKeys } from '../keys'
import { AIWorkbenchPage } from '../page'

const accessLabel = {
  not_requested: '尚未开通',
  pending: '等待批准',
  approved: '已开通',
  rejected: '申请未通过',
  revoked: '开通已撤销',
}

export function HeyaPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const owner = useAuthStore((state) => state.user?.userId)
  const permissions = usePermissionSnapshot()
  const canChat = hasPermission(readableQueryData(permissions)?.data, 'observe.ai.chat')
  const canManage = hasPermission(readableQueryData(permissions)?.data, 'ai.environments.update')
  const client = useQueryClient()
  const key = workbenchKeys.personalAgentAccess(owner)
  const access = useQuery({
    queryKey: key,
    queryFn: workbenchApi.personalAgent.access,
    enabled: !!owner && canChat,
    retry: false,
    refetchInterval: (query) => (query.state.status === 'error' ? false : 5000),
  })
  const requests = useQuery({
    queryKey: workbenchKeys.personalAgentAccessRequests(owner),
    queryFn: workbenchApi.personalAgent.requests,
    enabled: !!owner && canManage && location.pathname === '/heya/access',
    retry: false,
    refetchInterval: (query) => (query.state.status === 'error' ? false : 5000),
  })
  const request = useOperationMutation(
    {
      mutationFn: workbenchApi.personalAgent.request,
      onSuccess: async () => {
        await client.invalidateQueries({ queryKey: key })
      },
    },
    { label: ['申请开通 Heya', 'Request Heya access'], target: () => owner || '当前账号' },
  )
  const decision = useOperationMutation(
    {
      mutationFn: workbenchApi.personalAgent.decide,
      onSuccess: async () => {
        await client.invalidateQueries({ queryKey: workbenchKeys.personalAgentAccessRequests() })
        await client.invalidateQueries({ queryKey: key })
      },
    },
    { label: ['更新 Heya 开通决定', 'Update Heya decision'], target: (input) => input.userId },
  )
  if (
    canChat &&
    readableQueryData(access)?.data?.status === 'approved' &&
    location.pathname === '/heya'
  )
    return (
      <>
        <ReadFeedback label="Heya 开通状态" query={access} canRead={canChat} />
        <AIWorkbenchPage />
      </>
    )
  const status = canChat ? readableQueryData(access)?.data?.status : undefined
  return (
    <div className="soha-heya-access">
      <ReadFeedback label="Heya 开通状态" query={access} canRead={canChat} />
      <ReadFeedback
        label="Heya 开通申请"
        query={requests}
        canRead={canManage && location.pathname === '/heya/access'}
      />
      {request.feedback}
      {decision.feedback}
      <Flex justify="space-between" align="center">
        <Button type="text" onClick={() => navigate('/ai-workbench/overview')}>
          返回 AI 工作台
        </Button>
        {canManage && location.pathname !== '/heya/access' ? (
          <Button onClick={() => navigate('/heya/access')}>开通管理</Button>
        ) : null}
      </Flex>
      <Result
        icon={<img className="soha-heya-access__avatar" src="/heya-avatar.svg" alt="Heya 嘿呀" />}
        title="Heya · 嘿呀"
        subTitle="你的个人 Agent，拥有独立对话、动态与执行环境。新助手的默认名字是 heya，你也可以给它改名。"
        extra={
          <Space wrap>
            {status === 'approved' ? (
              <Button type="primary" onClick={() => navigate('/heya')}>
                进入 Heya
              </Button>
            ) : (
              <Button
                type="primary"
                loading={request.isPending}
                disabled={!canChat || !status || status === 'pending' || request.blocked}
                onClick={() => request.mutate()}
              >
                {status === 'pending'
                  ? '申请已提交'
                  : status === 'rejected' || status === 'revoked'
                    ? '重新申请'
                    : '申请开通'}
              </Button>
            )}
            <Button onClick={() => navigate('/ai-workbench/chat')}>普通 AI 对话</Button>
          </Space>
        }
      />
      <Card size="small" title="开通状态">
        <Typography.Paragraph>
          {!canChat && !permissions.isPending
            ? '当前账号没有 AI 对话权限。'
            : access.isError
              ? '开通状态读取失败。'
              : status
                ? accessLabel[status]
                : '正在读取开通状态…'}
        </Typography.Paragraph>
        <Typography.Text type="secondary">
          开通申请由管理员批准。批准后仍需准备个人执行环境；环境就绪状态会单独显示。未开通时可以使用普通
          AI 对话。
        </Typography.Text>
        {access.isError ? <Button onClick={() => void access.refetch()}>重试</Button> : null}
      </Card>
      {canManage && location.pathname === '/heya/access' ? (
        <Card
          title="个人 Agent 开通申请"
          extra={
            <Button loading={requests.isFetching} onClick={() => void requests.refetch()}>
              刷新
            </Button>
          }
        >
          {requests.isError ? (
            <Typography.Text type="danger">申请列表读取失败，请重试。</Typography.Text>
          ) : requests.isPending ? (
            '正在读取申请…'
          ) : !requests.data?.data?.length ? (
            '暂无申请'
          ) : (
            requests.data.data.map((item) => (
              <Flex
                className="soha-heya-access__request"
                key={item.userId}
                align="center"
                justify="space-between"
                gap={16}
                wrap
              >
                <span>
                  <Typography.Text strong>{item.userId}</Typography.Text>
                  <br />
                  <Typography.Text type="secondary">
                    {accessLabel[item.status]} ·{' '}
                    {item.requestedAt ? new Date(item.requestedAt).toLocaleString() : ''}
                  </Typography.Text>
                </span>
                <Space>
                  {(item.status === 'pending'
                    ? (['approved', 'rejected'] as const)
                    : item.status === 'approved'
                      ? (['revoked'] as const)
                      : []
                  ).map((next) => (
                    <Popconfirm
                      key={next}
                      disabled={decision.blocked}
                      title={`${next === 'approved' ? '批准' : next === 'rejected' ? '拒绝' : '撤销'} ${item.userId} 的 Heya 开通？`}
                      onConfirm={() =>
                        decision.mutateAsync({
                          userId: item.userId,
                          status: next,
                          version: item.version,
                        })
                      }
                    >
                      <Button
                        disabled={decision.blocked}
                        loading={decision.isPending}
                        danger={next === 'revoked'}
                      >
                        {next === 'approved' ? '批准' : next === 'rejected' ? '拒绝' : '撤销'}
                      </Button>
                    </Popconfirm>
                  ))}
                </Space>
              </Flex>
            ))
          )}
        </Card>
      ) : null}
    </div>
  )
}
