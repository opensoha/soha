import { Card, Statistic, Typography } from 'antd'
import type { ReactNode } from 'react'
import { OverviewChartSlot } from './overview-chart-slot'
import type { OverviewMetricCardProps, OverviewTone } from './overview-types'

const { Text } = Typography

function toneClassName(tone: OverviewTone = 'default') {
  return `is-${tone}`
}

function renderStatisticValue(value: ReactNode) {
  return typeof value === 'number' ? (
    <Statistic value={value} />
  ) : (
    <span className="soha-overview-value">{value}</span>
  )
}

export function OverviewMetricCard({
  label,
  value,
  helper,
  icon,
  tone = 'default',
  className,
  chart,
  loading,
  variant = 'metric',
}: OverviewMetricCardProps) {
  const baseClassName = variant === 'pod' ? 'soha-overview-pod-card' : 'soha-overview-metric-card'
  const headClassName =
    variant === 'pod' ? 'soha-overview-pod-card-head' : 'soha-overview-metric-card-head'

  return (
    <Card
      size="small"
      variant="outlined"
      loading={loading}
      className={[baseClassName, toneClassName(tone), className].filter(Boolean).join(' ')}
    >
      <div className={headClassName}>
        <div className="soha-overview-metric-copy">
          <Text className="soha-overview-metric-label">{label}</Text>
          {renderStatisticValue(value)}
        </div>
        {icon ? <span className="soha-overview-metric-icon">{icon}</span> : null}
      </div>
      {helper ? <Text className="soha-overview-metric-helper">{helper}</Text> : null}
      <OverviewChartSlot chart={chart} tone={tone} />
    </Card>
  )
}
