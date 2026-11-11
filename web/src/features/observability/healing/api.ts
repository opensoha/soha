import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  HealingPolicy,
  HealingPolicyPayload,
  HealingRun,
  ReviewHealingRunInput,
  UpdateHealingPolicyInput,
} from './types'

async function unwrapList<T>(request: Promise<ApiResponse<T[]>>): Promise<T[]> {
  const response = await request
  return response.data ?? []
}

export const observabilityHealingApi = {
  listPolicies: () => unwrapList(api.get<ApiResponse<HealingPolicy[]>>('/healing-policies')),
  listRuns: () => unwrapList(api.get<ApiResponse<HealingRun[]>>('/healing-runs')),
  listRecentRuns: (limit: number) =>
    unwrapList(api.get<ApiResponse<HealingRun[]>>(`/healing-runs?limit=${limit}`)),
  createPolicy: (payload: HealingPolicyPayload) => api.post('/healing-policies', payload),
  updatePolicy: ({ id, payload }: UpdateHealingPolicyInput) =>
    api.put(`/healing-policies/${id}`, payload),
  approveRun: ({ id, comment }: ReviewHealingRunInput) =>
    api.post(`/healing-runs/${id}/approve`, { comment }),
  rejectRun: ({ id, comment }: ReviewHealingRunInput) =>
    api.post(`/healing-runs/${id}/reject`, { comment }),
  retryRun: (id: string) => api.post(`/healing-runs/${id}/retry`),
}
