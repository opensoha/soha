import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'

export interface RootCauseRunSummary {
  id: string
  status: string
  severity: string
}

export interface InspectionTaskSummary {
  id: string
  enabled: boolean
}

export interface CopilotSessionSummary {
  id: string
}

export const copilotRootCauseRunsQueryKey = ['copilot-root-cause-runs'] as const
export const copilotInspectionTasksQueryKey = ['copilot-inspection-tasks'] as const
export const copilotSessionsQueryKey = ['copilot-sessions'] as const

export function copilotRootCauseRunsQueryOptions<T extends RootCauseRunSummary = RootCauseRunSummary>() {
  return {
    queryKey: copilotRootCauseRunsQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/copilot/root-cause/runs'),
  }
}

export function copilotInspectionTasksQueryOptions<T extends InspectionTaskSummary = InspectionTaskSummary>() {
  return {
    queryKey: copilotInspectionTasksQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/copilot/inspection-tasks'),
  }
}

export function copilotSessionsQueryOptions<T extends CopilotSessionSummary = CopilotSessionSummary>() {
  return {
    queryKey: copilotSessionsQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/copilot/sessions'),
  }
}
