import { ArrowRightOutlined } from '@ant-design/icons'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

export type GraphicSignalTone = 'primary' | 'success' | 'warning' | 'danger' | 'neutral'

export interface SpatialRingItem {
  key: string
  label: ReactNode
  value: ReactNode
  ratio: number
  tone: GraphicSignalTone
  to?: string
}

export interface SpatialSpectrumBarItem {
  key: string
  label: string
  value: number
  displayValue?: ReactNode
  tone?: GraphicSignalTone
  to?: string
}

export interface SpatialRadarBlipItem {
  key: string
  label: string
  badge?: ReactNode
  tone: GraphicSignalTone
  intensity?: number
  to?: string
}

export type SpatialGraphicPod =
  | {
      key: string
      kind: 'rings'
      title: ReactNode
      subtitle?: ReactNode
      actionLabel?: ReactNode
      actionTo?: string
      centerValue: ReactNode
      centerLabel?: ReactNode
      rings: SpatialRingItem[]
    }
  | {
      key: string
      kind: 'spectrum'
      title: ReactNode
      subtitle?: ReactNode
      actionLabel?: ReactNode
      actionTo?: string
      bars: SpatialSpectrumBarItem[]
    }
  | {
      key: string
      kind: 'radar'
      title: ReactNode
      subtitle?: ReactNode
      actionLabel?: ReactNode
      actionTo?: string
      emptyLabel?: ReactNode
      blips: SpatialRadarBlipItem[]
    }

const TONE_STROKES: Record<GraphicSignalTone, string> = {
  primary: 'var(--soha-stage-c1, #3b82f6)',
  success: '#10b981',
  warning: '#f59e0b',
  danger: '#f43f5e',
  neutral: '#94a3b8',
}

function clamp01(val: number) {
  if (!Number.isFinite(val)) return 0
  return Math.max(0, Math.min(1, val))
}

function PodHeader({
  title,
  subtitle,
  actionLabel,
  actionTo,
}: {
  title: ReactNode
  subtitle?: ReactNode
  actionLabel?: ReactNode
  actionTo?: string
}) {
  return (
    <div className="soha-spatial-graphic__head">
      <div className="soha-spatial-graphic__titles">
        <span className="soha-spatial-graphic__title">{title}</span>
        {subtitle ? <span className="soha-spatial-graphic__sub">{subtitle}</span> : null}
      </div>
      {actionTo ? (
        <Link to={actionTo} className="soha-spatial-graphic__more">
          <span>{actionLabel ?? '进入'}</span>
          <ArrowRightOutlined />
        </Link>
      ) : null}
    </div>
  )
}

