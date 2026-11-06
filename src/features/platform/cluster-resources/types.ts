export interface ClusterScope {
  readonly clusterId: string | null
  readonly namespace: null
}

export interface ResourceQuantity {
  cpu?: string
  memory?: string
  ephemeralStorage?: string
  pods?: string
}

export interface ResourcePercentage {
  cpu?: number
  memory?: number
  ephemeralStorage?: number
  pods?: number
}

export interface NodeResourceSummary {
  capacity?: ResourceQuantity
  allocatable?: ResourceQuantity
  requests?: ResourceQuantity
  limits?: ResourceQuantity
  usage?: ResourceQuantity
  requestPercentages?: ResourcePercentage
  limitPercentages?: ResourcePercentage
  usagePercentages?: ResourcePercentage
}

export interface ClusterNode {
  labels?: Record<string, string>
  name: string
  status: string
  unschedulable: boolean
  taints?: NodeTaint[] | null
  conditions?: NodeCondition[] | null
  roles: string[]
  version?: string
  internalIp?: string
  podCount: number
  ageSeconds: number
  resources?: NodeResourceSummary
  allowedActions?: string[]
}

export interface NodeTaint {
  key: string
  value?: string
  effect: string
}

export interface NodePod {
  name: string
  namespace: string
  phase: string
  podIp?: string
  readyContainers: string
  restarts: number
  cpu?: string
  memory?: string
  labels?: Record<string, string>
  requests?: ResourceQuantity
  limits?: ResourceQuantity
  ageSeconds: number
}

export interface NodeCondition {
  type: string
  status: string
  reason?: string
  message?: string
  lastTransitionTime?: string
}

export interface ClusterNodeDetail extends ClusterNode {
  taints?: NodeTaint[]
  conditions?: NodeCondition[]
  labels?: Record<string, string>
  annotations?: Record<string, string>
  metricsConfigured?: boolean
  metricsMessage?: string
  pods?: NodePod[]
}

export interface ClusterNamespace {
  name: string
  status: string
  ageSeconds?: number
  labels: Record<string, string>
  annotations?: Record<string, string>
  allowedActions?: string[]
}

export interface NodeYAMLView {
  kind: string
  name: string
  namespace?: string
  content: string
}

export interface NodeTarget {
  scope: ClusterScope
  name: string
}

export interface NodeUpdateInput {
  labels: Record<string, string>
  taints: NodeTaint[]
}

export interface UpdateNodeVariables extends NodeTarget {
  input: NodeUpdateInput
}

export interface ApplyNodeYAMLVariables extends NodeTarget {
  content: string
}

export interface SetNodeSchedulabilityVariables extends NodeTarget {
  unschedulable: boolean
}

export interface DrainNodeVariables extends NodeTarget {
  force: boolean
  deleteEmptyDirData: boolean
  timeoutSeconds: number
}

export interface NamespaceInput {
  name: string
  labels: Record<string, string>
  annotations: Record<string, string>
}

export interface NamespaceTarget {
  scope: ClusterScope
  name: string
}

export interface CreateNamespaceVariables {
  scope: ClusterScope
  input: NamespaceInput
}

export interface UpdateNamespaceVariables extends NamespaceTarget {
  input: NamespaceInput
}
