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
  refreshing = false,
  refreshFailed = false,
}: {
  status: KubernetesResourceStreamStatus
  lastEventAt?: string
  localeCode: 'zh_CN' | 'en_US'
  showLabel?: boolean
  refreshing?: boolean
  refreshFailed?: boolean
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
  const streamDetail = eventTime
    ? `${statusLabel} · ${localeCode === 'zh_CN' ? '更新于' : 'Updated at'} ${eventTime}`
    : statusLabel

  const readLabel = refreshing
    ? localeCode === 'zh_CN'
      ? '刷新中'
      : 'Refreshing'
    : refreshFailed
      ? localeCode === 'zh_CN'
        ? '刷新失败，请重试'
        : 'Refresh failed; retry'
      : null
  const detail = readLabel ? `${readLabel} · ${streamDetail}` : streamDetail

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
          status={
            refreshing ? 'warning' : refreshFailed ? 'error' : badgeStatusByStreamStatus[status]
          }
          text={showLabel ? (readLabel ?? statusLabel) : undefined}
        />
      </span>
    </Tooltip>
  )
}
