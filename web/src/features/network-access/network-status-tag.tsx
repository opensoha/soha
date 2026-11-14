import { StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'

const states: Record<string, { value: string; zh: string; en: string }> = {
  offline: { value: 'disconnected', zh: '离线', en: 'Offline' },
  compliant: { value: 'healthy', zh: '合规', en: 'Compliant' },
  non_compliant: { value: 'denied', zh: '不合规', en: 'Non-compliant' },
  quarantined: { value: 'denied', zh: '已隔离', en: 'Quarantined' },
  revoked: { value: 'denied', zh: '已吊销', en: 'Revoked' },
  issued: { value: 'pending', zh: '已签发', en: 'Issued' },
  consumed: { value: 'completed', zh: '已使用', en: 'Consumed' },
  unregistered: { value: 'unknown', zh: '未注册', en: 'Unregistered' },
}

// Domain states retain their labels while sharing the application's status semantics.
export function NetworkStatusTag({ value }: { value?: string | null }) {
  const { localeCode } = useI18n()
  const state = value ? states[value] : undefined
  return (
    <StatusTag
      value={state?.value ?? value}
      label={state?.[localeCode === 'zh_CN' ? 'zh' : 'en']}
    />
  )
}
