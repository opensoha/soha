import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  ConformanceRun,
  CreateConformanceInput,
  CreateRolloutInput,
  ProviderRollout,
} from './types'
export const providerFleetApi = {
  rollouts: {
    list: () => api.get<ApiResponse<ProviderRollout[]>>('/ai/agent-providers/rollouts'),
    create: (i: CreateRolloutInput) =>
      api.post<ApiResponse<ProviderRollout>>('/ai/agent-providers/rollouts', i),
    action: ({ id, action }: { id: string; action: 'pause' | 'resume' | 'rollback' }) =>
      api.post<ApiResponse<ProviderRollout>>(
        `/ai/agent-providers/rollouts/${encodeURIComponent(id)}/${action}`,
      ),
  },
  conformance: {
    list: () => api.get<ApiResponse<ConformanceRun[]>>('/ai/agent-providers/conformance-runs'),
    create: (i: CreateConformanceInput) =>
      api.post<ApiResponse<ConformanceRun>>('/ai/agent-providers/conformance-runs', i),
  },
}
