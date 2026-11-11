import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { observabilityKeys, observabilityMutationKeys } from '../keys'
import { createAlertIntegration, testAlertIntegration, updateAlertIntegration } from './api'

export function invalidateAlertIntegrations(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: observabilityKeys.integrations.all }),
    queryClient.invalidateQueries({
      queryKey: observabilityKeys.legacy.monitoringOverviewIntegrations,
    }),
  ])
}

export const observabilityIntegrationMutations = {
  create: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.integrations.create,
      mutationFn: createAlertIntegration,
      onSuccess: () => invalidateAlertIntegrations(queryClient),
    }),
  update: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.integrations.update,
      mutationFn: updateAlertIntegration,
      onSuccess: () => invalidateAlertIntegrations(queryClient),
    }),
  test: () =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.integrations.test,
      mutationFn: testAlertIntegration,
    }),
}
