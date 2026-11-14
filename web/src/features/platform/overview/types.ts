export interface AlertSummary {
  totalCount: number
  firingCount: number
  resolvedCount: number
  criticalCount: number
  warningCount: number
  infoCount: number
  channelCount: number
  lastReceivedAt?: string
}

export interface WorkloadOverviewNamespace {
  namespace: string
  totalPods: number
  runningPods: number
  atRiskPods: number
  restartingPods: number
}

export interface WorkloadOverviewPod {
  name: string
  namespace: string
  phase: string
  readyContainers: string
  restarts: number
  nodeName?: string
  ageSeconds: number
}

export interface WorkloadOverview {
  clusterId: string
  namespace?: string
  source: string
  generatedAt: string
  totalPods: number
  runningPods: number
  pendingPods: number
  succeededPods: number
  failedPods: number
  unknownPods: number
  restartingPods: number
  atRiskPods: number
  namespaceBreakdown?: WorkloadOverviewNamespace[]
  problematicPods?: WorkloadOverviewPod[]
}

export interface AggregatedNamespaceBreakdown extends WorkloadOverviewNamespace {
  clusterId: string
  clusterName: string
}

export interface AggregatedProblematicPod extends WorkloadOverviewPod {
  clusterId: string
  clusterName: string
}

export interface AggregatedWorkloadOverview extends Omit<
  WorkloadOverview,
  'clusterId' | 'namespace' | 'generatedAt' | 'source' | 'namespaceBreakdown' | 'problematicPods'
> {
  generatedAt: string
  source: string
  namespaceBreakdown: AggregatedNamespaceBreakdown[]
  problematicPods: AggregatedProblematicPod[]
}

export type OverviewActivitySource = 'kubernetes' | 'audit' | 'operation'

export interface OverviewActivityItem {
  id: string
  source: OverviewActivitySource
  title: string
  description: string
  status: string
  timestamp: string
  path: string
}

export interface OverviewCapacitySummary {
  cpuPercent?: number
  memoryPercent?: number
  cpuRequestPercent?: number
  memoryRequestPercent?: number
  podPercent?: number
  readyNodes: number
  totalNodes: number
  unschedulableNodes: number
  signalNodes: number
}

export interface OverviewCapabilityGovernance {
  approvalRequired: number
  highRisk: number
  partial: number
  supported: number
  items: Array<{
    key: string
    label: string
    riskLevel: string
    requiresApproval: boolean
    status: string
  }>
}

export interface OverviewFleetReadiness {
  healthy: number
  stale: number
  unhealthy: number
  versions: number
}

export type OverviewResourceKind =
  | 'pods'
  | 'deployments'
  | 'services'
  | 'configmaps'
  | 'namespaces'
  | 'nodes'
  | 'hpas'
  | 'networkpolicies'

export interface OverviewResourceSearchResult {
  key: string
  kind: OverviewResourceKind
  name: string
  namespace?: string
  status?: string
  path: string
}

export interface OverviewResourceSearchPage {
  items: OverviewResourceSearchResult[]
  truncated: boolean
}
