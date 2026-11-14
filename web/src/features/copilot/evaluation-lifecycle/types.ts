import type { ProductionRecord } from '../production/operations-page'
export interface EvaluationReplay extends ProductionRecord {
  baselineRunId: string
  candidateRunId: string
  status: string
}
export interface GatePolicy extends ProductionRecord {
  version: string
  status: string
}
export interface FeedbackSample extends ProductionRecord {
  traceRef: string
  disposition: string
  status: string
}
export interface ExecuteEvaluationInput {
  runId: string
  executorProfileId: string
}
export interface CreateReplayInput {
  id: string
  baselineRunId: string
  candidateRunId: string
  executorProfileId: string
}
export interface CreateGatePolicyInput {
  id: string
  name: string
  version: string
  metric: string
  threshold: number
}
export interface CreateFeedbackInput {
  sessionId?: string
  messageId?: string
  id: string
  traceRef: string
  disposition: string
}
