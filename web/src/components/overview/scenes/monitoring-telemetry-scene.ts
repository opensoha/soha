import {
  createProjector,
  drawBox3D,
  drawSpatialHotspot,
  rgba,
  rotate3D,
  type ProjPt,
  type SceneRenderArgs,
} from './math3d'

/**
 * MONITORING & OBSERVABILITY WORKBENCH 3D SCENE:
 * Real-Time 3D Multi-Channel Time-Series Ribbon Oscilloscope & SLA Breach Radar:
 * - 5 Parallel 3D Perspective Metric Ribbons (Latency P99, Error 5xx, Ingress QPS, Saturation, Events)
 *   scrolling live across a graduated 3D observability stage!
 * - Translucent Red 3D SLA Alert Threshold Plane: wherever the Error/Latency wave breaches the threshold,
 *   a fiery Crimson/Amber Anomaly Peak & Rotating Sonar Target Lock Ring locks onto the spike!
 */
export function renderMonitoringTelemetryScene(args: SceneRenderArgs) {
  const { ctx, cx, cy, sceneScale, timeSec, pitch, yaw, palette, nodesMap } = args
  const project = createProjector(cx, cy, sceneScale, 720)

  const critOv = nodesMap.get('critical-signal')
  const provOv = nodesMap.get('prom-metrics')
  const hasCritical = critOv ? critOv.status === 'critical' : true

  const spanX = 410
  const samples = 42
  const baseFloorY = 78
  const slaThresholdY = -12

  // 1. 3D Graduated Telemetry Stage Grid & Sweeping Radar Ring beneath Stage
  ctx.lineWidth = 0.75
  ctx.strokeStyle = rgba(palette.stroke, palette.isLight ? 0.16 : 0.13)
  for (let gx = -200; gx <= 200; gx += 50) {
    const a = project(rotate3D({ x: gx, y: baseFloorY, z: -125 }, pitch, yaw))
    const b = project(rotate3D({ x: gx, y: baseFloorY, z: 125 }, pitch, yaw))
    ctx.beginPath()
    ctx.moveTo(a.x, a.y)
    ctx.lineTo(b.x, b.y)
    ctx.stroke()
  }

  // Subtle Rotating Observability Sonar Sweep on Floor
  const sweepAngle = timeSec * 0.95
  ctx.beginPath()
  const floorOrigin = project(rotate3D({ x: 0, y: baseFloorY + 2, z: 0 }, pitch, yaw))
  ctx.moveTo(floorOrigin.x, floorOrigin.y)
  for (let s = 0; s <= 14; s += 1) {
    const a = sweepAngle - (s / 14) * 0.65
    const pt = project(
      rotate3D({ x: Math.cos(a) * 185, y: baseFloorY + 2, z: Math.sin(a) * 125 }, pitch, yaw),
    )
    ctx.lineTo(pt.x, pt.y)
  }
  ctx.closePath()
  ctx.fillStyle = rgba(palette.primary, palette.isLight ? 0.08 : 0.12)
  ctx.fill()

  // 2. 5 Parallel 3D Time-Series Metric Ribbon Channels (rendered back-to-front along Z!)
  const channels = [
    { z: -96, name: 'events', rgb: palette.secondary, freq: 3.4, amp: 18, spike: 0 },
    { z: -48, name: 'qps', rgb: palette.emerald, freq: 4.2, amp: 26, spike: 0 },
    {
      z: 0,
      name: 'error_5xx',
      rgb: hasCritical ? palette.rose : palette.amber,
      freq: 5.1,
      amp: 22,
      spike: hasCritical ? 78 : 44,
    },
    { z: 48, name: 'p99_latency', rgb: palette.amber, freq: 3.8, amp: 28, spike: 36 },
    { z: 96, name: 'prom_scrape', rgb: palette.primary, freq: 4.6, amp: 20, spike: 0 },
  ]

  let criticalPeakProj: ProjPt = project(rotate3D({ x: 24, y: -48, z: 0 }, pitch, yaw))
  let firingPeakProj: ProjPt = project(rotate3D({ x: -58, y: -18, z: 48 }, pitch, yaw))
  let eventsEdgeProj: ProjPt = project(rotate3D({ x: -165, y: 42, z: -96 }, pitch, yaw))

  channels.forEach((ch, chIdx) => {
    const topPts: ProjPt[] = []
    const basePts: ProjPt[] = []

    for (let i = 0; i <= samples; i += 1) {
      const u = i / samples
      const wx = (u - 0.5) * spanX
      // Live scrolling waveform + localized anomaly Gaussian burst!
      const scroll = timeSec * 1.65 + chIdx * 0.9
      let val =
        16 +
        Math.sin(u * ch.freq * Math.PI + scroll) * ch.amp * 0.55 +
        Math.cos(u * 9.5 - scroll * 1.3) * (ch.amp * 0.28)

      // Anomaly breach spike around u = 0.56 (for error_5xx) or u = 0.36 (for p99_latency)
      if (ch.spike > 0) {
        const centerU = ch.name === 'error_5xx' ? 0.56 : 0.34
        const pulseAmp = 0.86 + Math.sin(timeSec * 3.2 + chIdx) * 0.14
        const d = (u - centerU) / 0.085
        val += ch.spike * pulseAmp * Math.exp(-d * d)
      }

      const wy = baseFloorY - Math.max(3, val)
      const tp = project(rotate3D({ x: wx, y: wy, z: ch.z }, pitch, yaw))
      const bp = project(rotate3D({ x: wx, y: baseFloorY, z: ch.z }, pitch, yaw))
      topPts.push(tp)
      basePts.push(bp)

      if (ch.name === 'error_5xx' && i === Math.round(samples * 0.56)) {
        criticalPeakProj = tp
      }
      if (ch.name === 'p99_latency' && i === Math.round(samples * 0.34)) {
        firingPeakProj = tp
      }
      if (ch.name === 'events' && i === 8) {
        eventsEdgeProj = tp
      }
    }

    // Filled 3D Translucent Ribbon Curtain under the Time-Series Curve
    ctx.beginPath()
    topPts.forEach((p, idx) => {
      if (idx === 0) ctx.moveTo(p.x, p.y)
      else ctx.lineTo(p.x, p.y)
    })
    for (let idx = basePts.length - 1; idx >= 0; idx -= 1) {
      ctx.lineTo(basePts[idx].x, basePts[idx].y)
    }
    ctx.closePath()

    const grad = ctx.createLinearGradient(0, cy - 90 * sceneScale, 0, cy + 80 * sceneScale)
    grad.addColorStop(0, rgba(ch.rgb, palette.isLight ? 0.38 : 0.44))
    grad.addColorStop(1, rgba(ch.rgb, 0.03))
    ctx.fillStyle = grad
    ctx.fill()

    // Glowing Top Crest Line of the 3D Waveform
    ctx.beginPath()
    topPts.forEach((p, idx) => {
      if (idx === 0) ctx.moveTo(p.x, p.y)
      else ctx.lineTo(p.x, p.y)
    })
    ctx.lineWidth = ch.spike > 50 ? 2.4 : 1.75
    ctx.strokeStyle = rgba(ch.rgb, 0.92)
    ctx.stroke()
  })

  // 3. Translucent Red/Amber 3D SLA Alert Threshold Plane cutting horizontally across y = slaThresholdY!
  const slaCorners = [
    project(rotate3D({ x: -195, y: slaThresholdY, z: -110 }, pitch, yaw)),
    project(rotate3D({ x: 195, y: slaThresholdY, z: -110 }, pitch, yaw)),
    project(rotate3D({ x: 195, y: slaThresholdY, z: 110 }, pitch, yaw)),
    project(rotate3D({ x: -195, y: slaThresholdY, z: 110 }, pitch, yaw)),
  ]
  ctx.save()
  ctx.setLineDash([5, 4])
  ctx.beginPath()
  slaCorners.forEach((c, i) => {
    if (i === 0) ctx.moveTo(c.x, c.y)
    else ctx.lineTo(c.x, c.y)
  })
  ctx.closePath()
  ctx.fillStyle = rgba(palette.rose, palette.isLight ? 0.05 : 0.06)
  ctx.fill()
  ctx.lineWidth = 1.1
  ctx.strokeStyle = rgba(palette.rose, 0.52)
  ctx.stroke()
  ctx.restore()

  // Provider Collector Cube at Front-Right Ingestion Edge
  const providerCube = drawBox3D(ctx, { x: 178, y: 78, z: 96 }, 18, 26, 16, pitch, yaw, project, {
    topRgb: provOv?.status === 'warning' ? palette.amber : palette.primary,
    sideRgb: palette.primary,
    edgeRgb: '255, 255, 255',
    topAlpha: 0.78,
    litAlpha: 0.48,
    darkAlpha: 0.28,
    edgeAlpha: 0.85,
  })

  drawSpatialHotspot(args, criticalPeakProj, {
    id: 'critical-signal',
    label: 'CRITICAL',
    title: '高危阈值突破波峰 (SLA Breach)',
    metric: '点击定位 Critical 告警源',
    status: hasCritical ? 'critical' : 'healthy',
    offsetY: -22,
  })
  drawSpatialHotspot(args, firingPeakProj, {
    id: 'signal-hub',
    label: 'FIRING',
    title: 'P99 黄金信号与活跃告警评估',
    metric: '时序异常检测引擎',
    status: 'warning',
    offsetY: -20,
  })
  drawSpatialHotspot(args, providerCube.topProj, {
    id: 'prom-metrics',
    label: 'PROVIDERS',
    title: 'Prometheus / Loki 数据源采集器',
    metric: '观测数据源运行态',
    status: provOv?.status ?? 'healthy',
    offsetY: -18,
  })
  drawSpatialHotspot(args, eventsEdgeProj, {
    id: 'event-bus',
    label: 'EVENTS',
    title: '实时集群与平台事件流',
    metric: 'Event Timeline',
    status: 'primary',
    offsetY: -18,
  })
}
