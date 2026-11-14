import type { StatefulSetDetail } from './types'
import type { StatefulSetConfiguration } from './statefulset-configuration'
export type { StatefulSetConfiguration } from './statefulset-configuration'
export function statefulSetState(
  detail: StatefulSetDetail,
  configuration?: StatefulSetConfiguration,
) {
  const desired = detail.desiredReplicas,
    ready = detail.readyReplicas
  if (desired === undefined || ready === undefined) return 'unknown'
  if (desired === 0) return 'scaled-to-zero'
  if (ready < desired) return 'degraded'
  if (!configuration) return 'unknown'
  if (
    configuration?.generation !== undefined &&
    configuration.observedGeneration !== undefined &&
    configuration.observedGeneration < configuration.generation
  )
    return 'progressing'
  // OnDelete and partitioned rollouts intentionally retain older revisions.
  if (
    configuration?.strategy === 'RollingUpdate' &&
    configuration.updatedReplicas !== undefined &&
    configuration.updatedReplicas < Math.max(0, desired - (configuration.partition ?? 0))
  )
    return 'progressing'
  return 'healthy'
}
