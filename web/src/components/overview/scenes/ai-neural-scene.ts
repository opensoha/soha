import {
  createProjector,
  drawBox3D,
  drawPath3DWithPulse,
  drawSpatialHotspot,
  rgba,
  rotate3D,
  type ProjPt,
  type SceneRenderArgs,
  type Vec3,
} from './math3d'

interface NeuronPt {
  pos: Vec3
  layer: number
  phase: number
}

const NEURONS: NeuronPt[] = (() => {
  const list: NeuronPt[] = []
  // Organic 3-shell 3D Cortical Hemisphere / Reasoning Cloud (36 neurons)
  const goldenAngle = Math.PI * (3 - Math.sqrt(5))
  for (let i = 0; i < 36; i += 1) {
    const yNorm = 1 - (i / 35) * 2
    const rAtY = Math.sqrt(Math.max(0, 1 - yNorm * yNorm))
    const theta = goldenAngle * i
    const shell = i % 3 === 0 ? 52 : i % 2 === 0 ? 74 : 88
    list.push({
      pos: {
        x: -12 + Math.cos(theta) * rAtY * shell * 1.08,
        y: yNorm * shell * 0.84,
        z: Math.sin(theta) * rAtY * shell,
      },
      layer: i % 3,
      phase: i * 0.47,
    })
  }
  return list
})()

const SYNAPSE_LINKS: Array<[number, number]> = (() => {
  const pairs: Array<[number, number]> = []
  for (let i = 0; i < NEURONS.length; i += 1) {
    for (let j = i + 1; j < NEURONS.length; j += 1) {
      const a = NEURONS[i].pos
      const b = NEURONS[j].pos
      const d = Math.hypot(a.x - b.x, a.y - b.y, a.z - b.z)
      if (d > 26 && d < 56 && pairs.length < 74) {
        pairs.push([i, j])
      }
    }
  }
  return pairs
})()

/**
 * AI WORKBENCH 3D SCENE:
 * Organic + Optical Agentic Architecture (ZERO server racks or generic circles!):
 *   1. Left: 3D Tilted RAG Vector Knowledge Shards (glass document slices streaming embeddings)
 *   2. Center: Bioluminescent 3D Neural Synapse Cortex (36 organic neurons firing cascading
 *      synaptic lightning pulses as Heya Sessions & Agent Runs reason!)
 *   3. Right: Refractive 3D Octahedral AI Gateway Prism + Human-Approval Shield splitting
 *      reasoning output into colored MCP Tool Laser Beams!
 */
