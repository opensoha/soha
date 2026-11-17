import { memo, useEffect, type KeyboardEvent } from 'react'
import { useDeferredOverlay } from '@/components/use-deferred-overlay'
import { Popover, Progress, Tooltip, Typography } from 'antd'
import { MetadataTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { formatMetricValue } from '@/components/resource-metrics-format'
import { qualityLabels, reasonLabels } from '../resource-monitoring/model'
import type { NodeTaint } from './types'
import type { nodeUsage } from './node-list-model'

export const NodeTaints = memo(function NodeTaints({
  taints,
  compact = false,
}: {
  taints?: NodeTaint[] | null
  compact?: boolean
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const { activated, open, setOpen, activate, activateOnHover, cancelHover, triggerRef } =
    useDeferredOverlay<HTMLButtonElement>()
  const hasTaints = Boolean(taints?.length)
  useEffect(() => {
    if (!hasTaints) setOpen(false)
  }, [hasTaints, setOpen])
  const closeOnEscape = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== 'Escape') return
    event.preventDefault()
    event.stopPropagation()
    setOpen(false)
    triggerRef.current?.focus()
  }
  if (taints == null)
    return <Typography.Text type="secondary">{zh ? '未提供' : 'Not reported'}</Typography.Text>
  if (!taints.length)
    return <Typography.Text type="secondary">{zh ? '无污点' : 'None'}</Typography.Text>
  const content = activated ? (
    <div className="soha-node-taint-details" onKeyDown={closeOnEscape}>
      {taints.map((t) => (
        <div key={`${t.key}:${t.effect}`}>
          <Typography.Text
            code
          >{`${t.key}${t.value ? `=${t.value}` : ''}:${t.effect}`}</Typography.Text>
        </div>
      ))}
    </div>
  ) : null
  const trigger = (
    <button
      ref={triggerRef}
      aria-expanded={open}
      onMouseEnter={activated ? undefined : activateOnHover}
      onMouseLeave={activated ? undefined : cancelHover}
      onClick={activated ? undefined : activate}
      onKeyDown={(event) => {
        closeOnEscape(event)
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          if (activated) setOpen((current) => !current)
          else activate()
        }
      }}
      type="button"
      className="soha-node-taint-trigger"
      aria-label={zh ? '查看节点污点' : 'View node taints'}
    >
      <span className="soha-node-taints">
        {(compact ? taints.slice(0, 1) : taints).map((t) => (
          <MetadataTag
            key={`${t.key}:${t.effect}`}
            tone={t.effect === 'NoExecute' ? 'orange' : 'purple'}
            label={
              <span className="soha-node-taint-label">
                {t.key.split('/').pop()}
                {t.value ? `=${t.value}` : ''}
              </span>
            }
          />
        ))}
        {compact && taints.length > 1 && <MetadataTag label={`+${taints.length - 1}`} />}
      </span>
    </button>
  )
  if (!activated) return trigger
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      trigger={['hover', 'click']}
      title={zh ? '节点污点' : 'Node taints'}
      content={content}
    >
      {trigger}
    </Popover>
  )
})
export const NodeUsageCell = memo(
  function NodeUsageCell({
    usage,
    kind,
    canRead = true,
    failed = false,
  }: {
    usage: ReturnType<typeof nodeUsage>
    kind: 'cpu' | 'memory'
    canRead?: boolean
    failed?: boolean
  }) {
    const { localeCode, t } = useI18n()
    const { activated, open, setOpen, activate, activateOnHover, cancelHover, triggerRef } =
      useDeferredOverlay<HTMLSpanElement>()
    const zh = localeCode === 'zh_CN'
    const label =
      usage.value === null
        ? '—'
        : formatMetricValue(usage.value, kind === 'cpu' ? 'cores' : 'bytes')
    const reason = !canRead
      ? zh
        ? '无监控查看权限'
        : 'Monitoring permission required'
      : failed
        ? zh
          ? '监控读取失败'
          : 'Monitoring read failed'
        : t(
            `resourceMonitoring.quality.${usage.quality}`,
            qualityLabels[usage.quality] || usage.quality,
          )
    const detail =
      usage.value === null
        ? reason
        : `${zh ? '主机实际使用' : 'Host usage'} ${label}${usage.total != null ? ` / ${formatMetricValue(usage.total, kind === 'cpu' ? 'cores' : 'bytes')}` : ''}${usage.percent != null ? ` · ${usage.percent.toFixed(1)}%${kind === 'cpu' ? (zh ? ' · 基准：节点 CPU 容量快照' : ' · Basis: Node CPU capacity snapshot') : ''}` : ''}`
    const trigger = (
      <span
        ref={triggerRef}
        onMouseEnter={activated ? undefined : activateOnHover}
        onMouseLeave={activated ? undefined : cancelHover}
        onFocus={activated ? undefined : activate}
        onKeyDown={(event) => {
          if (event.key !== 'Escape') return
          event.preventDefault()
          event.stopPropagation()
          setOpen(false)
        }}
        className="soha-node-usage"
        tabIndex={0}
        aria-label={`${kind} ${detail}`}
      >
        {usage.percent === null ? (
          <span className="soha-node-usage-rail" aria-hidden="true" />
        ) : (
          <Progress
            percent={Math.min(100, Math.max(0, usage.percent ?? 0))}
            showInfo={false}
            styles={{ rail: { background: 'var(--soha-fill-medium)' }, track: { opacity: 0.28 } }}
            strokeColor={
              usage.percent != null && usage.percent >= 85
                ? 'var(--soha-warning)'
                : 'var(--soha-primary)'
            }
            size={{ height: 20 }}
          />
        )}
        <span>{label}</span>
      </span>
    )
    if (!activated) return trigger
    return (
      <Tooltip
        open={open}
        onOpenChange={setOpen}
        trigger={['hover', 'focus']}
        title={
          <>
            <div>{detail}</div>
            {usage.reason && (
              <div>
                {t(
                  `resourceMonitoring.reason.${usage.reason}`,
                  reasonLabels[usage.reason] || usage.reason,
                )}
              </div>
            )}
            {usage.sampledAt && (
              <div>
                {zh ? '采样' : 'Sample'} {new Date(usage.sampledAt).toLocaleTimeString()}
              </div>
            )}
          </>
        }
      >
        {trigger}
      </Tooltip>
    )
  },
  (previous, next) =>
    previous.kind === next.kind &&
    previous.canRead === next.canRead &&
    previous.failed === next.failed &&
    (['value', 'quality', 'reason', 'sampledAt', 'total', 'percent'] as const).every((field) =>
      Object.is(previous.usage[field], next.usage[field]),
    ),
)
