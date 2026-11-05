import { parseDocument, stringify } from 'yaml'
import type { RolloutHistory } from '@/types'
const object = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
function document(content: string) {
  if (!content.trim() || new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('ControllerRevision YAML is unavailable or exceeds the size limit')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid ControllerRevision YAML')
  return object(doc.toJS({ maxAliasCount: 100 }))
}
export function readControllerRevision(content: string, record: RolloutHistory) {
  const value = document(content),
    metadata = object(value.metadata)
  if (
    value.kind !== 'ControllerRevision' ||
    metadata.name !== record.name ||
    metadata.namespace !== record.namespace ||
    String(value.revision) !== record.revision
  )
    throw new Error('ControllerRevision identity does not match the selected revision')
  const template = object(object(value.data).spec).template
  if (!template || !object(template).spec)
    throw new Error('Retained ControllerRevision has no Pod template')
  return { template: stringify(template, { sortMapEntries: true }) }
}
