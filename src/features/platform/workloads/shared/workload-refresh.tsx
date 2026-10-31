import { useCallback, useState, type ReactNode } from 'react'
import { Badge, Button, Tooltip } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { useI18n } from '@/i18n'
import { useDocumentHidden } from '@/utils/use-document-hidden'
import './workload-refresh.css'

const INTERVAL = 15_000

/** Only the resource owner attaches this cadence to its named queries. */
export function useWorkloadRefresh() {
  const [enabled, setEnabled] = useState(true)
  const hidden = useDocumentHidden()
  const interval = useCallback(
    (query: { state: { status: string } }) =>
      enabled && !hidden && query.state.status !== 'error' ? INTERVAL : false,
    [enabled, hidden],
  )
  return { enabled, setEnabled, hidden, interval }
}

type RefreshQuery = { isFetching?: boolean; isError?: boolean; isEnabled?: boolean }

export function WorkloadRefresh({
  settings,
  queries,
  onRefresh,
  resourceLabel,
  label,
  compact = false,
  disabled = false,
  beforeRefresh,
}: {
  settings: ReturnType<typeof useWorkloadRefresh>
  queries: readonly RefreshQuery[]
  onRefresh: () => unknown
  resourceLabel: string
  label?: string
  compact?: boolean
  disabled?: boolean
  beforeRefresh?: ReactNode
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const active = queries.filter((query) => query.isEnabled !== false)
  const fetching = active.some((query) => query.isFetching)
  const failed = active.some((query) => query.isError)
  const state = fetching
    ? 'refreshing'
    : failed
      ? 'failed'
      : disabled || !settings.enabled || settings.hidden || !active.length
        ? 'paused'
        : 'live'
  const status = {
    refreshing: 'warning',
    failed: 'error',
    paused: 'default',
    live: 'success',
  } as const
  const stateLabel =
    state === 'refreshing'
      ? zh
        ? '刷新中'
        : 'Refreshing'
      : state === 'failed'
        ? zh
          ? '刷新失败，请重试'
          : 'Refresh failed; retry'
        : state === 'paused'
          ? zh
            ? '自动刷新已暂停'
            : 'Auto-refresh paused'
          : zh
            ? '自动刷新正常，每 15 秒'
            : 'Auto-refresh active, every 15 seconds'
  const toggle = settings.enabled
    ? zh
      ? '关闭自动刷新'
      : 'Turn off auto-refresh'
    : zh
      ? '开启自动刷新'
      : 'Turn on auto-refresh'
  const hint = settings.hidden
    ? `${stateLabel} · ${zh ? '页面在后台' : 'Page is in the background'} · ${toggle}`
    : `${stateLabel} · ${toggle}`
  return (
    <span className={`soha-workload-refresh${beforeRefresh ? ' has-actions' : ''}`}>
      <Tooltip title={hint} trigger={['hover', 'focus']}>
        <Button
          type="text"
          size="small"
          disabled={disabled}
          role="switch"
          aria-checked={settings.enabled}
          aria-label={`${zh ? '自动刷新' : 'Auto-refresh'}：${hint}`}
          data-refresh-state={state}
          className="soha-workload-refresh-dot"
          onClick={() => settings.setEnabled((value) => !value)}
        >
          <Badge status={status[state]} aria-hidden />
        </Button>
      </Tooltip>
      {beforeRefresh}
      <Tooltip title={label ?? (zh ? `刷新 ${resourceLabel}` : `Refresh ${resourceLabel}`)}>
        <Button
          type={compact ? 'text' : 'default'}
          size={compact ? 'small' : undefined}
          disabled={disabled}
          icon={<ReloadOutlined />}
          loading={fetching}
          aria-label={label ?? (zh ? `刷新 ${resourceLabel}` : `Refresh ${resourceLabel}`)}
          onClick={() => {
            void onRefresh()
          }}
        >
          {compact ? null : zh ? '刷新' : 'Refresh'}
        </Button>
      </Tooltip>
    </span>
  )
}
