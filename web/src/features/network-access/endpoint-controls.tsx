import { useEffect, useState } from 'react'
import { Alert, Button, Form, Popconfirm, Space, Switch, Typography } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type {
  EndpointChannelControls,
  EndpointChannelPolicy,
  EndpointChannelReceipt,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAuthStore } from '@/stores/auth-store'
import { useI18n } from '@/i18n'
import { endpointControlKeys } from './keys'
import { readableQueryData } from '@/utils/query-feedback'
import {
  endpointControlQuery,
  publishEndpointControls,
  saveEndpointControls,
} from './endpoint-controls-api'

export function EndpointChannelControlsPanel({
  deviceId,
  platform,
}: {
  deviceId: string
  platform: string
}) {
  const viewer = useAuthStore((s) => s.user?.userId ?? '')
  const permissions = usePermissionSnapshot()
  const ready = permissions.isSuccess && !permissions.isFetching && permissions.isEnabled !== false
  const snapshot = ready ? readableQueryData(permissions)?.data : undefined
  const access = ['view', 'update', 'publish'].map((x) =>
    hasPermission(snapshot, `network_access.endpoint_controls.${x}`),
  )
  return (
    <ChannelPolicy
      key={`${deviceId}:${viewer}:${access.join()}`}
      deviceId={deviceId}
      viewer={viewer}
      platform={platform}
      access={access}
    />
  )
}
function ChannelPolicy({
  deviceId,
  viewer,
  platform,
  access,
}: {
  deviceId: string
  viewer: string
  platform: string
  access: boolean[]
}) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const label = (value: string) => channelLabels[value]?.[localeCode === 'zh_CN' ? 0 : 1] ?? value
  const [read, edit, publish] = access
  const scope = access.join()
  const query = useQuery(endpointControlQuery(deviceId, viewer, scope, read))
  const data = read ? readableQueryData(query) : undefined
  const client = useQueryClient()
  const [form] = Form.useForm<EndpointChannelControls>()
  const [revision, setRevision] = useState<number>()
  const [dirty, setDirty] = useState(false)
  useEffect(() => {
    if (data && !dirty) {
      form.setFieldsValue(data.policy.draft)
      setRevision(data.policy.revision)
    }
  }, [data, dirty, form])
  const accept = (policy: EndpointChannelPolicy) => {
    client.setQueryData(endpointControlKeys.detail(deviceId, viewer, scope), {
      ...data,
      policy,
      fresh: false,
    })
    form.setFieldsValue(policy.draft)
    setRevision(policy.revision)
    setDirty(false)
    void client.invalidateQueries({ queryKey: endpointControlKeys.project(deviceId) })
  }
  const save = useOperationMutation(
    {
      mutationFn: (draft: EndpointChannelControls) =>
        saveEndpointControls(deviceId, { expectedRevision: revision ?? 0, draft }),
      onSuccess: accept,
      onError: () => void query.refetch(),
    },
    {
      label: ['保存端点通道草稿', 'Save endpoint channel draft'],
      target: () => deviceId,
      receipt: (p) => ({
        message: text('草稿已保存，尚未生效', 'Draft saved; not active'),
        evidence: `r${p.revision}`,
      }),
    },
  )
  const release = useOperationMutation(
    {
      mutationFn: () => publishEndpointControls(deviceId, revision ?? 0),
      onSuccess: accept,
      onError: () => void query.refetch(),
    },
    {
      label: ['发布端点通道策略', 'Publish endpoint channel policy'],
      target: () => deviceId,
      receipt: (p) => ({
        message: text(
          '版本已发布，等待受信任服务领取和回执',
          'Version published; waiting for the trusted service and receipt',
        ),
        evidence: `v${p.publishedVersion}`,
      }),
    },
  )
  const blocked =
    save.blocked ||
    release.blocked ||
    query.isError ||
    query.isFetching ||
    !data ||
    revision !== data.policy.revision ||
    platform !== 'windows'
  return (
    <Space orientation="vertical" size={16} style={{ width: '100%' }}>
      <Alert
        type="warning"
        showIcon
        title={text(
          'Windows 原生通道控制，按明确范围阻断',
          'Windows native channel controls with explicit scope',
        )}
        description={text(
          'USB：当前可安全锁定的 USB 卷，忙碌或未知卷不算成功；打印：本机队列新任务，不撤回已有任务/句柄；剪贴板：登记用户当前控制台会话的 Win32 访问，不含历史、云同步或其他会话。不是按内容选择性放行的全机 DLP。',
          'USB: current safely lockable USB volumes; busy or unknown volumes are not successful. Print: new jobs on local queues, excluding existing jobs/handles. Clipboard: Win32 access in the enrolled user’s current console, excluding history, cloud sync and other sessions. This is not machine-wide content-selective DLP.',
        )}
      />
      {platform !== 'windows' ? (
        <Alert
          type="info"
          title={text('该平台原生通道尚不支持', 'Native channels are unsupported on this platform')}
        />
      ) : null}
      {!read && (edit || publish) ? (
        <Alert
          type="info"
          showIcon
          title={text(
            '此界面还需查看通道策略权限',
            'Viewing channel policies is also required here',
          )}
          description={text(
            '保存与发布权限分别授权。交互界面还需查看权限来核对已保存草稿和版本；请联系管理员补齐查看权限。',
            'Save and publish permissions are independent. This interface also requires view permission to verify the saved draft and revision; ask an administrator to grant it.',
          )}
        />
      ) : null}
      <ReadFeedback
        canRead={read}
        query={query}
        label={text('通道策略与回执', 'Channel policy and receipt')}
      />
      {save.feedback}
      {release.feedback}
      {data && revision !== undefined && revision !== data.policy.revision ? (
        <Alert
          type="warning"
          showIcon
          title={text('策略已被其他管理员更新', 'Policy updated by another administrator')}
          description={text(
            '本地草稿尚未提交。请重新读取最新版本；重新读取会丢弃本地编辑，再按最新版本重新修改。',
            'Your local draft has not been submitted. Reload the latest version, which discards local edits, then edit that version.',
          )}
        />
      ) : null}
      {data ? (
        <>
          <Typography.Text>
            {text('已发布版本', 'Published version')}:{' '}
            {data.policy.publishedVersion || text('未配置', 'Unconfigured')} ·{' '}
            {text('最新单实例回执时间', 'Latest individual receipt')}: {data.receivedAt ?? '—'} ·{' '}
            {text('已领取策略的运行时 / 待确认', 'Runtimes served / pending')}: {data.runtimeCount}{' '}
            / {data.pendingRuntimeCount}
          </Typography.Text>
          <StatusTag
            value={data.fresh ? 'info' : 'warning'}
            label={
              data.fresh
                ? text('全部已知运行时回执新鲜', 'All known runtime receipts are fresh')
                : text(
                    '仍有未确认或缺少新鲜回执的运行时',
                    'Runtime confirmation is missing or stale',
                  )
            }
          />
          <Form
            form={form}
            layout="vertical"
            disabled={!edit || blocked}
            onValuesChange={() => setDirty(true)}
            onFinish={(v) =>
              save.mutate({
                usb: Boolean(v.usb),
                print: Boolean(v.print),
                clipboard: Boolean(v.clipboard),
              })
            }
          >
            <Form.Item
              name="usb"
              valuePropName="checked"
              label={text('限制当前 USB 卷访问', 'Restrict current USB volume access')}
            >
              <Switch />
            </Form.Item>
            <Form.Item
              name="print"
              valuePropName="checked"
              label={text('拒绝本机队列的新打印任务', 'Deny new jobs on local print queues')}
            >
              <Switch />
            </Form.Item>
            <Form.Item
              name="clipboard"
              valuePropName="checked"
              label={text(
                '限制当前登记用户会话的剪贴板访问',
                'Restrict enrolled console clipboard access',
              )}
            >
              <Switch />
            </Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              disabled={!edit || blocked}
              loading={save.isPending}
            >
              {text('保存草稿', 'Save draft')}
            </Button>
          </Form>
          <Space wrap>
            <Popconfirm
              title={text(
                '发布已保存的端点控制版本？',
                'Publish the saved endpoint control version?',
              )}
              description={text(
                '可能中断该设备的 USB、打印和剪贴板使用，需独立发布权限及全主体范围。关闭限制也必须发布更高版本，恢复冲突时保留原限制；VPN 断开或控制面失联不会自动解除。',
                'May interrupt USB, printing and clipboard use. Requires independent publish permission and all-subject scope. Releasing controls requires a higher published version; restoration conflicts retain restrictions. VPN disconnect or control loss does not release them.',
              )}
              onConfirm={() => release.mutate()}
              disabled={!publish || blocked || dirty || !revision}
            >
              <Button
                disabled={!publish || blocked || dirty || !revision}
                loading={release.isPending}
              >
                {text('发布已保存版本', 'Publish saved version')}
              </Button>
            </Popconfirm>
            <Button
              disabled={save.isPending || release.isPending}
              onClick={() => {
                setDirty(false)
                void query.refetch()
              }}
            >
              {text('重新读取（丢弃本地编辑）', 'Reload (discard local edits)')}
            </Button>
          </Space>
          <Typography.Text type="secondary">
            {text(
              '下表仅展示最新单实例回执；不能代表其他运行时已执行或解除。',
              'The table shows only the latest individual receipt; other runtimes may still be pending.',
            )}
          </Typography.Text>
          <AdminTable
            rowKey="name"
            dataSource={data.receipt?.channels ?? []}
            pagination={false}
            columns={[
              {
                title: text('通道', 'Channel'),
                dataIndex: 'name',
                render: (v: string) => label(v),
              },
              {
                title: text('有效范围', 'Scope'),
                dataIndex: 'scope',
                render: (v: string) => label(v),
              },
              {
                title: text('执行状态', 'Control state'),
                dataIndex: 'state',
                render: (v: string) => (
                  <StatusTag
                    value={
                      v === 'failed' || v === 'conflict'
                        ? 'error'
                        : v === 'partial' || v === 'unsupported'
                          ? 'warning'
                          : 'info'
                    }
                    label={label(v)}
                  />
                ),
              },
              {
                title: text('控制数 / 当前目标数', 'Controlled / current targets'),
                key: 'count',
                render: (_: unknown, c: EndpointChannelReceipt) => `${c.controlled} / ${c.targets}`,
              },
              {
                title: text('原因', 'Reason'),
                dataIndex: 'reasonCode',
                render: (v: string) => <span title={v}>{label(v)}</span>,
              },
            ]}
          />
          <Typography.Paragraph type="secondary">
            {text(
              '新鲜回执只证明当前受认证服务报告了状态；不等于数据面或现场验收。服务退出、热插拔及并发管理员 ACL 修改存在明确边界。未解除或恢复未确认时，先保留服务、策略和恢复日志，再核对执行状态。',
              'A fresh receipt establishes only the authenticated service report, not data-plane acceptance. Service exit, hotplug and concurrent administrator ACL edits have explicit limits. Keep the service, policy and restoration journal until release is confirmed.',
            )}
          </Typography.Paragraph>
        </>
      ) : null}
    </Space>
  )
}

