import { ContextFeedback } from '@/components/context-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { api } from '@/services/api-client'
import type {
  AssistantMeshDelegation,
  AssistantMeshDelegationListEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Empty, List, Segmented, Space, Tag, Typography } from 'antd'
import { workbenchKeys } from '../keys'
import { useState } from 'react'

const PATH = '/copilot/assistant-mesh/delegations'
const STATUS: Record<AssistantMeshDelegation['status'], string> = {
  awaiting_owner_approval: '等待本人批准',
  approved: '已批准',
  executing: '执行中',
  awaiting_confirmation: '等待回传确认',
  completed: '已完成',
  declined: '已拒绝',
  invalidated: '已失效',
  expired: '已过期',
  failed: '执行失败',
}

export function AssistantDelegations({ owner }: { owner: string }) {
  const client = useQueryClient()
  const [direction, setDirection] = useState<'inbox' | 'sent'>('inbox')
  const key = workbenchKeys.assistantMesh.delegations(owner)
  const query = useQuery({
    queryKey: workbenchKeys.assistantMesh.delegationList(owner, direction),
    queryFn: async () =>
      (await api.getEnvelope<AssistantMeshDelegationListEnvelope>(`${PATH}?direction=${direction}`)).items,
    retry: false,
    refetchInterval: 5_000,
    refetchOnMount: 'always',
  })
  const decline = useOperationMutation({
    mutationFn: (id: string) => api.post(`${PATH}/${encodeURIComponent(id)}/decline`, {}),
    onSettled: () => client.invalidateQueries({ queryKey: key }),
  }, {
    label: ['拒绝委托', 'Decline delegation'],
    target: (id) => id,
    receipt: () => ({ message: '委托已拒绝' }),
  })
  const items = query.isSuccess ? query.data.filter((item) =>
    direction === 'inbox' ? item.ownerId === owner : item.callerId === owner) : undefined
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Segmented
        aria-label="委托方向"
        value={direction}
        onChange={setDirection}
        options={[{ label: '收到的委托', value: 'inbox' }, { label: '发出的委托', value: 'sent' }]}
      />
      <ContextFeedback
        type="info"
        title="使用 Soha App 发起委托。批准和回传须在机主的 macOS App 内完成。"
      />
      <ReadFeedback label="本机委托" query={query} canRead={query.isEnabled} />
      {decline.feedback}
      {items && (items.length === 0 ? <Empty description="暂无本机委托" /> : (
        <List dataSource={items} rowKey="id" renderItem={(item) => (
          <List.Item actions={
            direction === 'inbox' && query.isSuccess &&
            ['awaiting_owner_approval', 'approved', 'awaiting_confirmation'].includes(item.status)
              ? [<Button key="decline" size="small" danger disabled={decline.isPending || decline.blocked} onClick={() => decline.mutate(item.id)}>拒绝委托</Button>]
              : []
          }>
            <Space orientation="vertical" size={4}>
              <Space wrap>
                <Typography.Text strong>{item.field === 'buildVersion' ? 'macOS 构建号' : 'macOS 产品版本'}</Typography.Text>
                <Tag>{STATUS[item.status]}</Tag>
              </Space>
              <Typography.Text type="secondary">{direction === 'inbox' ? item.callerName : item.ownerName} · 截止 {new Date(item.expiresAt).toLocaleString('zh-CN')}</Typography.Text>
              {item.status === 'completed' && item.result && <Typography.Text copyable>{item.result}</Typography.Text>}
            </Space>
          </List.Item>
        )} />
      ))}
    </Space>
  )
}
