import type {
  WorkbenchModelPreferences,
  WorkbenchModelOption,
  WorkbenchAgentRun as ContractWorkbenchAgentRun,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type { WorkbenchRuntimeAssembly } from './runtime-profile'

export type WorkbenchMode = NonNullable<NonNullable<WorkbenchSession['metadata']>['mode']>

// API adapter stays compatible with the currently published contracts package.
export interface PersonalAssistant {
  id: string
  name: string
  avatarUrl?: string
  avatarPosterUrl?: string
  avatarMotion?: 'auto' | 'off'
  mainSessionId: string
  createdAt: string
  updatedAt: string
}

export interface PersonalAssistantAvatarInput {
  avatarUrl: string
  avatarPosterUrl?: string
  avatarMotion?: 'auto' | 'off'
}
// Optional wire extension; compatible with the currently published SDK.
export interface AssistantChatTask {
  kind: 'avatar' | 'undo'
  image?: PersonalAssistantAvatarInput
  useCurrentAvatar?: boolean
  publicModel?: string
  messageId?: string
}
export interface PersonalArtifact {
  id: string
  sessionId?: string
  messageId?: string
  taskId?: string
  artifact: WorkbenchArtifact
  createdAt: string
}

export interface WorkbenchSessionScope {
  clusterId?: string
  namespace?: string
  workload?: string
  service?: string
  pod?: string
  node?: string
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
  activity?: 'queued' | 'running' | 'waiting_approval'
  id: string
  title: string
  createdBy?: string
  updatedAt: string
  createdAt?: string
  metadata?: {
    conversationKind?: 'main' | 'side' | 'personal_main' | 'personal_side'
    modelPreferences?: WorkbenchModelPreferences
    runtimeProfileId?: string
    mode?: 'general' | 'root_cause' | 'performance' | 'trace' | 'inspection_review'
    status?: string
    agentProviderId?: string
    summary?: string
    source?: string
    pinnedContext?: Record<string, unknown>
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

export interface WorkbenchGraphNode {
  id: string
  kind: string
  title: string
  subtitle?: string
  severity?: string
  evidenceIds?: string[]
  sourceRefs?: string[]
  attributes?: Record<string, unknown>
}

export interface WorkbenchGraphEdge {
  id: string
  source: string
  target: string
  relation: string
  severity?: string
  evidenceIds?: string[]
  attributes?: Record<string, unknown>
}

export interface WorkbenchGraph {
  layout?: string
  focusNodeId?: string
  nodes?: WorkbenchGraphNode[]
  edges?: WorkbenchGraphEdge[]
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
  graph?: WorkbenchGraph
  dataSourceSnapshot?: Record<string, unknown>
}

export interface WorkbenchAgentToolBinding {
  id: string
  capabilityId: string
  providerId?: string
  providerKind?: string
  toolKind: string
  adapterId?: string
  toolName?: string
  permissionKey?: string
  config?: Record<string, unknown>
}

export interface WorkbenchAgentSkillBinding {
  id: string
  skillId: string
  providerId?: string
  providerKind?: string
  providerSkillRef?: string
  capabilityRefs?: string[]
  promptTemplateId?: string
  config?: Record<string, unknown>
}

export interface WorkbenchAgentRun extends Pick<
  ContractWorkbenchAgentRun,
  'executionOwner' | 'phase' | 'stopReason' | 'eventSequence' | 'usageKnown'
> {
  runtimeAssembly?: WorkbenchRuntimeAssembly
  parentRunId?: string
  id: string
  providerId: string
  providerKind: string
  capabilityId: string
  skillIds?: string[]
  sessionId?: string
  rootCauseRunId?: string
  createdBy?: string
  status: string
  scope?: WorkbenchSessionScope
  toolset?: WorkbenchSessionToolset
  toolBindings?: WorkbenchAgentToolBinding[]
  skillBindings?: WorkbenchAgentSkillBinding[]
  input?: Record<string, unknown>
  output?: Record<string, unknown>
  toolExecutions?: WorkbenchToolCall[]
  analysisArtifacts?: WorkbenchArtifact[]
  claimedByAgentId?: string
  externalRunId?: string
  errorMessage?: string
  timeoutSeconds?: number
  queuedAt?: string
  startedAt?: string
  lastHeartbeatAt?: string
  completedAt?: string
  createdAt?: string
  updatedAt?: string
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

export interface WorkbenchSkill {
  id: string
  name: string
  description?: string
  enabled: boolean
  scopes?: string[]
  capabilityRefs?: string[]
  scopeRules?: string[]
  category?: string
}

export interface WorkbenchAnalysisProfile {
  id: string
  name: string
  mode: string
  enabled: boolean
}

export interface WorkbenchAgentProvider {
  id: string
  kind: string
  name: string
  description?: string
  enabled: boolean
  default?: boolean
  capabilities?: string[]
  supportedModes?: string[]
  supportsAsync?: boolean
  supportsSkills?: boolean
  supportsToolsets?: boolean
  config?: Record<string, unknown>
  runtimeStatus?: {
    state: string
    reason?: string
    queuedRuns: number
    runningRuns: number
    recentFailures: number
    lastRunId?: string
    lastRunStatus?: string
    lastAgentId?: string
    lastHeartbeatAt?: string
    lastCompletedAt?: string
    observedAt?: string
  }
}

export interface WorkbenchAgentCapability {
  id: string
  name: string
  category?: string
  description?: string
  analysisKinds?: string[]
  requiredScopes?: string[]
  toolRefs?: string[]
  toolBindings?: WorkbenchAgentToolBinding[]
  skillBindings?: WorkbenchAgentSkillBinding[]
}

export interface WorkbenchCatalog {
  modelOptions?: WorkbenchModelOption[]
  defaultPublicModel?: string
  modelOptionsError?: string
  adapters: WorkbenchAdapter[]
  dataSources: WorkbenchDataSource[]
  analysisProfiles: WorkbenchAnalysisProfile[]
  skillsRegistry?: WorkbenchSkill[]
  agentProviders?: WorkbenchAgentProvider[]
  capabilities?: WorkbenchAgentCapability[]
  toolBindings?: WorkbenchAgentToolBinding[]
  skillBindings?: WorkbenchAgentSkillBinding[]
}
