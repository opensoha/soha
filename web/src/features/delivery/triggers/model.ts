import dayjs from 'dayjs'
import utc from 'dayjs/plugin/utc'
import timezone from 'dayjs/plugin/timezone'
import type { DeliveryTrigger, DeliveryTriggerInput } from '../types'

dayjs.extend(utc)
dayjs.extend(timezone)

export function calendarInput(instant: string, zone: string) {
  return dayjs(instant).tz(zone).format('YYYY-MM-DDTHH:mm')
}

export function calendarInstant(value: string, zone: string) {
  const parsed = dayjs.tz(value, zone)
  if (!parsed.isValid() || parsed.format('YYYY-MM-DDTHH:mm') !== value)
    throw new Error('该时区不存在此时间，请调整发布日历。')
  return parsed.toISOString()
}

export function triggerInput(item: DeliveryTrigger, enabled = item.enabled): DeliveryTriggerInput {
  const common = {
    expectedRevision: item.revision,
    name: item.name,
    enabled,
    targetKind: item.targetKind,
    targetId: item.targetId,
    ...(item.workflowVersion ? { workflowVersion: item.workflowVersion } : {}),
  }
  if (item.type === 'webhook' && item.webhook)
    return { ...common, type: 'webhook', webhook: item.webhook }
  if (item.type !== 'webhook' && item.schedule)
    return { ...common, type: item.type, schedule: item.schedule }
  throw new Error('触发器配置不完整，请刷新后重试。')
}

export const triggerReasonLabels: Record<string, string> = {
  trigger_changed: '触发器已更改或停用',
  configuration_changed: '目标配置或版本已变更，请更新触发器',
  stale_commit: '已有更新的提交',
  source_unchanged: '提交未变化',
  previous_batch_active: '上次交付仍在执行',
  missed_schedule: '已错过计划时间',
  ref_deleted: '引用已删除',
  authorization_revoked: '执行身份或授权已失效',
  invalid_configuration: '配置无效',
  target_unavailable: '目标不可用',
  recovery_exhausted: '恢复重试已用尽',
  dispatch_interrupted: '派发被中断',
  dispatch_failed: '派发失败',
  batch_accepted: '已创建交付记录',
  drafts_applied: '已更新草稿',
}
