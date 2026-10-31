import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Popconfirm, Space, Typography } from 'antd'
import { getVPNRevocation, releaseVPNRevocation, revokeVPNSession, vpnKeys } from './vpn-api'

export function VPNSessionRevocation({
  sessionId,
  canView,
  canRevoke,
  canRelease,
}: {
  sessionId: string
  canView: boolean
  canRevoke: boolean
  canRelease: boolean
}) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const client = useQueryClient()
  const query = useQuery({
    queryKey: vpnKeys.revocation(sessionId),
    queryFn: () => getVPNRevocation(sessionId),
    enabled: canView,
    retry: false,
    refetchInterval: (q) =>
      q.state.error ||
      !q.state.data?.active ||
      !q.state.data.executions.some((e) => e.applyStatus === 'pending')
        ? false
        : 5_000,
  })
  const receipt = query.isEnabled === false ? undefined : readableQueryData(query)
  const change = useOperationMutation(
    {
      mutationFn: (action: { kind: 'revoke' } | { kind: 'release'; expectedEpoch: number }) =>
        action.kind === 'revoke'
          ? revokeVPNSession(sessionId)
          : releaseVPNRevocation(sessionId, action.expectedEpoch),
      onSuccess: (result) => {
        client.setQueryData(vpnKeys.revocation(sessionId), result)
        void client.invalidateQueries({ queryKey: vpnKeys.all })
      },
    },
    {
      label: ['更新 VPN 会话限制', 'Update VPN session restriction'],
      target: () => sessionId,
      receipt: (result) => ({
        message: result.active
          ? text(
              '重连限制已持久保存，正在等待执行回执。',
              'Reconnect restriction persisted; waiting for execution receipts.',
            )
          : text(
              '限制已解除；旧会话仍已撤销，必须重新授权连接。',
              'Restriction released; revoked sessions remain revoked and need fresh authorization.',
            ),
        evidence: `${text('代次', 'Epoch')} ${result.epoch}`,
      }),
    },
  )
  const blocked = change.blocked || query.isPending || query.isError || queryAccessDenied(query)
  if (!canView)
    return (
      <Alert
        type="info"
        title={text('无权限读取 VPN 撤销状态', 'Permission required to read VPN revocation state')}
      />
    )
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Typography.Text>{sessionId}</Typography.Text>
      {change.feedback}
      <ReadFeedback
        canRead={query.isEnabled}
        query={query}
        label={text('VPN 撤销状态', 'VPN revocation state')}
      />
      <Alert
        showIcon
        type={receipt?.active ? 'warning' : 'info'}
        title={
          receipt?.active
            ? text('VPN 重连限制已记录', 'VPN reconnect restriction recorded')
            : receipt
              ? text(
                  '限制已解除，需要重新授权',
                  'Restriction released; fresh authorization required',
                )
              : receipt === null
                ? text('未记录 VPN 重连限制', 'No VPN reconnect restriction recorded')
                : text('VPN 撤销状态未知', 'VPN revocation state unknown')
        }
        description={text(
          '撤销范围包含该用户、设备、站点的所有活动 VPN 会话。回执仅表示配置执行读回，不代表端到端流量验收。',
          'Revocation covers every active VPN session for this subject, device and site. Receipts report configuration readback, not end-to-end traffic verification.',
        )}
        action={
          <Button loading={query.isFetching} onClick={() => void query.refetch()}>
            {text('刷新', 'Refresh')}
          </Button>
        }
      />
      {receipt && (
        <Typography.Text type="secondary">
          {text('限制代次', 'Restriction epoch')} {receipt.epoch} ·{' '}
          {formatDateTime(receipt.createdAt)} · {receipt.reasonCode}
        </Typography.Text>
      )}
      {(canRevoke || canRelease) && (
        <Space>
          <Popconfirm
            title={text(
              '撤销活动 VPN 会话并限制重连？',
              'Revoke active VPN sessions and restrict reconnect?',
            )}
            description={text(
              '其他设备和站点不受影响。解除限制后仍须重新授权。',
              'Other devices and sites are unaffected. Releasing this restriction still requires fresh authorization.',
            )}
            disabled={!canRevoke || blocked || Boolean(receipt?.active)}
            onConfirm={() => change.mutate({ kind: 'revoke' })}
          >
            <Button danger disabled={!canRevoke || blocked || Boolean(receipt?.active)}>
              {text('撤销并限制重连', 'Revoke and restrict reconnect')}
            </Button>
          </Popconfirm>
          {receipt?.active && canRelease && (
            <Popconfirm
              title={text('解除此 VPN 重连限制？', 'Release this VPN reconnect restriction?')}
              description={text(
                '不会恢复旧会话或旧租约。',
                'Old sessions and leases will not be restored.',
              )}
              disabled={blocked}
              onConfirm={() => change.mutate({ kind: 'release', expectedEpoch: receipt.epoch })}
            >
              <Button disabled={blocked}>{text('解除限制', 'Release restriction')}</Button>
            </Popconfirm>
          )}
        </Space>
      )}
      {receipt && (
        <AdminTable
          rowKey="runtimeId"
          dataSource={receipt.executions}
          pagination={false}
          columns={[
            { title: text('执行点', 'Runtime'), dataIndex: 'runtimeId' },
            { title: text('配置版本', 'Configuration version'), dataIndex: 'configurationVersion' },
            {
              title: text('配置回执', 'Configuration receipt'),
              dataIndex: 'applyStatus',
              render: (value: string) => <StatusTag value={value} />,
            },
            { title: text('原因', 'Reason'), dataIndex: 'reasonCode' },
            {
              title: text('回执时间', 'Receipt time'),
              dataIndex: 'appliedAt',
              render: (value?: string) => (value ? formatDateTime(value) : '—'),
            },
          ]}
        />
      )}
    </Space>
  )
}
