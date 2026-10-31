import { Alert, Modal, Space, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import type {
  ManagedContentRecord,
  DockerVolumeDLPNetworkTarget,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { readableQueryData } from '@/utils/query-feedback'
import { networkStateQuery } from './api'
import { useTransferContext } from './context'
export function NetworkStateDialog({
  record,
  close,
}: {
  record: ManagedContentRecord
  close: () => void
}) {
  const { viewer, scope, access, text } = useTransferContext()
  const canRead = access.events || (record.actorId === viewer && access.view)
  const query = useQuery({
    ...networkStateQuery(viewer, scope, record.id, canRead),
    refetchInterval: (q) => (q.state.status !== 'error' && q.state.data?.active ? 5000 : false),
  })
  const data = canRead ? readableQueryData(query) : undefined
  const labels: Record<string, string> = {
    pending: text('待执行/确认', 'Execution/confirmation pending'),
    configuration_applied: text(
      '配置已应用；尚非流量证明',
      'Configuration applied; traffic not verified',
    ),
    applied_unverified: text('命令已应用；会话未验证', 'Command applied; session unverified'),
    verified_stopped: text('可信会话证据确认终止', 'Trusted session evidence confirms termination'),
    enforcement_unknown: text('无法确认执行效果', 'Enforcement unknown'),
    unsupported: text('归属不明，不覆盖', 'Unattributed; outside coverage'),
    applied: text('执行点已报告应用', 'Execution point reports applied'),
    expired: text('命令已过期', 'Command expired'),
    failed: text('执行失败', 'Execution failed'),
    delivered: text('已送达执行点', 'Delivered to execution point'),
  }
  return (
    <Modal
      open
      title={text('内容事件与网络限制', 'Content event and network restriction')}
      onCancel={close}
      footer={null}
      width={880}
    >
      <Space orientation="vertical" size={12} style={{ width: '100%' }}>
        <Alert
          type="info"
          title={text(
            '执行回执与网络流量事实分别判断。',
            'Execution receipts and actual network traffic are separate evidence.',
          )}
          description={text(
            '同一限制的全部关联命中都获批准后才解除；其他来源限制继续生效。解除仅允许重新认证，不恢复旧会话。',
            'Every finding linked to this hold must be approved before release. Other restrictions remain active. Release permits fresh authorization and never restores old sessions.',
          )}
        />
        <ReadFeedback
          canRead={canRead}
          query={query}
          label={text('主体网络限制', 'Subject network restriction')}
        />
        {data ? (
          <>
            <StatusTag
              value={data.active ? 'warning' : 'info'}
              label={
                data.active
                  ? text('持久限制仍活动', 'Durable restriction active')
                  : text(
                      '本条限制已解除；其他限制仍独立',
                      'This hold released; other restrictions remain independent',
                    )
              }
            />
            <Typography.Text copyable>{data.id}</Typography.Text>
            <Typography.Text>
              {text('目的连接', 'Destination')}: {data.destinationId} ·{' '}
              {text('来源记录', 'Source record')}: {data.sourceRecordId}
            </Typography.Text>
            <AdminTable
              rowKey={(target: DockerVolumeDLPNetworkTarget) =>
                `${target.kind}:${target.runtimeId ?? ''}:${target.sessionId ?? ''}`
              }
              dataSource={data.targets}
              loading={query.isFetching}
              onRefresh={() => void query.refetch()}
              pagination={{ pageSize: 10, showSizeChanger: false }}
              columns={[
                {
                  title: text('执行范围', 'Execution target'),
                  key: 'target',
                  render: (_: unknown, t: DockerVolumeDLPNetworkTarget) => (
                    <Space orientation="vertical" size={2}>
                      <span>{t.kind}</span>
                      <Typography.Text>{t.runtimeId ?? t.sessionId ?? '—'}</Typography.Text>
                    </Space>
                  ),
                },
                {
                  title: text('版本', 'Version'),
                  key: 'version',
                  render: (_: unknown, t: DockerVolumeDLPNetworkTarget) =>
                    `${t.baseVersion} → ${t.observedVersion}`,
                },
                {
                  title: text('回执', 'Receipt'),
                  key: 'status',
                  render: (_: unknown, t: DockerVolumeDLPNetworkTarget) =>
                    labels[t.status] ?? t.status,
                },
              ]}
            />
          </>
        ) : null}
      </Space>
    </Modal>
  )
}
