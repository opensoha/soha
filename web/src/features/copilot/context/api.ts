import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { ContextInspectInput, ContextInspection } from './types'

export const contextApi = {
  inspect: (input: ContextInspectInput) =>
    api.post<ApiResponse<ContextInspection>>('/ai/context/inspect', input),
}
