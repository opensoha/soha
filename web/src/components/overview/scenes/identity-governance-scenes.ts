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
} from './math3d'

/**
 * INTERNAL WORKBENCH (IDENTITY & SSO PORTAL) 3D SCENE:
 * Zero-Trust Triple Security Glass Verification Gates (OIDC -> Outpost Proxy -> Session Policy)
 * verifying incoming identity light-tokens before fanning out into a 3D Floating Enterprise App Wall!
 */
export function renderIdentityScene(args: SceneRenderArgs) {
  const { ctx, cx, cy, sceneScale, timeSec, pitch, yaw, palette, nodesMap } = args
  const project = createProjector(cx, cy, sceneScale, 720)

  const outpostOv = nodesMap.get('ztna-outpost')
  const outpostWarn = outpostOv ? outpostOv.status === 'warning' : false

  // 1. 3 Upright 3D Hexagonal Security Glass Lenses standing in sequence along X (-148, -52, +36)
  const gates = [
    { x: -148, id: 'oidc-idp', rgb: palette.primary, radius: 56 },
    { x: -52, id: 'ztna-outpost', rgb: outpostWarn ? palette.amber : palette.emerald, radius: 62 },
    { x: 36, id: 'sessions-live', rgb: palette.secondary, radius: 54 },
  ]

  // Identity Token Verification Laser Beams passing straight through the 3 Zero-Trust Lenses!
  for (const bz of [-16, 0, 16]) {
    drawPath3DWithPulse(
      ctx,
      [
        { x: -215, y: 10 + bz * 0.4, z: bz },
        { x: 98, y: 10 + bz * 0.4, z: bz },
      ],
      pitch,
      yaw,
      project,
      {
        rgb: bz === 0 ? palette.emerald : palette.primary,
        lineAlpha: 0.46,
        lineWidth: 1.6,
        pulseProgress: timeSec * 0.48 + (bz + 16) * 0.02,
        pulseRgb: '255, 255, 255',
      },
    )
  }

  const gateAnchors: ProjPt[] = []

  gates.forEach((g, idx) => {
    // Standing Vertical 3D Hexagonal Shield Lens (in Y-Z plane at x = g.x!)
    const hexPts: ProjPt[] = []
    for (let i = 0; i < 6; i += 1) {
      const a = (i / 6) * Math.PI * 2
      const wy = 10 + Math.sin(a) * g.radius
      const wz = Math.cos(a) * g.radius
      hexPts.push(project(rotate3D({ x: g.x, y: wy, z: wz }, pitch, yaw)))
    }

    ctx.beginPath()
    hexPts.forEach((p, i) => {
      if (i === 0) ctx.moveTo(p.x, p.y)
      else ctx.lineTo(p.x, p.y)
    })
    ctx.closePath()
    ctx.fillStyle = rgba(g.rgb, palette.isLight ? 0.14 : 0.18)
    ctx.fill()
    ctx.lineWidth = 1.8
    ctx.strokeStyle = rgba(g.rgb, 0.85)
    ctx.stroke()

    // Inner Concentric Cryptographic Ring inside Shield Lens
    ctx.beginPath()
    for (let i = 0; i <= 6; i += 1) {
      const a = (i / 6) * Math.PI * 2 + timeSec * 0.2 * (idx % 2 === 0 ? 1 : -1)
      const wy = 10 + Math.sin(a) * (g.radius * 0.58)
      const wz = Math.cos(a) * (g.radius * 0.58)
      const pt = project(rotate3D({ x: g.x, y: wy, z: wz }, pitch, yaw))
      if (i === 0) ctx.moveTo(pt.x, pt.y)
      else ctx.lineTo(pt.x, pt.y)
    }
    ctx.lineWidth = 1
    ctx.strokeStyle = rgba('255, 255, 255', 0.58)
    ctx.stroke()

    // Pedestal Foot under each Security Gate
    drawBox3D(ctx, { x: g.x, y: 78, z: 0 }, 8, 12, 34, pitch, yaw, project, {
      topRgb: g.rgb,
      sideRgb: g.rgb,
      edgeRgb: g.rgb,
      topAlpha: 0.45,
      litAlpha: 0.35,
      darkAlpha: 0.22,
      edgeAlpha: 0.65,
    })

    gateAnchors.push(project(rotate3D({ x: g.x, y: 10 - g.radius, z: 0 }, pitch, yaw)))
  })

  // 2. RIGHT: 3D Floating Enterprise SSO App & Software Portal Matrix Wall (2 rows x 3 cols at x = 166)
  let appWallTop = project(rotate3D({ x: 166, y: -48, z: 0 }, pitch, yaw))
  for (let row = 0; row < 2; row += 1) {
    for (let col = -1; col <= 1; col += 1) {
      const az = col * 42
      const ay = -8 + row * 40 + Math.sin(timeSec * 1.5 + row + col) * 3
      const tileRgb = (row + col) % 2 === 0 ? palette.primary : palette.emerald

      // Fan-out connection from Session Gate -> App Tile
      drawPath3DWithPulse(
        ctx,
        [
          { x: 58, y: 10, z: 0 },
          { x: 158, y: ay - 12, z: az },
        ],
        pitch,
        yaw,
        project,
        {
          rgb: tileRgb,
          lineAlpha: 0.26,
          lineWidth: 1,
          pulseProgress: timeSec * 0.35 + (row * 3 + col + 1) * 0.16,
          pulseRgb: tileRgb,
        },
      )

      const tile = drawBox3D(ctx, { x: 166, y: ay, z: az }, 6, 26, 15, pitch, yaw, project, {
        topRgb: tileRgb,
        sideRgb: tileRgb,
        edgeRgb: '255, 255, 255',
        topAlpha: 0.78,
        litAlpha: 0.48,
        darkAlpha: 0.3,
        edgeAlpha: 0.84,
      })
      if (row === 0 && col === 0) appWallTop = tile.topProj
    }
  }

  drawSpatialHotspot(args, gateAnchors[0], {
    id: 'oidc-idp',
    label: 'OIDC / SAML',
    title: '统一身份源认证协议门',
    metric: 'OIDC · SAML · Proxy',
    status: 'healthy',
    offsetY: -18,
  })
  drawSpatialHotspot(args, gateAnchors[1], {
    id: 'ztna-outpost',
    label: 'OUTPOST',
    title: '零信任边缘接入代理门禁',
    metric: 'Outpost 在线状态',
    status: outpostWarn ? 'warning' : 'healthy',
    offsetY: -20,
  })
  drawSpatialHotspot(args, gateAnchors[2], {
    id: 'sessions-live',
    label: 'SESSIONS',
    title: '已签发活跃身份会话流',
    metric: '实时在线令牌校验',
    status: 'healthy',
    offsetY: -18,
  })
  drawSpatialHotspot(args, appWallTop, {
    id: 'trust-core',
    label: 'SSO-PORTAL',
    title: '企业统一应用目录与软件仓',
    metric: '点击进入应用目录',
    status: 'primary',
    offsetY: -18,
  })
}

