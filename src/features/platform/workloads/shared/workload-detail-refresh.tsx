import { useCallback, useState } from 'react'
import { Badge, Button, Tooltip } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { useI18n } from '@/i18n'
import { useDocumentHidden } from '@/utils/use-document-hidden'

const INTERVAL = 15_000

/** Only the resource owner attaches this cadence to its named queries. */
export function useWorkloadDetailRefresh() {
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

export function WorkloadDetailRefresh({
  settings,
  queries,
  onRefresh,
  resourceLabel,
  label,
}: {
  settings: ReturnType<typeof useWorkloadDetailRefresh>
  queries: readonly RefreshQuery[]
  onRefresh: () => unknown
  resourceLabel: string
  label?: string
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
      : !settings.enabled || settings.hidden || !active.length
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
            ? '资源摘要自动刷新正常，每 15 秒'
            : 'Resource summary auto-refresh active, every 15 seconds'
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
    <span className="soha-workload-detail-refresh">
      <Tooltip title={hint} trigger={['hover', 'focus']}>
        <Button
          type="text"
          size="small"
          role="switch"
          aria-checked={settings.enabled}
          aria-label={`${zh ? '自动刷新' : 'Auto-refresh'}：${hint}`}
          data-refresh-state={state}
          className="soha-workload-detail-refresh-dot"
          onClick={() => settings.setEnabled((value) => !value)}
        >
          <Badge status={status[state]} aria-hidden />
        </Button>
      </Tooltip>
      <Button
        icon={<ReloadOutlined />}
        loading={fetching}
        aria-label={label ?? (zh ? `刷新 ${resourceLabel}` : `Refresh ${resourceLabel}`)}
        onClick={() => {
          void onRefresh()
        }}
      >
        {zh ? '刷新' : 'Refresh'}
      </Button>
    </span>
  )
}
