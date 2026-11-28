export type OverviewArtworkVariant =
  | 'platform'
  | 'compute'
  | 'delivery'
  | 'monitoring'
  | 'ai'
  | 'identity'
  | 'governance'
  | 'security'

export type SpatialNodeStatus = 'primary' | 'healthy' | 'warning' | 'critical'

export interface SpatialTelemetryNode {
  id: string
  label?: string
  title?: string
  metric?: string
  status?: SpatialNodeStatus
  path?: string
}

export interface Vec3 {
  x: number
  y: number
  z: number
}

export interface ProjPt {
  x: number
  y: number
  z: number
  scale: number
  depthAlpha: number
}

export interface SceneHitTarget {
  id: string
  x: number
  y: number
  hitR: number
  z: number
  label?: string
  title: string
  metric?: string
  status: SpatialNodeStatus
  path?: string
}

export interface ScenePalette {
  isLight: boolean
  primary: string
  secondary: string
  emerald: string
  amber: string
  rose: string
  surfaceDark: string
  stroke: string
}

export interface SceneRenderArgs {
  ctx: CanvasRenderingContext2D
  width: number
  height: number
  cx: number
  cy: number
  sceneScale: number
  timeSec: number
  pitch: number
  yaw: number
  hoveredId: string | null
  palette: ScenePalette
  nodesMap: Map<string, SpatialTelemetryNode>
  hitTargets: SceneHitTarget[]
}

export function rgba(rgb: string, alpha: number): string {
  const clamped = Math.max(0, Math.min(1, alpha))
  return `rgba(${rgb}, ${clamped.toFixed(3)})`
}

export function statusRgb(status: SpatialNodeStatus, palette: ScenePalette): string {
  if (status === 'critical') return palette.rose
  if (status === 'warning') return palette.amber
  if (status === 'healthy') return palette.emerald
  return palette.primary
}

export function rotate3D(p: Vec3, pitch: number, yaw: number): Vec3 {
  const cy = Math.cos(yaw)
  const sy = Math.sin(yaw)
  const x1 = p.x * cy + p.z * sy
  const z1 = -p.x * sy + p.z * cy

  const cp = Math.cos(pitch)
  const sp = Math.sin(pitch)
  const y2 = p.y * cp - z1 * sp
  const z2 = p.y * sp + z1 * cp

  return { x: x1, y: y2, z: z2 }
}

export function createProjector(
  cx: number,
  cy: number,
  sceneScale: number,
  focalLength = 680,
): (p: Vec3) => ProjPt {
  return (p: Vec3): ProjPt => {
    const zCam = p.z
    const scale = (focalLength / Math.max(200, focalLength + zCam)) * sceneScale
    const depthAlpha = Math.max(0.24, Math.min(1, 1 - (zCam + 220) / 560))
    return {
      x: cx + p.x * scale,
      y: cy + p.y * scale,
      z: zCam,
      scale,
      depthAlpha,
    }
  }
}

export interface Box3DStyle {
  topRgb: string
  sideRgb?: string
  edgeRgb: string
  topAlpha: number
  litAlpha: number
  darkAlpha: number
  edgeAlpha: number
  localYaw?: number
}

/**
 * Renders a shaded 3D box (6 faces, back-face culled) anchored at `baseCenter` (bottom center, +Y down).
 */
