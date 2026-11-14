export type ReleaseDagNodeType =
  | 'build'
  | 'manual_approval'
  | 'deploy_update_image'
  | 'wait_rollout'
  | 'check_http'
  | 'check_k8s_event'
  | 'verify'
  | 'smoke_test'
  | 'external'
  | 'notify'
  | 'restart_workload'
  | 'scale_workload'
  | 'delete_pod'
  | 'evict_pod'
  | 'http_callback'
  | 'create_silence'
  | 'rollback_to_previous'

export type ReleaseDagEdgeCondition = 'success' | 'failure' | 'always'
export type ReleaseDagMode = 'release_dag' | 'delivery_dag'
export type DeliveryDagArtifactKind = 'image' | 'test_report' | 'scan_report' | 'sbom' | 'screenshot' | 'video' | 'junit' | 'log'
export type DeliveryDagFailurePolicy = 'stop' | 'continue' | 'rollback' | 'notify'

export interface DeliveryDagSelector {
  id?: string
  key?: string
  keys?: string[]
  matchLabels?: Record<string, string>
  matchExpressions?: Array<Record<string, unknown>>
}

export interface DeliveryDagArtifactOutput {
  name: string
  kind: DeliveryDagArtifactKind | string
  ref?: string
  path?: string
  required?: boolean
}

export interface ReleaseDagNodeDefinition {
  id: string
  type: ReleaseDagNodeType
  name: string
  position: { x: number; y: number }
  executorKind?: string
  targetKind?: string
  capabilityRef?: string
  providerRef?: string
  timeoutSeconds?: number
  continueOnFailure?: boolean
  inputs?: string[]
  outputs?: string[]
  serviceSelector?: DeliveryDagSelector
  environmentSelector?: DeliveryDagSelector
  targetSelector?: DeliveryDagSelector
  inputMapping?: Record<string, unknown>
  artifactOutputs?: DeliveryDagArtifactOutput[]
  artifactKinds?: string[]
  runCondition?: string
  failurePolicy?: DeliveryDagFailurePolicy | string
  observability?: Record<string, unknown>
  config: Record<string, unknown>
}

export interface ReleaseDagEdgeDefinition {
  id: string
  source: string
  target: string
  condition?: ReleaseDagEdgeCondition
}

export interface ReleaseDagDefinition {
  schemaVersion: number
  mode: ReleaseDagMode
  nodes: ReleaseDagNodeDefinition[]
  edges: ReleaseDagEdgeDefinition[]
}

export type ReleaseDagSeverity = 'error' | 'warning'

export interface ReleaseDagValidationIssue {
  severity: ReleaseDagSeverity
  message: string
  nodeIds?: string[]
  edgeIds?: string[]
}

export interface ReleaseDagAnalysis {
  definition: ReleaseDagDefinition
  nodeCount: number
  edgeCount: number
  validationNodeCount: number
  rollbackNodeCount: number
  approvalNodeCount: number
  buildNodeCount: number
  deployNodeCount: number
  artifactOutputCount: number
  selectorNodeCount: number
  conditionalNodeCount: number
  failureBranchCount: number
  isDeliveryDag: boolean
  entryNodeIds: string[]
  terminalNodeIds: string[]
  isolatedNodeIds: string[]
  invalidEdgeIds: string[]
  duplicateNodeIds: string[]
  selfLoopEdgeIds: string[]
  hasCycle: boolean
  isReleaseDagCompatible: boolean
  issues: ReleaseDagValidationIssue[]
}

export const RELEASE_DAG_VALIDATION_NODE_TYPES = new Set<string>([
  'check',
  'check_http',
  'check_k8s_event',
  'smoke_test',
  'verify',
  'verification',
  'test',
  'quality_gate',
])

const RELEASE_DAG_ROLLBACK_NODE_TYPES = new Set<string>(['rollback_to_previous', 'rollback'])
const RELEASE_DAG_APPROVAL_NODE_TYPES = new Set<string>(['manual_approval', 'approval'])
const RELEASE_DAG_BUILD_NODE_TYPES = new Set<string>(['build'])
const RELEASE_DAG_DEPLOY_NODE_TYPES = new Set<string>([
  'deploy',
  'deploy_update_image',
  'restart_workload',
  'scale_workload',
  'delete_pod',
  'evict_pod',
  'wait_rollout',
])

