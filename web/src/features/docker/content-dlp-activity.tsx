import { useState } from 'react'
import { Alert, Button, Drawer, Input, Popconfirm, Select, Space, Typography } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type {
  DockerVolumeDLPCase,
  DockerVolumeDLPNetworkTarget,
  DockerVolumeDLPAppealReviewInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAuthStore } from '@/stores/auth-store'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import {
  dlpWorkflowKeys,
  dlpWorkflowQueries,
  reviewDLPAppeal,
  submitDLPAppeal,
} from './content-dlp-workflow-api'

export function DockerDLPActivity({
  projectId,
  eventId,
  onRetry,
  busy,
}: {
  projectId: string
  eventId?: string
  onRetry?: (eventId: string, action: 'acknowledge' | 'approved_appeal') => void
  busy?: boolean
}) {
  const [open, setOpen] = useState(false)
  const viewer = useAuthStore((s) => s.user?.userId ?? '')
  const permissions = usePermissionSnapshot()
  const ready = permissions.isEnabled !== false && permissions.isSuccess && !permissions.isFetching
  const snapshot = ready ? readableQueryData(permissions)?.data : undefined
  const review = hasPermission(snapshot, 'docker.dlp-appeals.review')
  const global = review || hasPermission(snapshot, 'docker.dlp-events.view')
  const link = hasPermission(snapshot, 'network_access.dlp_linkage.manage')
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  return (
    <>
      <Button size="small" disabled={!viewer || !ready} onClick={() => setOpen(true)}>
        {text('申诉与联动记录', 'Appeals and network response')}
      </Button>
      {open && ready ? (
        <ActivityDrawer
          key={`${projectId}:${viewer}:${global}:${review}:${link}`}
          projectId={projectId}
          viewer={viewer}
          scope={`${global}:${review}:${link}`}
          review={review}
          link={link}
          initialEvent={eventId}
          onClose={() => setOpen(false)}
          onRetry={onRetry}
          busy={busy}
        />
      ) : null}
    </>
  )
}

function ActivityDrawer({
  projectId,
  viewer,
  scope,
  review,
  link,
  initialEvent,
  onClose,
  onRetry,
  busy,
}: {
  projectId: string
  viewer: string
  scope: string
  review: boolean
  link: boolean
  initialEvent?: string
  onClose: () => void
  onRetry?: (id: string, action: 'acknowledge' | 'approved_appeal') => void
  busy?: boolean
}) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const client = useQueryClient()
  const [selected, setSelected] = useState(initialEvent ?? '')
  const activity = useQuery(dlpWorkflowQueries.activity(projectId, viewer, scope, true))
  const detail = useQuery(
    dlpWorkflowQueries.case(projectId, selected, viewer, scope, Boolean(selected)),
  )
  const item = detail.isSuccess && !detail.isFetching ? detail.data : undefined
  const refresh = () => client.invalidateQueries({ queryKey: dlpWorkflowKeys.project(projectId) })
  return (
    <Drawer
      open
      destroyOnHidden
      size={900}
      title={text('内容传输申诉与网络联动', 'Content transfer appeals and network response')}
      onClose={onClose}
    >
      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        <Alert
          type="info"
          showIcon
          title={text(
            '记录不包含文件正文或匹配片段',
            'Records contain no file bodies or matched snippets',
          )}
          description={text(
            '普通用户仅可查看自己的事件；审查需要独立权限并禁止自审。批准只允许原文件的一次重试，不会修改策略。',
            'Users see their own events. Review requires a separate permission and cannot be self-approved. Approval permits one retry of the original file and does not change policy.',
          )}
        />
        <ReadFeedback query={activity} label={text('最近 100 条记录', 'Latest 100 records')} />
        <Input.Search
          maxLength={128}
          placeholder={text(
            '按事件 ID 查询（包含较早事件）',
            'Look up an event ID, including older records',
          )}
          onSearch={(value) => setSelected(value.trim())}
          enterButton={text('查询事件', 'Find event')}
        />
        <AdminTable
          rowKey={(row) => row.event.id}
          dataSource={activity.isSuccess && !activity.isFetching ? activity.data : []}
          loading={activity.isFetching}
          pagination={{ pageSize: 8, showSizeChanger: false }}
          onRefresh={() => void refresh()}
          columns={[
            {
              title: text('事件 / 时间', 'Event / Time'),
              key: 'event',
              render: (_: unknown, row: DockerVolumeDLPCase) => (
                <Button type="link" onClick={() => setSelected(row.event.id)}>
                  {row.event.id}
                  <br />
                  {row.event.createdAt}
                </Button>
              ),
            },
            {
              title: text('主体 / 操作', 'Actor / Action'),
              key: 'actor',
              render: (_: unknown, row: DockerVolumeDLPCase) => (
                <>
                  {row.event.actorId}
                  <br />
                  {row.event.action}
                </>
              ),
            },
            {
              title: text('检查 / 申诉', 'Inspection / Appeal'),
              key: 'state',
              render: (_: unknown, row: DockerVolumeDLPCase) => (
                <>
                  {row.event.mode} · {row.event.status}
                  <br />
                  {row.appeal?.status ?? '—'}
                </>
              ),
            },
            {
              title: text('网络限制来源', 'Network restriction source'),
              key: 'hold',
              render: (_: unknown, row: DockerVolumeDLPCase) =>
                row.event.networkHoldId ? (
                  <Button type="link" onClick={() => setSelected(row.event.networkHoldId!)}>
                    {row.event.networkHoldId}
                  </Button>
                ) : (
                  '—'
                ),
            },
          ]}
        />
        {selected ? (
          <ReadFeedback query={detail} label={text('事件详情', 'Event details')} />
        ) : null}
        {item ? (
          <CaseActions
            key={`${item.event.id}:${item.appeal?.revision ?? 0}`}
            item={item}
            projectId={projectId}
            viewer={viewer}
            review={review}
            link={link}
            refresh={refresh}
            onRetry={onRetry}
            busy={busy}
          />
        ) : null}
      </Space>
    </Drawer>
  )
}

