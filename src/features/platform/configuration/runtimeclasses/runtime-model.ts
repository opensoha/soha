import { parseDocument } from 'yaml'
import type { RuntimeClassResource } from './types'
const object = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
export interface RuntimeClassDetail extends RuntimeClassResource {
  readonly uid?: string
  readonly podFixed: Record<string, string>
  readonly nodeSelector: Record<string, string>
  readonly tolerations: {
    key: string
    operator: string
    value: string
    effect: string
    tolerationSeconds?: number
  }[]
}
export function runtimeClassFromYAML(content: string, name: string): RuntimeClassDetail {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('RuntimeClass read budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid RuntimeClass YAML')
  const root = object(doc.toJS({ maxAliasCount: 50 })),
    metadata = object(root.metadata)
  if (
    root.kind !== 'RuntimeClass' ||
    root.apiVersion !== 'node.k8s.io/v1' ||
    metadata.name !== name ||
    (metadata.namespace != null && metadata.namespace !== '')
  )
    throw new Error('RuntimeClass identity does not match the requested cluster resource')
  if (typeof root.handler !== 'string' || !root.handler.trim())
    throw new Error('RuntimeClass handler was not returned')
  for (const field of ['overhead', 'scheduling'])
    if (root[field] != null && (typeof root[field] !== 'object' || Array.isArray(root[field])))
      throw new Error('Invalid RuntimeClass ' + field)
  const overhead = object(root.overhead),
    scheduling = object(root.scheduling)
  const stringMap = (value: unknown, quantity = false): Record<string, string> => {
    if (value == null) return {}
    if (typeof value !== 'object' || Array.isArray(value))
      throw new Error('Invalid RuntimeClass map')
    return Object.fromEntries(
      Object.entries(object(value)).map(([key, item]) => {
        if (
          typeof item !== 'string' &&
          !(quantity && typeof item === 'number' && Number.isFinite(item))
        )
          throw new Error('Invalid RuntimeClass map value')
        return [key, String(item)]
      }),
    )
  }
  if (scheduling.tolerations != null && !Array.isArray(scheduling.tolerations))
    throw new Error('Invalid tolerations')
  const tolerations = ((scheduling.tolerations || []) as unknown[]).map((raw) => {
    const item = object(raw)
    for (const field of ['key', 'operator', 'value', 'effect'])
      if (item[field] != null && typeof item[field] !== 'string')
        throw new Error('Invalid toleration field')
    if (
      item.tolerationSeconds != null &&
      (typeof item.tolerationSeconds !== 'number' || !Number.isSafeInteger(item.tolerationSeconds))
    )
      throw new Error('Invalid toleration duration')
    return {
      key: typeof item.key === 'string' ? item.key : '',
      operator: typeof item.operator === 'string' && item.operator ? item.operator : 'Equal',
      value: typeof item.value === 'string' ? item.value : '',
      effect: typeof item.effect === 'string' ? item.effect : '',
      tolerationSeconds:
        typeof item.tolerationSeconds === 'number' ? item.tolerationSeconds : undefined,
    }
  })
  const createdAt =
    typeof metadata.creationTimestamp === 'string' ? metadata.creationTimestamp : undefined
  return {
    name,
    handler: root.handler,
    uid: typeof metadata.uid === 'string' ? metadata.uid : undefined,
    createdAt,
    ageSeconds:
      createdAt && Number.isFinite(Date.parse(createdAt))
        ? Math.max(0, Math.floor((Date.now() - Date.parse(createdAt)) / 1000))
        : 0,
    podFixed: stringMap(overhead.podFixed, true),
    nodeSelector: stringMap(scheduling.nodeSelector),
    tolerations,
    labels: stringMap(metadata.labels),
    annotations: stringMap(metadata.annotations),
  }
}