/**
 * SETTINGS & GOVERNANCE WORKBENCH 3D SCENE:
 * Precision 3D Governance Architecture:
 * - Center: 3D Stacked Immutable Cryptographic Audit Ledger Column (beveled blocks locking in!)
 * - Surrounding: 3 Calibrated Chronometer / Astrolabe Policy Rings (with radial degree tick marks!)
 *   for Users, Organization Teams, and RBAC Allow/Deny Policies!
 */
export function renderGovernanceScene(args: SceneRenderArgs) {
  const { ctx, cx, cy, sceneScale, timeSec, pitch, yaw, palette, nodesMap } = args
  const project = createProjector(cx, cy, sceneScale, 700)

  const usersOv = nodesMap.get('trust-core')
  const hasDisabledWarn = usersOv ? usersOv.status === 'warning' : false

  // 1. 3 Precision Calibrated Astrolabe Governance Rings (with Swiss chronometer radial ticks!)
  const rings = [
    { r: 192, y: 34, tilt: 0.08, speed: 0.08, rgb: palette.primary, ticks: 48 },
    { r: 142, y: 18, tilt: -0.14, speed: -0.12, rgb: palette.amber, ticks: 36 },
    { r: 96, y: 2, tilt: 0.18, speed: 0.16, rgb: palette.emerald, ticks: 24 },
  ]

  rings.forEach((ring) => {
    const rot = timeSec * ring.speed
    ctx.beginPath()
    for (let s = 0; s <= 64; s += 1) {
      const a = (s / 64) * Math.PI * 2
      const lx = Math.cos(a) * ring.r
      const lz = Math.sin(a) * ring.r
      const ly = ring.y + Math.sin(a + rot) * ring.r * ring.tilt
      const pt = project(rotate3D({ x: lx, y: ly, z: lz }, pitch, yaw))
      if (s === 0) ctx.moveTo(pt.x, pt.y)
      else ctx.lineTo(pt.x, pt.y)
    }
    ctx.lineWidth = 1.35
    ctx.strokeStyle = rgba(ring.rgb, palette.isLight ? 0.42 : 0.48)
    ctx.stroke()

    // Precision Chronometer Degree Ticks along the Astrolabe Ring!
    ctx.lineWidth = 1
    ctx.strokeStyle = rgba(ring.rgb, 0.58)
    for (let t = 0; t < ring.ticks; t += 1) {
      const a = rot + (t / ring.ticks) * Math.PI * 2
      const innerR = ring.r - (t % 4 === 0 ? 9 : 4.5)
      const pIn = project(
        rotate3D(
          {
            x: Math.cos(a) * innerR,
            y: ring.y + Math.sin(a + rot) * innerR * ring.tilt,
            z: Math.sin(a) * innerR,
          },
          pitch,
          yaw,
        ),
      )
      const pOut = project(
        rotate3D(
          {
            x: Math.cos(a) * ring.r,
            y: ring.y + Math.sin(a + rot) * ring.r * ring.tilt,
            z: Math.sin(a) * ring.r,
          },
          pitch,
          yaw,
        ),
      )
      ctx.beginPath()
      ctx.moveTo(pIn.x, pIn.y)
      ctx.lineTo(pOut.x, pOut.y)
      ctx.stroke()
    }
  })

  // 2. Centerpiece: 3D Stacked Immutable Cryptographic Audit Ledger Column (6 beveled octagon slabs!)
  let ledgerTopProj: ProjPt = project(rotate3D({ x: 0, y: -48, z: 0 }, pitch, yaw))
  for (let blk = 0; blk < 6; blk += 1) {
    const isMintingTop = blk === 5
    const mintFloat = isMintingTop ? Math.abs(Math.sin(timeSec * 1.4)) * 12 : 0
    const by = 68 - blk * 18 - mintFloat
    const bRgb = isMintingTop ? palette.amber : blk % 2 === 0 ? palette.primary : palette.secondary

    const slab = drawPolygonPrism3D(
      ctx,
      { x: 0, y: by, z: 0 },
      8,
      36 - blk * 1.2,
      13,
      pitch,
      yaw,
      project,
      {
        topRgb: bRgb,
        sideRgb: bRgb,
        edgeRgb: '255, 255, 255',
        topAlpha: isMintingTop ? 0.86 : 0.56,
        sideAlpha: 0.42,
        edgeAlpha: isMintingTop ? 0.94 : 0.65,
        phaseAngle: Math.PI / 8,
      },
    )
    if (isMintingTop) ledgerTopProj = slab.topProj
  }

  // 3. Orbiting Governance Satellites (Users, Teams, RBAC Policies) mounted on the 3 Astrolabe Rings!
  const userSat = drawBox3D(ctx, { x: -162, y: 36, z: 48 }, 15, 24, 15, pitch, yaw, project, {
    topRgb: hasDisabledWarn ? palette.amber : palette.primary,
    sideRgb: palette.primary,
    edgeRgb: '255, 255, 255',
    topAlpha: 0.82,
    litAlpha: 0.5,
    darkAlpha: 0.3,
    edgeAlpha: 0.85,
  })
  const teamSat = drawPolygonPrism3D(
    ctx,
    { x: 132, y: 24, z: -48 },
    6,
    19,
    22,
    pitch,
    yaw,
    project,
    {
      topRgb: palette.emerald,
      sideRgb: palette.emerald,
      edgeRgb: '255, 255, 255',
      topAlpha: 0.8,
      sideAlpha: 0.48,
      edgeAlpha: 0.85,
    },
  )
  const policySat = drawPolygonPrism3D(
    ctx,
    { x: 102, y: 46, z: 106 },
    4,
    18,
    24,
    pitch,
    yaw,
    project,
    {
      topRgb: palette.amber,
      sideRgb: palette.amber,
      edgeRgb: '255, 255, 255',
      topAlpha: 0.84,
      sideAlpha: 0.5,
      edgeAlpha: 0.88,
      phaseAngle: Math.PI / 4,
    },
  )

  drawSpatialHotspot(args, ledgerTopProj, {
    id: 'audit-ledger',
    label: 'AUDIT-LEDGER',
    title: '不可篡改安全与操作审计账本',
    metric: '全量操作留存追踪',
    status: 'primary',
    offsetY: -20,
  })
  drawSpatialHotspot(args, userSat.topProj, {
    id: 'trust-core',
    label: 'USERS',
    title: '平台账户目录与启停治理',
    metric: '点击管理用户状态',
    status: hasDisabledWarn ? 'warning' : 'primary',
    offsetY: -18,
  })
  drawSpatialHotspot(args, teamSat.topProj, {
    id: 'org-teams',
    label: 'TEAMS',
    title: '组织架构与业务团队映射',
    metric: '多团队成员边界',
    status: 'healthy',
    offsetY: -18,
  })
  drawSpatialHotspot(args, policySat.topProj, {
    id: 'rbac-guard',
    label: 'RBAC-POLICY',
    title: 'RBAC 角色与 Allow/Deny 策略护栏',
    metric: '最小权限访问控制',
    status: 'healthy',
    offsetY: -18,
  })
}
