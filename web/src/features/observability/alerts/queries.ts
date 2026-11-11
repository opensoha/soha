import { queryOptions } from '@tanstack/react-query'
import { observabilityKeys } from '../keys'
import { observabilityAlertApi } from './api'

export const observabilityAlertQueries = {
  list: () =>
    queryOptions({
      queryKey: observabilityKeys.alerts.list(),
      queryFn: observabilityAlertApi.list,
      refetchInterval: (query) => (query.state.status === 'error' ? false : 15_000),
    }),
  recent: (limit: number, clusterId?: string) =>
    queryOptions({
      queryKey: observabilityKeys.alerts.recent(limit, clusterId),
      queryFn: () => observabilityAlertApi.recent(limit, clusterId),
      refetchInterval: (query) => (query.state.status === 'error' ? false : 15_000),
    }),
  detail: (eventId: string) =>
    queryOptions({
      queryKey: observabilityKeys.alerts.detail(eventId),
      queryFn: () => observabilityAlertApi.detail(eventId),
    }),
  healingRuns: (eventId: string) =>
    queryOptions({
      queryKey: observabilityKeys.alerts.healingRuns(eventId),
      queryFn: () => observabilityAlertApi.healingRuns(eventId),
    }),
  preview: (eventId: string, policyId: string) =>
    queryOptions({
      queryKey: observabilityKeys.alerts.preview(eventId, policyId),
      queryFn: () => observabilityAlertApi.preview({ eventId, policyId }),
    }),
  deliveryLogs: (eventId: string) =>
    queryOptions({
      queryKey: observabilityKeys.alerts.deliveryLogs(eventId),
      queryFn: () => observabilityAlertApi.deliveryLogs(eventId),
    }),
  healingPolicies: () =>
    queryOptions({
      queryKey: observabilityKeys.alerts.healingPolicies(),
      queryFn: observabilityAlertApi.healingPolicies,
    }),
}
