import { parseDocument } from 'yaml'
import type { LeaseResource } from './types'

export interface LeaseDetail extends LeaseResource {
  readonly uid?: string
  readonly leaseTransitions?: number
  readonly preferredHolder?: string
  readonly strategy?: string
}
const record = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}

export function leaseFromYAML(content: string, name: string, namespace?: string): LeaseDetail {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('Lease read budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid Lease YAML')
  const root = record(doc.toJS({ maxAliasCount: 50 })),
    metadata = record(root.metadata)
  if (
    root.apiVersion !== 'coordination.k8s.io/v1' ||
    root.kind !== 'Lease' ||
    metadata.name !== name ||
    !namespace ||
    metadata.namespace !== namespace
  )
    throw new Error('Lease identity does not match the requested resource')
  if (root.spec != null && (typeof root.spec !== 'object' || Array.isArray(root.spec)))
    throw new Error('Invalid Lease spec')
  const spec = record(root.spec)
  const text = (value: unknown): string | undefined => {
    if (value == null) return undefined
    if (typeof value !== 'string') throw new Error('Invalid Lease string')
    return value
  }
  const integer = (value: unknown): number | undefined => {
    if (value == null) return undefined
    if (typeof value !== 'number' || !Number.isInteger(value) || value < 0 || value > 2147483647)
      throw new Error('Invalid Lease integer')
    return value
  }
  const map = (value: unknown): Record<string, string> => {
    if (value == null) return {}
    if (typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid metadata map')
    return Object.fromEntries(
      Object.entries(record(value)).map(([key, item]) => {
        if (typeof item !== 'string') throw new Error('Invalid metadata value')
        return [key, item]
      }),
    )
  }
  const createdAt = text(metadata.creationTimestamp)
  return {
    name,
    namespace,
    uid: text(metadata.uid),
    createdAt,
    ageSeconds:
      createdAt && Number.isFinite(Date.parse(createdAt))
        ? Math.max(0, Math.floor((Date.now() - Date.parse(createdAt)) / 1000))
        : 0,
    holderIdentity: text(spec.holderIdentity),
    acquireTime: text(spec.acquireTime),
    renewTime: text(spec.renewTime),
    leaseDurationSeconds: integer(spec.leaseDurationSeconds),
    leaseTransitions: integer(spec.leaseTransitions),
    preferredHolder: text(spec.preferredHolder),
    strategy: text(spec.strategy),
    labels: map(metadata.labels),
    annotations: map(metadata.annotations),
  }
}

export function leaseTiming(detail: LeaseDetail, now: number) {
  if (!detail.holderIdentity) return { state: 'unheld' as const }
  const renewed = detail.renewTime ? Date.parse(detail.renewTime) : NaN
  const duration = detail.leaseDurationSeconds
  if (!Number.isFinite(renewed) || duration == null || duration <= 0)
    return { state: 'unknown' as const }
  const expiresAt = renewed + duration * 1000
  if (!Number.isFinite(expiresAt) || Math.abs(expiresAt) > 8640000000000000)
    return { state: 'unknown' as const }
  if (!Number.isFinite(now) || renewed > now) return { state: 'clock' as const, expiresAt }
  return { state: expiresAt <= now ? ('elapsed' as const) : ('within' as const), expiresAt }
}
