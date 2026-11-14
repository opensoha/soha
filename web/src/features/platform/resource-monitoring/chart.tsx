import { localeText, useI18n } from '@/i18n'
import { memo, useMemo } from 'react'
import { theme } from 'antd'
import { AreaChart, LineChart } from '@visactor/react-vchart'
import type { IAreaChartSpec, ILineChartSpec } from '@visactor/vchart'
import { usePreferencesStore } from '@/stores/preferences-store'
import { resolveThemeMode, resolveThemeColorReference } from '@/theme/app-theme'
import { formatMetricValue } from '@/components/resource-metrics-format'
import type { MonitoringPanel } from './types'
import type { SummaryPoint } from './dashboard-model'
import { monitoringNodeLimit } from './model'

export const MonitoringSparkline = memo(
  function MonitoringSparkline({
    points,
    label,
    tone,
    unit,
    animated = true,
  }: {
    points: SummaryPoint[]
    label: string
    tone: string
    unit: string
    animated?: boolean
  }) {
    const { token } = theme.useToken()
    const themeMode = usePreferencesStore((state) => state.themeMode)
    const color = resolveThemeColorReference(`var(--soha-graph-${tone})`, token.colorPrimary)
    const spec = useMemo<IAreaChartSpec>(
      () => ({
        type: 'area',
        theme: resolveThemeMode(themeMode),
        background: token.colorBgContainer,
        padding: 0,
        animation: token.motion && animated,
        animationAppear: false,
        animationEnter: false,
        animationExit: false,
        animationUpdate: {
          duration: Number.parseFloat(token.motionDurationSlow) * 1000,
          easing: 'linear',
        },
        invalidType: 'break',
        data: { id: 'observations', values: [] },
        dataKey: 'time',
        xField: 'time',
        yField: 'value',
        color: [color],
        axes: [
          { orient: 'bottom', visible: false },
          { orient: 'left', visible: false, zero: true },
        ],
        point: { visible: false },
        line: { style: { lineWidth: 2 } },
        area: { style: { fillOpacity: 0.12 } },
      }),
      [token.colorBgContainer, token.motion, token.motionDurationSlow, color, animated, themeMode],
    )
    return (
      <div className="soha-monitoring-sparkline" role="img" aria-label={`${label} · ${unit}`}>
        <AreaChart spec={spec} data={{ id: 'observations', values: points }} />
      </div>
    )
  },
  (previous, next) =>
    previous.label === next.label &&
    previous.tone === next.tone &&
    previous.unit === next.unit &&
    previous.animated === next.animated &&
    previous.points.length === next.points.length &&
    previous.points.every(
      (point, index) =>
        point.time === next.points[index].time && point.value === next.points[index].value,
    ),
)

export function MonitoringChart({ panel, names }: { panel: MonitoringPanel; names: string[] }) {
  const { localeCode } = useI18n()
  const { token } = theme.useToken()
  const themeMode = usePreferencesStore((state) => state.themeMode)
  const unit = panel.unit
  const series = names
    .slice(0, monitoringNodeLimit)
    .flatMap((name) => panel.series.filter((item) => item.nodeRef.name === name))
  const data = {
    id: 'observations',
    values: series.flatMap((item) =>
      item.points.map((point) => ({
        id: `${item.nodeRef.clusterId}:${item.nodeRef.name}:${item.nodeRef.uid || ''}:${point.timestamp}`,
        time: point.timestamp,
        value: point.value,
        node: item.nodeRef.name,
      })),
    ),
  }
  const spec = useMemo(
    () =>
      ({
        type: 'line',
        theme: resolveThemeMode(themeMode),
        background: token.colorBgContainer,
        animation: false,
        invalidType: 'break',
        data: { id: 'observations', values: [] },
        dataKey: 'id',
        xField: 'time',
        yField: 'value',
        seriesField: 'node',
        color: [
          'var(--soha-graph-metric)',
          'var(--soha-graph-service)',
          'var(--soha-warning)',
          'var(--soha-graph-span)',
          'var(--soha-graph-scope)',
          'var(--soha-graph-muted)',
          'var(--soha-graph-log)',
          'var(--soha-graph-hypothesis)',
          'var(--soha-graph-recommendation)',
          'var(--soha-danger)',
        ].map((value) =>
          resolveThemeColorReference(value, themeMode === 'dark' ? '#94a3b8' : '#64748b'),
        ),
        axes: [
          {
            orient: 'bottom',
            type: 'band',
            label: {
              formatMethod: (value: string | string[]) =>
                new Date(String(value)).toLocaleTimeString([], {
                  hour: '2-digit',
                  minute: '2-digit',
                }),
              autoHide: true,
            },
            domainLine: { visible: false },
            tick: { visible: false },
          },
          {
            orient: 'left',
            type: 'linear',
            zero: true,
            label: {
              formatMethod: (value: string | string[]) =>
                unit === 'count' && !Number.isInteger(Number(value))
                  ? ''
                  : formatMetricValue(Number(value), unit),
            },
            domainLine: { visible: false },
          },
        ],
        legends: { visible: true, orient: 'bottom' },
        point: { visible: true, style: { size: 3 } },
        line: { style: { lineWidth: 2 } },
      }) satisfies ILineChartSpec,
    [token.colorBgContainer, unit, themeMode],
  )
  return (
    <div
      className="soha-monitoring-chart"
      role="img"
      aria-label={`${panel.label}: ${names.join(', ')}. ${localeText(localeCode, '详细数值可在采样数据表中查看。', 'Exact values are available in the samples table.')}`}
    >
      <LineChart spec={spec} data={data} />
    </div>
  )
}
