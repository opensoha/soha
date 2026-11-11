import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { EvaluationDataset, EvaluationResult, EvaluationRun } from './types'

export const evaluationApi = {
  datasets: {
    list: () => api.get<ApiResponse<EvaluationDataset[]>>('/ai/evaluations/datasets'),
    create: (dataset: EvaluationDataset) =>
      api.post<ApiResponse<EvaluationDataset>>('/ai/evaluations/datasets', dataset),
  },
  runs: {
    list: () => api.get<ApiResponse<EvaluationRun[]>>('/ai/evaluations/runs'),
    create: (run: EvaluationRun) =>
      api.post<ApiResponse<EvaluationRun>>('/ai/evaluations/runs', run),
    get: (runId: string) => api.get<ApiResponse<EvaluationRun>>(`/ai/evaluations/runs/${runId}`),
    results: (runId: string) =>
      api.get<ApiResponse<EvaluationResult[]>>(`/ai/evaluations/runs/${runId}/results`),
  },
}
