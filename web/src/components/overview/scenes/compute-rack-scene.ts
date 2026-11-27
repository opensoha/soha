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

/**
 * COMPUTE WORKBENCH 3D SCENE:
 * Physical Data-Center Hardware Architecture:
 * - Two 42U-style 3D Server Blade Racks (with stacked 1U/2U blades & blinking drive LEDs)
 * - Center Stage: Exploded Bare-Metal Hypervisor Chassis sliding out to expose a glowing
 *   CPU/GPU Silicon Wafer Die + Translucent 3D Virtual Machine (VM) Containers levitating above!
 * - Floor: Strictly 90-degree Orthogonal ToR Switch & Fiber Bus Traces (zero curved space-arcs!).
 */
export function renderComputeRackScene(args: SceneRenderArgs) {
  const { ctx, cx, cy, sceneScale, timeSec, pitch, yaw, palette, nodesMap } = args
  const project = createProjector(cx, cy, sceneScale, 720)

  const vmAlertOv = nodesMap.get('vm-alert')
  const hostOv = nodesMap.get('runtime-hosts')
  const taskOv = nodesMap.get('tasks-node')

  const hasVmError = vmAlertOv
    ? vmAlertOv.status === 'critical' || vmAlertOv.status === 'warning'
    : false
  const hasHostError = hostOv ? hostOv.status === 'critical' || hostOv.status === 'warning' : false

  // 1. Datacenter Raised-Floor Tile Grid (Orthogonal Tiles at y = 92)
  ctx.lineWidth = 0.75
  ctx.strokeStyle = rgba(palette.stroke, palette.isLight ? 0.16 : 0.12)
  for (let gx = -210; gx <= 210; gx += 42) {
    const a = project(rotate3D({ x: gx, y: 92, z: -135 }, pitch, yaw))
    const b = project(rotate3D({ x: gx, y: 92, z: 135 }, pitch, yaw))
    ctx.beginPath()
    ctx.moveTo(a.x, a.y)
    ctx.lineTo(b.x, b.y)
    ctx.stroke()
  }
  for (let gz = -135; gz <= 135; gz += 45) {
    const a = project(rotate3D({ x: -210, y: 92, z: gz }, pitch, yaw))
    const b = project(rotate3D({ x: 210, y: 92, z: gz }, pitch, yaw))
    ctx.beginPath()
    ctx.moveTo(a.x, a.y)
    ctx.lineTo(b.x, b.y)
    ctx.stroke()
  }

  // 2. 90-Degree Manhattan Copper/Fiber ToR Bus Traces on Floor
  const busTraces: Array<{ pts: Vec3[]; rgb: string; speed: number; phase: number }> = [
    {
      pts: [
        { x: -148, y: 90, z: -18 },
        { x: -78, y: 90, z: -18 },
        { x: -78, y: 90, z: 16 },
        { x: 0, y: 90, z: 16 },
      ],
      rgb: palette.emerald,
      speed: 0.45,
      phase: 0.1,
    },
    {
      pts: [
        { x: 148, y: 90, z: -18 },
        { x: 78, y: 90, z: -18 },
        { x: 78, y: 90, z: 16 },
        { x: 0, y: 90, z: 16 },
      ],
      rgb: hasHostError ? palette.rose : palette.primary,
      speed: 0.52,
      phase: 0.55,
    },
    {
      pts: [
        { x: 0, y: 90, z: 62 },
        { x: 0, y: 90, z: 112 },
        { x: 115, y: 90, z: 112 },
      ],
      rgb: palette.amber,
      speed: 0.36,
      phase: 0.3,
    },
  ]
  busTraces.forEach((b) => {
    drawPath3DWithPulse(ctx, b.pts, pitch, yaw, project, {
      rgb: b.rgb,
      lineAlpha: palette.isLight ? 0.42 : 0.46,
      lineWidth: 1.7,
      pulseProgress: timeSec * b.speed + b.phase,
      pulseRgb: b.rgb,
    })
  })

  // Helper to draw a realistic 3D 42U Server Blade Cabinet with individual 1U Blades & Drive LEDs!
  const drawServerRackCabinet = (
    rackPos: Vec3,
    hasFaultBlade: boolean,
    rackTintRgb: string,
  ): { topProj: ProjPt; faultProj: ProjPt } => {
    // Outer Anodized Server Rack Frame
    const outer = drawBox3D(ctx, rackPos, 40, 142, 34, pitch, yaw, project, {
      topRgb: rackTintRgb,
      sideRgb: palette.surfaceDark,
      edgeRgb: rackTintRgb,
      topAlpha: palette.isLight ? 0.24 : 0.32,
      litAlpha: palette.isLight ? 0.18 : 0.48,
      darkAlpha: palette.isLight ? 0.12 : 0.36,
      edgeAlpha: 0.68,
    })

    let faultProj = outer.topProj

    // 6 Stacked 1U/2U Server Blades slotted inside the front face (+Z = front)
    for (let slot = 0; slot < 6; slot += 1) {
      const bladeBaseY = rackPos.y - 14 - slot * 20
      const isFault = hasFaultBlade && slot === 4
      const pullOutZ = isFault ? 8 + Math.sin(timeSec * 2.2) * 2.5 : 2
      const bladeRgb = isFault ? palette.rose : slot % 2 === 0 ? palette.primary : palette.emerald

      const bRes = drawBox3D(
        ctx,
        { x: rackPos.x, y: bladeBaseY, z: rackPos.z + pullOutZ },
        34,
        14,
        30,
        pitch,
        yaw,
        project,
        {
          topRgb: bladeRgb,
          sideRgb: isFault ? palette.rose : rackTintRgb,
          edgeRgb: isFault ? palette.rose : palette.stroke,
          topAlpha: isFault ? 0.72 : 0.34,
          litAlpha: isFault ? 0.65 : 0.38,
          darkAlpha: 0.24,
          edgeAlpha: isFault ? 0.92 : 0.46,
        },
      )

      if (isFault) faultProj = bRes.topProj

      // Front Bezel Blinking NVMe Drive Activity LEDs!
      for (let led = -2; led <= 2; led += 1) {
        const blink = Math.sin(timeSec * (5 + slot * 1.3 + led * 2.1)) > -0.15
        const ledPt = project(
          rotate3D(
            {
              x: rackPos.x + led * 10,
              y: bladeBaseY - 7,
              z: rackPos.z + pullOutZ + 31,
            },
            pitch,
            yaw,
          ),
        )
        ctx.beginPath()
        ctx.arc(ledPt.x, ledPt.y, 1.9 * ledPt.scale, 0, Math.PI * 2)
        const lRgb = isFault ? palette.rose : led === 2 ? palette.amber : palette.emerald
        ctx.fillStyle = rgba(lRgb, blink || isFault ? 0.96 : 0.24)
        ctx.fill()
      }
    }

    return { topProj: outer.topProj, faultProj }
  }

  // Depth-sort Left Rack (PVE Hypervisors), Center Exploded Silicon Board + Levitating VMs, Right Rack (Docker/Agent Hosts)
  const sections = [
    { key: 'left-rack', z: rotate3D({ x: -148, y: 30, z: -24 }, pitch, yaw).z },
    { key: 'center-hypervisor', z: rotate3D({ x: 0, y: 40, z: 16 }, pitch, yaw).z },
    { key: 'right-rack', z: rotate3D({ x: 148, y: 30, z: -24 }, pitch, yaw).z },
  ].sort((a, b) => b.z - a.z)

  let leftRackTop = project(rotate3D({ x: -148, y: -50, z: -24 }, pitch, yaw))
  let rightRackTop = project(rotate3D({ x: 148, y: -50, z: -24 }, pitch, yaw))
  let rightFaultProj = rightRackTop
  let vmFleetTop = project(rotate3D({ x: 0, y: -25, z: 16 }, pitch, yaw))
  let vmErrTop = project(rotate3D({ x: 34, y: -18, z: 16 }, pitch, yaw))
  let svcTop = project(rotate3D({ x: -34, y: -18, z: 48 }, pitch, yaw))

  sections.forEach((sec) => {
    if (sec.key === 'left-rack') {
      const r = drawServerRackCabinet({ x: -148, y: 92, z: -24 }, false, palette.primary)
      leftRackTop = r.topProj
    } else if (sec.key === 'right-rack') {
      const r = drawServerRackCabinet({ x: 148, y: 92, z: -24 }, hasHostError, palette.emerald)
      rightRackTop = r.topProj
      rightFaultProj = r.faultProj
    } else {
      // CENTER: Exploded Bare-Metal Server Chassis + Glowing CPU/GPU Silicon Die + Levitating VM Sandbox Slices!
      drawBox3D(ctx, { x: 0, y: 90, z: 16 }, 74, 14, 58, pitch, yaw, project, {
        topRgb: palette.emerald,
        sideRgb: palette.surfaceDark,
        edgeRgb: palette.emerald,
        topAlpha: palette.isLight ? 0.22 : 0.28,
        litAlpha: palette.isLight ? 0.3 : 0.46,
        darkAlpha: 0.32,
        edgeAlpha: 0.72,
      })

      // Gold/Amber CPU/GPU Socket Pins Frame + Glowing Silicon Wafer Die in the center of the PCB!
      drawBox3D(ctx, { x: 0, y: 76, z: 16 }, 34, 6, 28, pitch, yaw, project, {
        topRgb: palette.amber,
        sideRgb: palette.amber,
        edgeRgb: palette.amber,
        topAlpha: 0.45,
        litAlpha: 0.4,
        darkAlpha: 0.25,
        edgeAlpha: 0.78,
      })
      drawBox3D(ctx, { x: 0, y: 70, z: 16 }, 22, 7, 18, pitch, yaw, project, {
        topRgb: palette.emerald,
        sideRgb: palette.primary,
        edgeRgb: '255, 255, 255',
        topAlpha: 0.85,
        litAlpha: 0.62,
        darkAlpha: 0.4,
        edgeAlpha: 0.92,
      })

      // 4 Translucent 3D Virtual Machine (VM) Containers levitating above the Hypervisor Silicon Die!
      const vmSlices: Array<{ dx: number; dz: number; h: number; fault?: boolean }> = [
        { dx: -30, dz: -18, h: 32 },
        { dx: 30, dz: -18, h: 38, fault: hasVmError },
        { dx: -30, dz: 22, h: 28 },
        { dx: 30, dz: 22, h: 34 },
      ]

      const sortedVms = vmSlices
        .map((v, i) => ({
          ...v,
          i,
          z: rotate3D({ x: v.dx, y: 36, z: 16 + v.dz }, pitch, yaw).z,
        }))
        .sort((a, b) => b.z - a.z)

      sortedVms.forEach((vm) => {
        const hoverFloat = Math.sin(timeSec * 1.7 + vm.i * 1.2) * 4.5
        const baseY = 44 + hoverFloat
        const vRgb = vm.fault ? palette.rose : vm.i % 2 === 0 ? palette.primary : palette.emerald

        // Vertical Hypervisor Virtualization Beam from PCB to levitating VM
        const pcbAnchor = project(
          rotate3D({ x: vm.dx * 0.65, y: 70, z: 16 + vm.dz * 0.65 }, pitch, yaw),
        )
        const vmBottom = project(rotate3D({ x: vm.dx, y: baseY, z: 16 + vm.dz }, pitch, yaw))
        ctx.save()
        ctx.setLineDash([2, 3])
        ctx.beginPath()
        ctx.moveTo(pcbAnchor.x, pcbAnchor.y)
        ctx.lineTo(vmBottom.x, vmBottom.y)
        ctx.lineWidth = 1.1
        ctx.strokeStyle = rgba(vRgb, 0.55)
        ctx.stroke()
        ctx.restore()

        const vmBox = drawBox3D(
          ctx,
          { x: vm.dx, y: baseY, z: 16 + vm.dz },
          17,
          vm.h,
          14,
          pitch,
          yaw,
          project,
          {
            topRgb: vRgb,
            sideRgb: vRgb,
            edgeRgb: '255, 255, 255',
            topAlpha: vm.fault ? 0.86 : 0.56,
            litAlpha: vm.fault ? 0.58 : 0.34,
            darkAlpha: 0.22,
            edgeAlpha: vm.fault ? 0.95 : 0.68,
          },
        )

        if (vm.i === 0) vmFleetTop = vmBox.topProj
        if (vm.i === 1) vmErrTop = vmBox.topProj
        if (vm.i === 2) svcTop = vmBox.topProj
      })
    }
  })

  // Task Queue Module at Front-Right Bus Endpoint
  const taskBox = drawBox3D(ctx, { x: 115, y: 90, z: 112 }, 20, 22, 16, pitch, yaw, project, {
    topRgb: taskOv?.status === 'warning' ? palette.amber : palette.primary,
    sideRgb: palette.amber,
    edgeRgb: palette.amber,
    topAlpha: 0.62,
    litAlpha: 0.4,
    darkAlpha: 0.25,
    edgeAlpha: 0.78,
  })

  drawSpatialHotspot(args, leftRackTop, {
    id: 'virt-conn',
    label: 'HYPERVISOR',
    title: '虚拟化刀片机柜与连接池',
    metric: 'PVE / KVM 宿主机阵列',
    status: 'healthy',
    offsetY: -18,
  })
  drawSpatialHotspot(args, vmFleetTop, {
    id: 'vm-fleet',
    label: 'VMS',
    title: '悬浮虚拟机实例沙箱',
    metric: 'CPU / GPU 算力切片分配',
    status: 'primary',
    offsetY: -18,
  })
  drawSpatialHotspot(args, vmErrTop, {
    id: 'vm-alert',
    label: 'VM-CHECK',
    title: '虚拟机健康探针',
    metric: '实例运行状态监控',
    status: hasVmError ? 'critical' : 'healthy',
    offsetY: -20,
  })
  drawSpatialHotspot(args, svcTop, {
    id: 'runtime-svc',
    label: 'CONTAINERS',
    title: '容器运行时编排服务',
    metric: 'Compose / Docker 服务栈',
    status: 'primary',
    offsetY: 26,
  })
  drawSpatialHotspot(args, hasHostError ? rightFaultProj : rightRackTop, {
    id: 'runtime-hosts',
    label: 'HOSTS',
    title: '裸金属运行时主机与 Agent',
    metric: '节点在线与探针状态',
    status: hasHostError ? 'critical' : 'healthy',
    offsetY: -18,
  })
  drawSpatialHotspot(args, taskBox.baseProj, {
    id: 'tasks-node',
    label: 'TASKS',
    title: '异步资源操作与同步队列',
    metric: '任务中心流水',
    status: taskOv?.status ?? 'healthy',
    offsetY: 22,
  })
}