export function drawBox3D(
  ctx: CanvasRenderingContext2D,
  baseCenter: Vec3,
  halfW: number,
  height: number,
  halfD: number,
  pitch: number,
  yaw: number,
  project: (p: Vec3) => ProjPt,
  style: Box3DStyle,
): { topWorld: Vec3; topProj: ProjPt; baseProj: ProjPt; avgZ: number } {
  const ly = style.localYaw ?? 0
  const cl = Math.cos(ly)
  const sl = Math.sin(ly)

  const toWorld = (lx: number, yOff: number, lz: number): Vec3 => {
    const rx = lx * cl + lz * sl
    const rz = -lx * sl + lz * cl
    return rotate3D(
      { x: baseCenter.x + rx, y: baseCenter.y + yOff, z: baseCenter.z + rz },
      pitch,
      yaw,
    )
  }

  const h = -Math.max(1, height)
  const cw: Vec3[] = [
    toWorld(-halfW, h, -halfD),
    toWorld(halfW, h, -halfD),
    toWorld(halfW, h, halfD),
    toWorld(-halfW, h, halfD),
    toWorld(-halfW, 0, -halfD),
    toWorld(halfW, 0, -halfD),
    toWorld(halfW, 0, halfD),
    toWorld(-halfW, 0, halfD),
  ]
  const cp = cw.map(project)
  const sideRgb = style.sideRgb ?? style.topRgb

  const faces: Array<{ idx: [number, number, number, number]; alpha: number; rgb: string }> = [
    { idx: [0, 1, 2, 3], alpha: style.topAlpha, rgb: style.topRgb },
    { idx: [3, 2, 6, 7], alpha: style.litAlpha, rgb: sideRgb },
    { idx: [2, 1, 5, 6], alpha: style.darkAlpha, rgb: sideRgb },
    { idx: [1, 0, 4, 5], alpha: style.darkAlpha * 0.85, rgb: sideRgb },
    { idx: [0, 3, 7, 4], alpha: style.litAlpha * 0.92, rgb: sideRgb },
  ]

  const visible = faces
    .map((f) => {
      const p0 = cp[f.idx[0]]
      const p1 = cp[f.idx[1]]
      const p2 = cp[f.idx[2]]
      const p3 = cp[f.idx[3]]
      const cross = (p1.x - p0.x) * (p2.y - p0.y) - (p1.y - p0.y) * (p2.x - p0.x)
      const fz = (cw[f.idx[0]].z + cw[f.idx[1]].z + cw[f.idx[2]].z + cw[f.idx[3]].z) * 0.25
      return { ...f, pts: [p0, p1, p2, p3], cross, fz }
    })
    .filter((f) => f.cross > 0)
    .sort((a, b) => b.fz - a.fz)

  visible.forEach((f) => {
    ctx.beginPath()
    ctx.moveTo(f.pts[0].x, f.pts[0].y)
    ctx.lineTo(f.pts[1].x, f.pts[1].y)
    ctx.lineTo(f.pts[2].x, f.pts[2].y)
    ctx.lineTo(f.pts[3].x, f.pts[3].y)
    ctx.closePath()
    ctx.fillStyle = rgba(f.rgb, f.alpha)
    ctx.fill()
    if (style.edgeAlpha > 0.01) {
      ctx.lineWidth = 0.95
      ctx.strokeStyle = rgba(style.edgeRgb, style.edgeAlpha)
      ctx.stroke()
    }
  })

  const topWorld = toWorld(0, h, 0)
  const topProj = project(topWorld)
  const baseProj = project(toWorld(0, 0, 0))
  const avgZ = cw.reduce((acc, v) => acc + v.z, 0) / 8
  return { topWorld, topProj, baseProj, avgZ }
}

/**
 * Renders an N-sided extruded 3D prism (e.g. 6 = Hexagon for K8s Node, 7 = Heptagon for K8s Control Plane).
 */
export function drawPolygonPrism3D(
  ctx: CanvasRenderingContext2D,
  baseCenter: Vec3,
  sides: number,
  radius: number,
  height: number,
  pitch: number,
  yaw: number,
  project: (p: Vec3) => ProjPt,
  style: {
    topRgb: string
    sideRgb?: string
    edgeRgb: string
    topAlpha: number
    sideAlpha: number
    edgeAlpha: number
    phaseAngle?: number
  },
): { topWorld: Vec3; topProj: ProjPt; avgZ: number } {
  const h = -Math.max(1, height)
  const phase = style.phaseAngle ?? 0
  const sideRgb = style.sideRgb ?? style.topRgb

  const topW: Vec3[] = []
  const botW: Vec3[] = []
  for (let i = 0; i < sides; i += 1) {
    const a = phase + (i / sides) * Math.PI * 2
    const lx = Math.cos(a) * radius
    const lz = Math.sin(a) * radius
    topW.push(
      rotate3D({ x: baseCenter.x + lx, y: baseCenter.y + h, z: baseCenter.z + lz }, pitch, yaw),
    )
    botW.push(rotate3D({ x: baseCenter.x + lx, y: baseCenter.y, z: baseCenter.z + lz }, pitch, yaw))
  }
  const topP = topW.map(project)
  const botP = botW.map(project)

  // Side walls (depth-sorted)
  const walls: Array<{ pts: [ProjPt, ProjPt, ProjPt, ProjPt]; fz: number; shade: number }> = []
  for (let i = 0; i < sides; i += 1) {
    const ni = (i + 1) % sides
    const p0 = topP[ni]
    const p1 = topP[i]
    const p2 = botP[i]
    const p3 = botP[ni]
    const cross = (p1.x - p0.x) * (p2.y - p0.y) - (p1.y - p0.y) * (p2.x - p0.x)
    if (cross > 0) {
      const fz = (topW[i].z + topW[ni].z + botW[i].z + botW[ni].z) * 0.25
      const nx = topW[ni].x - topW[i].x
      const shade = nx > 0 ? 1.15 : 0.78
      walls.push({ pts: [p0, p1, p2, p3], fz, shade })
    }
  }
  walls.sort((a, b) => b.fz - a.fz)

  walls.forEach((w) => {
    ctx.beginPath()
    ctx.moveTo(w.pts[0].x, w.pts[0].y)
    ctx.lineTo(w.pts[1].x, w.pts[1].y)
    ctx.lineTo(w.pts[2].x, w.pts[2].y)
    ctx.lineTo(w.pts[3].x, w.pts[3].y)
    ctx.closePath()
    ctx.fillStyle = rgba(sideRgb, style.sideAlpha * w.shade)
    ctx.fill()
    if (style.edgeAlpha > 0.01) {
      ctx.lineWidth = 0.9
      ctx.strokeStyle = rgba(style.edgeRgb, style.edgeAlpha * 0.85)
      ctx.stroke()
    }
  })

  // Top polygon face
  ctx.beginPath()
  topP.forEach((pt, idx) => {
    if (idx === 0) ctx.moveTo(pt.x, pt.y)
    else ctx.lineTo(pt.x, pt.y)
  })
  ctx.closePath()
  ctx.fillStyle = rgba(style.topRgb, style.topAlpha)
  ctx.fill()
  if (style.edgeAlpha > 0.01) {
    ctx.lineWidth = 1.15
    ctx.strokeStyle = rgba(style.edgeRgb, style.edgeAlpha)
    ctx.stroke()
  }

  const topWorld = rotate3D({ x: baseCenter.x, y: baseCenter.y + h, z: baseCenter.z }, pitch, yaw)
  const topProj = project(topWorld)
  const avgZ = topW.reduce((s, v) => s + v.z, 0) / sides
  return { topWorld, topProj, avgZ }
}

