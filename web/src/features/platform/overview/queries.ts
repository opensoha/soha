import { clusterQueries } from '@/features/platform/clusters/queries'
import { queryOptions } from '@tanstack/react-query'
import {
  getOverviewEvents,
  getOverviewMonitoringSummary,
  getOverviewWorkload,
  searchOverviewResources,
} from './api'
import { platformOverviewKeys } from './keys'
import type { OverviewResourceKind } from './types'

export const platformOverviewQueries = {
  clusters: clusterQueries.list,
  monitoringSummary: (enabled = true) =>
    queryOptions({
      queryKey: platformOverviewKeys.monitoringSummary(),
      queryFn: getOverviewMonitoringSummary,
      enabled,
      refetchInterval: (query) => (query.state.status === 'error' ? false : 15_000),
    }),
  events: (clusterId: string | null | undefined, enabled = true, limit = 20) =>
    queryOptions({
      queryKey: platformOverviewKeys.events(clusterId, limit),
      queryFn: () => getOverviewEvents(clusterId!, limit),
      enabled: enabled && Boolean(clusterId),
    }),
  resourceSearch: (
    clusterId: string | null | undefined,
    kind: OverviewResourceKind,
    keyword: string,
    enabled = true,
  ) =>
    queryOptions({
      queryKey: platformOverviewKeys.resourceSearch(clusterId, kind, keyword),
      queryFn: () => searchOverviewResources(clusterId!, kind, keyword),
      enabled: enabled && Boolean(clusterId) && Boolean(keyword.trim()),
    }),
  workload: (clusterId: string | null | undefined, enabled = true) =>
    queryOptions({
      queryKey: platformOverviewKeys.workload(clusterId),
      queryFn: () => getOverviewWorkload(clusterId!),
      enabled: enabled && Boolean(clusterId),
    }),
}
