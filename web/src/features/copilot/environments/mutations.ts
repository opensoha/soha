import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { environmentsApi } from './api'
import { environmentKeys, environmentMutationKeys } from './keys'
export const environmentMutations = {
  create: (c: QueryClient) =>
    mutationOptions({
      mutationKey: environmentMutationKeys.create,
      mutationFn: environmentsApi.templates.create,
      onSuccess: () => c.invalidateQueries({ queryKey: environmentKeys.templates() }),
    }),
  release: (c: QueryClient) =>
    mutationOptions({
      mutationKey: environmentMutationKeys.release,
      mutationFn: environmentsApi.leases.release,
      onSuccess: () => c.invalidateQueries({ queryKey: environmentKeys.leases() }),
    }),
  gc: (c: QueryClient) =>
    mutationOptions({
      mutationKey: environmentMutationKeys.gc,
      mutationFn: environmentsApi.gc,
      onSuccess: () => c.invalidateQueries({ queryKey: environmentKeys.leases() }),
    }),
}
