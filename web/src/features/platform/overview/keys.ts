import { clusterKeys } from '../clusters/keys'

export const platformOverviewKeys = {
  clusters: clusterKeys.list,
  monitoringSummary: () => ['monitoring-summary'] as const,
  events: (clusterId: string | null | undefined, limit: number) =>
    ['overview-events', clusterId, '__all__', { limit }] as const,
  resourceSearch: (clusterId: string | null | undefined, kind: string, keyword: string) =>
    ['overview-resource-search', clusterId, kind, keyword.trim().toLowerCase()] as const,
  workload: (clusterId: string | null | undefined) =>
    ['overview-workload', clusterId, '__all__'] as const,
}
