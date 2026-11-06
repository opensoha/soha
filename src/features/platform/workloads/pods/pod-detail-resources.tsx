import { WorkloadRelationships } from '@/features/platform/shared/resource-insights/workload-relationships'
import type { PodDetail, ScopeKey } from '@/types'

export function PodDetailResources({
  pod,
  scope,
  active,
}: {
  pod: PodDetail
  scope: ScopeKey
  active: boolean
}) {
  return (
    <WorkloadRelationships
      kind="Pod"
      name={pod.name}
      scope={{ ...scope, namespace: pod.namespace }}
      active={active}
    />
  )
}
