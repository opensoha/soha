import { ArrowRightOutlined, CompassOutlined, InsertRowRightOutlined } from '@ant-design/icons'
import { Button, Tooltip } from 'antd'
import { useEffect, useId, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { useI18n } from '@/i18n'
import { useSplitCardDrawerStore } from '../split-card-drawer'
import {
  OverviewDomainArtwork,
  type OverviewArtworkVariant,
  type SpatialTelemetryNode,
} from './overview-artwork'
import type { OverviewTone } from './overview-types'
import { SpatialGraphicDeck, type SpatialGraphicPod } from './spatial-graphics'

export type SpectrumTone = 'success' | 'warning' | 'danger' | 'primary' | 'neutral'

export interface OverviewSpectrumSegment {
  key: string
  label: ReactNode
  value: number | string | undefined
  numericValue?: number
  tone: SpectrumTone
}

export interface OverviewHeroKpi {
  key: string
  label: ReactNode
  value: ReactNode
  tone?: SpectrumTone
}

export interface OverviewEmbeddedPod {
  key: string
  label: ReactNode
  value: ReactNode
  helper?: ReactNode
  icon?: ReactNode
  tone?: OverviewTone
  visual?: ReactNode
  to?: string
  ariaLabel?: string
  readonly?: boolean
  loading?: boolean
  className?: string
}

export interface OverviewHeroCardProps {
  variant: OverviewArtworkVariant
  eyebrowIcon?: ReactNode
  eyebrowLabel: ReactNode
  value: ReactNode
  valueSuffix?: ReactNode
  caption?: ReactNode
  segments?: OverviewSpectrumSegment[]
  kpis?: OverviewHeroKpi[]
  telemetryNodes?: SpatialTelemetryNode[]
  embeddedPods?: OverviewEmbeddedPod[]
  graphicDeck?: SpatialGraphicPod[]
  detailDrawerTitle?: ReactNode
  ctaLabel?: ReactNode
  ctaTo?: string
  ctaAriaLabel?: string
  ctaReadonly?: boolean
  onCtaClick?: () => void
  extraTopRight?: ReactNode
  interactiveHint?: ReactNode
  heroMetricClassName?: string
  className?: string
  loading?: boolean
  children?: ReactNode
}

export function OverviewSpectrumBar({ segments }: { segments: OverviewSpectrumSegment[] }) {
  const numericItems = segments.map((seg) => {
    const num =
      typeof seg.numericValue === 'number'
        ? seg.numericValue
        : typeof seg.value === 'number'
          ? seg.value
          : Number.NaN
    return {
      ...seg,
      resolvedNum: Number.isFinite(num) && num >= 0 ? num : 0,
    }
  })
  const total = numericItems.reduce((acc, item) => acc + item.resolvedNum, 0)

  return (
    <div className="soha-overview-spectrum">
      <div className="soha-overview-spectrum__track" aria-hidden="true">
        {total > 0 ? (
          numericItems
            .filter((item) => item.resolvedNum > 0)
            .map((item) => (
              <span
                key={item.key}
                className={`soha-overview-spectrum__bar is-${item.tone}`}
                style={{ flexGrow: Math.max(item.resolvedNum / total, 0.04) }}
              />
            ))
        ) : (
          <span className="soha-overview-spectrum__bar is-empty" style={{ flexGrow: 1 }} />
        )}
      </div>
      <div className="soha-overview-spectrum__legend">
        {segments.map((item) => (
          <span key={item.key} className={`soha-overview-spectrum__pill is-${item.tone}`}>
            <span className="soha-overview-spectrum__dot" aria-hidden="true" />
            <span className="soha-overview-spectrum__pill-label">{item.label}</span>
            <strong className="soha-overview-spectrum__pill-val">{item.value ?? '—'}</strong>
          </span>
        ))}
      </div>
    </div>
  )
}

export function OverviewNodeMatrix({
  items,
  maxDots = 18,
}: {
  items: Array<{ count?: number; tone: SpectrumTone }>
  maxDots?: number
}) {
  const valid = items.filter(
    (item) => typeof item.count === 'number' && Number.isFinite(item.count) && item.count > 0,
  )
  const total = valid.reduce((sum, item) => sum + (item.count ?? 0), 0)
  if (total <= 0) return null

  const scale = total > maxDots ? maxDots / total : 1
  const dots: SpectrumTone[] = []
  valid.forEach((item) => {
    const n = Math.max(1, Math.round((item.count ?? 0) * scale))
    for (let i = 0; i < n && dots.length < maxDots; i += 1) {
      dots.push(item.tone)
    }
  })

  return (
    <div className="soha-overview-node-matrix" aria-hidden="true">
      {dots.map((tone, idx) => (
        <span key={idx} className={`soha-overview-node-matrix__dot is-${tone}`} />
      ))}
    </div>
  )
}

function SpatialDashboardPod({ pod }: { pod: OverviewEmbeddedPod }) {
  const toneClass = pod.tone && pod.tone !== 'default' ? `is-${pod.tone}` : ''
  const inner = (
    <div
      className={['soha-overview-spatial-pod', toneClass, pod.className].filter(Boolean).join(' ')}
    >
      <div className="soha-overview-spatial-pod__head">
        <div className="soha-overview-spatial-pod__copy">
          <span className="soha-overview-spatial-pod__label">{pod.label}</span>
          <strong className="soha-overview-spatial-pod__value">
            {pod.loading ? '…' : pod.value}
          </strong>
        </div>
        {pod.visual ? (
          <div className="soha-overview-spatial-pod__visual">{pod.visual}</div>
        ) : pod.icon ? (
          <span className="soha-overview-spatial-pod__icon">{pod.icon}</span>
        ) : null}
      </div>
      {pod.helper ? <div className="soha-overview-spatial-pod__helper">{pod.helper}</div> : null}
    </div>
  )

  if (pod.to && !pod.readonly) {
    return (
      <Link to={pod.to} aria-label={pod.ariaLabel} className="soha-overview-spatial-pod-link">
        {inner}
      </Link>
    )
  }

  return <div className="soha-overview-spatial-pod-wrap">{inner}</div>
}

export function OverviewHeroCard({
  variant,
  eyebrowIcon,
  eyebrowLabel,
  value,
  valueSuffix,
  caption,
  segments,
  kpis,
  telemetryNodes,
  embeddedPods,
  graphicDeck,
  detailDrawerTitle,
  ctaLabel,
  ctaTo,
  ctaAriaLabel,
  ctaReadonly,
  onCtaClick,
  extraTopRight,
  interactiveHint,
  heroMetricClassName,
  className,
  children,
}: OverviewHeroCardProps) {
  const { localeCode } = useI18n()
  const drawerOwnerKey = useId()
  const openSplitCard = useSplitCardDrawerStore((state) => state.openSplitCard)
  const drawerOpen = useSplitCardDrawerStore((state) => state.open)
  const activeDrawerOwnerKey = useSplitCardDrawerStore((state) => state.payload?.ownerKey)
  const hasEmbeddedPods = Boolean(embeddedPods && embeddedPods.length > 0)
  const hasGraphicDeck = Boolean(graphicDeck && graphicDeck.length > 0)
  const hasSpatialDeck = Boolean(children) && !hasGraphicDeck

  const openRawDetailCard = () => {
    if (!children) return
    openSplitCard({
      ownerKey: drawerOwnerKey,
      title: detailDrawerTitle ?? eyebrowLabel,
      body: <div className="soha-overview-drawer-details">{children}</div>,
    })
  }

  useEffect(() => {
    if (!drawerOpen || activeDrawerOwnerKey !== drawerOwnerKey || !children) return
    openSplitCard({
      ownerKey: drawerOwnerKey,
      title: detailDrawerTitle ?? eyebrowLabel,
      body: <div className="soha-overview-drawer-details">{children}</div>,
    })
  }, [
    drawerOpen,
    activeDrawerOwnerKey,
    drawerOwnerKey,
    children,
    detailDrawerTitle,
    eyebrowLabel,
    openSplitCard,
  ])

  useEffect(
    () => () => {
      if (useSplitCardDrawerStore.getState().payload?.ownerKey === drawerOwnerKey) {
        useSplitCardDrawerStore.getState().resetSplitCard()
      }
    },
    [drawerOwnerKey],
  )

  const ctaNode =
    !hasGraphicDeck && !hasSpatialDeck && ctaLabel ? (
      ctaTo && !ctaReadonly ? (
        <Link to={ctaTo} aria-label={ctaAriaLabel} className="soha-overview-hero-cta">
          <span>{ctaLabel}</span>
          <span className="soha-overview-hero-cta__circle" aria-hidden="true">
            <ArrowRightOutlined />
          </span>
        </Link>
      ) : onCtaClick && !ctaReadonly ? (
        <button
          type="button"
          aria-label={ctaAriaLabel}
          onClick={onCtaClick}
          className="soha-overview-hero-cta"
        >
          <span>{ctaLabel}</span>
          <span className="soha-overview-hero-cta__circle" aria-hidden="true">
            <ArrowRightOutlined />
          </span>
        </button>
      ) : (
        <span className="soha-overview-hero-cta is-readonly" aria-hidden="true">
          <span>{ctaLabel}</span>
          <span className="soha-overview-hero-cta__circle">
            <ArrowRightOutlined />
          </span>
        </span>
      )
    ) : null

  return (
    <div
      className={[
        'soha-overview-hero-shell',
        hasEmbeddedPods ? 'is-panoramic-stage' : '',
        hasSpatialDeck || hasGraphicDeck ? 'is-full-spatial-dashboard' : '',
        hasGraphicDeck ? 'is-graphic-cockpit' : '',
        `is-${variant}`,
        className,
      ]
        .filter(Boolean)
        .join(' ')}
    >
      <div className="soha-overview-hero-card">
        <div className="soha-overview-hero-art-stage">
          <OverviewDomainArtwork
            variant={variant}
            telemetryNodes={telemetryNodes}
            centerBias={hasEmbeddedPods ? 0.465 : 0.65}
          />
        </div>

        <div className="soha-overview-stage-grid">
          <div
            className={['soha-overview-hero-body', heroMetricClassName].filter(Boolean).join(' ')}
          >
            <div className="soha-overview-hero-top">
              <span className="soha-overview-hero-eyebrow">
                {eyebrowIcon ? (
                  <span className="soha-overview-hero-eyebrow__icon">{eyebrowIcon}</span>
                ) : null}
                <span>{eyebrowLabel}</span>
              </span>
              {extraTopRight || (hasGraphicDeck && children) ? (
                <div className="soha-overview-hero-extra">
                  {extraTopRight}
                  {hasGraphicDeck && children ? (
                    <Tooltip
                      title={
                        localeCode === 'zh_CN'
                          ? '在右侧并排卡片岛展开明细数据'
                          : 'Expand details in right card panel'
                      }
                    >
                      <Button
                        size="small"
                        type="text"
                        icon={<InsertRowRightOutlined />}
                        className="soha-overview-detail-drawer-trigger"
                        onClick={openRawDetailCard}
                      >
                        {localeCode === 'zh_CN' ? '明细卡' : 'Details'}
                      </Button>
                    </Tooltip>
                  ) : null}
                </div>
              ) : null}
            </div>

            <div className="soha-overview-hero-main">
              <div className="soha-overview-hero-figure-row">
                <strong className="soha-overview-hero-value">{value}</strong>
                {valueSuffix ? (
                  <span className="soha-overview-hero-suffix">{valueSuffix}</span>
                ) : null}
              </div>
              {caption ? <div className="soha-overview-hero-caption">{caption}</div> : null}
            </div>

            {kpis?.length ? (
              <div className="soha-overview-hero-kpis">
                {kpis.map((kpi) => (
                  <div
                    key={kpi.key}
                    className={`soha-overview-hero-kpi ${kpi.tone ? `is-${kpi.tone}` : ''}`}
                  >
                    <span className="soha-overview-hero-kpi__label">{kpi.label}</span>
                    <strong className="soha-overview-hero-kpi__val">{kpi.value}</strong>
                  </div>
                ))}
              </div>
            ) : null}

            <div className="soha-overview-hero-footer">
              {segments?.length ? (
                <div className="soha-overview-hero-footer__left">
                  <OverviewSpectrumBar segments={segments} />
                </div>
              ) : null}
              <div className="soha-overview-hero-actions-row">
                {ctaNode}
                <span className="soha-overview-stage-hint" aria-hidden="true">
                  <CompassOutlined />
                  <span>
                    {interactiveHint ??
                      (localeCode === 'zh_CN'
                        ? '拖拽旋转 · 点击 3D 节点下钻'
                        : 'Drag to rotate · Click 3D nodes to drill down')}
                  </span>
                </span>
              </div>
            </div>
          </div>

          {hasEmbeddedPods ? (
            <div
              className={[
                'soha-overview-spatial-pods',
                embeddedPods!.length >= 4 ? 'is-two-col' : '',
              ]
                .filter(Boolean)
                .join(' ')}
            >
              {embeddedPods!.map((pod) => (
                <SpatialDashboardPod key={pod.key} pod={pod} />
              ))}
            </div>
          ) : null}
        </div>

        {hasGraphicDeck ? (
          <SpatialGraphicDeck items={graphicDeck!} />
        ) : hasSpatialDeck ? (
          <div className="soha-overview-spatial-deck">{children}</div>
        ) : null}
      </div>
    </div>
  )
}
