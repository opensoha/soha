import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { CreateMemoryPolicyInput, MemoryPolicy, MemoryRecord } from './types'
export const memoryApi = {
  records: {
    put: (input: { record: MemoryRecord; policyId: string; policyVersion: string }) =>
      api.post<ApiResponse<MemoryRecord>>('/ai/memory', input),
    list: (includeDeleted = false) =>
      api.get<ApiResponse<MemoryRecord[]>>(
        `/ai/memory${includeDeleted ? '?includeDeleted=true' : ''}`,
      ),
    delete: (id: string) => api.delete<void>(`/ai/memory/${encodeURIComponent(id)}`),
  },
  policies: {
    list: () => api.get<ApiResponse<MemoryPolicy[]>>('/ai/memory/policies'),
    create: (input: CreateMemoryPolicyInput) =>
      api.post<ApiResponse<MemoryPolicy>>('/ai/memory/policies', input),
  },
}
