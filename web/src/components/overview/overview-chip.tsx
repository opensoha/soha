import { OverviewChartSlot } from './overview-chart-slot'
import type { OverviewChipProps, OverviewTone } from './overview-types'

function toneClassName(tone: OverviewTone = 'default') {
  return `is-${tone}`
}

export function OverviewChip({
  label,
  value,
  helper,
  icon,
  tone = 'default',
  className,
  chart,
}: OverviewChipProps) {
  return (
    <div
      className={['soha-overview-chip', toneClassName(tone), className].filter(Boolean).join(' ')}
    >
      <div className="soha-overview-chip-head">
        {icon ? <span className="soha-overview-metric-icon">{icon}</span> : null}
        <span className="soha-overview-chip-label">{label}</span>
      </div>
      <span className="soha-overview-chip-value">{value}</span>
      {helper ? <span className="soha-overview-metric-helper">{helper}</span> : null}
      <OverviewChartSlot chart={chart} tone={tone} />
    </div>
  )
}