function CaseActions({
  item,
  projectId,
  viewer,
  review,
  link,
  refresh,
  onRetry,
  busy,
}: {
  item: DockerVolumeDLPCase
  projectId: string
  viewer: string
  review: boolean
  link: boolean
  refresh: () => Promise<unknown>
  onRetry?: (id: string, action: 'acknowledge' | 'approved_appeal') => void
  busy?: boolean
}) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const [reason, setReason] = useState<'false_positive' | 'business_need'>('false_positive')
  const [resolution, setResolution] = useState<'approved' | 'rejected'>('rejected')
  const [reviewReason, setReviewReason] =
    useState<DockerVolumeDLPAppealReviewInput['reasonCode']>('policy_required')
  const { event, appeal, linkage } = item
  const own = event.actorId === viewer
  const denied = event.action === 'transfer' && !event.allowed && event.status === 'matched'
  const submit = useOperationMutation(
    {
      mutationFn: () => submitDLPAppeal(projectId, event.id, { reasonCode: reason }),
      onSuccess: () => void refresh(),
    },
    {
      label: ['提交内容申诉', 'Submit content appeal'],
      target: () => event.id,
      receipt: () => ({
        message: text('申诉已登记，等待独立审查', 'Appeal recorded; awaiting independent review'),
      }),
    },
  )
  const decide = useOperationMutation(
    {
      mutationFn: () =>
        reviewDLPAppeal(projectId, event.id, {
          expectedRevision: appeal?.revision ?? 0,
          resolution,
          reasonCode: reviewReason,
        }),
      onSuccess: () => void refresh(),
      onError: () => void refresh(),
    },
    {
      label: ['审查内容申诉', 'Review content appeal'],
      target: () => event.id,
      receipt: (value) => ({
        message: text(
          '审查已保存；网络状态以回执为准',
          'Review saved; network state follows receipts',
        ),
        evidence: `${value.status} · r${value.revision}`,
      }),
    },
  )
  const needsLink = resolution === 'approved' && Boolean(event.networkHoldId)
  const blocked = submit.blocked || decide.blocked || busy
  return (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      <Typography.Text strong>
        {event.id} · {event.mode} · v{event.policyVersion}
      </Typography.Text>
      <Typography.Text>
        {text('规则', 'Rules')}: {event.matchedRuleIds.join(', ') || '—'} ·{' '}
        {appeal?.status ?? text('未申诉', 'No appeal')}
      </Typography.Text>
      {submit.feedback}
      {decide.feedback}
      {own && denied && !appeal ? (
        <Space wrap>
          <Select
            value={reason}
            disabled={blocked}
            onChange={setReason}
            options={[
              { value: 'false_positive', label: text('疑似误报', 'Possible false positive') },
              { value: 'business_need', label: text('业务需要', 'Business need') },
            ]}
          />
          <Button disabled={blocked} loading={submit.isPending} onClick={() => submit.mutate()}>
            {text('提交申诉', 'Submit appeal')}
          </Button>
        </Space>
      ) : null}
      {review && !own && appeal?.status === 'pending' ? (
        <Space wrap>
          <Select
            value={resolution}
            disabled={blocked}
            onChange={(value) => {
              setResolution(value)
              setReviewReason(value === 'approved' ? 'confirmed_false_positive' : 'policy_required')
            }}
            options={[
              { value: 'rejected', label: text('驳回', 'Reject') },
              { value: 'approved', label: text('批准一次重试', 'Approve one retry') },
            ]}
          />
          <Select
            value={reviewReason}
            disabled={blocked}
            onChange={setReviewReason}
            style={{ minWidth: 180 }}
            options={
              resolution === 'approved'
                ? [
                    {
                      value: 'confirmed_false_positive',
                      label: text('已确认误报', 'Confirmed false positive'),
                    },
                    {
                      value: 'approved_business_exception',
                      label: text('批准业务例外', 'Approved business exception'),
                    },
                  ]
                : [
                    { value: 'policy_required', label: text('策略仍需执行', 'Policy required') },
                    {
                      value: 'insufficient_context',
                      label: text('依据不足', 'Insufficient context'),
                    },
                  ]
            }
          />
          <Popconfirm
            title={text('保存审查决定？', 'Save review decision?')}
            description={
              needsLink
                ? text(
                    '仅当该网络限制的所有关联命中均获批准时才解除；其他限制保留，需要重新认证。',
                    'The network hold is released only after all linked findings are approved. Other restrictions remain; fresh authentication is required.',
                  )
                : text('决定只适用于该事件。', 'The decision applies only to this event.')
            }
            onConfirm={() => decide.mutate()}
            disabled={blocked || (needsLink && !link)}
          >
            <Button disabled={blocked || (needsLink && !link)} loading={decide.isPending}>
              {text('确认审查', 'Confirm review')}
            </Button>
          </Popconfirm>
          {needsLink && !link ? (
            <Typography.Text type="secondary">
              {text(
                '解除网络限制还需网络联动管理权限',
                'Releasing the network hold also requires linkage management permission',
              )}
            </Typography.Text>
          ) : null}
        </Space>
      ) : null}
      {own && denied && appeal?.status === 'approved' && onRetry ? (
        <Popconfirm
          title={text(
            '为当前选择的原文件领取并使用一次性授权？',
            'Claim and use one authorization for the currently selected original file?',
          )}
          description={text(
            '内容、位置或策略变化会重新阻断；授权不能重复领取。',
            'Changed content, location or policy is blocked again. Authorization cannot be reissued.',
          )}
          onConfirm={() => onRetry(event.id, 'approved_appeal')}
          disabled={blocked}
        >
          <Button disabled={blocked}>
            {text('对当前文件使用批准', 'Use approval for current file')}
          </Button>
        </Popconfirm>
      ) : null}
      {linkage ? (
        <>
          <StatusTag
            value={linkage.active ? 'warning' : 'success'}
            label={
              linkage.active
                ? text('网络限制有效', 'Network hold active')
                : text('本来源限制已解除', 'This source hold released')
            }
          />
          <Typography.Paragraph type="secondary">
            {text('来源事件', 'Source event')}: {linkage.sourceEventId}.{' '}
            {text(
              '配置待下发不等于已断网；NAS 仅可信 Accounting 停止证据标为 verified_stopped；共享代理无法归属用户的流量未覆盖。解除不会恢复旧会话。',
              'Pending configuration does not prove disconnection. NAS is verified_stopped only with trusted Accounting stop evidence. Unattributed shared proxy traffic is outside this scope. Release does not revive old sessions.',
            )}
          </Typography.Paragraph>
          <AdminTable
            rowKey={(row) => `${row.kind}:${row.runtimeId}:${row.sessionId}`}
            dataSource={linkage.targets}
            pagination={{ pageSize: 6, showSizeChanger: false }}
            columns={[
              { title: text('通道', 'Channel'), dataIndex: 'kind' },
              {
                title: text('运行时 / 会话', 'Runtime / Session'),
                key: 'target',
                render: (_: unknown, row: DockerVolumeDLPNetworkTarget) => (
                  <>
                    {row.runtimeId ?? '—'}
                    <br />
                    {row.sessionId ?? ''}
                  </>
                ),
              },
              { title: text('回执', 'Receipt'), dataIndex: 'status' },
              {
                title: text('版本（基线 / 观察）', 'Version (baseline / observed)'),
                key: 'version',
                render: (_: unknown, row: DockerVolumeDLPNetworkTarget) =>
                  `${row.baseVersion} / ${row.observedVersion}`,
              },
            ]}
          />
        </>
      ) : null}
    </Space>
  )
}
