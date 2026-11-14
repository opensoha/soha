import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { MonitoringQuery, MonitoringView } from './types'
export async function getResourceMonitoring(
  clusterId: string,
  input: MonitoringQuery,
  signal?: AbortSignal,
): Promise<MonitoringView> {
  const params = new URLSearchParams({
    view: input.view,
    rangeMinutes: String(input.rangeMinutes),
    stepSeconds: String(input.stepSeconds),
  })
  if (input.node) params.set('node', input.node)
  if (input.from) params.set('from', input.from)
  if (input.to) params.set('to', input.to)
  const response = await api.get<ApiResponse<MonitoringView>>(
    `/clusters/${encodeURIComponent(clusterId)}/resource-monitoring?${params}`,
    { signal },
  )
  return response.data
}
