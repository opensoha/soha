import type { CapabilityPlan, WorkbenchInspectionTrigger } from '@opensoha/contracts/gen/ts/sohaapi'
export interface Insight {
  title: string
  description: string
  severity: string
  actions?: string[]
}

export interface RootCauseRun {
  id: string
  kind?: string
  title: string
  status: string
  severity: string
  summary: string
}

export interface InspectionRunSummary {
  id: string
  status: string
  severity: string
  summary: string
}

export interface InspectionTask {
  revision?: number
  capabilityPlan?: CapabilityPlan
  trigger?: WorkbenchInspectionTrigger
  aiClientId?: string
  skillId?: string
  id: string
  title: string
  scopeType: string
  clusterId?: string
  namespace?: string
  checks?: string[]
  enabled: boolean
  intervalMinutes: number
  lastRunAt?: string
  metadata?: Record<string, unknown>
}

export interface InspectionTaskFormValues {
  id?: string
  expectedRevision?: number
  mode?: 'legacy' | 'capability'
  planJSON?: string
  triggerKind?: 'schedule' | 'alert'
  alertRuleId?: string
  maxEventAgeSeconds?: number
  aiClientId?: string
  skillId?: string
  metadata?: Record<string, unknown>
  title?: string
  scopeType?: string
  clusterId?: string
  namespace?: string
  checks?: string[]
  enabled?: boolean
  intervalMinutes?: number
  analysisProfileId?: string
}

export interface AnalysisProfile {
  id: string
  name: string
  mode: string
  enabled: boolean
  remediationPolicy?: string
}

export interface AutomationPolicy {
  id: string
  name: string
  enabled: boolean
  triggerType: string
  analysisKinds?: string[]
  agentProviderId?: string
  triggerConditions?: Record<string, unknown>
  dedupWindowSeconds?: number
  analysisProfileId: string
  remediationPolicy: string
  approvalPolicy?: Record<string, unknown>
  cooldownSeconds?: number
}

export interface InspectionRun {
  report?: Record<string, unknown>
  id: string
  taskId: string
  status: string
  severity: string
  summary: string
  findings?: Array<{ id: string; title: string; severity: string }>
  startedAt: string
  completedAt?: string
}

export interface AutomationPolicyFormValues {
  name?: string
  triggerType?: string
  analysisKinds?: string[]
  agentProviderId?: string
  analysisProfileId?: string
  remediationPolicy?: string
  dedupWindowSeconds?: number
  cooldownSeconds?: number
  enabled?: boolean
  triggerSeverity?: string[]
  triggerStatus?: string[]
  triggerMinDurationSeconds?: number
  triggerLabelKey?: string
  triggerLabelValue?: string
  triggerTimeRangeMinutes?: number
  approvalRequired?: boolean
  approvalRoles?: string[]
}

export interface InspectionTaskMutationInput {
  taskId: string
  values: InspectionTaskFormValues
}

export interface AutomationPolicyMutationInput {
  policyId: string
  values: AutomationPolicyFormValues
}

export interface PatchSessionInput {
  sessionId: string
  body: Record<string, unknown>
}

export interface InspectionExecutionInput {
  taskId: string
  idempotencyKey: string
  expectedRevision: number
}
