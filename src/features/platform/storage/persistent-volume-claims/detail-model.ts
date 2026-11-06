import { parseDocument } from 'yaml'
import type { WorkloadCondition } from '@/types'

export interface PvcSourceReference {
  apiGroup?: string
  kind: string
  name: string
  namespace?: string
}
export interface PvcConfiguration {
  conditions: WorkloadCondition[]
  volumeMode?: string
  storageClassName?: string
  selector?: {
    matchLabels?: Record<string, string>
    matchExpressions?: { key: string; operator: string; values?: string[] }[]
  }
  dataSource?: PvcSourceReference
  dataSourceRef?: PvcSourceReference
}
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid PVC object')
  return value as Record<string, unknown>
}
function optionalText(value: unknown): string | undefined {
  if (value == null) return undefined
  if (typeof value !== 'string') throw new Error('Invalid PVC field')
  return value
}
function requiredText(value: unknown): string {
  const text = optionalText(value)
  if (!text) throw new Error('Missing PVC field')
  return text
}
function array(value: unknown): unknown[] {
  if (value == null) return []
  if (!Array.isArray(value)) throw new Error('Invalid PVC list')
  return value
}
function source(value: unknown): PvcSourceReference | undefined {
  if (value == null) return undefined
  const item = object(value)
  return {
    name: requiredText(item.name),
    kind: requiredText(item.kind),
    apiGroup: optionalText(item.apiGroup),
    namespace: optionalText(item.namespace),
  }
}
/** Only the requested object's authorized YAML supplies supplemental configuration facts. */
export function pvcConfiguration(
  content: string,
  name: string,
  namespace: string,
): PvcConfiguration {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('PVC read budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid PVC YAML')
  const root = object(doc.toJS({ maxAliasCount: 50 }))
  const metadata = object(root.metadata)
  if (
    root.apiVersion !== 'v1' ||
    root.kind !== 'PersistentVolumeClaim' ||
    metadata.name !== name ||
    metadata.namespace !== namespace
  )
    throw new Error('PVC identity does not match the requested resource')
  const spec = root.spec == null ? {} : object(root.spec)
  const status = root.status == null ? {} : object(root.status)
  let selector: PvcConfiguration['selector']
  if (spec.selector != null) {
    const raw = object(spec.selector)
    selector = {}
    if (raw.matchLabels != null)
      selector.matchLabels = Object.fromEntries(
        Object.entries(object(raw.matchLabels)).map(([key, value]) => [
          key,
          (() => {
            const text = optionalText(value)
            if (text === undefined) throw new Error('Invalid selector value')
            return text
          })(),
        ]),
      )
    if (raw.matchExpressions != null)
      selector.matchExpressions = array(raw.matchExpressions).map((entry) => {
        const term = object(entry)
        return {
          key: requiredText(term.key),
          operator: requiredText(term.operator),
          ...(term.values == null
            ? {}
            : {
                values: array(term.values).map((value) => {
                  const text = optionalText(value)
                  if (text === undefined) throw new Error('Invalid selector value')
                  return text
                }),
              }),
        }
      })
  }
  return {
    storageClassName: optionalText(spec.storageClassName),
    volumeMode: optionalText(spec.volumeMode),
    selector,
    dataSource: source(spec.dataSource),
    dataSourceRef: source(spec.dataSourceRef),
    conditions: array(status.conditions).map((entry) => {
      const condition = object(entry)
      return {
        type: requiredText(condition.type),
        status: requiredText(condition.status),
        reason: optionalText(condition.reason),
        message: optionalText(condition.message),
        lastTransitionTime: optionalText(condition.lastTransitionTime),
      }
    }),
  }
}
