import { useDeferredOverlay } from '@/components/use-deferred-overlay'
import { memo, useMemo } from 'react'
import { Progress, Tooltip } from 'antd'
import { useI18n } from '@/i18n'
import { quotaPercentLabel, quotaPeak, type QuotaRow } from './quota-model'
import type { ResourceQuotaResource } from './types'
import './styles.css'

export function QuotaUsageBar({ row }: { row: QuotaRow }) {
  const { localeCode } = useI18n()
  const label = quotaPercentLabel(row, localeCode === 'zh_CN')
  const score = row.over ? 101 : row.percent
  return (
    <div className="soha-quota-usage-bar">
      {score !== null ? (
        <Progress
          aria-label={row.resource + ' ' + label}
          percent={Math.min(100, score)}
          showInfo={false}
          strokeColor={
            score >= 100
              ? 'var(--soha-danger)'
              : score >= 90
                ? 'var(--soha-warning)'
                : 'var(--soha-primary)'
          }
          size="small"
        />
      ) : (
        <span className="soha-quota-unknown-track" aria-hidden />
      )}
      <span className={row.over ? 'is-over' : undefined}>{label}</span>
    </div>
  )
}

export const ResourceQuotaUsage = memo(function ResourceQuotaUsage({
  record,
}: {
  record: ResourceQuotaResource
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const { row } = useMemo(() => quotaPeak(record), [record])
  const { triggerRef, activated, open, setOpen, activate, activateOnHover, cancelHover } =
    useDeferredOverlay<HTMLDivElement>()
  if (!row)
    return <span className="soha-quota-muted">{zh ? '尚未返回占用' : 'Usage not reported'}</span>
  const usage = (
    <div
      ref={triggerRef}
      tabIndex={0}
      className="soha-quota-list-usage"
      onMouseEnter={activateOnHover}
      onMouseLeave={cancelHover}
      onFocus={activated ? undefined : activate}
      onClick={activated ? undefined : activate}
      onKeyDown={(event) => {
        if (event.key === 'Escape') setOpen(false)
      }}
    >
      <span title={row.resource}>{row.resource}</span>
      <QuotaUsageBar row={row} />
    </div>
  )
  return activated ? (
    <Tooltip
      title={row.resource + ': ' + row.usedRaw + ' / ' + row.hardRaw}
      trigger={['hover', 'focus', 'click']}
      open={open}
      onOpenChange={setOpen}
    >
      {usage}
    </Tooltip>
  ) : (
    usage
  )
})
