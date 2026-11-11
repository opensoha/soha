import { parseDocument } from 'yaml'

export interface ScConfiguration {
  isDefault: boolean
  mountOptions?: string[]
  allowedTopologies?: { matchLabelExpressions: { key: string; values: string[] }[] }[]
}
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid StorageClass object')
  return value as Record<string, unknown>
}
function array(value: unknown): unknown[] {
  if (!Array.isArray(value)) throw new Error('Invalid StorageClass list')
  return value
}
function text(value: unknown): string {
  if (typeof value !== 'string') throw new Error('Invalid StorageClass text')
  return value
}
export function scConfiguration(content: string, name: string): ScConfiguration {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('StorageClass budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid StorageClass YAML')
  const root = object(doc.toJS({ maxAliasCount: 50 }))
  const metadata = object(root.metadata)
  if (
    root.kind !== 'StorageClass' ||
    root.apiVersion !== 'storage.k8s.io/v1' ||
    metadata.name !== name ||
    (metadata.namespace != null && metadata.namespace !== '')
  )
    throw new Error('StorageClass identity mismatch')
  const annotations = metadata.annotations == null ? {} : object(metadata.annotations)
  return {
    isDefault:
      annotations['storageclass.kubernetes.io/is-default-class'] === 'true' ||
      annotations['storageclass.beta.kubernetes.io/is-default-class'] === 'true',
    mountOptions: root.mountOptions == null ? undefined : array(root.mountOptions).map(text),
    allowedTopologies:
      root.allowedTopologies == null
        ? undefined
        : array(root.allowedTopologies).map((entry) => ({
            matchLabelExpressions: array(object(entry).matchLabelExpressions ?? []).map(
              (expression) => {
                const item = object(expression)
                return { key: text(item.key), values: array(item.values).map(text) }
              },
            ),
          })),
  }
}
