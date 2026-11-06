import { Space, Typography } from 'antd'
import type { ManagedContentRecord } from '@opensoha/contracts/gen/ts/sohaapi'
import { DecisionEvidence } from '@/components/content-dlp-policy-tools'
import { StatusTag } from '@/components/status-tag'
import { ApiError } from '@/services/api-client'
import { base } from './api'
import { useTransferContext } from './context'
const labels: Record<string, [string, string]> = {
  prepared: ['意图已记录，等待确认', 'Intent recorded; confirmation pending'],
  sending: ['传输中，尚未确认', 'Sending; unconfirmed'],
  blocked: ['内容已阻断', 'Content blocked'],
  completed: ['本次操作完成', 'Operation completed'],
  failed: ['传输前置条件未满足', 'Transfer preconditions failed'],
  unknown: ['结果未确认，请核对目标', 'Outcome unknown; inspect the target'],
  clean: ['规则未命中', 'No configured rule matched'],
  matched: ['内容命中规则', 'Rule match'],
  unsupported: ['格式不支持完整检查', 'Format not fully inspectable'],
  encrypted: ['加密内容未检查', 'Encrypted content not inspected'],
  too_large: ['超出检查范围', 'Inspection size exceeded'],
  timeout: ['检查超时', 'Inspection timed out'],
  error: ['检查错误', 'Inspection error'],
  none: ['无申诉', 'No appeal'],
  pending: ['待审查', 'Review pending'],
  approved: [
    '申诉已批准；原传输仍受期限和版本约束',
    'Appeal approved; original transfer expiry and versions still apply',
  ],
  rejected: ['申诉已拒绝', 'Appeal rejected'],
  audit: ['审计，可放行未检查内容', 'Audit; may allow uninspected content'],
  warn: ['警告确认', 'Warn and confirm'],
  block: ['阻断', 'Block'],
}
export function TransferState({ record }: { record: ManagedContentRecord }) {
  const { text } = useTransferContext()
  const label = (value: string) => (labels[value] ? text(...labels[value]) : value)
  return (
    <Space orientation="vertical" size={4}>
      <StatusTag
        value={
          record.state === 'completed' ? 'info' : record.state === 'failed' ? 'error' : 'warning'
        }
        label={label(record.state)}
      />
      {record.decision ? (
        <Typography.Text>
          {label(record.decision.mode)} · {label(record.decision.status)}
        </Typography.Text>
      ) : null}
      <Typography.Text type="secondary">{label(record.appealStatus)}</Typography.Text>
      {record.decision ? <DecisionEvidence decision={record.decision} /> : null}
    </Space>
  )
}
export function uncertainRecord(record: ManagedContentRecord) {
  if (['unknown', 'prepared', 'sending'].includes(record.state))
    return new ApiError(
      503,
      'Transfer outcome is unconfirmed. Keep the request ID and inspect the record and target before any new submission.',
      { method: 'POST', path: base },
    )
  return undefined
}
export function recordReceipt(
  record: ManagedContentRecord,
  text: (zh: string, en: string) => string,
) {
  return {
    message:
      record.state === 'completed'
        ? text('本次操作已完成', 'This operation completed')
        : record.state === 'blocked'
          ? text(
              '内容未提交，阻断决定已记录',
              'Content was not submitted; the blocking decision was recorded',
            )
          : text('操作未确认完成，请查看记录', 'Completion is not confirmed; inspect the record'),
    evidence: record.id,
  }
}