/**
 * Draws a polyline in 3D space (e.g. 90-degree orthogonal PCB/switch fiber trace) + animated data pulse.
 */
export function drawPath3DWithPulse(
  ctx: CanvasRenderingContext2D,
  waypoints: Vec3[],
  pitch: number,
  yaw: number,
  project: (p: Vec3) => ProjPt,
  options: {
    rgb: string
    lineAlpha: number
    lineWidth?: number
    pulseProgress?: number
    pulseRgb?: string
    dashed?: boolean
  },
) {
  if (waypoints.length < 2) return
  const rotPts = waypoints.map((w) => rotate3D(w, pitch, yaw))
  const projPts = rotPts.map(project)

  ctx.save()
  if (options.dashed) ctx.setLineDash([4, 4])
  ctx.beginPath()
  projPts.forEach((p, i) => {
    if (i === 0) ctx.moveTo(p.x, p.y)
    else ctx.lineTo(p.x, p.y)
  })
  ctx.lineWidth = options.lineWidth ?? 1.4
  ctx.strokeStyle = rgba(options.rgb, options.lineAlpha)
  ctx.stroke()
  ctx.restore()

  if (options.pulseProgress !== undefined) {
    const segLens: number[] = []
    let totalLen = 0
    for (let i = 0; i < waypoints.length - 1; i += 1) {
      const d = Math.hypot(
        waypoints[i + 1].x - waypoints[i].x,
        waypoints[i + 1].y - waypoints[i].y,
        waypoints[i + 1].z - waypoints[i].z,
      )
      segLens.push(d)
      totalLen += d
    }
    if (totalLen <= 0) return

    const sampleAt = (u: number): ProjPt => {
      const target = Math.max(0, Math.min(1, u)) * totalLen
      let acc = 0
      for (let i = 0; i < segLens.length; i += 1) {
        if (acc + segLens[i] >= target || i === segLens.length - 1) {
          const t = segLens[i] > 0 ? (target - acc) / segLens[i] : 0
          const wx = waypoints[i].x + (waypoints[i + 1].x - waypoints[i].x) * t
          const wy = waypoints[i].y + (waypoints[i + 1].y - waypoints[i].y) * t
          const wz = waypoints[i].z + (waypoints[i + 1].z - waypoints[i].z) * t
          return project(rotate3D({ x: wx, y: wy, z: wz }, pitch, yaw))
        }
        acc += segLens[i]
      }
      return projPts[projPts.length - 1]
    }

    const pRgb = options.pulseRgb ?? '255, 255, 255'
    const uHead = ((options.pulseProgress % 1) + 1) % 1
    const pTail = sampleAt(Math.max(0, uHead - 0.14))
    const pHead = sampleAt(uHead)

    const grad = ctx.createLinearGradient(pTail.x, pTail.y, pHead.x, pHead.y)
    grad.addColorStop(0, rgba(options.rgb, 0))
    grad.addColorStop(1, rgba(pRgb, 0.95))
    ctx.beginPath()
    ctx.moveTo(pTail.x, pTail.y)
    ctx.lineTo(pHead.x, pHead.y)
    ctx.lineWidth = 2.6 * pHead.scale
    ctx.lineCap = 'round'
    ctx.strokeStyle = grad
    ctx.stroke()

    ctx.beginPath()
    ctx.arc(pHead.x, pHead.y, 3.2 * pHead.scale, 0, Math.PI * 2)
    ctx.fillStyle = rgba(pRgb, 0.96)
    ctx.fill()
  }
}

