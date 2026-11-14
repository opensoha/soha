import { WorkloadRelationships } from '@/features/platform/shared/resource-insights/workload-relationships'
import {
  cronJobRelationshipGraph,
  type WorkloadKind,
} from '@/features/platform/shared/resource-insights/relationship-model'
import type { ScopeKey, Pod, WorkloadRelation } from '@/types'
import type { ReactNode } from 'react'

export function ControllerRelatedResources({
  detail,
  kind,
  scope,
  childControls,
  selectedChildName,
  active = true,
}: {
  detail: {
    name: string
    namespace: string
    pods?: Pod[]
    jobs?: { name: string; namespace: string }[]
    relatedResources?: WorkloadRelation[]
  }
  kind: Exclude<WorkloadKind, 'Pod' | 'Deployment'>
  scope: ScopeKey
  onShowPods: () => void
  childControls?: ReactNode
  selectedChildName?: string
  active?: boolean
}) {
  return (
    <WorkloadRelationships
      kind={kind}
      name={detail.name}
      scope={{ ...scope, namespace: detail.namespace }}
      active={active}
      controls={childControls}
      filterGraph={
        kind === 'CronJob'
          ? (graph) => cronJobRelationshipGraph(graph, selectedChildName)
          : undefined
      }
    />
  )
}
