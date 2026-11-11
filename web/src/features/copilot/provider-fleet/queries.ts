import { queryOptions } from '@tanstack/react-query'
import { providerFleetApi } from './api'
import { providerFleetKeys } from './keys'
export const providerFleetQueries = {
  rollouts: () =>
    queryOptions({
      queryKey: providerFleetKeys.rollouts(),
      queryFn: providerFleetApi.rollouts.list,
      refetchInterval: 10_000,
    }),
  conformance: () =>
    queryOptions({
      queryKey: providerFleetKeys.conformance(),
      queryFn: providerFleetApi.conformance.list,
      refetchInterval: 10_000,
    }),
}
