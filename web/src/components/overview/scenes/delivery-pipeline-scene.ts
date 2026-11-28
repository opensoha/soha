import {
  createProjector,
  drawBox3D,
  drawPath3DWithPulse,
  drawPolygonPrism3D,
  drawSpatialHotspot,
  rgba,
  rotate3D,
  type ProjPt,
  type SceneRenderArgs,
  type Vec3,
} from './math3d'

/**
 * DELIVERY (CI/CD) WORKBENCH 3D SCENE:
 * Directional Left-to-Right Isometric 3D Release Pipeline Viaduct (NO circular spinning!):
 *   [GIT SOURCE] ===> [CI BUILD & LASER SCANNER] ===> [APPROVAL / CANARY GATE] ===> [PROD CLUSTERS A & B]
 * OCI Release Artifact Containers physically glide along illuminated 3D rails, get scanned by
 * laser portals, queue at the Amber Approval Barrier when waiting, and split into Canary/Prod pads!
 */
export function renderDeliveryPipelineScene(args: SceneRenderArgs) {
  const { ctx, cx, cy, sceneScale, timeSec, pitch, yaw, palette, nodesMap } = args
  const project = createProjector(cx, cy, sceneScale, 720)

  const gateOv = nodesMap.get('canary-gate')
  const hasWaitingApproval = gateOv
    ? gateOv.status === 'warning' || gateOv.status === 'critical'
    : true

  // Helper to draw an upright 3D Gantry / Portal Gate straddling the pipeline track at `gateX`
  const drawPortalGate = (
    gateX: number,
    gateRgb: string,
    hasBarrierCurtain: boolean,
    scanWave: boolean,
  ): ProjPt => {
    // Left Pillar, Right Pillar, Top Crossbeam
    drawBox3D(ctx, { x: gateX, y: 66, z: -36 }, 6, 74, 7, pitch, yaw, project, {
      topRgb: gateRgb,
      sideRgb: gateRgb,
      edgeRgb: '255, 255, 255',
      topAlpha: 0.72,
      litAlpha: 0.48,
      darkAlpha: 0.3,
      edgeAlpha: 0.76,
    })
    drawBox3D(ctx, { x: gateX, y: 66, z: 36 }, 6, 74, 7, pitch, yaw, project, {
      topRgb: gateRgb,
      sideRgb: gateRgb,
      edgeRgb: '255, 255, 255',
      topAlpha: 0.72,
      litAlpha: 0.48,
      darkAlpha: 0.3,
      edgeAlpha: 0.76,
    })
    const beam = drawBox3D(ctx, { x: gateX, y: -8, z: 0 }, 7, 11, 43, pitch, yaw, project, {
      topRgb: gateRgb,
      sideRgb: gateRgb,
      edgeRgb: '255, 255, 255',
      topAlpha: 0.84,
      litAlpha: 0.56,
      darkAlpha: 0.36,
      edgeAlpha: 0.88,
    })

    // Translucent Laser Curtain / Scanner Sheet inside the Portal!
    const pTL = project(rotate3D({ x: gateX, y: -6, z: -29 }, pitch, yaw))
    const pTR = project(rotate3D({ x: gateX, y: -6, z: 29 }, pitch, yaw))
    const pBR = project(rotate3D({ x: gateX, y: 62, z: 29 }, pitch, yaw))
    const pBL = project(rotate3D({ x: gateX, y: 62, z: -29 }, pitch, yaw))

    ctx.beginPath()
    ctx.moveTo(pTL.x, pTL.y)
    ctx.lineTo(pTR.x, pTR.y)
    ctx.lineTo(pBR.x, pBR.y)
    ctx.lineTo(pBL.x, pBL.y)
    ctx.closePath()
    ctx.fillStyle = rgba(gateRgb, hasBarrierCurtain ? 0.24 + Math.sin(timeSec * 3.5) * 0.08 : 0.1)
    ctx.fill()

    if (scanWave) {
      const sy = -4 + (Math.sin(timeSec * 2.6) * 0.5 + 0.5) * 62
      const sL = project(rotate3D({ x: gateX, y: sy, z: -29 }, pitch, yaw))
      const sR = project(rotate3D({ x: gateX, y: sy, z: 29 }, pitch, yaw))
      ctx.beginPath()
      ctx.moveTo(sL.x, sL.y)
      ctx.lineTo(sR.x, sR.y)
      ctx.lineWidth = 2.2
      ctx.strokeStyle = rgba('255, 255, 255', 0.92)
      ctx.stroke()
    }

    if (hasBarrierCurtain) {
      // Horizontal Amber Laser Lock Bars across Approval Gate
      for (let barY = 10; barY <= 50; barY += 14) {
        const bL = project(rotate3D({ x: gateX, y: barY, z: -29 }, pitch, yaw))
        const bR = project(rotate3D({ x: gateX, y: barY, z: 29 }, pitch, yaw))
        ctx.beginPath()
        ctx.moveTo(bL.x, bL.y)
        ctx.lineTo(bR.x, bR.y)
        ctx.lineWidth = 1.4
        ctx.strokeStyle = rgba(gateRgb, 0.78)
        ctx.stroke()
      }
    }

    return beam.topProj
  }

  // 1. Main 3D Elevated Pipeline Viaduct Deck (-210 -> +70)
  drawBox3D(ctx, { x: -70, y: 74, z: 0 }, 148, 8, 24, pitch, yaw, project, {
    topRgb: palette.primary,
    sideRgb: palette.secondary,
    edgeRgb: palette.primary,
    topAlpha: palette.isLight ? 0.16 : 0.2,
    litAlpha: palette.isLight ? 0.26 : 0.36,
    darkAlpha: 0.24,
    edgeAlpha: 0.58,
  })

  // Twin Illuminated Conveyor Rails along Main Pipeline
  for (const rz of [-12, 12]) {
    drawPath3DWithPulse(
      ctx,
      [
        { x: -212, y: 65, z: rz },
        { x: 68, y: 65, z: rz },
      ],
      pitch,
      yaw,
      project,
      {
        rgb: palette.primary,
        lineAlpha: 0.55,
        lineWidth: 1.5,
        pulseProgress: timeSec * 0.42 + (rz > 0 ? 0.5 : 0),
      },
    )
  }

  // 2. Y-Fork Canary (10%) & Prod (90%) Rollout Branches (+68 -> +196)
  const canaryBranch: Vec3[] = [
    { x: 68, y: 66, z: -8 },
    { x: 122, y: 66, z: -64 },
    { x: 188, y: 66, z: -64 },
  ]
  const prodBranch: Vec3[] = [
    { x: 68, y: 66, z: 8 },
    { x: 122, y: 66, z: 64 },
    { x: 188, y: 66, z: 64 },
  ]
  drawPath3DWithPulse(ctx, canaryBranch, pitch, yaw, project, {
    rgb: palette.amber,
    lineAlpha: 0.62,
    lineWidth: 1.8,
    pulseProgress: timeSec * 0.34,
    pulseRgb: palette.amber,
  })
  drawPath3DWithPulse(ctx, prodBranch, pitch, yaw, project, {
    rgb: palette.emerald,
    lineAlpha: 0.72,
    lineWidth: 2.2,
    pulseProgress: timeSec * 0.5,
    pulseRgb: palette.emerald,
  })

  // 3. Stage 1 Dock: Git Commit / Source Repository Pedestal (Left, x = -196)
  const gitBase = drawBox3D(ctx, { x: -196, y: 66, z: 0 }, 24, 34, 22, pitch, yaw, project, {
    topRgb: palette.primary,
    sideRgb: palette.secondary,
    edgeRgb: '255, 255, 255',
    topAlpha: 0.68,
    litAlpha: 0.45,
    darkAlpha: 0.28,
    edgeAlpha: 0.76,
  })

  // 4. Gliding 3D OCI Release Artifact Containers along the Conveyor Track!
  const containerCount = 3
  for (let c = 0; c < containerCount; c += 1) {
    const rawU = (timeSec * 0.22 + c / containerCount) % 1
    const cxWorld = -162 + rawU * 218
    // Container changes tint from Indigo (building) -> Cyan (verified) as it passes CI Scanner at x = -74
    const passedScan = cxWorld > -74
    const cRgb = passedScan ? palette.emerald : palette.primary
    drawBox3D(ctx, { x: cxWorld, y: 64, z: 0 }, 15, 20, 12, pitch, yaw, project, {
      topRgb: cRgb,
      sideRgb: cRgb,
      edgeRgb: '255, 255, 255',
      topAlpha: 0.86,
      litAlpha: 0.56,
      darkAlpha: 0.34,
      edgeAlpha: 0.9,
    })
  }

  // If waiting approval, render a pulsing Amber Staged Release Bundle right in front of Approval Gate!
  if (hasWaitingApproval) {
    drawBox3D(
      ctx,
      { x: 24, y: 64 + Math.sin(timeSec * 3) * 1.5, z: 0 },
      16,
      23,
      14,
      pitch,
      yaw,
      project,
      {
        topRgb: palette.amber,
        sideRgb: palette.amber,
        edgeRgb: '255, 255, 255',
        topAlpha: 0.9,
        litAlpha: 0.62,
        darkAlpha: 0.38,
        edgeAlpha: 0.95,
      },
    )
  }

  // 5. Stage 2 & Stage 3 Upright Portal Gantries: CI Scanner (x = -74) & Approval/Canary Gate (x = 58)
  const ciGateTop = drawPortalGate(-74, palette.primary, false, true)
  const approvalGateTop = drawPortalGate(
    58,
    hasWaitingApproval ? palette.amber : palette.emerald,
    hasWaitingApproval,
    false,
  )

  // 6. Stage 4 Right: Canary Cluster Pad (z = -64) & Production Fleet Pad (z = +64)
  drawPolygonPrism3D(ctx, { x: 188, y: 74, z: -64 }, 6, 34, 10, pitch, yaw, project, {
    topRgb: palette.amber,
    sideRgb: palette.amber,
    edgeRgb: palette.amber,
    topAlpha: 0.24,
    sideAlpha: 0.36,
    edgeAlpha: 0.72,
    phaseAngle: Math.PI / 6,
  })
  drawBox3D(ctx, { x: 188, y: 64, z: -64 }, 13, 22, 13, pitch, yaw, project, {
    topRgb: palette.amber,
    sideRgb: palette.amber,
    edgeRgb: '255, 255, 255',
    topAlpha: 0.8,
    litAlpha: 0.52,
    darkAlpha: 0.3,
    edgeAlpha: 0.85,
  })

  const prodPad = drawPolygonPrism3D(
    ctx,
    { x: 188, y: 74, z: 64 },
    6,
    42,
    12,
    pitch,
    yaw,
    project,
    {
      topRgb: palette.emerald,
      sideRgb: palette.emerald,
      edgeRgb: palette.emerald,
      topAlpha: 0.28,
      sideAlpha: 0.42,
      edgeAlpha: 0.82,
      phaseAngle: Math.PI / 6,
    },
  )
  const prodCube = drawBox3D(ctx, { x: 188, y: 62, z: 64 }, 16, 28, 16, pitch, yaw, project, {
    topRgb: palette.emerald,
    sideRgb: palette.emerald,
    edgeRgb: '255, 255, 255',
    topAlpha: 0.88,
    litAlpha: 0.58,
    darkAlpha: 0.36,
    edgeAlpha: 0.92,
  })
  void prodPad

  drawSpatialHotspot(args, gitBase.topProj, {
    id: 'git-source',
    label: 'APPS',
    title: '源码仓库与受管应用',
    metric: 'Git Webhook / 构建入口',
    status: 'primary',
    offsetY: -18,
  })
  drawSpatialHotspot(args, ciGateTop, {
    id: 'ci-build',
    label: 'CI-BUILD',
    title: '镜像构建与制品校验龙门',
    metric: '流水线与发布批次执行中',
    status: 'primary',
    offsetY: -18,
  })
  drawSpatialHotspot(args, approvalGateTop, {
    id: 'canary-gate',
    label: 'APPROVAL',
    title: '人工审批与灰度金丝雀门禁',
    metric: '点击进入发布看板处置',
    status: hasWaitingApproval ? 'warning' : 'healthy',
    offsetY: -20,
  })
  drawSpatialHotspot(args, prodCube.topProj, {
    id: 'prod-live',
    label: 'PROD-RELEASE',
    title: '多集群生产环境就绪交付',
    metric: '生产流量稳定收敛',
    status: 'healthy',
    offsetY: -18,
  })
}
