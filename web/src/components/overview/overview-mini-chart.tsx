import { Tiny } from '@ant-design/charts'
import { resolveThemeColorReference } from '@/theme/app-theme'
import type { OverviewMiniChartConfig, OverviewTone } from './overview-types'

function resolveOverviewToneColor(tone: OverviewTone = 'default') {
  switch (tone) {
    case 'success':
      return resolveThemeColorReference('var(--soha-success)', '#22c55e')
    case 'warning':
      return resolveThemeColorReference('var(--soha-warning)', '#f97316')
    case 'danger':
      return resolveThemeColorReference('var(--soha-danger)', '#ef4444')
    default:
      return resolveThemeColorReference('var(--soha-primary)', '#1677ff')
  }
}

export default function OverviewMiniChart({
  chart,
  tone,
}: {
  chart: OverviewMiniChartConfig
  tone?: OverviewTone
}) {
  const baseColor = resolveOverviewToneColor(tone)
  const weakColor = resolveThemeColorReference('var(--soha-fill-weak)', '#f9f9fb')

  if (chart.kind === 'progress') {
    return (
      <div className="soha-overview-mini-chart is-progress" aria-hidden="true">
        <Tiny.Progress percent={chart.percent} color={[weakColor, baseColor]} />
      </div>
    )
  }

  if (chart.kind === 'ring') {
    return (
      <div className="soha-overview-mini-chart is-ring" aria-hidden="true">
        <Tiny.Ring percent={chart.percent} color={[weakColor, baseColor]} radius={0.74} />
      </div>
    )
  }

  if (chart.kind === 'column') {
    return (
      <div className="soha-overview-mini-chart" aria-hidden="true">
        <Tiny.Column data={chart.data} color={baseColor} autoFit />
      </div>
    )
  }

  if (chart.kind === 'area') {
    return (
      <div className="soha-overview-mini-chart" aria-hidden="true">
        <Tiny.Area data={chart.data} color={baseColor} shapeField="smooth" autoFit />
      </div>
    )
  }

  return (
    <div className="soha-overview-mini-chart" aria-hidden="true">
      <Tiny.Line
        data={chart.data.map((value, index) => ({ index, value }))}
        xField="index"
        yField="value"
        color={baseColor}
        shapeField="smooth"
        autoFit
      />
    </div>
  )
}