export function normalizeReleaseDagNodeType(value: unknown) {
  return String(value || '').trim().toLowerCase()
}

export function isReleaseDagValidationNodeType(value: unknown) {
  return RELEASE_DAG_VALIDATION_NODE_TYPES.has(normalizeReleaseDagNodeType(value))
}

function createGraphId(prefix: string) {
  return `${prefix}-${Math.random().toString(36).slice(2, 10)}`
}

export function getDefaultReleaseDagNodeLabel(type: ReleaseDagNodeType) {
  switch (type) {
    case 'manual_approval':
      return '审批'
    case 'build':
      return '构建'
    case 'deploy_update_image':
      return '更新镜像'
    case 'wait_rollout':
      return '等待 Rollout'
    case 'check_http':
      return 'HTTP 检查'
    case 'check_k8s_event':
      return 'K8s 事件检查'
    case 'verify':
      return '验证'
    case 'smoke_test':
      return 'Smoke Test'
    case 'external':
      return '外部执行'
    case 'notify':
      return '通知'
    case 'restart_workload':
      return '重启工作负载'
    case 'scale_workload':
      return '扩缩容'
    case 'delete_pod':
      return '删除 Pod'
    case 'evict_pod':
      return '驱逐 Pod'
    case 'http_callback':
      return 'HTTP 回调'
    case 'create_silence':
      return '创建静默'
    case 'rollback_to_previous':
      return '失败回滚'
  }
}

export function createNodeConfig(type: ReleaseDagNodeType): Record<string, unknown> {
  switch (type) {
    case 'build':
      return { sourceRef: 'binding', refType: 'branch', refValue: '' }
    case 'manual_approval':
      return {
        approvalMode: 'any',
        approverRoles: ['release-manager'],
        approverUsers: [],
        approverTeams: [],
        required: true,
        requiredApprovals: 1,
        slaMinutes: 60,
        onTimeout: 'block',
        rejectAction: 'stop',
        changeWindow: {
          enabled: false,
          timezone: 'Asia/Shanghai',
          startsAt: '',
          endsAt: '',
        },
      }
    case 'deploy_update_image':
      return { targetRef: 'primary', imageTagSource: 'workflow_input' }
    case 'wait_rollout':
      return { timeoutSeconds: 300 }
    case 'check_http':
      return { url: '', expectedStatus: 200 }
    case 'check_k8s_event':
      return { eventType: 'Warning', reasonContains: '' }
    case 'smoke_test':
      return { endpoint: '', expectedStatus: 200 }
    case 'notify':
      return { channel: '', template: 'release-result' }
    case 'restart_workload':
      return { deploymentName: '' }
    case 'scale_workload':
      return { deploymentName: '', replicas: 1 }
    case 'delete_pod':
      return { podName: '' }
    case 'evict_pod':
      return { podName: '' }
    case 'http_callback':
      return { url: '', method: 'POST', expectedStatus: 200, body: '{}' }
    case 'create_silence':
      return { name: '', reason: '', durationMinutes: 60, matchers: { severity: ['critical'] } }
    case 'rollback_to_previous':
      return {}
    default:
      return {}
  }
}

export function createDagNode(type: ReleaseDagNodeType, position?: { x: number; y: number }): ReleaseDagNodeDefinition {
  return {
    id: createGraphId('node'),
    type,
    name: getDefaultReleaseDagNodeLabel(type),
    position: position ?? { x: 120, y: 120 },
    timeoutSeconds: 300,
    continueOnFailure: false,
    config: createNodeConfig(type),
  }
}

