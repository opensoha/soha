import type { DeliveryBatch, DeliveryTargetInput, DeliveryWorkflowDefinition } from '../types'

export const deliveryActionLabels = {
  build_deploy: '构建并部署',
  build: '仅构建',
  deploy: '部署已有产物',
  config_update: '仅更新配置',
}
export const deliveryTargetActionLabel = (target: DeliveryTargetInput) =>
  target.action === 'config_update' && target.helmRevision
    ? `恢复 Helm 版本 ${target.helmRevision}`
    : deliveryActionLabels[target.action]
export const deliveryModeLabels = {
  service_serial: '逐服务完整交付',
  build_all_then_deploy: '先构建后部署',
}
export const deliveryStatusLabels: Record<string, string> = {
  queued: '等待执行',
  pending: '等待执行',
  running: '执行中',
  waiting_execution: '等待执行器',
  waiting_approval: '待审批',
  canceling: '正在取消',
  completed: '已完成',
  partially_completed: '部分完成',
  failed: '失败',
  canceled: '已取消',
  skipped: '已跳过',
}
export const deliveryStageLabels = {
  build: '构建',
  plan: '预检与审批',
  deploy: '部署',
  health: '就绪',
}
export const isBatchTerminal = (status: string) =>
  ['completed', 'partially_completed', 'failed', 'canceled'].includes(status)

export function newDeliveryTarget(): DeliveryTargetInput {
  return { id: crypto.randomUUID(), applicationId: '', serviceId: '', action: 'build_deploy' }
}

export function deliveryServiceCount(targets: DeliveryTargetInput[]) {
  return new Set(targets.map((item) => `${item.applicationId}/${item.serviceId}`)).size
}

export function validateDeliveryEditor(definition: DeliveryWorkflowDefinition): string | undefined {
  if (!definition.name.trim()) return '请填写名称'
  if (!definition.targets.length) return '请添加交付目标'
  const identities = new Set<string>()
  for (const [index, target] of definition.targets.entries()) {
    const identity = `${target.applicationId}/${target.serviceId}/${target.applicationEnvironmentId ?? ''}`
    if (identities.has(identity)) return `第 ${index + 1} 项重复选择了同一服务环境`
    identities.add(identity)
    if (
      !target.applicationId ||
      !target.serviceId ||
      (target.action !== 'build' && !target.applicationEnvironmentId)
    )
      return `请补全第 ${index + 1} 项的应用、服务和环境`
    for (const dependency of target.dependsOn ?? []) {
      const position = definition.targets.findIndex((item) => item.id === dependency)
      if (position < 0 || position === index) return `第 ${index + 1} 项的依赖无效`
      if (definition.mode !== 'build_all_then_deploy' && position >= index)
        return `第 ${index + 1} 项依赖后续服务，请先调整顺序`
      if (
        definition.mode === 'build_all_then_deploy' &&
        (definition.targets[position].group ?? 0) > (target.group ?? 0)
      )
        return `第 ${index + 1} 项依赖后续分组，请先调整分组`
    }
  }
  const done = new Set<string>(),
    visiting = new Set<string>()
  const cycle = (id: string): boolean => {
    if (visiting.has(id)) return true
    if (done.has(id)) return false
    visiting.add(id)
    if (definition.targets.find((item) => item.id === id)?.dependsOn?.some(cycle)) return true
    visiting.delete(id)
    done.add(id)
    return false
  }
  if (definition.targets.some((item) => cycle(item.id))) return '成功依赖形成循环，请调整依赖'
}

export function targetStageNode(batch: DeliveryBatch, targetId: string, stage: string) {
  const snapshot = batch.targets.find((item) => item.target.id === targetId)
  return batch.nodes.find((node) =>
    stage === 'build'
      ? node.nodeId === snapshot?.buildNodeId
      : node.targetId === targetId && node.stage === stage,
  )
}
