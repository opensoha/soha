import type { components } from '@opensoha/contracts/gen/ts/sohaapi'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { KubernetesClusterEvent } from '@opensoha/contracts/gen/ts/sohaapi'
import { buildKubernetesResourcePath } from '../shared/resource-ref'
import type {
  AlertSummary,
  OverviewResourceKind,
  OverviewResourceSearchPage,
  WorkloadOverview,
} from './types'

type KubernetesResourceRef = components['schemas']['KubernetesResourceRef']

// ponytail: compatibility shim until the next contracts release exports the search envelope.
interface KubernetesResourceSearchResult {
  items: Array<{ resource: KubernetesResourceRef; status?: string }>
  truncated: boolean
}

export function getOverviewMonitoringSummary() {
  return api.get<ApiResponse<AlertSummary>>('/monitoring/summary')
}

export function getOverviewWorkload(clusterId: string) {
  return api.get<ApiResponse<WorkloadOverview>>(
    buildClusterScopedPath(clusterId, 'workloads/overview', null),
  )
}

export async function getOverviewEvents(clusterId: string, limit = 20) {
  const response = await api.get<ApiResponse<KubernetesClusterEvent[]>>(
    buildClusterScopedPath(clusterId, 'events', null, { limit }),
  )
  return response.data ?? []
}

const resourceKindNames: Record<OverviewResourceKind, string> = {
  configmaps: 'ConfigMap',
  deployments: 'Deployment',
  hpas: 'HorizontalPodAutoscaler',
  namespaces: 'Namespace',
  networkpolicies: 'NetworkPolicy',
  nodes: 'Node',
  pods: 'Pod',
  services: 'Service',
}

export async function searchOverviewResources(
  clusterId: string,
  kind: OverviewResourceKind,
  keyword: string,
): Promise<OverviewResourceSearchPage> {
  const query = keyword.trim()
  if (!query) return { items: [], truncated: false }
  const response = await api.get<ApiResponse<KubernetesResourceSearchResult>>(
    buildClusterScopedPath(clusterId, 'resources/search', null, {
      q: query,
      kinds: resourceKindNames[kind],
      limit: 12,
    }),
  )
  const result = response.data
  const items = (result?.items ?? []).flatMap((item) => {
    const path = buildKubernetesResourcePath(item.resource)
    if (!path) return []
    return [
      {
        key: `${kind}:${item.resource.namespace ?? ''}:${item.resource.name}`,
        kind,
        name: item.resource.name,
        namespace: item.resource.namespace,
        status: item.status,
        path,
      },
    ]
  })
  return { items, truncated: result?.truncated ?? false }
}
