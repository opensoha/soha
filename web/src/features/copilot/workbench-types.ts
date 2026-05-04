export interface WorkbenchSessionScope {
  clusterId?: string
  namespace?: string
  workload?: string
  service?: string
  alertId?: string
  timeRangeMinutes?: number
}

export interface WorkbenchSessionToolset {
  enabledAdapterIds?: string[]
  enabledSkillIds?: string[]
  disabledToolNames?: string[]
  budgetOverrides?: Record<string, unknown>
  scopeOverrides?: Record<string, unknown>
}

export interface WorkbenchRunRef {
  id: string
  kind: string
  status?: string
  createdAt?: string
}

export interface WorkbenchSession {
  id: string
  title: string
  createdBy?: string
  updatedAt: string
  createdAt?: string
  metadata?: {
    mode?: 'general' | 'root_cause' | 'performance' | 'trace' | 'inspection_review'
    status?: string
    summary?: string
    tags?: string[]
    archivedAt?: string
    scope?: WorkbenchSessionScope
    toolset?: WorkbenchSessionToolset
    analysisRunRefs?: WorkbenchRunRef[]
  }
}

export interface WorkbenchMessage {
  id: string
  sessionId: string
  role: 'user' | 'assistant' | 'system'
  content: string
  metadata?: Record<string, unknown>
  createdAt: string
}

export interface WorkbenchToolCall {
  id: string
  adapterId: string
  toolName: string
  status: string
  summary?: string
  input?: Record<string, unknown>
  output?: Record<string, unknown>
  startedAt: string
  completedAt?: string
}

export interface WorkbenchEvidence {
  id: string
  kind: string
  title: string
  summary: string
  severity?: string
  attributes?: Record<string, unknown>
}

export interface WorkbenchHypothesis {
  id: string
  title: string
  summary: string
  confidence: number
  evidenceIds?: string[]
  recommendations?: string[]
}

export interface WorkbenchArtifact {
  kind: string
  runId: string
  title?: string
  summary: string
  scope?: WorkbenchSessionScope
  evidence?: WorkbenchEvidence[]
  hypotheses?: WorkbenchHypothesis[]
  recommendations?: string[]
  toolExecutions?: WorkbenchToolCall[]
  dataSourceSnapshot?: Record<string, unknown>
}

export interface WorkbenchMessageEnvelope {
  messages: WorkbenchMessage[]
  toolCalls?: WorkbenchToolCall[]
  analysisArtifacts?: WorkbenchArtifact[]
  sessionPatch?: Record<string, unknown>
}

export interface WorkbenchDataSource {
  id: string
  name: string
  sourceKind: string
  backendType: string
  enabled: boolean
  mcpAdapter: string
  validationStatus?: string
  validationMessage?: string
}

export interface WorkbenchAdapter {
  id: string
  name: string
  description: string
  sourceKind: string
  supportedBackends?: string[]
  category?: string
  requiresConfig?: boolean
  supportsSessionOverride?: boolean
  tools?: Array<{ name: string; description: string }>
  defaultBudget?: Record<string, unknown>
}
