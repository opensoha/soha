export type ReleaseDagNodeType =
  | 'build'
  | 'manual_approval'
  | 'deploy_update_image'
  | 'wait_rollout'
  | 'check_http'
  | 'check_k8s_event'
  | 'smoke_test'
  | 'notify'
  | 'restart_workload'
  | 'scale_workload'
  | 'delete_pod'
  | 'evict_pod'
  | 'http_callback'
  | 'create_silence'
  | 'rollback_to_previous'

export type ReleaseDagEdgeCondition = 'success' | 'failure' | 'always'

export interface ReleaseDagNodeDefinition {
  id: string
  type: ReleaseDagNodeType
  name: string
  position: { x: number; y: number }
  timeoutSeconds?: number
  continueOnFailure?: boolean
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
  mode: 'release_dag'
  nodes: ReleaseDagNodeDefinition[]
  edges: ReleaseDagEdgeDefinition[]
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
    case 'smoke_test':
      return 'Smoke Test'
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
      return { approverRoles: ['release-manager'], required: true }
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
  if (nodeItems.length === 0) {
    return normalizeLegacyDefinition(value)
  }
  return {
    schemaVersion: Number(value.schemaVersion || 2),
    mode: 'release_dag',
    nodes: nodeItems.map((nodeRaw, index) => {
      const node = toConfigObject(nodeRaw)
      const type = String(node.type || 'deploy_update_image') as ReleaseDagNodeType
      const position = toConfigObject(node.position)
      return {
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
    }),
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
