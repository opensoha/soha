import { ArrowRightOutlined } from '@ant-design/icons'
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useI18n } from '@/i18n'
import { renderAINeuralScene } from './scenes/ai-neural-scene'
import { renderComputeRackScene } from './scenes/compute-rack-scene'
import { renderDeliveryPipelineScene } from './scenes/delivery-pipeline-scene'
import { renderIdentityScene, renderGovernanceScene } from './scenes/identity-governance-scenes'
import {
  type OverviewArtworkVariant,
  type SceneHitTarget,
  type ScenePalette,
  type SpatialNodeStatus,
  type SpatialTelemetryNode,
} from './scenes/math3d'
import { renderMonitoringTelemetryScene } from './scenes/monitoring-telemetry-scene'
import { renderPlatformK8sScene } from './scenes/platform-k8s-scene'

export type { OverviewArtworkVariant, SpatialNodeStatus, SpatialTelemetryNode }

interface HoveredNodeHud {
  x: number
  y: number
  title: string
  metric?: string
  status: SpatialNodeStatus
  clickable: boolean
}

interface CameraPreset {
  pitch: number
  yaw: number
  swayAmp: number
  swaySpeed: number
  continuousOrbit?: number
}

function getCameraPreset(variant: OverviewArtworkVariant): CameraPreset {
  switch (variant) {
    case 'platform':
      return { pitch: 0.5, yaw: -0.46, swayAmp: 0, swaySpeed: 0, continuousOrbit: 0.065 }
    case 'compute':
      return { pitch: 0.44, yaw: -0.34, swayAmp: 0.08, swaySpeed: 0.36 }
    case 'delivery':
      return { pitch: 0.42, yaw: -0.26, swayAmp: 0.06, swaySpeed: 0.32 }
    case 'monitoring':
      return { pitch: 0.46, yaw: -0.3, swayAmp: 0.065, swaySpeed: 0.3 }
    case 'ai':
      return { pitch: 0.26, yaw: -0.18, swayAmp: 0.07, swaySpeed: 0.34 }
    case 'governance':
      return { pitch: 0.44, yaw: -0.4, swayAmp: 0, swaySpeed: 0, continuousOrbit: 0.055 }
    case 'identity':
    case 'security':
    default:
      return { pitch: 0.34, yaw: -0.32, swayAmp: 0.065, swaySpeed: 0.32 }
  }
}

function getDomainPalette(variant: OverviewArtworkVariant, isPrismLight: boolean): ScenePalette {
  const stroke = isPrismLight ? '71, 85, 105' : '148, 163, 184'
  const surfaceDark = isPrismLight ? '226, 232, 240' : '15, 23, 42'
  const emerald = isPrismLight ? '5, 150, 105' : '52, 211, 153'
  const amber = isPrismLight ? '217, 119, 6' : '251, 191, 36'
  const rose = isPrismLight ? '225, 29, 72' : '251, 113, 133'

  switch (variant) {
    case 'platform':
      // Kubernetes Helm Cobalt & Sky Cyan
      return {
        isLight: isPrismLight,
        primary: isPrismLight ? '37, 99, 235' : '96, 165, 250',
        secondary: isPrismLight ? '2, 132, 199' : '56, 189, 248',
        emerald,
        amber,
        rose,
        surfaceDark,
        stroke,
      }
    case 'compute':
      // Hardware NVIDIA Teal-Emerald & Silicon Gold
      return {
        isLight: isPrismLight,
        primary: isPrismLight ? '13, 148, 136' : '45, 212, 191',
        secondary: isPrismLight ? '5, 150, 105' : '16, 185, 129',
        emerald,
        amber,
        rose,
        surfaceDark,
        stroke,
      }
    case 'delivery':
      // CI/CD Pipeline Violet-Orchid & Canary Amber
      return {
        isLight: isPrismLight,
        primary: isPrismLight ? '124, 58, 237' : '192, 132, 252',
        secondary: isPrismLight ? '79, 70, 229' : '129, 140, 248',
        emerald,
        amber,
        rose,
        surfaceDark,
        stroke,
      }
    case 'monitoring':
      // Telemetry Oscilloscope Phosphor Cyan & Ember Rose
      return {
        isLight: isPrismLight,
        primary: isPrismLight ? '8, 145, 178' : '34, 211, 238',
        secondary: isPrismLight ? '37, 99, 235' : '96, 165, 250',
        emerald,
        amber,
        rose,
        surfaceDark,
        stroke,
      }
    case 'ai':
      // Heya Neural Aurora Pink-Magenta & Violet
      return {
        isLight: isPrismLight,
        primary: isPrismLight ? '147, 51, 234' : '244, 114, 182',
        secondary: isPrismLight ? '79, 70, 229' : '167, 139, 250',
        emerald: isPrismLight ? '2, 132, 199' : '56, 189, 248',
        amber,
        rose,
        surfaceDark,
        stroke,
      }
    case 'governance':
      // Governance Chronometer Champagne-Gold & Platinum
      return {
        isLight: isPrismLight,
        primary: isPrismLight ? '79, 70, 229' : '165, 180, 252',
        secondary: isPrismLight ? '71, 85, 105' : '203, 213, 225',
        emerald,
        amber,
        rose,
        surfaceDark,
        stroke,
      }
    case 'identity':
    case 'security':
    default:
      // Zero-Trust Security Glass Mint & Ice-Blue
      return {
        isLight: isPrismLight,
        primary: isPrismLight ? '2, 132, 199' : '56, 189, 248',
        secondary: isPrismLight ? '5, 150, 105' : '52, 211, 153',
        emerald,
        amber,
        rose,
        surfaceDark,
        stroke,
      }
  }
}

