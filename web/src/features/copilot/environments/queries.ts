import { queryOptions } from '@tanstack/react-query'
import { environmentsApi } from './api'
import { environmentKeys } from './keys'
export const environmentQueries = {
  templates: () =>
    queryOptions({
      queryKey: environmentKeys.templates(),
      queryFn: environmentsApi.templates.list,
    }),
  leases: () =>
    queryOptions({
      queryKey: environmentKeys.leases(),
      queryFn: environmentsApi.leases.list,
      refetchInterval: 10_000,
    }),
}
