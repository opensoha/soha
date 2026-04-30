import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'

export interface MonitoringSummary {
  totalCount: number
  firingCount: number
  resolvedCount?: number
  criticalCount: number
  warningCount?: number
  infoCount?: number
  channelCount: number
  lastReceivedAt?: string
}

export interface AlertRecord {
  id: string
  status: string
  severity: string
}

export interface EventRecord {
  id: string
}

export interface NotificationChannelRecord {
  id: string
}

export interface NotificationRouteRecord {
  id: string
}

export interface NotificationSilenceRecord {
  id: string
}

export const monitoringSummaryQueryKey = ['monitoring-summary'] as const
export const alertsQueryKey = ['alerts'] as const
export const eventsQueryKey = ['events'] as const
export const notificationChannelsQueryKey = ['notification-channels'] as const
export const notificationRoutesQueryKey = ['notification-routes'] as const
export const notificationSilencesQueryKey = ['notification-silences'] as const

export function monitoringSummaryQueryOptions() {
  return {
    queryKey: monitoringSummaryQueryKey,
    queryFn: () => api.get<ApiResponse<MonitoringSummary>>('/monitoring/summary'),
  }
}

export function alertsQueryOptions<T extends AlertRecord = AlertRecord>() {
  return {
    queryKey: alertsQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/alerts'),
  }
}

export function eventsQueryOptions<T extends EventRecord = EventRecord>() {
  return {
    queryKey: eventsQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/events'),
  }
}

export function notificationChannelsQueryOptions<T extends NotificationChannelRecord = NotificationChannelRecord>() {
  return {
    queryKey: notificationChannelsQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/notification-channels'),
  }
}

export function notificationRoutesQueryOptions<T extends NotificationRouteRecord = NotificationRouteRecord>() {
  return {
    queryKey: notificationRoutesQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/alert-routes'),
  }
}

export function notificationSilencesQueryOptions<T extends NotificationSilenceRecord = NotificationSilenceRecord>() {
  return {
    queryKey: notificationSilencesQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/alert-silences'),
  }
}
