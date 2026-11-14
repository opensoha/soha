import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { CreateEnvironmentTemplateInput, EnvironmentLease, EnvironmentTemplate } from './types'
export const environmentsApi = {
  templates: {
    list: () => api.get<ApiResponse<EnvironmentTemplate[]>>('/ai/environments/templates'),
    create: (input: CreateEnvironmentTemplateInput) =>
      api.post<ApiResponse<EnvironmentTemplate>>('/ai/environments/templates', input),
  },
  leases: {
    list: () => api.get<ApiResponse<EnvironmentLease[]>>('/ai/environments/leases'),
    release: (id: string) =>
      api.post<ApiResponse<EnvironmentLease>>(
        `/ai/environments/leases/${encodeURIComponent(id)}/release`,
      ),
  },
  gc: () => api.post<ApiResponse<ProductionRecord>>('/ai/environments/gc'),
}
type ProductionRecord = { id: string; status?: string }
