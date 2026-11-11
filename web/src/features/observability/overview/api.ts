import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { MonitoringSummary } from './types'

export async function getMonitoringSummary(): Promise<MonitoringSummary> {
  const response = await api.get<ApiResponse<MonitoringSummary>>('/monitoring/summary')
  return response.data
}
