import {
  createProjector,
  drawBox3D,
  drawPath3DWithPulse,
  drawPolygonPrism3D,
  drawSpatialHotspot,
  rgba,
  rotate3D,
  statusRgb,
  type SceneRenderArgs,
  type SpatialNodeStatus,
  type Vec3,
} from './math3d'

/**
 * KUBERNETES WORKBENCH 3D SCENE:
 * Isometric 7-sided K8s Control Plane Turret (API Server / etcd / Scheduler)
 * surrounded by 5 Hexagonal Worker Node Honeycomb Platforms, each physically
 * packed with 3D Miniature Pod Containers (Running = Emerald, Pending = Amber,
 * CrashLoop/At-Risk = Crimson) + Live Scheduler Pod Drop!
 */
export function renderPlatformK8sScene(args: SceneRenderArgs) {
  const { ctx, cx, cy, sceneScale, timeSec, pitch, yaw, palette, nodesMap } = args
  const project = createProjector(cx, cy, sceneScale, 700)

  const riskOv = nodesMap.get('risk-pods')
  const nodesOv = nodesMap.get('nodes-pool')
  const alertsOv = nodesMap.get('cluster-alerts')

  const hasRiskPods = riskOv ? riskOv.status === 'critical' || riskOv.status === 'warning' : true
  const hasUnreadyNode = nodesOv
    ? nodesOv.status === 'warning' || nodesOv.status === 'critical'
    : false

  // 1. Subtle K8s Service Mesh Hexagonal Blueprint Ring on Floor (y = 82)
  for (const r of [145, 228]) {
    ctx.beginPath()
    for (let i = 0; i <= 6; i += 1) {
      const a = (i / 6) * Math.PI * 2 + Math.PI / 6
      const pt = project(rotate3D({ x: Math.cos(a) * r, y: 84, z: Math.sin(a) * r }, pitch, yaw))
      if (i === 0) ctx.moveTo(pt.x, pt.y)
      else ctx.lineTo(pt.x, pt.y)
    }
    ctx.lineWidth = 1
    ctx.strokeStyle = rgba(palette.primary, palette.isLight ? 0.18 : 0.16)
    ctx.stroke()
  }

  // 5 Worker Node Hexagonal Platforms arranged around the Control Plane
  const workerNodes: Array<{
    id: string
    pos: Vec3
    podStates: Array<'healthy' | 'warning' | 'critical' | 'empty'>
    nodeStatus: SpatialNodeStatus
  }> = [
    {
      id: 'node-01',
      pos: { x: -162, y: 74, z: -56 },
      podStates: ['healthy', 'healthy', 'healthy', 'healthy', 'healthy', 'healthy', 'healthy'],
      nodeStatus: 'healthy',
    },
    {
      id: 'node-02',
      pos: { x: 162, y: 74, z: -56 },
      podStates: [
        'healthy',
        'healthy',
        hasRiskPods ? 'critical' : 'healthy',
        'healthy',
        'warning',
        'healthy',
        'healthy',
      ],
      nodeStatus: hasRiskPods ? 'critical' : 'healthy',
    },
    {
      id: 'node-03',
      pos: { x: -138, y: 74, z: 96 },
      podStates: ['healthy', 'healthy', 'healthy', 'warning', 'healthy', 'empty', 'healthy'],
      nodeStatus: hasUnreadyNode ? 'warning' : 'healthy',
    },
    {
      id: 'node-04',
      pos: { x: 138, y: 74, z: 96 },
      podStates: [
        'healthy',
        hasRiskPods ? 'critical' : 'healthy',
        'healthy',
        'healthy',
        'healthy',
        'healthy',
        'empty',
      ],
      nodeStatus: 'healthy',
    },
    {
      id: 'node-05',
      pos: { x: 0, y: 74, z: -158 },
      podStates: ['healthy', 'healthy', 'healthy', 'healthy', 'healthy', 'healthy', 'empty'],
      nodeStatus: 'healthy',
    },
  ]

  // Sub-hex offsets inside each Worker Node for the 7 miniature 3D Pod Containers
  const podHexOffsets: Array<{ dx: number; dz: number }> = [
    { dx: 0, dz: 0 },
    { dx: 20, dz: 0 },
    { dx: 10, dz: 17.5 },
    { dx: -10, dz: 17.5 },
    { dx: -20, dz: 0 },
    { dx: -10, dz: -17.5 },
    { dx: 10, dz: -17.5 },
  ]

  // Kube-proxy / CNI Orthogonal Network Channels from Control Plane to each Worker Node
  workerNodes.forEach((wn, idx) => {
    const mid: Vec3 = { x: wn.pos.x * 0.52, y: 80, z: 0 }
    const pre: Vec3 = { x: wn.pos.x * 0.52, y: 80, z: wn.pos.z }
    drawPath3DWithPulse(
      ctx,
      [{ x: 0, y: 80, z: 0 }, mid, pre, { x: wn.pos.x, y: 80, z: wn.pos.z }],
      pitch,
      yaw,
      project,
      {
        rgb: statusRgb(wn.nodeStatus === 'critical' ? 'warning' : 'primary', palette),
        lineAlpha: palette.isLight ? 0.34 : 0.38,
        pulseProgress: timeSec * 0.38 + idx * 0.21,
        pulseRgb: wn.nodeStatus === 'critical' ? palette.rose : palette.primary,
      },
    )
  })

  // Depth-sort Control Plane + 5 Worker Node Hexagons together for true 3D painter's order!
  type RenderableClusterUnit =
    | { kind: 'control-plane'; z: number }
    | { kind: 'worker-node'; wn: (typeof workerNodes)[number]; idx: number; z: number }

  const units: RenderableClusterUnit[] = [
    { kind: 'control-plane' as const, z: rotate3D({ x: 0, y: 50, z: 12 }, pitch, yaw).z },
    ...workerNodes.map((wn, idx) => ({
      kind: 'worker-node' as const,
      wn,
      idx,
      z: rotate3D(wn.pos, pitch, yaw).z,
    })),
  ].sort((a, b) => b.z - a.z)

  let cpAnchor = project(rotate3D({ x: 0, y: -26, z: 12 }, pitch, yaw))
  let nodesAnchor = project(rotate3D({ x: -162, y: 42, z: -56 }, pitch, yaw))
  let riskAnchor = project(rotate3D({ x: 162, y: 36, z: -56 }, pitch, yaw))
  let deployAnchor = project(rotate3D({ x: -138, y: 42, z: 96 }, pitch, yaw))
  let alertsAnchor = project(rotate3D({ x: 138, y: 42, z: 96 }, pitch, yaw))

  units.forEach((u) => {
    if (u.kind === 'control-plane') {
      // 7-Sided Kubernetes Heptagon Pedestal + Control Plane Core!
      drawPolygonPrism3D(ctx, { x: 0, y: 80, z: 8 }, 7, 68, 18, pitch, yaw, project, {
        topRgb: palette.primary,
        sideRgb: palette.secondary,
        edgeRgb: palette.primary,
        topAlpha: palette.isLight ? 0.2 : 0.24,
        sideAlpha: palette.isLight ? 0.32 : 0.42,
        edgeAlpha: 0.72,
        phaseAngle: timeSec * 0.14,
      })

      const turret = drawPolygonPrism3D(
        ctx,
        { x: 0, y: 62, z: 8 },
        7,
        42,
        46,
        pitch,
        yaw,
        project,
        {
          topRgb: palette.primary,
          sideRgb: palette.primary,
          edgeRgb: '255, 255, 255',
          topAlpha: palette.isLight ? 0.56 : 0.62,
          sideAlpha: palette.isLight ? 0.34 : 0.44,
          edgeAlpha: 0.78,
          phaseAngle: -timeSec * 0.18,
        },
      )
      cpAnchor = turret.topProj

      // Etched 7-Spoke Kubernetes Steering Wheel Emblem floating on top of Control Plane!
      const wheelY = 12
      const wheelCenter = project(rotate3D({ x: 0, y: wheelY, z: 8 }, pitch, yaw))
      ctx.beginPath()
      for (let s = 0; s <= 7; s += 1) {
        const a = timeSec * 0.28 + (s / 7) * Math.PI * 2
        const rim = project(
          rotate3D({ x: Math.cos(a) * 28, y: wheelY, z: 8 + Math.sin(a) * 28 }, pitch, yaw),
        )
        if (s === 0) ctx.moveTo(rim.x, rim.y)
        else ctx.lineTo(rim.x, rim.y)
      }
      ctx.lineWidth = 1.6
      ctx.strokeStyle = rgba('255, 255, 255', 0.88)
      ctx.stroke()

      for (let s = 0; s < 7; s += 1) {
        const a = timeSec * 0.28 + (s / 7) * Math.PI * 2
        const spokeTip = project(
          rotate3D({ x: Math.cos(a) * 33, y: wheelY, z: 8 + Math.sin(a) * 33 }, pitch, yaw),
        )
        ctx.beginPath()
        ctx.moveTo(wheelCenter.x, wheelCenter.y)
        ctx.lineTo(spokeTip.x, spokeTip.y)
        ctx.lineWidth = 1.3
        ctx.strokeStyle = rgba('255, 255, 255', 0.78)
        ctx.stroke()
      }
    } else {
      const { wn, idx } = u
      const nodeRgb =
        wn.nodeStatus === 'warning'
          ? palette.amber
          : wn.nodeStatus === 'critical'
            ? palette.primary
            : palette.primary

      // 3D Extruded Hexagonal Worker Node Base
      drawPolygonPrism3D(ctx, wn.pos, 6, 52, 13, pitch, yaw, project, {
        topRgb: nodeRgb,
        sideRgb: palette.secondary,
        edgeRgb: nodeRgb,
        topAlpha: palette.isLight ? 0.16 : 0.2,
        sideAlpha: palette.isLight ? 0.26 : 0.36,
        edgeAlpha: 0.62,
        phaseAngle: Math.PI / 6,
      })

      // Depth-sort the 7 Pod Micro-Containers sitting ON TOP of this Worker Node!
      const podCells = wn.podStates
        .map((st, pIdx) => {
          const px = wn.pos.x + podHexOffsets[pIdx].dx
          const pz = wn.pos.z + podHexOffsets[pIdx].dz
          const py = wn.pos.y - 13
          return {
            st,
            pIdx,
            pos: { x: px, y: py, z: pz },
            z: rotate3D({ x: px, y: py, z: pz }, pitch, yaw).z,
          }
        })
        .sort((a, b) => b.z - a.z)

      podCells.forEach((cell) => {
        if (cell.st === 'empty') {
          // Empty Pod Slot Socket
          drawPolygonPrism3D(ctx, cell.pos, 6, 7.5, 2, pitch, yaw, project, {
            topRgb: palette.stroke,
            edgeRgb: palette.stroke,
            topAlpha: 0.08,
            sideAlpha: 0.08,
            edgeAlpha: 0.25,
            phaseAngle: Math.PI / 6,
          })
          return
        }

        const podRgb =
          cell.st === 'critical'
            ? palette.rose
            : cell.st === 'warning'
              ? palette.amber
              : palette.emerald
        const podH =
          cell.st === 'critical'
            ? 24 + Math.sin(timeSec * 4) * 3
            : cell.st === 'warning'
              ? 17
              : 14 + Math.sin(timeSec * 1.6 + idx + cell.pIdx) * 1.8

        const podRes = drawBox3D(ctx, cell.pos, 6.2, podH, 6.2, pitch, yaw, project, {
          topRgb: podRgb,
          sideRgb: podRgb,
          edgeRgb: '255, 255, 255',
          topAlpha: cell.st === 'critical' ? 0.92 : 0.78,
          litAlpha: 0.56,
          darkAlpha: 0.34,
          edgeAlpha: cell.st === 'critical' ? 0.9 : 0.52,
        })

        if (idx === 1 && cell.st === 'critical') {
          riskAnchor = podRes.topProj
        }
      })

      const nodeTopProj = project(
        rotate3D({ x: wn.pos.x, y: wn.pos.y - 30, z: wn.pos.z }, pitch, yaw),
      )
      if (idx === 0) nodesAnchor = nodeTopProj
      if (idx === 2) deployAnchor = nodeTopProj
      if (idx === 3) alertsAnchor = nodeTopProj
    }
  })

  // Live K8s Scheduler Dispatch Arc: A Pod Capsule flying from Control Plane into a Worker Node slot!
  const schedCycle = (timeSec * 0.42) % 1
  const targetIdx = Math.floor(timeSec * 0.42) % workerNodes.length
  const dstNode = workerNodes[targetIdx]
  const arcX = dstNode.pos.x * schedCycle
  const arcZ = 8 + (dstNode.pos.z - 8) * schedCycle
  const arcY = 12 + (dstNode.pos.y - 28 - 12) * schedCycle - Math.sin(schedCycle * Math.PI) * 68

  // Dashed scheduler trajectory preview
  ctx.save()
  ctx.setLineDash([3, 4])
  ctx.beginPath()
  for (let s = 0; s <= 16; s += 1) {
    const u = s / 16
    const tp = project(
      rotate3D(
        {
          x: dstNode.pos.x * u,
          y: 12 + (dstNode.pos.y - 28 - 12) * u - Math.sin(u * Math.PI) * 68,
          z: 8 + (dstNode.pos.z - 8) * u,
        },
        pitch,
        yaw,
      ),
    )
    if (s === 0) ctx.moveTo(tp.x, tp.y)
    else ctx.lineTo(tp.x, tp.y)
  }
  ctx.lineWidth = 1.1
  ctx.strokeStyle = rgba(palette.emerald, 0.48)
  ctx.stroke()
  ctx.restore()

  // Flying 3D Scheduled Pod Capsule
  drawBox3D(ctx, { x: arcX, y: arcY + 6, z: arcZ }, 6.8, 13, 6.8, pitch, yaw, project, {
    topRgb: palette.emerald,
    sideRgb: palette.primary,
    edgeRgb: '255, 255, 255',
    topAlpha: 0.94,
    litAlpha: 0.68,
    darkAlpha: 0.42,
    edgeAlpha: 0.95,
    localYaw: timeSec * 1.8,
  })

  // Register Spatial Interactive Hotspots
  drawSpatialHotspot(args, cpAnchor, {
    id: 'api-core',
    label: 'API-SERVER',
    title: 'Kubernetes 控制面核心',
    metric: 'etcd · kube-scheduler · controller',
    status: 'primary',
    offsetY: -22,
  })
  drawSpatialHotspot(args, nodesAnchor, {
    id: 'nodes-pool',
    label: 'NODES',
    title: '集群 Worker 节点蜂巢池',
    metric: '节点计算池与 kubelet 状态',
    status: 'healthy',
    offsetY: -18,
  })
  drawSpatialHotspot(args, deployAnchor, {
    id: 'deployments',
    label: 'DEPLOYMENTS',
    title: '工作负载副本编排',
    metric: 'ReplicaSet 滚动调度',
    status: 'healthy',
    offsetY: -18,
  })
  drawSpatialHotspot(args, riskAnchor, {
    id: 'risk-pods',
    label: 'POD-HEALTH',
    title: '异常与重启 Pod 容器',
    metric: '点击定位异常 Pod 实例',
    status: hasRiskPods ? 'critical' : 'healthy',
    offsetY: -22,
  })
  drawSpatialHotspot(args, alertsAnchor, {
    id: 'cluster-alerts',
    label: 'ALERTS',
    title: '当前集群活跃告警',
    metric: alertsOv?.metric ?? '集群告警流',
    status: alertsOv?.status ?? 'warning',
    offsetY: -18,
  })
}
