import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  AlertDeliveryLog,
  AlertEvent,
  AlertNotificationPreviewItem,
  AlertPreviewInput,
  HealAlertInput,
  HealingPolicyOption,
  HealingRun,
} from './types'

async function unwrapList<T>(request: Promise<ApiResponse<T[]>>): Promise<T[]> {
  const response = await request
  return response.data ?? []
}

async function unwrapItem<T>(request: Promise<ApiResponse<T>>): Promise<T> {
  const response = await request
  return response.data
}

export const observabilityAlertApi = {
  list: () => unwrapList(api.get<ApiResponse<AlertEvent[]>>('/alert-events')),
  recent: (limit: number, clusterId?: string) => {
    const query = new URLSearchParams({ limit: String(Math.max(1, Math.trunc(limit))) })
    if (clusterId?.trim()) query.set('clusterId', clusterId.trim())
    return unwrapList(api.get<ApiResponse<AlertEvent[]>>(`/alert-events?${query.toString()}`))
  },
  detail: (eventId: string) =>
    unwrapItem(api.get<ApiResponse<AlertEvent>>(`/alert-events/${encodeURIComponent(eventId)}`)),
  healingRuns: (eventId: string) =>
    unwrapList(
      api.get<ApiResponse<HealingRun[]>>(`/healing-runs?eventId=${encodeURIComponent(eventId)}`),
    ),
  preview: ({ eventId, policyId }: AlertPreviewInput) =>
    unwrapList(
      api.get<ApiResponse<AlertNotificationPreviewItem[]>>(
        `/notification-policies/${encodeURIComponent(policyId)}/preview?eventId=${encodeURIComponent(eventId)}`,
      ),
    ),
  deliveryLogs: (eventId: string) =>
    unwrapList(
      api.get<ApiResponse<AlertDeliveryLog[]>>(
        `/alert-delivery-logs?alertId=${encodeURIComponent(eventId)}`,
      ),
    ),
  healingPolicies: () =>
    unwrapList(api.get<ApiResponse<HealingPolicyOption[]>>('/healing-policies')),
  acknowledge: (eventId: string) =>
    unwrapItem(
      api.post<ApiResponse<AlertEvent>>(`/alert-events/${encodeURIComponent(eventId)}/acknowledge`),
    ),
  resolve: (eventId: string) =>
    unwrapItem(
      api.post<ApiResponse<AlertEvent>>(`/alert-events/${encodeURIComponent(eventId)}/resolve`),
    ),
  heal: ({ eventId, policyId }: HealAlertInput) =>
    unwrapItem(
      api.post<ApiResponse<HealingRun>>(
        `/alert-events/${encodeURIComponent(eventId)}/heal?policyId=${encodeURIComponent(policyId)}`,
      ),
    ),
}
