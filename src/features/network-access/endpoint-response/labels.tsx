import { StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { formatStatusLabel } from '@/i18n/status'
import type { LocaleCode } from '@/i18n/types'

const labels: Record<string, [string, string]> = {
  observing: ['观察中', 'Observing'],
  enforcing: ['执行中', 'Enforcing'],
  enforcement_unknown: ['执行未验证', 'Execution unverified'],
  recovering: ['恢复观察中', 'Observing recovery'],
  restored: ['已解除本条限制', 'This restriction cleared'],
  restricted: ['已限制', 'Restricted'],
  disconnected: ['已下线', 'Disconnected'],
  not_requested: ['未要求通知', 'Notification not requested'],
  delivered: ['已送达', 'Delivered'],
  record: ['记录', 'Record'],
  notify: ['通知', 'Notify'],
  restrict: ['降级', 'Restrict'],
  disconnect: ['下线', 'Disconnect'],
  full: ['完整访问', 'Full access'],
  quarantine: ['隔离', 'Quarantine'],
  onboarding: ['注册网络', 'Onboarding network'],
  observed: ['已观测', 'Observed'],
  unverified: ['未验证', 'Unverified'],
}

export function responseLabel(value: string, locale: LocaleCode) {
  return labels[value]?.[locale === 'zh_CN' ? 0 : 1] ?? formatStatusLabel(value, locale)
}

export function EndpointResponseTag({ value }: { value: string }) {
  const { localeCode } = useI18n()
  const tone =
    (
      {
        observing: 'checking',
        enforcing: 'processing',
        enforcement_unknown: 'warning',
        recovering: 'processing',
        restored: 'resolved',
        restricted: 'warning',
        delivered: 'acknowledged',
      } as Record<string, string>
    )[value] ?? value
  return <StatusTag value={tone} label={responseLabel(value, localeCode)} />
}
