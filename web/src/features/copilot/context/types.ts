export interface ContextInspectInput {
  requestId?: string
  sessionId?: string
  agentRunId?: string
  task: {
    mode?: string
    goal: string
  }
  prompt?: {
    id?: string
    version?: string
  }
  skills?: Array<{ id: string; version?: string }>
  knowledge: {
    enabled: boolean
    knowledgeBaseIds: string[]
    query?: string
    topK?: number
  }
  tools?: Array<{ name: string; schemaVersion?: string }>
  environment?: {
    mode?: string
    observationRefs?: string[]
  }
  budgets?: {
    maxInputTokens?: number
    maxEvidenceTokens?: number
    maxSteps?: number
  }
}

export interface ContextEnvelope {
  version?: string
  requestId?: string
  sessionId?: string
  agentRunId?: string
  task?: Record<string, unknown>
  prompt?: Record<string, unknown>
  skills?: Array<Record<string, unknown>>
  session?: Record<string, unknown>
  memoryRefs?: unknown[]
  evidence?: Array<Record<string, unknown>>
  tools?: Array<Record<string, unknown>>
  environment?: Record<string, unknown>
  budgets?: Record<string, unknown>
  policySnapshot?: Record<string, unknown>
  [key: string]: unknown
}

export interface ContextInspection {
  envelope: ContextEnvelope
  sections: string[]
  truncations?: string[]
  retrievalTimeMs?: number
}

export interface ContextInspectorFormValues {
  goal: string
  mode?: string
  knowledgeBaseIds?: string[]
  query?: string
  topK?: number
  maxInputTokens?: number
  maxEvidenceTokens?: number
  maxSteps?: number
}