/**
 * Registers an interactive 3D hotspot and draws a clean spatial callout pill + alert pulse if needed.
 */
export function drawSpatialHotspot(
  args: SceneRenderArgs,
  anchor: ProjPt,
  defaults: {
    id: string
    label: string
    title: string
    metric: string
    status: SpatialNodeStatus
    path?: string
    offsetY?: number
  },
) {
  const { ctx, width, hoveredId, palette, nodesMap, hitTargets, timeSec } = args
  const ov = nodesMap.get(defaults.id)
  const label = ov?.label !== undefined ? ov.label : defaults.label
  const title = ov?.title ?? defaults.title
  const metric = ov?.metric ?? defaults.metric
  const status = ov?.status ?? defaults.status
  const path = ov?.path !== undefined ? ov.path : defaults.path
  const isHovered = hoveredId === defaults.id
  const rgb = statusRgb(status, palette)

  const pillY = anchor.y + (defaults.offsetY ?? -24) * anchor.scale

  hitTargets.push({
    id: defaults.id,
    x: anchor.x,
    y: (anchor.y + pillY) * 0.5,
    hitR: Math.max(24, 26 * anchor.scale),
    z: anchor.z,
    label,
    title,
    metric,
    status,
    path,
  })

  if (status === 'critical' || status === 'warning') {
    const wave = (timeSec * (status === 'critical' ? 1.4 : 0.95)) % 1
    const r = (9 + wave * 24) * anchor.scale
    ctx.beginPath()
    ctx.arc(anchor.x, anchor.y, r, 0, Math.PI * 2)
    ctx.lineWidth = 1.5 * anchor.scale
    ctx.strokeStyle = rgba(rgb, (1 - wave) * 0.85)
    ctx.stroke()
  }

  // Anchor pin + subtle leader stem
  ctx.beginPath()
  ctx.moveTo(anchor.x, anchor.y)
  ctx.lineTo(anchor.x, pillY + 9)
  ctx.lineWidth = 1
  ctx.strokeStyle = rgba(rgb, isHovered ? 0.9 : 0.52)
  ctx.stroke()

  ctx.beginPath()
  ctx.arc(anchor.x, anchor.y, (isHovered ? 4.5 : 3.2) * anchor.scale, 0, Math.PI * 2)
  ctx.fillStyle = rgba('255, 255, 255', 0.95)
  ctx.fill()

  if (!label) return

  ctx.save()
  ctx.font = `600 ${Math.round(10.5 * Math.min(1.1, Math.max(0.9, anchor.scale)))}px "JetBrains Mono", ui-monospace, SFMono-Regular, monospace`
  const tw = ctx.measureText(label).width
  const padX = 8
  const dotSpace = 10
  const pillW = tw + padX * 2 + dotSpace
  const pillH = 20
  const minX = width > 1060 ? 296 : 18
  const maxX = width > 1060 ? width - 306 : width - 18
  const px = Math.max(minX, Math.min(maxX - pillW, anchor.x - pillW * 0.5))
  const py = pillY - pillH * 0.5

  if (py > 10 && py + pillH < args.height - 10) {
    ctx.beginPath()
    ctx.roundRect(px, py, pillW, pillH, 6)
    ctx.fillStyle = palette.isLight ? 'rgba(255, 255, 255, 0.9)' : 'rgba(7, 12, 24, 0.82)'
    ctx.fill()
    ctx.lineWidth = isHovered ? 1.4 : 0.95
    ctx.strokeStyle = rgba(rgb, isHovered ? 0.96 : 0.56)
    ctx.stroke()

    ctx.beginPath()
    ctx.arc(px + padX + 3, py + pillH * 0.5, 2.8, 0, Math.PI * 2)
    ctx.fillStyle = rgba(rgb, 0.98)
    ctx.fill()

    ctx.fillStyle = palette.isLight ? 'rgba(15, 23, 42, 0.94)' : 'rgba(248, 250, 252, 0.96)'
    ctx.textBaseline = 'middle'
    ctx.fillText(label, px + padX + dotSpace, py + pillH * 0.5 + 0.5)
  }
  ctx.restore()
}
