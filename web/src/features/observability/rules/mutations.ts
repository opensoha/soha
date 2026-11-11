import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { observabilityKeys, observabilityMutationKeys } from '../keys'
import { observabilityRuleApi } from './api'

export function invalidateAlertRules(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: observabilityKeys.rules.all })
}

export const observabilityRuleMutations = {
  create: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.rules('create'),
      mutationFn: observabilityRuleApi.create,
      onSuccess: () => invalidateAlertRules(queryClient),
    }),
  update: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.rules('update'),
      mutationFn: observabilityRuleApi.update,
      onSuccess: () => invalidateAlertRules(queryClient),
    }),
  test: () =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.rules('test'),
      mutationFn: observabilityRuleApi.test,
    }),
}
