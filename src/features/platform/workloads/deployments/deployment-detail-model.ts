import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import type { DeploymentDetail, RolloutHistory, ScopeKey } from '@/types'
import { isOwnership } from '@/features/platform/shared/resource-insights/relationship-model'

export type DeploymentRelationNode = KubernetesResourceGraph['nodes'][number] & {
  relation?: string
  evidence?: string
}

// Rollout/Pod table facts only. Relationship browsing consumes the server graph unchanged.
export function buildDeploymentFacts(
  detail: DeploymentDetail,
  scope: ScopeKey,
  history: RolloutHistory[],
  revision?: string,
  graph?: KubernetesResourceGraph,
) {
  const make = (
    kind: string,
    name: string,
    namespace = detail.namespace,
  ): DeploymentRelationNode => ({
    id: `${kind}:${namespace}:${name}`,
    resource: {
      kind,
      name,
      namespace,
      clusterId: scope.clusterId || '',
      apiVersion: 'unknown',
      scopeMode: 'namespace',
    },
  })
  const nodes = graph?.nodes ?? []
  const graphRoot = nodes.find(
    (node) =>
      node.id === graph?.rootId &&
      node.resource.kind === 'Deployment' &&
      node.resource.name === detail.name &&
      node.resource.namespace === detail.namespace &&
      node.resource.clusterId === scope.clusterId &&
      (!detail.uid || !node.resource.uid || node.resource.uid === detail.uid),
  )
  const root: DeploymentRelationNode = graphRoot
    ? { ...graphRoot, evidence: 'graph' }
    : { ...make('Deployment', detail.name), evidence: 'detail' }
  const owned = nodes.filter(
    (node) =>
      graphRoot &&
      node.resource.kind === 'ReplicaSet' &&
      graph?.edges.some(
        (edge) =>
          edge.sourceId === graphRoot.id && edge.targetId === node.id && isOwnership(edge.relation),
      ),
  )
  const records = new Map(history.map((record) => [`${record.namespace}/${record.name}`, record]))
  const sets = [
    ...owned,
    ...history
      .filter(
        (record) =>
          !owned.some(
            (node) =>
              node.resource.name === record.name && node.resource.namespace === record.namespace,
          ),
      )
      .map((record) => make('ReplicaSet', record.name, record.namespace)),
  ].map((node) => {
    const record = records.get(`${node.resource.namespace}/${node.resource.name}`)
    const pods = nodes.filter(
      (pod) =>
        pod.resource.kind === 'Pod' &&
        graphRoot &&
        graph?.edges.some(
          (edge) =>
            edge.sourceId === node.id && edge.targetId === pod.id && isOwnership(edge.relation),
        ),
    )
    return {
      node: { ...node, relation: 'owns', evidence: owned.includes(node) ? 'graph' : 'history' },
      record,
      pods,
      historical: Boolean(record && record.replicas === 0 && record.revision !== revision),
    }
  })
  const podOwners = new Map(
    sets.flatMap((set) =>
      set.pods.map((pod) => [`${pod.resource.namespace}/${pod.resource.name}`, set] as const),
    ),
  )
  return { root, sets, podOwners }
}
