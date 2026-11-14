import { parseDocument } from 'yaml'
export interface SaConfiguration {
  automount?: boolean
}
export function saConfiguration(content: string, name: string, namespace: string): SaConfiguration {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('ServiceAccount budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid ServiceAccount YAML')
  const root = doc.toJS({ maxAliasCount: 50 })
  if (
    !root ||
    root.apiVersion !== 'v1' ||
    root.kind !== 'ServiceAccount' ||
    root.metadata?.name !== name ||
    root.metadata?.namespace !== namespace
  )
    throw new Error('ServiceAccount identity mismatch')
  const value = root.automountServiceAccountToken
  if (value != null && typeof value !== 'boolean') throw new Error('Invalid automount value')
  return { automount: value ?? undefined }
}
