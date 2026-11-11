import type { DeploymentRolloutStatus, RolloutHistory } from '@/types'
import type { ReplicaSetDetail } from './types'
import {
  readReplicaControllerConfiguration,
  replicaControllerHealth,
} from '../shared/replica-controller-model'
export type { ReplicaControllerConfiguration as ReplicaSetConfiguration } from '../shared/replica-controller-model'
export const readReplicaSetConfiguration = (content: string, name: string, namespace: string) =>
  readReplicaControllerConfiguration(content, name, namespace, 'ReplicaSet')
export const replicaSetHealth = replicaControllerHealth
export function replicaSetOwner(detail: ReplicaSetDetail) {
  const owner = detail.relatedResources?.find(
    (r) =>
      r.kind === 'Deployment' &&
      r.relation === 'owner' &&
      r.namespace === detail.namespace &&
      Boolean(r.name.trim()),
  )
  return owner ? { ...owner, namespace: detail.namespace } : undefined
}
export function replicaSetRevision(detail: ReplicaSetDetail) {
  return detail.annotations?.['deployment.kubernetes.io/revision']?.trim() || undefined
}
export function replicaSetTemplateState(
  detail: ReplicaSetDetail,
  history?: RolloutHistory[],
  parentStatus?: DeploymentRolloutStatus,
) {
  const owner = replicaSetOwner(detail)
  if (!owner) return 'standalone'
  const revision = replicaSetRevision(detail)
  const record = history?.find(
    (r) => r.namespace === detail.namespace && r.name === detail.name && r.revision === revision,
  )
  if (
    !record ||
    !revision ||
    parentStatus?.name !== owner.name ||
    parentStatus.namespace !== detail.namespace ||
    !parentStatus.revision?.trim()
  )
    return 'unknown'
  return revision === parentStatus.revision ? 'current' : 'retained'
}