export function renderAINeuralScene(args: SceneRenderArgs) {
  const { ctx, cx, cy, sceneScale, timeSec, pitch, yaw, palette, nodesMap } = args
  const project = createProjector(cx, cy, sceneScale, 700)

  const approvalOv = nodesMap.get('ai-approvals')
  const runsOv = nodesMap.get('agent-runs')
  const hasPendingApproval = approvalOv ? approvalOv.status === 'warning' : true
  const hasRunError = runsOv ? runsOv.status === 'critical' : false

  // 1. LEFT: 4 Floating 3D RAG Vector Knowledge Shards (tilted crystal document slabs)
  let ragTopProj: ProjPt = project(rotate3D({ x: -186, y: -24, z: 0 }, pitch, yaw))
  for (let k = 0; k < 4; k += 1) {
    const kz = (k - 1.5) * 32
    const ky = 18 + Math.sin(timeSec * 1.5 + k * 0.9) * 6
    const shard = drawBox3D(
      ctx,
      { x: -186 + k * 8, y: ky + 22, z: kz },
      4,
      46,
      20,
      pitch,
      yaw,
      project,
      {
        topRgb: palette.emerald,
        sideRgb: palette.primary,
        edgeRgb: '255, 255, 255',
        topAlpha: 0.72,
        litAlpha: 0.34,
        darkAlpha: 0.2,
        edgeAlpha: 0.72,
        localYaw: 0.28,
      },
    )
    if (k === 1) ragTopProj = shard.topProj
  }

  // Vector Embedding Particle Stream from RAG Shards -> Neural Cortex!
  drawPath3DWithPulse(
    ctx,
    [
      { x: -168, y: 2, z: -12 },
      { x: -84, y: 0, z: -4 },
    ],
    pitch,
    yaw,
    project,
    {
      rgb: palette.emerald,
      lineAlpha: 0.42,
      lineWidth: 1.6,
      pulseProgress: timeSec * 0.58,
      pulseRgb: palette.emerald,
    },
  )
  drawPath3DWithPulse(
    ctx,
    [
      { x: -168, y: 14, z: 22 },
      { x: -84, y: 8, z: 10 },
    ],
    pitch,
    yaw,
    project,
    {
      rgb: palette.primary,
      lineAlpha: 0.38,
      lineWidth: 1.4,
      pulseProgress: timeSec * 0.48 + 0.4,
      pulseRgb: palette.primary,
    },
  )

  // 2. CENTER: Bioluminescent 3D Neural Synapse Cortex ( HeyA Reasoning Brain )
  const brainSpin = timeSec * 0.16
  const rotNeurons = NEURONS.map((n) => {
    const cs = Math.cos(brainSpin)
    const ss = Math.sin(brainSpin)
    const lx = n.pos.x + 12
    const rx = -12 + (lx * cs + n.pos.z * ss)
    const rz = -lx * ss + n.pos.z * cs
    const breathe = 1 + Math.sin(timeSec * 1.8 + n.phase) * 0.04
    const w = rotate3D({ x: rx * breathe, y: n.pos.y * breathe, z: rz * breathe }, pitch, yaw)
    return { ...n, world: w, proj: project(w) }
  })

  // Organic Cortical Aura Glow behind Brain
  const coreCenter = project(rotate3D({ x: -12, y: 0, z: 0 }, pitch, yaw))
  const auraR = 98 * coreCenter.scale
  const aura = ctx.createRadialGradient(
    coreCenter.x,
    coreCenter.y,
    6,
    coreCenter.x,
    coreCenter.y,
    auraR,
  )
  aura.addColorStop(0, rgba(palette.secondary, palette.isLight ? 0.24 : 0.34))
  aura.addColorStop(0.55, rgba(palette.primary, palette.isLight ? 0.1 : 0.14))
  aura.addColorStop(1, 'rgba(0, 0, 0, 0)')
  ctx.fillStyle = aura
  ctx.beginPath()
  ctx.arc(coreCenter.x, coreCenter.y, auraR, 0, Math.PI * 2)
  ctx.fill()

  // Synaptic Dendrite Filaments & Live Firing Action-Potentials!
  SYNAPSE_LINKS.forEach(([i, j], sIdx) => {
    const a = rotNeurons[i]
    const b = rotNeurons[j]
    const vis = (a.proj.depthAlpha + b.proj.depthAlpha) * 0.5
    const firing = Math.sin(timeSec * 3.4 + sIdx * 0.73) > 0.72
    const synRgb =
      hasRunError && sIdx % 9 === 0 ? palette.rose : firing ? palette.primary : palette.secondary

    ctx.beginPath()
    ctx.moveTo(a.proj.x, a.proj.y)
    ctx.lineTo(b.proj.x, b.proj.y)
    ctx.lineWidth = firing ? 1.65 * vis : 0.85 * vis
    ctx.strokeStyle = rgba(synRgb, (firing ? 0.78 : 0.24) * vis)
    ctx.stroke()

    if (firing) {
      const u = (timeSec * 1.4 + sIdx * 0.19) % 1
      const sx = a.proj.x + (b.proj.x - a.proj.x) * u
      const sy = a.proj.y + (b.proj.y - a.proj.y) * u
      ctx.beginPath()
      ctx.arc(sx, sy, 2.4 * a.proj.scale, 0, Math.PI * 2)
      ctx.fillStyle = rgba('255, 255, 255', 0.92 * vis)
      ctx.fill()
    }
  })

  // Glowing Cortical Soma Nodes (depth-sorted)
  const sortedNeurons = [...rotNeurons].sort((a, b) => b.world.z - a.world.z)
  sortedNeurons.forEach((n, idx) => {
    const pulse = 0.65 + Math.sin(timeSec * 2.6 + n.phase) * 0.35
    const nRgb =
      hasRunError && idx === 3
        ? palette.rose
        : n.layer === 0
          ? palette.primary
          : n.layer === 1
            ? palette.secondary
            : palette.emerald
    const r = (2.6 + pulse * 2.2) * n.proj.scale
    ctx.beginPath()
    ctx.arc(n.proj.x, n.proj.y, r * 2.1, 0, Math.PI * 2)
    ctx.fillStyle = rgba(nRgb, 0.22 * n.proj.depthAlpha)
    ctx.fill()

    ctx.beginPath()
    ctx.arc(n.proj.x, n.proj.y, r, 0, Math.PI * 2)
    ctx.fillStyle = rgba('255, 255, 255', 0.92 * n.proj.depthAlpha)
    ctx.fill()
  })

  // 3. RIGHT: Refractive 3D Octahedral AI Gateway Crystal Prism (x = 128) + MCP Tool Fan-out Rays!
  const prismCx = 128
  const pSpin = timeSec * 0.45
  const octVerts: Vec3[] = [
    { x: prismCx, y: -44, z: 0 }, // top apex
    { x: prismCx, y: 44, z: 0 }, // bottom apex
    { x: prismCx + Math.cos(pSpin) * 32, y: 0, z: Math.sin(pSpin) * 32 },
    {
      x: prismCx + Math.cos(pSpin + Math.PI * 0.5) * 32,
      y: 0,
      z: Math.sin(pSpin + Math.PI * 0.5) * 32,
    },
    { x: prismCx + Math.cos(pSpin + Math.PI) * 32, y: 0, z: Math.sin(pSpin + Math.PI) * 32 },
    {
      x: prismCx + Math.cos(pSpin + Math.PI * 1.5) * 32,
      y: 0,
      z: Math.sin(pSpin + Math.PI * 1.5) * 32,
    },
  ]
  const octW = octVerts.map((v) => rotate3D(v, pitch, yaw))
  const octP = octW.map(project)
  const octFaces: Array<[number, number, number]> = [
    [0, 2, 3],
    [0, 3, 4],
    [0, 4, 5],
    [0, 5, 2],
    [1, 3, 2],
    [1, 4, 3],
    [1, 5, 4],
    [1, 2, 5],
  ]

  // Input Coherent Reasoning Beam from Neural Cortex -> Gateway Prism!
  drawPath3DWithPulse(
    ctx,
    [
      { x: 64, y: 0, z: 0 },
      { x: prismCx - 18, y: 0, z: 0 },
    ],
    pitch,
    yaw,
    project,
    {
      rgb: '255, 255, 255',
      lineAlpha: 0.72,
      lineWidth: 2.4,
      pulseProgress: timeSec * 0.75,
    },
  )

  octFaces
    .map(([i0, i1, i2], fi) => ({
      p0: octP[i0],
      p1: octP[i1],
      p2: octP[i2],
      fz: (octW[i0].z + octW[i1].z + octW[i2].z) / 3,
      fi,
    }))
    .sort((a, b) => b.fz - a.fz)
    .forEach((f) => {
      ctx.beginPath()
      ctx.moveTo(f.p0.x, f.p0.y)
      ctx.lineTo(f.p1.x, f.p1.y)
      ctx.lineTo(f.p2.x, f.p2.y)
      ctx.closePath()
      const fRgb = f.fi % 2 === 0 ? palette.primary : palette.secondary
      ctx.fillStyle = rgba(fRgb, palette.isLight ? 0.24 : 0.28)
      ctx.fill()
      ctx.lineWidth = 1.15
      ctx.strokeStyle = rgba('255, 255, 255', 0.72)
      ctx.stroke()
    })

  // Refracted Spectrum MCP Tool Laser Rays fanning out to the right!
  const toolRays: Array<{ end: Vec3; rgb: string }> = [
    { end: { x: 228, y: -42, z: -36 }, rgb: palette.primary },
    { end: { x: 232, y: 0, z: 0 }, rgb: palette.emerald },
    { end: { x: 228, y: 42, z: 36 }, rgb: palette.amber },
  ]
  toolRays.forEach((r, idx) => {
    drawPath3DWithPulse(ctx, [{ x: prismCx + 16, y: 0, z: 0 }, r.end], pitch, yaw, project, {
      rgb: r.rgb,
      lineAlpha: 0.68,
      lineWidth: 1.8,
      pulseProgress: timeSec * 0.6 + idx * 0.3,
      pulseRgb: r.rgb,
    })
  })

  // Human-in-the-Loop Approval Shield Ring around Gateway Prism!
  const shieldAnchor = project(rotate3D({ x: prismCx, y: 48, z: 24 }, pitch, yaw))
  if (hasPendingApproval) {
    ctx.beginPath()
    for (let s = 0; s <= 32; s += 1) {
      const a = (s / 32) * Math.PI * 2
      const sp = project(
        rotate3D({ x: prismCx + Math.cos(a) * 46, y: 0, z: Math.sin(a) * 46 }, pitch, yaw),
      )
      if (s === 0) ctx.moveTo(sp.x, sp.y)
      else ctx.lineTo(sp.x, sp.y)
    }
    ctx.lineWidth = 1.6
    ctx.strokeStyle = rgba(palette.amber, 0.78)
    ctx.stroke()
  }

  const brainTop = project(rotate3D({ x: -12, y: -74, z: 0 }, pitch, yaw))
  const brainBot = project(rotate3D({ x: -24, y: 68, z: 28 }, pitch, yaw))

  drawSpatialHotspot(args, ragTopProj, {
    id: 'rag-kb',
    label: 'RAG-KB',
    title: '向量知识库与文档切片流',
    metric: 'Embedding 语义检索注入',
    status: 'healthy',
    offsetY: -18,
  })
  drawSpatialHotspot(args, brainTop, {
    id: 'heya-core',
    label: 'HEYA-BRAIN',
    title: 'Heya 智能体突触推理中枢',
    metric: '多轮会话与认知规划',
    status: 'primary',
    offsetY: -18,
  })
  drawSpatialHotspot(args, brainBot, {
    id: 'agent-runs',
    label: 'AGENT-RUNS',
    title: 'Agent 异步任务执行流',
    metric: '运行队列状态',
    status: hasRunError ? 'critical' : 'healthy',
    offsetY: 26,
  })
  drawSpatialHotspot(args, octP[0], {
    id: 'ai-gateway',
    label: 'MCP-GATEWAY',
    title: 'AI Gateway 工具折射棱镜',
    metric: 'Model Relay & MCP Tools',
    status: 'primary',
    offsetY: -18,
  })
  drawSpatialHotspot(args, shieldAnchor, {
    id: 'ai-approvals',
    label: 'APPROVAL',
    title: '高风险工具人工审批护盾',
    metric: '点击处置待审批调用',
    status: hasPendingApproval ? 'warning' : 'healthy',
    offsetY: 24,
  })
}
