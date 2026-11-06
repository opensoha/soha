import { WorkloadRelationships } from '@/features/platform/shared/resource-insights/workload-relationships'
import {
  filterRelationshipGraph,
  hiddenOwnershipBranches,
  isOwnership,
} from '@/features/platform/shared/resource-insights/relationship-model'
import { useI18n } from '@/i18n'
import type { ScopeKey } from '@/types'
import { Switch } from 'antd'
import { useState } from 'react'
import type { buildDeploymentFacts } from './deployment-detail-model'

export function DeploymentDetailResources({
  scope,
  name,
  model,
  active = true,
}: {
  scope: ScopeKey
  name: string
  model: ReturnType<typeof buildDeploymentFacts>
  onShowPods: () => void
  active?: boolean
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const [history, setHistory] = useState(false)
  const historicalNames = new Set(
    model.sets
      .filter((set) => set.historical)
      .map((set) => `${set.node.resource.namespace}/${set.node.resource.name}`),
  )
  return (
    <WorkloadRelationships
      kind="Deployment"
      name={name}
      scope={scope}
      active={active}
      controls={
        <label>
          <Switch
            size="small"
            aria-label={zh ? '历史 ReplicaSets' : 'Historical ReplicaSets'}
            checked={history}
            onChange={setHistory}
          />{' '}
          {zh ? '历史 ReplicaSets' : 'Historical ReplicaSets'}
        </label>
      }
      filterGraph={(graph) => {
        if (history) return graph
        const hidden = graph.nodes.filter(
          (node) =>
            node.resource.kind === 'ReplicaSet' &&
            historicalNames.has(`${node.resource.namespace}/${node.resource.name}`) &&
            graph.edges.some(
              (edge) =>
                edge.sourceId === graph.rootId &&
                edge.targetId === node.id &&
                isOwnership(edge.relation),
            ),
        )
        return filterRelationshipGraph(
          graph,
          hiddenOwnershipBranches(
            graph,
            hidden.map((node) => node.id),
          ),
        )
      }}
    />
  )
}
