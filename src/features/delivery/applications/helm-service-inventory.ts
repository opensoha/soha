import type { ApplicationRuntimeWorkload, ReleaseTarget } from '../types'
import type { ServiceCard } from './service-cards'

function observedIdentity(workload: ApplicationRuntimeWorkload) {
  const resource = workload.source?.resource
  if (
    !resource?.uid ||
    (workload.sourceStatus && !['linked', 'changed'].includes(workload.sourceStatus))
  )
    return undefined
  return JSON.stringify([
    workload.applicationEnvironmentId,
    resource.clusterId,
    resource.namespace,
    resource.apiVersion,
    resource.kind,
    resource.name,
    resource.uid,
  ])
}

export function visibleServiceInventory(cards: ServiceCard[], targets: ReleaseTarget[]) {
  const helmTargets = new Set(
    targets
      .filter((target) => target.helm || target.metadata?.importSource === 'helm')
      .map((target) => target.id),
  )
  const owners = new Map<string, Set<string>>()
  for (const card of cards) {
    if (card.kind !== 'Helm Release') continue
    for (const workload of card.workloads) {
      if (!workload.targetId || !helmTargets.has(workload.targetId)) continue
      const identity = observedIdentity(workload)
      if (!identity) continue
      const services = owners.get(identity) ?? new Set<string>()
      services.add(card.id)
      owners.set(identity, services)
    }
  }

  const nestedServices = new Map<string, ServiceCard[]>()
  const visibleCards = cards.filter((card) => {
    if (!card.service || card.kind !== 'Kubernetes 工作负载' || !card.workloads.length) return true

    // Keep independent delivery targets visible, even when their live workloads are absent.
    const serviceTargets = targets.filter((target) => target.metadata?.serviceId === card.id)
    if (
      serviceTargets.some(
        (target) =>
          target.metadata?.importSource !== 'kubernetes' ||
          target.metadata?.ownershipMode !== 'observe_only' ||
          !card.workloads.some((workload) => workload.targetId === target.id),
      )
    )
      return true

    const releaseIds = new Set<string>()
    for (const workload of card.workloads) {
      const identity = observedIdentity(workload)
      const releases = identity && owners.get(identity)
      if (!releases || releases.size !== 1) return true
      for (const release of releases) releaseIds.add(release)
    }
    if (releaseIds.size !== 1) return true
    const [releaseId] = releaseIds
    const children = nestedServices.get(releaseId) ?? []
    children.push(card)
    nestedServices.set(releaseId, children)
    return false
  })
  return { visibleCards, nestedServices }
}
