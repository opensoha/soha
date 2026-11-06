import { Progress, Tooltip } from 'antd'
import { formatMetricValue } from '@/components/resource-metrics-format'
import { localeText, useI18n } from '@/i18n'
import { monitoringRatio, qualityLabels } from './model'

/** Host capacity meter; never falls back to requests, limits, or a fabricated zero. */
export function MonitoringUsageMeter({
  value,
  capacity,
  unit,
  quality,
}: {
  value: number | null | undefined
  capacity: number | null | undefined
  unit: string
  quality?: string
}) {
  const { localeCode } = useI18n()
  const ratio = monitoringRatio(value, capacity)
  const label = value == null ? '—' : formatMetricValue(value, unit)
  const title = `${localeText(localeCode, '用量', 'Usage')} ${label} / ${localeText(localeCode, '容量', 'Capacity')} ${capacity == null ? '—' : formatMetricValue(capacity, unit)}${ratio == null ? '' : ` · ${ratio.toFixed(1)}%`}${quality && quality !== 'valid' ? ` · ${qualityLabels[quality] || quality}` : ''}`
  return (
    <Tooltip title={title}>
      <span className="soha-monitoring-usage" tabIndex={0} aria-label={title}>
        {ratio == null ? (
          <span className="soha-monitoring-usage-empty" aria-hidden />
        ) : (
          <Progress
            percent={Math.max(0, Math.min(100, ratio))}
            showInfo={false}
            size={['100%', 20]}
            strokeColor={`color-mix(in srgb, ${ratio != null && ratio > 90 ? 'var(--soha-warning)' : 'var(--soha-primary)'} 28%, transparent)`}
            railColor="var(--soha-fill-weak)"
          />
        )}
        <span className="soha-monitoring-usage-value">{label}</span>
      </span>
    </Tooltip>
  )
}
