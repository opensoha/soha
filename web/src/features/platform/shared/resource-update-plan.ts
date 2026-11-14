import type { OperationalPlan } from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'
import type { ApiResponse, ScopeKey } from '@/types'

export interface ResourceUpdatePlanVariables {
  scope: ScopeKey
  kind: string
  name: string
  content: string
  requireObservedIdentity?: boolean
}

export async function planResourceUpdate(
  variables: ResourceUpdatePlanVariables,
): Promise<OperationalPlan> {
  const clusterId = variables.scope.clusterId?.trim()
  if (!clusterId) throw new Error('A cluster is required')
  const response = await api.post<ApiResponse<OperationalPlan>>(
    `/clusters/${encodeURIComponent(clusterId)}/resources/update-plan`,
    {
      namespace: variables.scope.namespace ?? '',
      kind: variables.kind,
      name: variables.name,
      content: variables.content,
      ...(variables.requireObservedIdentity ? { requireObservedIdentity: true } : {}),
    },
  )
  return response.data
}