function toConfigObject(value: unknown) {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function toArray(value: unknown) {
  return Array.isArray(value) ? value : []
}

function toStringList(value: unknown) {
  if (!Array.isArray(value)) return undefined
  const items = value.map((item) => String(item).trim()).filter(Boolean)
  return items.length > 0 ? items : undefined
}

function normalizeSelector(value: unknown): DeliveryDagSelector | undefined {
  const selector = toConfigObject(value)
  if (Object.keys(selector).length === 0) return undefined
  const matchLabels = toConfigObject(selector.matchLabels)
  const keys = toStringList(selector.keys)
  return {
    ...(selector.id ? { id: String(selector.id) } : {}),
    ...(selector.key ? { key: String(selector.key) } : {}),
    ...(keys ? { keys } : {}),
    ...(Object.keys(matchLabels).length > 0
      ? { matchLabels: Object.fromEntries(Object.entries(matchLabels).map(([key, item]) => [key, String(item)])) }
      : {}),
    ...(Array.isArray(selector.matchExpressions) ? { matchExpressions: selector.matchExpressions as Array<Record<string, unknown>> } : {}),
  }
}

function normalizeArtifactOutputs(value: unknown): DeliveryDagArtifactOutput[] | undefined {
  const items = toArray(value)
    .map((raw, index) => {
      const item = toConfigObject(raw)
      const name = String(item.name || item.ref || `artifact-${index + 1}`).trim()
      const kind = String(item.kind || 'image').trim()
      if (!name || !kind) return null
      return {
        name,
        kind,
        ...(item.ref ? { ref: String(item.ref) } : {}),
        ...(item.path ? { path: String(item.path) } : {}),
        ...(item.required !== undefined ? { required: Boolean(item.required) } : {}),
      }
    })
    .filter(Boolean) as DeliveryDagArtifactOutput[]
  return items.length > 0 ? items : undefined
}

function normalizeDagMode(value: unknown): ReleaseDagMode {
  return value === 'delivery_dag' ? 'delivery_dag' : 'release_dag'
}

function normalizeDagNode(nodeRaw: unknown, index: number): ReleaseDagNodeDefinition {
  const node = toConfigObject(nodeRaw)
  const type = String(node.type || 'deploy_update_image') as ReleaseDagNodeType
  const position = toConfigObject(node.position)
  const normalized: ReleaseDagNodeDefinition = {
    id: String(node.id || createGraphId(`node-${index}`)),
    type,
    name: String(node.name || getDefaultReleaseDagNodeLabel(type)),
    position: {
      x: Number(position.x ?? 120 + index * 80),
      y: Number(position.y ?? 120 + index * 40),
    },
    timeoutSeconds: Number(node.timeoutSeconds || 300),
    continueOnFailure: Boolean(node.continueOnFailure),
    config: toConfigObject(node.config),
  }
  const inputs = toStringList(node.inputs)
  const outputs = toStringList(node.outputs)
  const artifactKinds = toStringList(node.artifactKinds)
  const artifactOutputs = normalizeArtifactOutputs(node.artifactOutputs)
  const serviceSelector = normalizeSelector(node.serviceSelector)
  const environmentSelector = normalizeSelector(node.environmentSelector)
  const targetSelector = normalizeSelector(node.targetSelector)
  const inputMapping = toConfigObject(node.inputMapping)
  if (node.executorKind) normalized.executorKind = String(node.executorKind)
  if (node.targetKind) normalized.targetKind = String(node.targetKind)
  if (node.capabilityRef) normalized.capabilityRef = String(node.capabilityRef)
  if (node.providerRef) normalized.providerRef = String(node.providerRef)
  if (inputs) normalized.inputs = inputs
  if (outputs) normalized.outputs = outputs
  if (artifactKinds) normalized.artifactKinds = artifactKinds
  if (artifactOutputs) normalized.artifactOutputs = artifactOutputs
  if (serviceSelector) normalized.serviceSelector = serviceSelector
  if (environmentSelector) normalized.environmentSelector = environmentSelector
  if (targetSelector) normalized.targetSelector = targetSelector
  if (Object.keys(inputMapping).length > 0) normalized.inputMapping = inputMapping
  if (node.runCondition) normalized.runCondition = String(node.runCondition)
  if (node.failurePolicy) normalized.failurePolicy = String(node.failurePolicy)
  if (node.observability && typeof node.observability === 'object' && !Array.isArray(node.observability)) {
    normalized.observability = node.observability as Record<string, unknown>
  }
  return normalized
}

function normalizeLegacyDefinition(raw: Record<string, unknown>): ReleaseDagDefinition {
  const stages = toArray(raw.stages)
  const legacySteps = toArray(raw.steps)
  const nodes: ReleaseDagNodeDefinition[] = []
  const edges: ReleaseDagEdgeDefinition[] = []
  let previousLastNodeId = ''

  const stageItems = stages.length > 0
    ? stages
    : legacySteps.length > 0
      ? [{ name: 'deploy', steps: legacySteps }]
      : []

  stageItems.forEach((stageRaw, stageIndex) => {
    const stage = toConfigObject(stageRaw)
    const steps = toArray(stage.steps)
    let previousStepId = ''
    steps.forEach((stepRaw, stepIndex) => {
      const step = toConfigObject(stepRaw)
      const type = String(step.type || 'deploy_update_image') as ReleaseDagNodeType
      const node = createDagNode(type, { x: 120 + stageIndex * 320, y: 80 + stepIndex * 180 })
      node.id = String(step.id || createGraphId('node'))
      node.name = String(step.name || node.name)
      node.timeoutSeconds = Number(step.timeoutSeconds || 300)
      node.continueOnFailure = Boolean(step.continueOnFailure)
      node.config = toConfigObject(step.config)
      nodes.push(node)
      if (previousStepId) {
        edges.push({ id: createGraphId('edge'), source: previousStepId, target: node.id, condition: 'success' })
      }
      previousStepId = node.id
      if (!previousLastNodeId && stageIndex === 0 && stepIndex === 0) {
        previousLastNodeId = node.id
      }
    })
    if (previousLastNodeId && previousStepId && previousLastNodeId !== previousStepId) {
      edges.push({ id: createGraphId('edge'), source: previousLastNodeId, target: steps.length > 0 ? nodes[nodes.length - steps.length].id : previousStepId, condition: 'success' })
    }
    if (previousStepId) {
      previousLastNodeId = previousStepId
    }
  })

  const onFailure = toArray(raw.onFailure)
  let previousFailureNodeId = ''
  onFailure.forEach((stepRaw, index) => {
    const step = toConfigObject(stepRaw)
    const type = String(step.type || 'notify') as ReleaseDagNodeType
    const node = createDagNode(type, { x: 520 + index * 260, y: 520 })
    node.id = String(step.id || createGraphId('node'))
    node.name = String(step.name || node.name)
    node.timeoutSeconds = Number(step.timeoutSeconds || 300)
    node.continueOnFailure = Boolean(step.continueOnFailure)
    node.config = toConfigObject(step.config)
    nodes.push(node)
    if (index === 0 && previousLastNodeId) {
      edges.push({ id: createGraphId('edge'), source: previousLastNodeId, target: node.id, condition: 'failure' })
    }
    if (previousFailureNodeId) {
      edges.push({ id: createGraphId('edge'), source: previousFailureNodeId, target: node.id, condition: 'always' })
    }
    previousFailureNodeId = node.id
  })

  if (nodes.length === 0) {
    return createDefaultReleaseDagDefinition()
  }

  return {
    schemaVersion: 2,
    mode: 'release_dag',
    nodes,
    edges,
  }
}

export function normalizeReleaseDagDefinition(raw: unknown): ReleaseDagDefinition {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
    return createDefaultReleaseDagDefinition()
  }
  const value = raw as Record<string, unknown>
  const nodeItems = toArray(value.nodes)
  const edgeItems = toArray(value.edges)
  if (nodeItems.length === 0 && value.mode !== 'release_dag' && value.mode !== 'delivery_dag' && !('nodes' in value) && !('edges' in value)) {
    return normalizeLegacyDefinition(value)
  }
  return {
    schemaVersion: Number(value.schemaVersion || 2),
    mode: normalizeDagMode(value.mode),
    nodes: nodeItems.map(normalizeDagNode),
    edges: edgeItems.map((edgeRaw, index) => {
      const edge = toConfigObject(edgeRaw)
      return {
        id: String(edge.id || createGraphId(`edge-${index}`)),
        source: String(edge.source || ''),
        target: String(edge.target || ''),
        condition: String(edge.condition || 'success') as ReleaseDagEdgeCondition,
      }
    }).filter((edge) => edge.source && edge.target),
  }
}

