import { Badge, Tooltip } from 'antd'
import type { KubernetesResourceStreamStatus } from './resource-stream'
import { formatStatusLabel } from '@/i18n/status'

const badgeStatusByStreamStatus = {
  idle: 'default',
  connecting: 'processing',
  live: 'success',
  reconnecting: 'processing',
  polling: 'error',
  degraded: 'error',
} as const

export function ResourceStreamStatus({
  status,
  lastEventAt,
  localeCode,
  showLabel = false,
}: {
  status: KubernetesResourceStreamStatus
  lastEventAt?: string
  localeCode: 'zh_CN' | 'en_US'
  showLabel?: boolean
}) {
  const statusLabel =
    status === 'live' && showLabel
      ? localeCode === 'zh_CN'
        ? '实时更新'
        : 'Live updates'
      : formatStatusLabel(status, localeCode)
  const eventDate = lastEventAt ? new Date(lastEventAt) : null
  const eventTime =
    eventDate && !Number.isNaN(eventDate.getTime())
      ? new Intl.DateTimeFormat(localeCode === 'zh_CN' ? 'zh-CN' : 'en-US', {
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit',
          hour12: false,
        }).format(eventDate)
      : null
  const detail = eventTime
    ? `${statusLabel} · ${localeCode === 'zh_CN' ? '更新于' : 'Updated at'} ${eventTime}`
    : statusLabel

  return (
    <Tooltip title={detail}>
      <span
        aria-label={detail}
        role="status"
        tabIndex={0}
        style={{
          alignItems: 'center',
          display: 'inline-flex',
          flex: showLabel ? '0 0 auto' : '0 0 24px',
          height: 24,
          justifyContent: 'center',
          width: showLabel ? undefined : 24,
        }}
      >
        <Badge
          status={badgeStatusByStreamStatus[status]}
          text={showLabel ? statusLabel : undefined}
        />
      </span>
    </Tooltip>
  )
}
