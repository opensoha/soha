import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData } from '@/utils/query-feedback'
import { useState } from 'react'
import { Button, Modal, Space, Switch, Typography } from 'antd'
import { HistoryOutlined, SettingOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { TableRowActionMenu } from '@/components/table-row-action-menu'
import { StatusTag } from '@/components/status-tag'
import { tableColumnPresets } from '@/utils/table-columns'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { formatDateTime } from '@/utils/time'
import { deliveryQueries } from '../queries'
import { deliveryMutations } from '../mutations'
import type { DeliveryTrigger, DeliveryTriggerEvent, DeliveryTriggerWebhook } from '../types'
import { triggerInput, triggerReasonLabels } from './model'
import { TriggerEditor } from './editor'

export interface TriggerTarget {
  targetKind: 'template_source' | 'workflow'
  targetId: string
  workflowVersion?: number
  webhookRefs: DeliveryTriggerWebhook[]
}

const typeLabels = { webhook: 'Git Webhook', poll: '轮询同步', schedule: '定时 / 发布日历' }

function EventStatus({ event }: { event?: DeliveryTriggerEvent }) {
  return event ? (
    <StatusTag
      value={event.status}
      label={
        event.status === 'succeeded' ? (event.batchId ? '已创建交付' : '已同步草稿') : undefined
      }
    />
  ) : (
    <span>尚未触发</span>
  )
}

export function DeliveryTriggerManager(target: TriggerTarget & { readOnly?: boolean }) {
  const permissions = usePermissionSnapshot().data?.data
  const allowed = (action: string) =>
    (action === 'view' || !target.readOnly) &&
    hasPermission(permissions, `delivery.triggers.${action}`)
  const [offset, setOffset] = useState(0)
  const [editing, setEditing] = useState<{ item?: DeliveryTrigger }>()
  const [history, setHistory] = useState<DeliveryTrigger>()
  const client = useQueryClient()
  const triggers = useQuery(
    deliveryQueries.triggers.list(target.targetKind, target.targetId, offset, allowed('view')),
  )
  const toggle = useOperationMutation(deliveryMutations.triggers.save(client), {
    label: ['更新触发器', 'Update trigger'],
    target: ({ input }) => input.name,
    receipt: () => ({ message: '触发器配置已更新' }),
  })
  const items = readableQueryData(triggers)
  if (!allowed('view'))
    return <ManagementState compact kind="no-permission" title="无权查看触发器" />
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Typography.Text type="secondary">
        {target.targetKind === 'template_source'
          ? '自动同步只更新草稿。发布模板和执行工作流由各自入口操作。'
          : `触发后创建交付记录，执行结果请查看关联交付。新配置固定使用工作流 v${target.workflowVersion}。`}
      </Typography.Text>
      {toggle.feedback}
      <ReadFeedback query={triggers} label="触发器" />
      {triggers.isError && !items ? null : (
        <AdminTable
          rowKey="id"
          loading={triggers.isPending}
          dataSource={items ?? []}
          pagination={false}
          enableColumnSelection={false}
          viewportScroll={false}
          scroll={{ x: 760 }}
          toolbar={<Typography.Text>触发器</Typography.Text>}
          toolbarExtra={
            <Space>
              <Button onClick={() => void triggers.refetch()}>刷新</Button>
              <Button
                type="primary"
                disabled={!allowed('create') || triggers.isError}
                onClick={() => setEditing({})}
              >
                添加触发器
              </Button>
            </Space>
          }
          columns={[
            {
              title: '名称',
              dataIndex: 'name',
              key: 'name',
              width: 180,
              render: (name: string, item: DeliveryTrigger) => (
                <Button
                  type="link"
                  style={{ padding: 0, height: 'auto', fontWeight: 600 }}
                  onClick={() => setHistory(item)}
                >
                  {name}
                </Button>
              ),
            },
            {
              title: '方式',
              key: 'type',
              width: 160,
              render: (_: unknown, item: DeliveryTrigger) => typeLabels[item.type],
            },
            { title: '执行身份', dataIndex: 'serviceAccountName', key: 'identity', width: 150 },
            {
              title: '最近触发',
              key: 'last',
              width: 160,
              render: (_: unknown, item: DeliveryTrigger) => <EventStatus event={item.lastEvent} />,
            },
            {
              title: '启用',
              key: 'enabled',
              width: 75,
              render: (_: unknown, item: DeliveryTrigger) => (
                <Switch
                  aria-label={`启用 ${item.name}`}
                  checked={item.enabled}
                  disabled={!allowed('update') || toggle.blocked || triggers.isError}
                  onChange={(enabled) =>
                    toggle.mutate({ id: item.id, input: triggerInput(item, enabled) })
                  }
                />
              ),
            },
            {
              ...tableColumnPresets.action,
              title: '操作',
              key: 'actions',
              render: (_: unknown, item: DeliveryTrigger) => (
                <TableRowActionMenu
                  targetLabel={item.name}
                  items={[
                    {
                      key: 'history',
                      label: '记录',
                      icon: <HistoryOutlined />,
                      onClick: () => setHistory(item),
                    },
                    {
                      key: 'edit',
                      label: '配置',
                      icon: <SettingOutlined />,
                      disabled: !allowed('update') || triggers.isError,
                      onClick: () => setEditing({ item }),
                    },
                  ]}
                />
              ),
            },
          ]}
        />
      )}
      {offset > 0 || items?.length === 50 ? (
        <Space>
          <Button
            disabled={offset === 0 || triggers.isFetching}
            onClick={() => setOffset(offset - 50)}
          >
            上一页
          </Button>
          <Button
            disabled={(items?.length ?? 0) < 50 || triggers.isFetching}
            onClick={() => setOffset(offset + 50)}
          >
            下一页
          </Button>
        </Space>
      ) : null}
      {editing ? (
        <TriggerEditor
          target={target}
          item={editing.item}
          readOnly={triggers.isError || !allowed(editing.item ? 'update' : 'create')}
          onClose={() => setEditing(undefined)}
        />
      ) : null}
      {history ? (
        <TriggerHistory key={history.id} item={history} onClose={() => setHistory(undefined)} />
      ) : null}
    </Space>
  )
}

