import { lazy, Suspense } from 'react'
import type { OverviewMiniChartConfig, OverviewTone } from './overview-types'

const OverviewMiniChart = lazy(() => import('./overview-mini-chart'))

function chartClassName(chart: OverviewMiniChartConfig) {
  if (chart.kind === 'progress') return 'soha-overview-mini-chart is-progress'
  if (chart.kind === 'ring') return 'soha-overview-mini-chart is-ring'
  return 'soha-overview-mini-chart'
}

export function OverviewChartSlot({
  chart,
  tone,
}: {
  chart?: OverviewMiniChartConfig
  tone?: OverviewTone
}) {
  if (!chart) return null
  return (
    <Suspense fallback={<div className={chartClassName(chart)} aria-hidden="true" />}>
      <OverviewMiniChart chart={chart} tone={tone} />
    </Suspense>
  )
}
