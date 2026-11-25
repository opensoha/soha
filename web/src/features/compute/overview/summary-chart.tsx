import { Bar, Pie } from '@ant-design/charts'
import { theme } from 'antd'
import { useSyncExternalStore } from 'react'
import { localeText, useI18n } from '@/i18n'
import { usePreferencesStore } from '@/stores/preferences-store'
import { OVERVIEW_COMPACT_CHART_SIZE, resolveThemeMode } from '@/theme/app-theme'

export type SummaryChartItem = {
  label: string
  value?: number
  tone: 'success' | 'danger' | 'neutral' | 'active' | 'violet'
}

export type SummaryChartProps = {
  items: SummaryChartItem[]
  kind?: 'ring' | 'bars'
  surface?: 'default' | 'hero'
  total?: number
}

const motionQuery = '(prefers-reduced-motion: reduce)'
function subscribeMotion(onChange: () => void) {
  const media = window.matchMedia(motionQuery)
  media.addEventListener('change', onChange)
  return () => media.removeEventListener('change', onChange)
}

export default function SummaryChart({
  items,
  kind = 'ring',
  surface = 'default',
  total,
}: SummaryChartProps) {
  const { token } = theme.useToken()
  const { localeCode } = useI18n()
  const themeMode = usePreferencesStore((state) => state.themeMode)
  const reducedMotion = useSyncExternalStore(
    subscribeMotion,
    () => window.matchMedia(motionQuery).matches,
    () => false,
  )
  const resolvedMode = resolveThemeMode(themeMode)
  const isHero = surface === 'hero'
  const chartSize = isHero ? 76 : OVERVIEW_COMPACT_CHART_SIZE
  const colors = isHero
    ? {
        success: '#8be0b9',
        danger: '#f59f7f',
        neutral: 'rgba(247, 244, 238, 0.24)',
        active: '#a7ebcb',
        violet: '#d8efe3',
      }
    : {
        success: resolvedMode === 'dark' ? '#5ec29e' : '#2b7a56',
        danger: token.colorError,
        neutral: token.colorFillSecondary,
        active: resolvedMode === 'dark' ? '#5ec29e' : '#1f4735',
        violet: resolvedMode === 'dark' ? '#b8b3a8' : '#4c5951',
      }
  const complete =
    items.length > 0 &&
    items.every(({ value }) => typeof value === 'number' && Number.isFinite(value) && value >= 0)
  const sum = items.reduce((result, { value }) => result + (value ?? 0), 0)
  const available = complete && (kind === 'bars' || sum === total)
  if (!available || sum === 0) {
    return (
      <div className="soha-compute-chart-empty" aria-hidden="true">
        <span>
          {available
            ? localeText(localeCode, '暂无资源', 'No resources')
            : localeText(localeCode, '数据不完整', 'Incomplete data')}
        </span>
      </div>
    )
  }

  const data = items.map(({ label, value }) => ({ label, value: value! }))
  const scale = {
    color: {
      domain: items.map(({ label }) => label),
      range: items.map(({ tone }) => colors[tone]),
    },
  }
  const animate = reducedMotion
    ? false
    : {
        enter: { type: kind === 'ring' ? 'waveIn' : 'growInX', duration: 700 },
        update: { duration: 400 },
      }

  return (
    <div
      className={`soha-compute-summary-chart is-${kind} ${isHero ? 'is-hero' : ''}`}
      aria-hidden="true"
    >
      {kind === 'ring' ? (
        <Pie
          data={data.filter(({ value }) => value > 0)}
          angleField="value"
          colorField="label"
          height={chartSize}
          innerRadius={isHero ? 0.78 : 0.8}
          radius={0.96}
          padding={0}
          margin={0}
          inset={0}
          legend={false}
          label={false}
          scale={scale}
          animate={animate}
          theme={isHero ? 'dark' : resolveThemeMode(themeMode)}
          style={{
            stroke: isHero ? 'rgba(23, 53, 40, 0.85)' : token.colorBgContainer,
            lineWidth: isHero ? 1.5 : 1,
          }}
          interaction={{ elementHighlight: true }}
          tooltip={{
            title: 'label',
            items: [
              {
                channel: 'y',
                name: localeText(localeCode, '数量', 'Count'),
                valueFormatter: (value: number) => String(value),
              },
            ],
          }}
        />
      ) : (
        <Bar
          data={data}
          xField="label"
          yField="value"
          colorField="label"
          height={chartSize}
          paddingTop={12}
          paddingBottom={0}
          paddingLeft={0}
          paddingRight={0}
          margin={0}
          legend={false}
          scale={scale}
          animate={animate}
          axis={false}
          label={{
            text: 'label',
            position: 'top-left',
            fill: token.colorTextSecondary,
            fontSize: 10,
            dy: -6,
            textBaseline: 'bottom',
          }}
          style={{ radius: 3, maxWidth: 6 }}
          theme={resolveThemeMode(themeMode)}
          tooltip={{
            title: 'label',
            items: [
              {
                channel: 'y',
                name: localeText(localeCode, '数量', 'Count'),
                valueFormatter: (value: number) => String(value),
              },
            ],
          }}
        />
      )}
    </div>
  )
}