export function OverviewDomainArtwork({
  variant,
  telemetryNodes,
  centerBias = 0.51,
  className,
}: {
  variant: OverviewArtworkVariant
  telemetryNodes?: SpatialTelemetryNode[]
  centerBias?: number
  className?: string
}) {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const containerRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const telemetryRef = useRef<SpatialTelemetryNode[] | undefined>(telemetryNodes)
  telemetryRef.current = telemetryNodes

  const [hoveredHud, setHoveredHud] = useState<HoveredNodeHud | null>(null)
  const hoveredIdRef = useRef<string | null>(null)

  useEffect(() => {
    const container = containerRef.current
    const canvas = canvasRef.current
    if (!container || !canvas) return

    let ctx: CanvasRenderingContext2D | null = null
    try {
      ctx = canvas.getContext('2d')
    } catch {
      ctx = null
    }
    if (!ctx) return

    const cam = getCameraPreset(variant)

    let width = 1160
    let height = 540
    let dpr = 1
    let rafId = 0
    let visible = true

    let dragYaw = 0
    let dragPitch = 0
    let isDragging = false
    let dragMoved = false
    let lastDragX = 0
    let lastDragY = 0
    let pointerYawTarget = 0
    let pointerPitchTarget = 0
    let smoothYawOffset = 0
    let smoothPitchOffset = 0
    let orbitAccum = 0
    let lastFrameSec = 0

    let hitTargets: SceneHitTarget[] = []

    const syncSize = () => {
      const rect = container.getBoundingClientRect()
      if (rect.width > 20 && rect.height > 20) {
        width = rect.width
        height = rect.height
      }
      dpr = Math.min(window.devicePixelRatio || 1, 2)
      canvas.width = Math.round(width * dpr)
      canvas.height = Math.round(height * dpr)
    }

    syncSize()

    const renderFrame = (timeSec: number) => {
      if (!ctx) return
      const dt = lastFrameSec > 0 ? Math.min(0.05, timeSec - lastFrameSec) : 0.016
      lastFrameSec = timeSec

      const isPrismLight =
        document.documentElement.dataset.overviewHero !== 'spotlight' &&
        document.documentElement.dataset.themeMode !== 'dark'
      const basePalette = getDomainPalette(variant, isPrismLight)
      const gradMode = document.documentElement.dataset.overviewGradient
      let palette = basePalette
      if (gradMode && gradMode !== 'workbench') {
        const rs = window.getComputedStyle(document.documentElement)
        const c1Rgb = rs.getPropertyValue('--soha-stage-custom-c1-rgb').trim()
        const c2Rgb = rs.getPropertyValue('--soha-stage-custom-c2-rgb').trim()
        if (c1Rgb && c2Rgb) {
          palette = {
            ...basePalette,
            primary: c1Rgb,
            secondary: c2Rgb,
          }
        }
      }

      if (cam.continuousOrbit && !isDragging) {
        orbitAccum += dt * cam.continuousOrbit * (hoveredIdRef.current ? 0.15 : 1)
      }

      smoothYawOffset += (pointerYawTarget + dragYaw - smoothYawOffset) * 0.09
      smoothPitchOffset += (pointerPitchTarget + dragPitch - smoothPitchOffset) * 0.09

      const sway = cam.swayAmp > 0 ? Math.sin(timeSec * cam.swaySpeed) * cam.swayAmp : 0
      const yaw = cam.yaw + orbitAccum + sway + smoothYawOffset
      const pitch = Math.max(
        0.14,
        Math.min(0.78, cam.pitch + Math.cos(timeSec * 0.3) * 0.02 + smoothPitchOffset),
      )

      const cx = width * (width < 980 ? 0.52 : centerBias)
      const cy = height * 0.48
      const sceneScale = Math.min(width / 920, height / 460) * 1.18

      const nodesMap = new Map<string, SpatialTelemetryNode>()
      telemetryRef.current?.forEach((n) => nodesMap.set(n.id, n))
      hitTargets = []

      ctx.save()
      ctx.scale(dpr, dpr)
      ctx.clearRect(0, 0, width, height)

      const renderArgs = {
        ctx,
        width,
        height,
        cx,
        cy,
        sceneScale,
        timeSec,
        pitch,
        yaw,
        hoveredId: hoveredIdRef.current,
        palette,
        nodesMap,
        hitTargets,
      }

      switch (variant) {
        case 'platform':
          renderPlatformK8sScene(renderArgs)
          break
        case 'compute':
          renderComputeRackScene(renderArgs)
          break
        case 'delivery':
          renderDeliveryPipelineScene(renderArgs)
          break
        case 'monitoring':
          renderMonitoringTelemetryScene(renderArgs)
          break
        case 'ai':
          renderAINeuralScene(renderArgs)
          break
        case 'governance':
          renderGovernanceScene(renderArgs)
          break
        case 'identity':
        case 'security':
        default:
          renderIdentityScene(renderArgs)
          break
      }

      ctx.restore()
    }

    const pickNodeAt = (clientX: number, clientY: number) => {
      const rect = canvas.getBoundingClientRect()
      const mx = clientX - rect.left
      const my = clientY - rect.top
      const candidates = [...hitTargets].sort((a, b) => a.z - b.z)
      for (const item of candidates) {
        const dx = mx - item.x
        const dy = my - item.y
        if (dx * dx + dy * dy <= item.hitR * item.hitR) {
          return item
        }
      }
      return undefined
    }

    const onPointerDown = (e: PointerEvent) => {
      isDragging = true
      dragMoved = false
      lastDragX = e.clientX
      lastDragY = e.clientY
    }

    const onPointerMove = (e: PointerEvent) => {
      const rect = canvas.getBoundingClientRect()
      if (rect.width <= 0 || rect.height <= 0) return

      if (isDragging) {
        const dx = e.clientX - lastDragX
        const dy = e.clientY - lastDragY
        if (Math.abs(dx) + Math.abs(dy) > 3) dragMoved = true
        const maxYawDrag = cam.continuousOrbit ? Math.PI * 2 : 0.48
        dragYaw = Math.max(-maxYawDrag, Math.min(maxYawDrag, dragYaw + dx * 0.0055))
        dragPitch = Math.max(-0.28, Math.min(0.32, dragPitch - dy * 0.0045))
        lastDragX = e.clientX
        lastDragY = e.clientY
        return
      }

      const nx = ((e.clientX - rect.left) / rect.width - 0.5) * 2
      const ny = ((e.clientY - rect.top) / rect.height - 0.5) * 2
      pointerYawTarget = nx * 0.16
      pointerPitchTarget = -ny * 0.1

      const picked = pickNodeAt(e.clientX, e.clientY)
      if (picked) {
        hoveredIdRef.current = picked.id
        canvas.style.cursor = picked.path ? 'pointer' : 'grab'
        setHoveredHud({
          x: picked.x,
          y: picked.y - 22,
          title: picked.title,
          metric: picked.metric,
          status: picked.status,
          clickable: Boolean(picked.path),
        })
      } else {
        if (hoveredIdRef.current !== null) {
          hoveredIdRef.current = null
          setHoveredHud(null)
        }
        canvas.style.cursor = 'grab'
      }
    }

    const onPointerUp = (e: PointerEvent) => {
      const wasClick = isDragging && !dragMoved
      isDragging = false
      if (wasClick) {
        const picked = pickNodeAt(e.clientX, e.clientY)
        if (picked?.path) {
          navigate(picked.path)
        }
      }
    }

    const onPointerLeave = () => {
      isDragging = false
      pointerYawTarget = 0
      pointerPitchTarget = 0
      if (hoveredIdRef.current !== null) {
        hoveredIdRef.current = null
        setHoveredHud(null)
      }
    }

    canvas.addEventListener('pointerdown', onPointerDown)
    window.addEventListener('pointermove', onPointerMove, { passive: true })
    window.addEventListener('pointerup', onPointerUp, { passive: true })
    container.addEventListener('pointerleave', onPointerLeave, { passive: true })

    let ro: ResizeObserver | null = null
    if (typeof ResizeObserver !== 'undefined') {
      ro = new ResizeObserver(() => {
        syncSize()
        renderFrame(performance.now() * 0.001)
      })
      ro.observe(container)
    }

    let io: IntersectionObserver | null = null
    if (typeof IntersectionObserver !== 'undefined') {
      io = new IntersectionObserver(
        (entries) => {
          visible = entries[0]?.isIntersecting ?? true
        },
        { threshold: 0.02 },
      )
      io.observe(container)
    }

    const reduceMotion =
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches

    if (reduceMotion) {
      renderFrame(2.4)
    } else {
      const tick = (nowMs: number) => {
        if (visible && !document.hidden) {
          renderFrame(nowMs * 0.001)
        }
        rafId = window.requestAnimationFrame(tick)
      }
      rafId = window.requestAnimationFrame(tick)
    }

    return () => {
      if (rafId) window.cancelAnimationFrame(rafId)
      ro?.disconnect()
      io?.disconnect()
      canvas.removeEventListener('pointerdown', onPointerDown)
      window.removeEventListener('pointermove', onPointerMove)
      window.removeEventListener('pointerup', onPointerUp)
      container.removeEventListener('pointerleave', onPointerLeave)
    }
  }, [variant, centerBias, navigate])

  return (
    <div
      ref={containerRef}
      className={['soha-overview-hero-art', `is-${variant}`, className].filter(Boolean).join(' ')}
    >
      <div
        className="soha-overview-hero-aurora soha-overview-hero-aurora--primary"
        aria-hidden="true"
      />
      <div
        className="soha-overview-hero-aurora soha-overview-hero-aurora--secondary"
        aria-hidden="true"
      />
      <canvas ref={canvasRef} className="soha-overview-hero-canvas" />
      {hoveredHud ? (
        <div
          className={`soha-overview-3d-tooltip is-${hoveredHud.status}`}
          style={{
            left: `${Math.max(96, Math.min(hoveredHud.x, 960))}px`,
            top: `${Math.max(48, hoveredHud.y)}px`,
          }}
        >
          <div className="soha-overview-3d-tooltip__head">
            <span className="soha-overview-3d-tooltip__dot" />
            <strong>{hoveredHud.title}</strong>
          </div>
          {hoveredHud.metric ? (
            <div className="soha-overview-3d-tooltip__metric">{hoveredHud.metric}</div>
          ) : null}
          {hoveredHud.clickable ? (
            <div className="soha-overview-3d-tooltip__hint">
              <span>{localeCode === 'zh_CN' ? '点击下钻进入视图' : 'Click to drill down'}</span>
              <ArrowRightOutlined />
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}
