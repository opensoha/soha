import type { EnvironmentServiceReferenceSummary } from '@opensoha/contracts/gen/ts/sohaapi'
import type { ReleaseTarget } from '../domain-types'

export type ServiceSlotReference = Pick<
  EnvironmentServiceReferenceSummary,
  'id' | 'serviceId' | 'revision'
>

export type ServiceSlot =
  | { kind: 'local'; targets: ReleaseTarget[] }
  | { kind: 'reference'; reference: ServiceSlotReference }
  | { kind: 'unconfigured' }
  | { kind: 'unknown' }

export function computeServiceSlots(
  serviceIds: readonly string[],
  localTargets: readonly ReleaseTarget[] | undefined,
  activeReferences: readonly ServiceSlotReference[] | undefined,
): {
  slots: Map<string, ServiceSlot>
  counts: { local: number; reference: number; unconfigured: number; unknown: number }
} {
  const visibleServiceIds = new Set<string>()
  serviceIds.forEach((serviceId) => {
    const trimmed = serviceId.trim()
    if (trimmed) visibleServiceIds.add(trimmed)
  })

  const localByService = new Map<string, ReleaseTarget[]>()
  localTargets?.forEach((target) => {
    const serviceId = localTargetServiceId(target)
    if (!serviceId || !visibleServiceIds.has(serviceId)) return
    const targets = localByService.get(serviceId)
    if (targets) targets.push(target)
    else localByService.set(serviceId, [target])
  })

  const referenceByService = new Map<string, ServiceSlotReference>()
  activeReferences?.forEach((reference) => {
    const serviceId = reference.serviceId.trim()
    if (!visibleServiceIds.has(serviceId) || referenceByService.has(serviceId)) return
    referenceByService.set(serviceId, reference)
  })

  const slots = new Map<string, ServiceSlot>()
  const counts = { local: 0, reference: 0, unconfigured: 0, unknown: 0 }

  visibleServiceIds.forEach((serviceId) => {
    const targets = localByService.get(serviceId)
    const reference = referenceByService.get(serviceId)

    let slot: ServiceSlot
    if (targets && reference) {
      slot = { kind: 'unknown' }
    } else if (targets) {
      slot = { kind: 'local', targets }
    } else if (reference) {
      slot = { kind: 'reference', reference }
    } else if (localTargets !== undefined && activeReferences !== undefined) {
      slot = { kind: 'unconfigured' }
    } else {
      slot = { kind: 'unknown' }
    }

    slots.set(serviceId, slot)
    counts[slot.kind] += 1
  })

  return { slots, counts }
}

function localTargetServiceId(target: ReleaseTarget): string | undefined {
  const explicit = target.serviceId?.trim()
  if (explicit) return explicit

  const legacy = target.metadata?.serviceId
  return typeof legacy === 'string' ? legacy.trim() || undefined : undefined
}