export function createDefaultReleaseDagDefinition(): ReleaseDagDefinition {
  const approval = createDagNode('manual_approval', { x: 120, y: 120 })
  const deploy = createDagNode('deploy_update_image', { x: 420, y: 120 })
  const rollout = createDagNode('wait_rollout', { x: 720, y: 120 })
  const verify = createDagNode('check_http', { x: 1020, y: 120 })
  const rollback = createDagNode('rollback_to_previous', { x: 720, y: 360 })
  const notify = createDagNode('notify', { x: 1020, y: 360 })

  return {
    schemaVersion: 2,
    mode: 'release_dag',
    nodes: [approval, deploy, rollout, verify, rollback, notify],
    edges: [
      { id: createGraphId('edge'), source: approval.id, target: deploy.id, condition: 'success' },
      { id: createGraphId('edge'), source: deploy.id, target: rollout.id, condition: 'success' },
      { id: createGraphId('edge'), source: rollout.id, target: verify.id, condition: 'success' },
      { id: createGraphId('edge'), source: rollout.id, target: rollback.id, condition: 'failure' },
      { id: createGraphId('edge'), source: verify.id, target: notify.id, condition: 'success' },
      { id: createGraphId('edge'), source: rollback.id, target: notify.id, condition: 'always' },
    ],
  }
}