function SpatialRingsPodView({ pod }: { pod: Extract<SpatialGraphicPod, { kind: 'rings' }> }) {
  const radii = [42, 33, 24, 15]
  const visibleRings = pod.rings.slice(0, 4)

  return (
    <div className="soha-spatial-graphic-pod is-rings">
      <PodHeader
        title={pod.title}
        subtitle={pod.subtitle}
        actionLabel={pod.actionLabel}
        actionTo={pod.actionTo}
      />
      <div className="soha-spatial-rings-body">
        <div className="soha-spatial-rings-dial-wrap" aria-hidden="true">
          <div className="soha-spatial-rings-dial">
            <svg viewBox="0 0 104 104" className="soha-spatial-rings-svg">
              {visibleRings.map((ring, idx) => {
                const r = radii[idx] ?? 16
                const c = 2 * Math.PI * r
                const ratio = ring.ratio > 0 ? Math.max(0.06, clamp01(ring.ratio)) : 0
                return (
                  <g key={ring.key}>
                    <circle
                      cx={52}
                      cy={52}
                      r={r}
                      fill="none"
                      stroke="currentColor"
                      strokeOpacity={0.11}
                      strokeWidth={5.2}
                    />
                    {ratio > 0 ? (
                      <circle
                        cx={52}
                        cy={52}
                        r={r}
                        fill="none"
                        stroke={TONE_STROKES[ring.tone]}
                        strokeWidth={5.2}
                        strokeLinecap="round"
                        strokeDasharray={`${(c * ratio).toFixed(2)} ${c.toFixed(2)}`}
                        transform="rotate(-90 52 52)"
                      />
                    ) : null}
                  </g>
                )
              })}
            </svg>
            <div className="soha-spatial-rings-center">
              <strong>{pod.centerValue}</strong>
              {pod.centerLabel ? <small>{pod.centerLabel}</small> : null}
            </div>
          </div>
        </div>
        <div className="soha-spatial-rings-legend">
          {visibleRings.map((ring) => {
            const content = (
              <>
                <span className={`soha-spatial-tone-dot is-${ring.tone}`} aria-hidden="true" />
                <span className="soha-spatial-rings-legend__label">{ring.label}</span>
                <strong className="soha-spatial-rings-legend__val">{ring.value}</strong>
              </>
            )
            return ring.to ? (
              <Link key={ring.key} to={ring.to} className="soha-spatial-rings-legend__row is-link">
                {content}
              </Link>
            ) : (
              <div key={ring.key} className="soha-spatial-rings-legend__row">
                {content}
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

function buildSmoothWavePath(points: Array<{ x: number; y: number }>) {
  if (points.length === 0) return ''
  if (points.length === 1) return `M ${points[0].x} ${points[0].y}`
  let d = `M ${points[0].x.toFixed(1)} ${points[0].y.toFixed(1)}`
  for (let i = 0; i < points.length - 1; i += 1) {
    const p0 = points[i]
    const p1 = points[i + 1]
    const cx = (p0.x + p1.x) / 2
    d += ` C ${cx.toFixed(1)} ${p0.y.toFixed(1)}, ${cx.toFixed(1)} ${p1.y.toFixed(1)}, ${p1.x.toFixed(1)} ${p1.y.toFixed(1)}`
  }
  return d
}

function SpatialSpectrumPodView({
  pod,
}: {
  pod: Extract<SpatialGraphicPod, { kind: 'spectrum' }>
}) {
  const items =
    pod.bars.length > 0
      ? pod.bars.slice(0, 6)
      : [{ key: 'idle', label: 'Idle', value: 0, displayValue: 0, tone: 'neutral' as const }]
  const maxVal = Math.max(1, ...items.map((b) => (Number.isFinite(b.value) ? b.value : 0)))
  const baseY = 66
  const topY = 12
  const spanX = 224
  const startX = 20

  const coords = items.map((bar, idx) => {
    const x = items.length === 1 ? 132 : startX + (idx / Math.max(1, items.length - 1)) * spanX
    const safeVal = Math.max(0, Number.isFinite(bar.value) ? bar.value : 0)
    const ratio = safeVal > 0 ? Math.max(0.12, safeVal / maxVal) : 0.04
    const y = baseY - ratio * (baseY - topY)
    return { ...bar, x, y, barHeight: baseY - y }
  })

  const linePath = buildSmoothWavePath(coords)
  const areaPath =
    coords.length >= 2
      ? `${linePath} L ${coords[coords.length - 1].x.toFixed(1)} ${baseY} L ${coords[0].x.toFixed(1)} ${baseY} Z`
      : ''
  const gradId = `soha-spec-grad-${pod.key.replace(/[^a-zA-Z0-9_-]/g, '')}`

  return (
    <div className="soha-spatial-graphic-pod is-spectrum">
      <PodHeader
        title={pod.title}
        subtitle={pod.subtitle}
        actionLabel={pod.actionLabel}
        actionTo={pod.actionTo}
      />
      <div className="soha-spatial-spectrum-chart" aria-hidden="true">
        <svg viewBox="0 0 264 74" preserveAspectRatio="none" className="soha-spatial-spectrum-svg">
          <defs>
            <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--soha-stage-c1, #3b82f6)" stopOpacity="0.28" />
              <stop offset="100%" stopColor="var(--soha-stage-c3, #06b6d4)" stopOpacity="0.02" />
            </linearGradient>
          </defs>
          <line
            x1="10"
            y1={baseY}
            x2="254"
            y2={baseY}
            stroke="currentColor"
            strokeOpacity="0.12"
            strokeDasharray="3 3"
          />
          <line
            x1="10"
            y1={(baseY + topY) / 2}
            x2="254"
            y2={(baseY + topY) / 2}
            stroke="currentColor"
            strokeOpacity="0.07"
            strokeDasharray="2 4"
          />
          {areaPath ? <path d={areaPath} fill={`url(#${gradId})`} /> : null}
          {coords.map((pt) => {
            const barW = Math.min(20, Math.max(10, 150 / Math.max(2, coords.length)))
            const strokeColor = TONE_STROKES[pt.tone ?? 'primary']
            return (
              <g key={pt.key}>
                <rect
                  x={pt.x - barW / 2}
                  y={topY}
                  width={barW}
                  height={baseY - topY}
                  rx={barW / 2}
                  fill="currentColor"
                  fillOpacity={0.05}
                />
                <rect
                  x={pt.x - barW / 2}
                  y={pt.y}
                  width={barW}
                  height={Math.max(3, pt.barHeight)}
                  rx={barW / 2}
                  fill={strokeColor}
                  fillOpacity={0.78}
                />
              </g>
            )
          })}
          {coords.length >= 2 ? (
            <path
              d={linePath}
              fill="none"
              stroke="var(--soha-stage-c2, #6366f1)"
              strokeWidth="2"
              strokeLinecap="round"
            />
          ) : null}
          {coords.map((pt) => (
            <circle
              key={`dot-${pt.key}`}
              cx={pt.x}
              cy={pt.y}
              r="3.2"
              fill="#ffffff"
              stroke={TONE_STROKES[pt.tone ?? 'primary']}
              strokeWidth="2"
            />
          ))}
        </svg>
      </div>
      <div className="soha-spatial-spectrum-pills">
        {items.slice(0, 5).map((bar) => {
          const inner = (
            <>
              <span
                className={`soha-spatial-tone-dot is-${bar.tone ?? 'primary'}`}
                aria-hidden="true"
              />
              <span className="soha-spatial-spectrum-pill__name">{bar.label}</span>
              <strong className="soha-spatial-spectrum-pill__val">
                {bar.displayValue ?? bar.value}
              </strong>
            </>
          )
          return bar.to ? (
            <Link key={bar.key} to={bar.to} className="soha-spatial-spectrum-pill is-link">
              {inner}
            </Link>
          ) : (
            <span key={bar.key} className="soha-spatial-spectrum-pill">
              {inner}
            </span>
          )
        })}
      </div>
    </div>
  )
}

function SpatialRadarPodView({ pod }: { pod: Extract<SpatialGraphicPod, { kind: 'radar' }> }) {
  const blips = pod.blips.slice(0, 5)
  const hasAlerts = blips.some((b) => b.tone === 'danger' || b.tone === 'warning')

  return (
    <div className="soha-spatial-graphic-pod is-radar">
      <PodHeader
        title={pod.title}
        subtitle={pod.subtitle}
        actionLabel={pod.actionLabel}
        actionTo={pod.actionTo}
      />
      <div className="soha-spatial-radar-body">
        <div
          className={`soha-spatial-radar-scope ${hasAlerts ? 'is-alert' : 'is-calm'}`}
          aria-hidden="true"
        >
          <div className="soha-spatial-radar-sweep" />
          <svg viewBox="0 0 100 100" className="soha-spatial-radar-svg">
            <circle cx="50" cy="50" r="43" fill="none" stroke="currentColor" strokeOpacity="0.14" />
            <circle
              cx="50"
              cy="50"
              r="29"
              fill="none"
              stroke="currentColor"
              strokeOpacity="0.12"
              strokeDasharray="2 3"
            />
            <circle cx="50" cy="50" r="15" fill="none" stroke="currentColor" strokeOpacity="0.14" />
            <line x1="7" y1="50" x2="93" y2="50" stroke="currentColor" strokeOpacity="0.1" />
            <line x1="50" y1="7" x2="50" y2="93" stroke="currentColor" strokeOpacity="0.1" />
            <circle cx="50" cy="50" r="3.2" fill={hasAlerts ? '#f59e0b' : '#10b981'} />
            {blips.map((blip, idx) => {
              const angle = -0.75 + idx * 1.32
              const radius = blip.tone === 'danger' ? 19 : blip.tone === 'warning' ? 28 : 36
              const bx = 50 + Math.cos(angle) * radius
              const by = 50 + Math.sin(angle) * radius
              const color = TONE_STROKES[blip.tone]
              return (
                <g key={blip.key}>
                  <circle cx={bx} cy={by} r="7.5" fill={color} fillOpacity="0.2" />
                  <circle cx={bx} cy={by} r="3.4" fill={color} stroke="#ffffff" strokeWidth="1.2" />
                </g>
              )
            })}
          </svg>
        </div>

        <div className="soha-spatial-radar-targets">
          {blips.length === 0 ? (
            <div className="soha-spatial-radar-clear">
              <span className="soha-spatial-tone-dot is-success" aria-hidden="true" />
              <span>{pod.emptyLabel ?? '零异常信号 · 全域稳定'}</span>
            </div>
          ) : (
            blips.slice(0, 5).map((blip) => {
              const inner = (
                <>
                  <span className={`soha-spatial-tone-dot is-${blip.tone}`} aria-hidden="true" />
                  <span className="soha-spatial-radar-target__label" title={blip.label}>
                    {blip.label}
                  </span>
                  {blip.badge ? (
                    <span className={`soha-spatial-radar-target__badge is-${blip.tone}`}>
                      {blip.badge}
                    </span>
                  ) : null}
                </>
              )
              return blip.to ? (
                <Link key={blip.key} to={blip.to} className="soha-spatial-radar-target is-link">
                  {inner}
                </Link>
              ) : (
                <div key={blip.key} className="soha-spatial-radar-target">
                  {inner}
                </div>
              )
            })
          )}
        </div>
      </div>
    </div>
  )
}

export function SpatialGraphicDeck({ items }: { items: SpatialGraphicPod[] }) {
  if (!items.length) return null
  return (
    <div className="soha-overview-graphic-dock">
      {items.map((pod) => {
        if (pod.kind === 'rings') return <SpatialRingsPodView key={pod.key} pod={pod} />
        if (pod.kind === 'spectrum') return <SpatialSpectrumPodView key={pod.key} pod={pod} />
        return <SpatialRadarPodView key={pod.key} pod={pod} />
      })}
    </div>
  )
}
