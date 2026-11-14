import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { EventStreamEntry } from './types'

export async function listObservabilityEvents(): Promise<EventStreamEntry[]> {
  const response = await api.get<ApiResponse<EventStreamEntry[]>>('/events')
  return response.data ?? []
}
