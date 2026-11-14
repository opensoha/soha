import { queryOptions } from '@tanstack/react-query'
import { getResourceMonitoring } from './api'
import { monitoringKeys } from './keys'
import type { MonitoringQuery } from './types'
export const monitoringQueries = {
  view: (
    principal: string,
    permissionSignature: string,
    clusterId: string,
    input: MonitoringQuery,
  ) =>
    queryOptions({
      queryKey: monitoringKeys.view(principal, permissionSignature, clusterId, input),
      queryFn: ({ signal }) => getResourceMonitoring(clusterId, input, signal),
      enabled: Boolean(clusterId),
      staleTime: 0,
      gcTime: 0,
    }),
}
