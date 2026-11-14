import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  App,
  Button,
  Descriptions,
  Drawer,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Space,
  Tabs,
} from 'antd'
import type { TableColumnsType } from 'antd'
import { EyeOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type {
  NetworkEndpointResponseEvent,
  NetworkEndpointMaintenanceInput,
  NetworkEndpointMaintenance,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import {
  ManagementIconButton,
  ManagementQueryPanel,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { EndpointResponsePolicies } from './policies'
import { endpointResponseMutations } from './mutations'
import { endpointResponseQueries } from './queries'
import type { EndpointResponseFilters } from './api'
import { EndpointResponseTag, responseLabel } from './labels'
import './styles.css'

type PaneProps = { can: (key: string) => boolean; filters: EndpointResponseFilters }

export function EndpointResponseEvents({ can, filters }: PaneProps) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const client = useQueryClient()
  const { message } = App.useApp()
  const events = useQuery(
    endpointResponseQueries.events(filters, can('network_access.endpoint_response_events.view')),
  )
  const [eventId, setEventId] = useState('')
  const [restoring, setRestoring] = useState(false)
  const [form] = Form.useForm<{ reason: string }>()
  const event = useQuery(endpointResponseQueries.event(eventId))
  const current = readableQueryData(event)
  const presence = useQuery(
    endpointResponseQueries.presence(
      current?.deviceId ?? '',
      can('network_access.endpoint_devices.view'),
    ),
  )
  const restore = useOperationMutation(endpointResponseMutations.restore(client), {
    label: ['受控恢复', 'Controlled recovery'],
    target: ({ eventId }) => eventId,
  })
  const displayedPresence = readableQueryData({
    data: presence.isError ? current?.presence : (presence.data ?? current?.presence),
    error: presence.error,
  })
  const columns: TableColumnsType<NetworkEndpointResponseEvent> = [
    { title: text('终端', 'Endpoint'), dataIndex: 'deviceId' },
    { title: text('会话', 'Session'), dataIndex: 'sessionId' },
    {
      title: text('检测信号', 'Signal'),
      dataIndex: 'signal',
      render: (signal) =>
        signal === 'service_unavailable'
          ? text('保护服务失联', 'Protection service unavailable')
          : text('UI 失联', 'UI unavailable'),
    },
    {
      title: text('处置状态', 'Response state'),
      dataIndex: 'status',
      render: (value) => <EndpointResponseTag value={value} />,
    },
    {
      title: text('当前步骤', 'Current action'),
      key: 'stage',
      render: (_, row) => `${row.stage + 1} · ${responseLabel(row.action, localeCode)}`,
    },
    {
      title: text('通知', 'Notification'),
      dataIndex: 'notificationState',
      render: (value) => <EndpointResponseTag value={value} />,
    },
    { title: text('触发时间', 'Triggered'), dataIndex: 'triggeredAt', render: formatDateTime },
    {
      title: text('操作', 'Action'),
      key: 'actions',
      width: 64,
      render: (_, row) => (
        <ManagementIconButton
          icon={<EyeOutlined />}
          aria-label={text('查看会话处置', 'View session response')}
          tooltip={text('查看', 'View')}
          onClick={() => {
            restore.reset()
            setEventId(row.id)
          }}
        />
      ),
    },
  ]
  return (
    <>
      <Alert
        className="endpoint-response-form-alert"
        type="info"
        showIcon
        title={text(
          '命令入队或 NAS 接收确认后，仍需核实实际网络状态',
          'Queued commands and NAS acknowledgments still require network verification',
        )}
        description={text(
          '执行未知的记录保留限制；恢复会重新评估当前授权，并要求旧会话重新认证。',
          'Unverified execution retains the restriction. Recovery reevaluates current authorization and requires old sessions to authenticate again.',
        )}
      />
      {!eventId && !restoring && restore.feedback}
      <AdminTable
        rowKey="id"
        columns={columns}
        dataSource={events.data ?? []}
        loading={events.isLoading}
        refreshing={events.isFetching}
        error={events.error}
        hasReadData={events.data !== undefined}
        onRetry={() => void events.refetch()}
        viewportScroll
        toolbar={
          <ManagementTableToolbar>
            <span>
              {text(
                '最近 200 条；每 10 秒读取 Core 状态',
                'Latest 200 records; Core state refreshes every 10 seconds',
              )}
            </span>
            <ManagementIconButton
              icon={<ReloadOutlined />}
              aria-label={text('刷新事件', 'Refresh events')}
              tooltip={text('刷新', 'Refresh')}
              loading={events.isFetching}
              onClick={() => void events.refetch()}
            />
          </ManagementTableToolbar>
        }
      />
      <Drawer
        open={Boolean(eventId)}
        title={text('会话联动详情', 'Session response details')}
        size="large"
        destroyOnHidden
        onClose={() => {
          if (!restore.isPending) setEventId('')
        }}
      >
        {event.isLoading && <ManagementState compact kind="loading" />}
        <ReadFeedback query={event} label={text('会话联动详情', 'Session response details')} />
        {eventId && !restoring && restore.feedback}
        {current && (
          <>
            <Descriptions
              column={1}
              bordered
              size="small"
              items={[
                {
                  key: 'state',
                  label: text('处置状态', 'Response state'),
                  children: <EndpointResponseTag value={current.status} />,
                },
                { key: 'reason', label: text('原因', 'Reason'), children: current.reasonCode },
                {
                  key: 'policy',
                  label: text('策略 / 发布版本', 'Policy / published revision'),
                  children: `${current.policyId} / ${current.policyRevision}`,
                },
                {
                  key: 'device',
                  label: text('终端 / 用户', 'Endpoint / user'),
                  children: `${current.deviceId} / ${current.subjectId}`,
                },
                {
                  key: 'session',
                  label: text('会话 / NAS', 'Session / NAS'),
                  children: `${current.sessionId} / ${current.nasId}`,
                },
                {
                  key: 'service',
                  label: text('保护服务', 'Protection service'),
                  children: (
                    <>
                      <StatusTag value={displayedPresence?.serviceState ?? 'unknown'} /> ·{' '}
                      {formatDateTime(displayedPresence?.lastReceivedAt)}
                    </>
                  ),
                },
                {
                  key: 'ui',
                  label: text('界面进程', 'UI process'),
                  children: (
                    <>
                      <StatusTag value={displayedPresence?.uiState ?? 'unknown'} /> ·{' '}
                      {formatDateTime(displayedPresence?.uiLastObservedAt)}
                    </>
                  ),
                },
                {
                  key: 'nas',
                  label: text('NAS 会话观测', 'NAS session observation'),
                  children: (
                    <>
                      <StatusTag value={current.nasEvidence?.state ?? 'unknown'} /> ·{' '}
                      {formatDateTime(current.nasEvidence?.lastReceivedAt)}
                    </>
                  ),
                },
                {
                  key: 'verification',
                  label: text('网络验证', 'Network verification'),
                  children: (
                    <EndpointResponseTag
                      value={current.nasEvidence?.verificationState ?? 'unknown'}
                    />
                  ),
                },
                {
                  key: 'notification',
                  label: text('通知结果', 'Notification outcome'),
                  children: (
                    <>
                      <EndpointResponseTag value={current.notificationState} />{' '}
                      {current.notificationReason}
                    </>
                  ),
                },
                {
                  key: 'commands',
                  label: text('命令 ID', 'Command IDs'),
                  children: current.commandIds.join(' · ') || '—',
                },
                {
                  key: 'epoch',
                  label: text('限制代次', 'Restriction epoch'),
                  children: current.restrictionEpoch,
                },
                {
                  key: 'updated',
                  label: text('最近变化', 'Last changed'),
                  children: formatDateTime(current.updatedAt),
                },
              ]}
            />
            <ReadFeedback
              query={{ ...presence, data: displayedPresence }}
              label={text('终端心跳', 'Endpoint presence')}
            />
            {can('network_access.endpoint_response_events.restore') && (
              <Button
                className="endpoint-response-detail"
                disabled={current.status === 'restored' || restore.blocked}
                onClick={() => {
                  if (restore.blocked) return
                  restore.reset()
                  form.resetFields()
                  setRestoring(true)
                }}
              >
                {text('申请受控恢复', 'Request controlled recovery')}
              </Button>
            )}
          </>
        )}
      </Drawer>
      <Modal
        open={restoring}
        title={text('受控恢复', 'Controlled recovery')}
        destroyOnHidden
        mask={{ closable: false }}
        confirmLoading={restore.isPending}
        okButtonProps={{ disabled: restore.blocked }}
        onCancel={() => {
          if (!restore.isPending) setRestoring(false)
        }}
        onOk={() => form.submit()}
        okText={text('重新评估并恢复', 'Reevaluate and recover')}
        cancelText={text('取消', 'Cancel')}
      >
        <Alert
          className="endpoint-response-form-alert"
          type="warning"
          showIcon
          title={text(
            'Core 会重新核实连续健康、当前权限和执行结果',
            'Core rechecks continuous health, current permissions and execution outcome',
          )}
          description={text(
            '本操作只解除这条事件的限制；其他限制保留，旧会话不会直接提权。',
            'This clears only this event’s restriction. Other restrictions remain and old sessions are not elevated.',
          )}
        />
        <Form
          form={form}
          layout="vertical"
          onFinish={async ({ reason }) => {
            if (!current || restore.blocked) return
            try {
              await restore.mutateAsync({
                eventId: current.id,
                input: { expectedRevision: current.revision, reason: reason.trim() },
              })
              setRestoring(false)
              void message.success(
                text(
                  '已解除本条限制，请重新认证',
                  'This restriction is cleared. Authenticate again.',
                ),
              )
            } catch {
              /* Keep the dialog open with the server reason. */
            }
          }}
        >
          <Form.Item
            name="reason"
            label={text('恢复原因', 'Recovery reason')}
            rules={[{ required: true, whitespace: true, max: 500 }]}
          >
            <Input.TextArea maxLength={500} autoSize={{ minRows: 3, maxRows: 5 }} />
          </Form.Item>
          {restore.feedback}
        </Form>
      </Modal>
    </>
  )
}

export function EndpointMaintenance({ can, filters }: PaneProps) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const client = useQueryClient()
  const windows = useQuery(
    endpointResponseQueries.maintenance(
      filters,
      can('network_access.endpoint_response_events.maintenance'),
    ),
  )
  const create = useOperationMutation(endpointResponseMutations.createMaintenance(client), {
    label: ['创建维护例外', 'Create maintenance exception'],
    target: ({ deviceId, siteId }) => `${deviceId} · ${siteId}`,
  })
  const end = useOperationMutation(endpointResponseMutations.endMaintenance(client), {
    label: ['结束维护例外', 'End maintenance exception'],
    target: (windowId) => windowId,
  })
  const pending = create.blocked || end.blocked
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<
    Omit<NetworkEndpointMaintenanceInput, 'expiresAt'> & { minutes: number }
  >()
  return (
    <>
      <Alert
        className="endpoint-response-form-alert"
        type="info"
        showIcon
        title={text('维护例外有明确到期时间', 'Maintenance exceptions expire at a set time')}
        description={text(
          '维护例外暂停新处置，不解除已生效限制，也不放宽准入健康条件。',
          'Maintenance pauses new actions. It does not clear active restrictions or relax admission health requirements.',
        )}
      />
      {!open && create.feedback}
      {end.feedback}
      <AdminTable
        rowKey="id"
        loading={windows.isLoading}
        dataSource={windows.data ?? []}
        refreshing={windows.isFetching}
        error={windows.error}
        hasReadData={windows.data !== undefined}
        onRetry={() => void windows.refetch()}
        viewportScroll
        columns={[
          { title: text('终端', 'Endpoint'), dataIndex: 'deviceId' },
          { title: text('站点', 'Site'), dataIndex: 'siteId' },
          { title: text('原因', 'Reason'), dataIndex: 'reason' },
          { title: text('到期时间', 'Expires'), dataIndex: 'expiresAt', render: formatDateTime },
          {
            title: text('操作', 'Action'),
            key: 'end',
            render: (_: unknown, row: NetworkEndpointMaintenance) =>
              can('network_access.endpoint_response_events.maintenance') && (
                <Popconfirm
                  title={text('立即结束维护例外？', 'End this maintenance exception now?')}
                  onConfirm={() => {
                    if (!pending) end.mutate(row.id)
                  }}
                >
                  <Button size="small" disabled={pending}>
                    {text('结束', 'End')}
                  </Button>
                </Popconfirm>
              ),
          },
        ]}
        toolbar={
          <ManagementTableToolbar>
            <ManagementIconButton
              icon={<ReloadOutlined />}
              aria-label={text('刷新维护例外', 'Refresh maintenance exceptions')}
              tooltip={text('刷新', 'Refresh')}
              loading={windows.isFetching}
              onClick={() => void windows.refetch()}
            />
            {can('network_access.endpoint_response_events.maintenance') && (
              <Button
                size="small"
                type="primary"
                icon={<PlusOutlined />}
                disabled={pending}
                onClick={() => {
                  create.reset()
                  form.resetFields()
                  setOpen(true)
                }}
              >
                {text('新增维护例外', 'New maintenance exception')}
              </Button>
            )}
          </ManagementTableToolbar>
        }
      />
      <Modal
        open={open}
        title={text('新增维护例外', 'New maintenance exception')}
        destroyOnHidden
        mask={{ closable: false }}
        confirmLoading={create.isPending}
        okButtonProps={{ disabled: pending }}
        onCancel={() => {
          if (!create.isPending) setOpen(false)
        }}
        onOk={() => form.submit()}
        okText={text('创建', 'Create')}
        cancelText={text('取消', 'Cancel')}
      >
        <Form
          form={form}
          layout="vertical"
          initialValues={{ minutes: 30 }}
          onFinish={async ({ minutes, ...values }) => {
            if (pending) return
            try {
              await create.mutateAsync({
                ...values,
                expiresAt: new Date(Date.now() + minutes * 60_000).toISOString(),
              })
              setOpen(false)
            } catch {
              /* The form retains its values on failure. */
            }
          }}
        >
          <Form.Item
            name="deviceId"
            label={text('终端 ID', 'Endpoint ID')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="siteId"
            label={text('站点 ID', 'Site ID')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            name="minutes"
            label={text('持续时间（分钟，最长 4 小时）', 'Duration (minutes, up to 4 hours)')}
            rules={[{ required: true }]}
          >
            <InputNumber min={1} max={240} precision={0} />
          </Form.Item>
          <Form.Item
            name="reason"
            label={text('维护原因', 'Maintenance reason')}
            rules={[{ required: true, whitespace: true, max: 500 }]}
          >
            <Input.TextArea maxLength={500} />
          </Form.Item>
          {create.feedback}
        </Form>
      </Modal>
    </>
  )
}

export default function EndpointResponsePage() {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const permissions = usePermissionSnapshot()
  const can = (key: string) => hasPermission(permissions.data?.data, key)
  const [filters, setFilters] = useState<EndpointResponseFilters>({})
  const [form] = Form.useForm<EndpointResponseFilters>()
  const canPolicies = can('network_access.endpoint_response_policies.view')
  const canEvents = can('network_access.endpoint_response_events.view')
  const canMaintenance = can('network_access.endpoint_response_events.maintenance')
  if (permissions.isLoading) return <ManagementState kind="loading" />
  if (permissions.isError)
    return <ReadFeedback query={permissions} label={text('权限', 'Permissions')} />
  if (!canPolicies && !canEvents && !canMaintenance) return <ManagementState kind="no-permission" />
  return (
    <div className="soha-page">
      <h1 className="endpoint-response-sr-only">{text('终端联动', 'Endpoint response')}</h1>
      <ManagementQueryPanel
        form={form}
        onFinish={(values) => setFilters(values)}
        actions={
          <Space>
            <Button type="primary" htmlType="submit">
              {text('查询', 'Search')}
            </Button>
            <Button
              onClick={() => {
                form.resetFields()
                setFilters({})
              }}
            >
              {text('重置', 'Reset')}
            </Button>
          </Space>
        }
      >
        <Form.Item name="siteId" label={text('站点 ID', 'Site ID')}>
          <Input maxLength={128} />
        </Form.Item>
        <Form.Item name="deviceId" label={text('终端 ID', 'Endpoint ID')}>
          <Input maxLength={128} />
        </Form.Item>
      </ManagementQueryPanel>
      <Tabs
        items={[
          ...(canPolicies
            ? [
                {
                  key: 'policies',
                  label: text('联动规则', 'Response policies'),
                  children: <EndpointResponsePolicies can={can} filters={filters} />,
                },
              ]
            : []),
          ...(canEvents
            ? [
                {
                  key: 'events',
                  label: text('会话处置', 'Session responses'),
                  children: <EndpointResponseEvents can={can} filters={filters} />,
                },
              ]
            : []),
          ...(canMaintenance
            ? [
                {
                  key: 'maintenance',
                  label: text('维护例外', 'Maintenance exceptions'),
                  children: <EndpointMaintenance can={can} filters={filters} />,
                },
              ]
            : []),
        ]}
      />
    </div>
  )
}
