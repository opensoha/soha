import type { DaemonSetDetail } from './types'
import type { DaemonSetConfiguration } from './daemonset-configuration'
import type { RolloutHistory } from '@/types'
export function daemonSetState(detail: DaemonSetDetail, configuration?: DaemonSetConfiguration) {
  const desired = detail.desiredNumber,
    ready = detail.readyNumber,
    available = detail.availableNumber
  if (desired === undefined || ready === undefined || available === undefined || !configuration)
    return 'unknown'
  if ((configuration.misscheduled ?? 0) > 0 || ready < desired || available < desired)
    return 'degraded'
  if (
    configuration.generation !== undefined &&
    configuration.observedGeneration !== undefined &&
    configuration.observedGeneration < configuration.generation
  )
    return 'progressing'
  if (desired === 0) return 'no-targets'
  if (
    configuration.strategy === 'RollingUpdate' &&
    detail.updatedNumber !== undefined &&
    detail.updatedNumber < desired
  )
    return 'progressing'
  return 'healthy'
}
export function daemonSetPodModel(detail: DaemonSetDetail, history: RolloutHistory[]) {
  const byHash = new Map(
    history.filter((v) => v.podRevisionHash).map((v) => [v.podRevisionHash!, v]),
  )
  return {
    podOwners: new Map(
      (detail.pods || []).map((p) => {
        const hash = p.labels?.['controller-revision-hash'],
          record = hash ? byHash.get(hash) : undefined
        return [
          `${p.namespace}/${p.name}`,
          {
            record: record
              ? { revision: record.revision }
              : hash
                ? { revision: `hash:${hash}` }
                : undefined,
          },
        ] as const
      }),
    ),
  }
}
export function daemonSetCurrentTemplate(history: RolloutHistory[]) {
  return [...history]
    .filter((v) => v.matchesCurrentTemplate === true)
    .sort((a, b) => b.revision.localeCompare(a.revision, undefined, { numeric: true }))[0]
}
