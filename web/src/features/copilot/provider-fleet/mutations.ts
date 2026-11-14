import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { providerFleetApi } from './api'
import { providerFleetKeys, providerFleetMutationKeys } from './keys'
export const providerFleetMutations = {
  rollout: (c: QueryClient) =>
    mutationOptions({
      mutationKey: providerFleetMutationKeys.rollout,
      mutationFn: providerFleetApi.rollouts.create,
      onSuccess: () => c.invalidateQueries({ queryKey: providerFleetKeys.rollouts() }),
    }),
  action: (c: QueryClient) =>
    mutationOptions({
      mutationKey: providerFleetMutationKeys.action,
      mutationFn: providerFleetApi.rollouts.action,
      onSuccess: () => c.invalidateQueries({ queryKey: providerFleetKeys.rollouts() }),
    }),
  conformance: (c: QueryClient) =>
    mutationOptions({
      mutationKey: providerFleetMutationKeys.conformance,
      mutationFn: providerFleetApi.conformance.create,
      onSuccess: () => c.invalidateQueries({ queryKey: providerFleetKeys.conformance() }),
    }),
}