function TriggerHistory({ item, onClose }: { item: DeliveryTrigger; onClose: () => void }) {
  const [offset, setOffset] = useState(0)
  const events = useQuery(deliveryQueries.triggers.events(item.id, offset))
  const items = readableQueryData(events)
  return (
    <Modal open title={`${item.name} · 触发记录`} width={1000} footer={null} onCancel={onClose}>
      <ReadFeedback query={events} label="触发记录" />
      {events.isError && !items ? null : (
        <AdminTable
          rowKey="id"
          dataSource={items ?? []}
          loading={events.isPending}
          pagination={false}
          enableColumnSelection={false}
          viewportScroll={false}
          scroll={{ x: 850 }}
          columns={[
            {
              title: '触发时间',
              key: 'time',
              width: 175,
              render: (_: unknown, event: DeliveryTriggerEvent) => formatDateTime(event.occurredAt),
            },
            {
              title: '结果',
              key: 'status',
              width: 130,
              render: (_: unknown, event: DeliveryTriggerEvent) => <EventStatus event={event} />,
            },
            {
              title: '说明',
              key: 'reason',
              width: 200,
              render: (_: unknown, event: DeliveryTriggerEvent) =>
                triggerReasonLabels[event.reason ?? ''] ?? event.reason ?? '—',
            },
            {
              title: '提交',
              key: 'commit',
              width: 160,
              render: (_: unknown, event: DeliveryTriggerEvent) =>
                event.resolvedCommit ? (
                  <Typography.Text
                    copyable={{ text: event.resolvedCommit }}
                    title={event.resolvedCommit}
                  >
                    {event.resolvedCommit.slice(0, 12)}
                  </Typography.Text>
                ) : (
                  '—'
                ),
            },
            {
              title: '执行记录',
              key: 'record',
              width: 185,
              render: (_: unknown, event: DeliveryTriggerEvent) =>
                event.batchId ? (
                  <Link to={`/delivery/batches/${encodeURIComponent(event.batchId)}`}>
                    查看交付
                  </Link>
                ) : event.syncRunId ? (
                  <Typography.Text copyable={{ text: event.syncRunId }} title={event.syncRunId}>
                    同步 {event.syncRunId.slice(0, 8)}
                  </Typography.Text>
                ) : (
                  '—'
                ),
            },
          ]}
        />
      )}
      <Space>
        <Button disabled={!offset || events.isFetching} onClick={() => setOffset(offset - 50)}>
          上一页
        </Button>
        <Button
          disabled={(items?.length ?? 0) < 50 || events.isFetching}
          onClick={() => setOffset(offset + 50)}
        >
          下一页
        </Button>
      </Space>
    </Modal>
  )
}
