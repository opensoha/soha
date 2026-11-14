import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { AIOperationSnapshot, RunbookEvidence, StartAIOperationInput } from './types'
export const aiProductionOperationsApi = {
  snapshots: () => api.get<ApiResponse<AIOperationSnapshot[]>>('/ai/operations'),
  evidence: () => api.get<ApiResponse<RunbookEvidence[]>>('/ai/operations/runbook-evidence'),
  start: (input: StartAIOperationInput) =>
    api.post<ApiResponse<AIOperationSnapshot>>('/ai/operations', input),
}
