import { Badge, Tooltip } from 'antd'
import type { ComputeFreshnessStatus } from '@opensoha/contracts/gen/ts/sohaapi'
import type { ComputeTaskStreamStatus } from './tasks/use-compute-task-stream'

type Status = ComputeTaskStreamStatus | ComputeFreshnessStatus

const badgeStatus = {
  idle: 'default',
  connecting: 'processing',
  live: 'success',
  reconnecting: 'processing',
  polling: 'error',
  degraded: 'error',
  done: 'default',
  fresh: 'success',
  stale: 'error',
  unknown: 'default',
} as const

const labels = {
  idle: ['未连接', 'Idle'],
  connecting: ['连接中', 'Connecting'],
  live: ['实时', 'Live'],
  reconnecting: ['重连中', 'Reconnecting'],
  polling: ['降级轮询', 'Polling fallback'],
  degraded: ['已降级', 'Degraded'],
  done: ['任务流已结束', 'Task stream ended'],
  fresh: ['数据新鲜', 'Fresh'],
  stale: ['数据过期', 'Stale'],
  unknown: ['状态未知', 'Unknown'],
} as const

export function ComputeStreamStatus({
  status,
  observedAt,
  localeCode,
}: {
  status: Status
  observedAt?: string
  localeCode: 'zh_CN' | 'en_US'
}) {
  const label = labels[status][localeCode === 'zh_CN' ? 0 : 1]
  const time = observedAt ? new Date(observedAt) : null
  const timeText =
    time && !Number.isNaN(time.getTime())
      ? new Intl.DateTimeFormat(localeCode === 'zh_CN' ? 'zh-CN' : 'en-US', {
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit',
          hour12: false,
        }).format(time)
      : ''
  const detail = timeText
    ? `${label} · ${localeCode === 'zh_CN' ? '更新于' : 'Updated at'} ${timeText}`
    : label

  return (
    <Tooltip title={detail}>
      <span
        aria-label={detail}
        role="status"
        tabIndex={0}
        style={{
          alignItems: 'center',
          display: 'inline-flex',
          flex: '0 0 24px',
          height: 24,
          justifyContent: 'center',
          width: 24,
        }}
      >
        <Badge status={badgeStatus[status]} />
      </span>
    </Tooltip>
  )
}
