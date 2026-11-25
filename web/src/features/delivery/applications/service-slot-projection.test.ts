import { describe, expect, it } from 'vitest'
import type { ReleaseTarget } from '../domain-types'
import { computeServiceSlots, type ServiceSlotReference } from './service-slot-projection'

function target(
  id: string,
  serviceId: string | undefined,
  metadataServiceId?: string,
): ReleaseTarget {
  return {
    id,
    clusterId: 'cluster',
    namespace: 'demo',
    workloadKind: 'Deployment',
    workloadName: id,
    serviceId,
    metadata: metadataServiceId === undefined ? undefined : { serviceId: metadataServiceId },
    enabled: false,
  }
}

function reference(serviceId: string, id = `reference-${serviceId}`): ServiceSlotReference {
  return { id, serviceId, revision: 1 }
}

describe('computeServiceSlots', () => {
  it('classifies explicit and legacy local mappings, including disabled and observe-only targets', () => {
    const direct = target('direct', ' cart ')
    const observe = { ...target('observe', undefined, ' legacy '), executorKind: 'observe_only' }
    const hidden = target('hidden', 'outside')

    const result = computeServiceSlots([' cart ', 'legacy', 'cart'], [direct, observe, hidden], [])

    expect([...result.slots.keys()]).toEqual(['cart', 'legacy'])
    expect(result.slots.get('cart')).toEqual({ kind: 'local', targets: [direct] })
    expect(result.slots.get('legacy')).toEqual({ kind: 'local', targets: [observe] })
    expect(result.counts).toEqual({ local: 2, reference: 0, unconfigured: 0, unknown: 0 })
  })

  it('uses positive evidence without requiring the other read', () => {
    const local = target('local', 'local-service')
    const active = reference('reference-service')

    const localOnly = computeServiceSlots(['local-service'], [local], undefined)
    expect(localOnly.slots.get('local-service')).toEqual({ kind: 'local', targets: [local] })

    const referenceOnly = computeServiceSlots(['reference-service'], undefined, [active])
    expect(referenceOnly.slots.get('reference-service')).toEqual({
      kind: 'reference',
      reference: active,
    })
  })

  it('treats absent evidence as unconfigured only when both reads are authoritative', () => {
    const empty = computeServiceSlots(['service'], [], [])
    expect(empty.slots.get('service')).toEqual({ kind: 'unconfigured' })
    expect(empty.counts).toEqual({ local: 0, reference: 0, unconfigured: 1, unknown: 0 })

    expect(computeServiceSlots(['service'], undefined, []).slots.get('service')).toEqual({
      kind: 'unknown',
    })
    expect(computeServiceSlots(['service'], [], undefined).slots.get('service')).toEqual({
      kind: 'unknown',
    })
    expect(computeServiceSlots(['service'], undefined, undefined).counts.unknown).toBe(1)
  })

  it('reports a local/reference conflict as unknown instead of either publish source', () => {
    const local = target('local', undefined, 'conflict')
    const referenceOnly = reference('reference-only')

    const result = computeServiceSlots(
      ['conflict', 'reference-only'],
      [local],
      [reference('conflict'), referenceOnly, reference('outside')],
    )

    expect(result.slots.get('conflict')).toEqual({ kind: 'unknown' })
    expect(result.slots.get('reference-only')).toEqual({
      kind: 'reference',
      reference: referenceOnly,
    })
    expect(result.counts).toEqual({ local: 0, reference: 1, unconfigured: 0, unknown: 1 })
  })
})
