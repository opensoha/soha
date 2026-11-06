import { parseDocument } from 'yaml'
import type { PriorityClassResource } from './types'
const object = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
export interface PriorityClassDetail extends PriorityClassResource {
  readonly uid?: string
  readonly policyDefaulted: boolean
  readonly globalDefaultDefaulted: boolean
}
export function priorityClassFromYAML(content: string, name: string): PriorityClassDetail {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('PriorityClass read budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid PriorityClass YAML')
  const root = object(doc.toJS({ maxAliasCount: 50 })),
    metadata = object(root.metadata)
  if (
    root.apiVersion !== 'scheduling.k8s.io/v1' ||
    root.kind !== 'PriorityClass' ||
    metadata.name !== name ||
    (metadata.namespace != null && metadata.namespace !== '')
  )
    throw new Error('PriorityClass identity does not match the requested cluster resource')
  if (
    typeof root.value !== 'number' ||
    !Number.isInteger(root.value) ||
    root.value < -2147483648 ||
    root.value > 2147483647
  )
    throw new Error('PriorityClass value was not returned as an int32')
  if (root.globalDefault != null && typeof root.globalDefault !== 'boolean')
    throw new Error('Invalid globalDefault')
  if (root.preemptionPolicy != null && typeof root.preemptionPolicy !== 'string')
    throw new Error('Invalid preemptionPolicy')
  if (root.description != null && typeof root.description !== 'string')
    throw new Error('Invalid description')
  const strings = (value: unknown) =>
    Object.fromEntries(
      Object.entries(object(value)).filter(
        (entry): entry is [string, string] => typeof entry[1] === 'string',
      ),
    )
  const createdAt =
    typeof metadata.creationTimestamp === 'string' ? metadata.creationTimestamp : undefined
  return {
    name,
    value: root.value,
    description: typeof root.description === 'string' ? root.description : undefined,
    globalDefault: root.globalDefault === true,
    preemptionPolicy:
      typeof root.preemptionPolicy === 'string' ? root.preemptionPolicy : 'PreemptLowerPriority',
    policyDefaulted: root.preemptionPolicy == null,
    globalDefaultDefaulted: root.globalDefault == null,
    uid: typeof metadata.uid === 'string' ? metadata.uid : undefined,
    createdAt,
    ageSeconds:
      createdAt && Number.isFinite(Date.parse(createdAt))
        ? Math.max(0, Math.floor((Date.now() - Date.parse(createdAt)) / 1000))
        : 0,
    labels: strings(metadata.labels),
    annotations: strings(metadata.annotations),
  }
}