const channelLabels: Record<string, [string, string]> = {
  usb: ['USB 存储卷', 'USB storage volumes'],
  print: ['打印', 'Printing'],
  clipboard: ['剪贴板', 'Clipboard'],
  current_usb_volumes: ['当前 USB 卷', 'Current USB volumes'],
  local_new_print_jobs: ['本机队列新任务', 'New jobs on local queues'],
  enrolled_console_win32: ['登记用户控制台 Win32 访问', 'Enrolled console Win32 access'],
  off: ['限制已解除', 'Restrictions released'],
  unconfigured: ['尚未发布策略', 'No policy published'],
  not_present: ['当前无适用目标', 'No current applicable targets'],
  controlled: ['已施加范围内控制', 'Scoped controls applied'],
  partial: ['部分目标未受控', 'Some targets uncontrolled'],
  failed: ['执行失败', 'Execution failed'],
  unsupported: ['不支持', 'Unsupported'],
  conflict: ['恢复冲突，需处理', 'Restoration conflict'],
  disabled: ['该通道限制已关闭', 'Channel restriction disabled'],
  native_scope_only: ['仅适用于声明的原生范围', 'Applies only to the declared native scope'],
  not_published: ['管理员尚未发布策略', 'No administrator policy published'],
  volume_close_failed: ['USB 卷锁释放未确认', 'USB volume unlock unconfirmed'],
  volume_inventory_failed: ['无法枚举存储卷', 'Volume inventory unavailable'],
  native_deadline: ['本轮协调超时', 'Reconciliation deadline reached'],
  volume_readback_unknown: ['现有卷锁身份读回不明', 'Locked volume identity unconfirmed'],
  volume_identity_unknown: ['存储卷身份无法确认', 'Volume identity unconfirmed'],
  volume_access_denied: ['无权锁定 USB 卷', 'USB volume lock access denied'],
  volume_busy_or_unsupported: ['USB 卷忙碌或不支持锁定', 'USB volume busy or lock unsupported'],
  no_current_usb_volumes: ['当前未发现 USB 卷', 'No current USB volumes'],
  printer_inventory_failed: ['无法读取本机打印队列', 'Local print queue inventory unavailable'],
  printer_restore_pending: ['打印权限恢复尚未完成', 'Printer permissions need restoration'],
  printer_acl_unconfirmed: ['打印权限修改未确认', 'Printer permission change unconfirmed'],
  no_local_print_queues: ['当前无本机打印队列', 'No local print queues'],
  clipboard_cleanup_failed: ['剪贴板守护进程退出未确认', 'Clipboard guard exit unconfirmed'],
  clipboard_guard_unavailable: [
    '无法建立登记会话的剪贴板控制',
    'Enrolled-session clipboard guard unavailable',
  ],
  waiting_for_control: ['等待控制面', 'Waiting for control plane'],
  control_or_policy_unavailable: [
    '控制面或策略不可用，保留原限制',
    'Control or policy unavailable; prior controls retained',
  ],
  receipt_unconfirmed: ['回执提交未确认', 'Receipt submission unconfirmed'],
}
