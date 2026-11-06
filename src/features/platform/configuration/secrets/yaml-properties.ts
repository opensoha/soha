import { parseDocument } from 'yaml'

// Read only the identity, version and mutability facts of authorized Secret YAML.
export function secretYAMLProperties(content: string, name: string, namespace: string) {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('Secret read budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid Secret YAML')
  const raw: unknown = doc.toJS({ maxAliasCount: 50 })
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('Invalid Secret')
  const root = raw as Record<string, unknown>,
    metadata = root.metadata as Record<string, unknown> | undefined
  if (
    root.apiVersion !== 'v1' ||
    root.kind !== 'Secret' ||
    metadata?.name !== name ||
    metadata.namespace !== namespace
  )
    throw new Error('Secret identity mismatch')
  if (root.immutable != null && typeof root.immutable !== 'boolean')
    throw new Error('Invalid immutable')
  return {
    immutable: root.immutable === true,
    uid: typeof metadata.uid === 'string' ? metadata.uid : '',
    resourceVersion: typeof metadata.resourceVersion === 'string' ? metadata.resourceVersion : '',
  }
}