export function countReleaseDagNodes(raw: unknown) {
  return normalizeReleaseDagDefinition(raw).nodes.length
}

function hasCycle(nodeIds: Set<string>, edges: ReleaseDagEdgeDefinition[]) {
  const indegree = new Map<string, number>()
  const adjacent = new Map<string, string[]>()
  nodeIds.forEach((id) => {
    indegree.set(id, 0)
    adjacent.set(id, [])
  })
  edges.forEach((edge) => {
    if (!nodeIds.has(edge.source) || !nodeIds.has(edge.target) || edge.source === edge.target) return
    adjacent.get(edge.source)?.push(edge.target)
    indegree.set(edge.target, (indegree.get(edge.target) ?? 0) + 1)
  })

  const queue = Array.from(indegree.entries()).filter(([, degree]) => degree === 0).map(([id]) => id)
  let visited = 0
  while (queue.length > 0) {
    const id = queue.shift()
    if (!id) continue
    visited += 1
    adjacent.get(id)?.forEach((target) => {
      const nextDegree = (indegree.get(target) ?? 0) - 1
      indegree.set(target, nextDegree)
      if (nextDegree === 0) queue.push(target)
    })
  }
  return visited !== nodeIds.size
}

export function analyzeReleaseDagDefinition(raw: unknown): ReleaseDagAnalysis {
  const definition = normalizeReleaseDagDefinition(raw)
  const issues: ReleaseDagValidationIssue[] = []
  const seenNodeIds = new Set<string>()
  const duplicateNodeIds = Array.from(new Set(definition.nodes.flatMap((node) => {
    if (seenNodeIds.has(node.id)) return [node.id]
    seenNodeIds.add(node.id)
    return []
  })))
  const nodeIds = new Set(definition.nodes.map((node) => node.id))
  const invalidEdgeIds = definition.edges
    .filter((edge) => !nodeIds.has(edge.source) || !nodeIds.has(edge.target))
    .map((edge) => edge.id)
  const selfLoopEdgeIds = definition.edges
    .filter((edge) => edge.source === edge.target)
    .map((edge) => edge.id)
  const validEdges = definition.edges.filter((edge) => nodeIds.has(edge.source) && nodeIds.has(edge.target) && edge.source !== edge.target)
  const incoming = new Map<string, number>()
  const outgoing = new Map<string, number>()
  definition.nodes.forEach((node) => {
    incoming.set(node.id, 0)
    outgoing.set(node.id, 0)
  })
  validEdges.forEach((edge) => {
    outgoing.set(edge.source, (outgoing.get(edge.source) ?? 0) + 1)
    incoming.set(edge.target, (incoming.get(edge.target) ?? 0) + 1)
  })

  const entryNodeIds = definition.nodes.filter((node) => (incoming.get(node.id) ?? 0) === 0).map((node) => node.id)
  const terminalNodeIds = definition.nodes.filter((node) => (outgoing.get(node.id) ?? 0) === 0).map((node) => node.id)
  const isolatedNodeIds = definition.nodes
    .filter((node) => (incoming.get(node.id) ?? 0) === 0 && (outgoing.get(node.id) ?? 0) === 0)
    .map((node) => node.id)
  const cycleDetected = hasCycle(nodeIds, validEdges)

  const validationNodeCount = definition.nodes.filter((node) => isReleaseDagValidationNodeType(node.type)).length
  const rollbackNodeCount = definition.nodes.filter((node) => RELEASE_DAG_ROLLBACK_NODE_TYPES.has(normalizeReleaseDagNodeType(node.type))).length
  const approvalNodeCount = definition.nodes.filter((node) => RELEASE_DAG_APPROVAL_NODE_TYPES.has(normalizeReleaseDagNodeType(node.type))).length
  const buildNodeCount = definition.nodes.filter((node) => RELEASE_DAG_BUILD_NODE_TYPES.has(normalizeReleaseDagNodeType(node.type))).length
  const deployNodeCount = definition.nodes.filter((node) => RELEASE_DAG_DEPLOY_NODE_TYPES.has(normalizeReleaseDagNodeType(node.type))).length
  const artifactOutputCount = definition.nodes.reduce((sum, node) => sum + (node.artifactOutputs?.length ?? 0), 0)
  const selectorNodeCount = definition.nodes.filter((node) => node.serviceSelector || node.environmentSelector || node.targetSelector).length
  const conditionalNodeCount = definition.nodes.filter((node) => Boolean(node.runCondition)).length
  const failureBranchCount = definition.edges.filter((edge) => edge.condition === 'failure').length

  if (definition.nodes.length === 0) {
    issues.push({ severity: 'error', message: '至少需要 1 个 DAG 节点' })
  }
  if (duplicateNodeIds.length > 0) {
    issues.push({ severity: 'error', message: '节点 ID 不能重复', nodeIds: duplicateNodeIds })
  }
  if (invalidEdgeIds.length > 0) {
    issues.push({ severity: 'error', message: '连线 source/target 必须指向存在的节点', edgeIds: invalidEdgeIds })
  }
  if (selfLoopEdgeIds.length > 0) {
    issues.push({ severity: 'error', message: 'DAG 连线不能指向自身', edgeIds: selfLoopEdgeIds })
  }
  if (cycleDetected) {
    issues.push({ severity: 'error', message: 'DAG 不能包含环路' })
  }
  if (isolatedNodeIds.length > 0) {
    issues.push({ severity: 'warning', message: '存在孤立节点', nodeIds: isolatedNodeIds })
  }
  if (entryNodeIds.length === 0 && definition.nodes.length > 0) {
    issues.push({ severity: 'warning', message: '未发现入口节点' })
  }
  if (terminalNodeIds.length === 0 && definition.nodes.length > 0) {
    issues.push({ severity: 'warning', message: '未发现终点节点' })
  }
  if (validationNodeCount === 0) {
    issues.push({ severity: 'warning', message: '未配置验证节点' })
  }
  if (rollbackNodeCount === 0) {
    issues.push({ severity: 'warning', message: '未配置回滚节点' })
  }

  return {
    definition,
    nodeCount: definition.nodes.length,
    edgeCount: definition.edges.length,
    validationNodeCount,
    rollbackNodeCount,
    approvalNodeCount,
    buildNodeCount,
    deployNodeCount,
    artifactOutputCount,
    selectorNodeCount,
    conditionalNodeCount,
    failureBranchCount,
    isDeliveryDag: definition.mode === 'delivery_dag',
    entryNodeIds,
    terminalNodeIds,
    isolatedNodeIds,
    invalidEdgeIds,
    duplicateNodeIds,
    selfLoopEdgeIds,
    hasCycle: cycleDetected,
    isReleaseDagCompatible: !issues.some((issue) => issue.severity === 'error'),
    issues,
  }
}
