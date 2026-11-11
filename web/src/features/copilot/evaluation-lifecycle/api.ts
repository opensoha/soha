import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  CreateFeedbackInput,
  CreateGatePolicyInput,
  CreateReplayInput,
  EvaluationReplay,
  ExecuteEvaluationInput,
  FeedbackSample,
  GatePolicy,
} from './types'
export const evaluationLifecycleApi = {
  execute: ({ runId, ...input }: ExecuteEvaluationInput) =>
    api.post<ApiResponse<ProductionRun>>(
      `/ai/evaluations/runs/${encodeURIComponent(runId)}/execute`,
      input,
    ),
  replays: {
    list: () => api.get<ApiResponse<EvaluationReplay[]>>('/ai/evaluations/replays'),
    create: (i: CreateReplayInput) =>
      api.post<ApiResponse<EvaluationReplay>>('/ai/evaluations/replays', i),
  },
  policies: {
    list: () => api.get<ApiResponse<GatePolicy[]>>('/ai/evaluations/gate-policies'),
    create: (i: CreateGatePolicyInput) =>
      api.post<ApiResponse<GatePolicy>>('/ai/evaluations/gate-policies', i),
  },
  evaluateGate: (input: { policyId: string; baselineRunId: string; candidateRunId: string }) =>
    api.post<ApiResponse<ProductionRun>>('/ai/evaluations/gates/evaluate', input),
  feedback: {
    list: () => api.get<ApiResponse<FeedbackSample[]>>('/ai/evaluations/feedback'),
    create: (i: CreateFeedbackInput) =>
      api.post<ApiResponse<FeedbackSample>>('/ai/evaluations/feedback', i),
  },
}
type ProductionRun = { id: string; status: string }
