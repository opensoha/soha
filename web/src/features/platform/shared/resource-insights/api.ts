import type {
  KubernetesResourceGraph,
  KubernetesSecurityPosture,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'
import type { ApiResponse, ScopeKey } from '@/types'

export type ResourceGraphRootAPI = { apiVersion: string; resource: string }

function clusterPath(scope: ScopeKey, suffix: string) {
  const clusterId = scope.clusterId?.trim()
  if (!clusterId) throw new Error('A cluster is required')
  const query = new URLSearchParams()
  if (scope.namespace?.trim()) query.set('namespace', scope.namespace.trim())
  const encoded = query.toString()
  return `/clusters/${encodeURIComponent(clusterId)}${suffix}${encoded ? `?${encoded}` : ''}`
}

function appendQuery(path: string, values: Record<string, string>) {
  const [pathname, rawQuery = ''] = path.split('?', 2)
  const query = new URLSearchParams(rawQuery)
  Object.entries(values).forEach(([key, value]) => query.set(key, value))
  return `${pathname}?${query.toString()}`
}

export async function getKubernetesResourceGraph(
  scope: ScopeKey,
  kind: string,
  name: string,
  rootAPI?: ResourceGraphRootAPI,
): Promise<KubernetesResourceGraph> {
  const path = clusterPath(scope, '/resources/graph')
  const response = await api.get<ApiResponse<KubernetesResourceGraph>>(
    appendQuery(path, { kind: kind.trim(), name: name.trim(), ...rootAPI }),
  )
  return response.data
}

export async function getKubernetesSecurityPosture(
  scope: ScopeKey,
  limit = 100,
): Promise<KubernetesSecurityPosture> {
  const path = clusterPath(scope, '/security/posture')
  const response = await api.get<ApiResponse<KubernetesSecurityPosture>>(
    appendQuery(path, { limit: String(limit) }),
  )
  return response.data
}
