import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { observabilityKeys, observabilityMutationKeys } from '../keys'
import { observabilityAlertApi } from './api'

export function invalidateAlerts(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: observabilityKeys.alerts.all })
}

export function invalidateAlertHealingRuns(queryClient: QueryClient, eventId: string) {
  return Promise.all([
    queryClient.invalidateQueries({
      queryKey: observabilityKeys.alerts.healingRuns(eventId),
    }),
    queryClient.invalidateQueries({ queryKey: observabilityKeys.healing.runs() }),
  ])
}

export const observabilityAlertMutations = {
  acknowledge: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.alerts('acknowledge'),
      mutationFn: observabilityAlertApi.acknowledge,
      onSuccess: () => invalidateAlerts(queryClient),
    }),
  resolve: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.alerts('resolve'),
      mutationFn: observabilityAlertApi.resolve,
      onSuccess: () => invalidateAlerts(queryClient),
    }),
  heal: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.alerts('heal'),
      mutationFn: observabilityAlertApi.heal,
      onSuccess: (_data, input) => invalidateAlertHealingRuns(queryClient, input.eventId),
    }),
}
