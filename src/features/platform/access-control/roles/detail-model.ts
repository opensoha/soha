import { parseDocument } from 'yaml'
export interface PolicyRule {
  apiGroups?: string[]
  resources?: string[]
  verbs: string[]
  resourceNames?: string[]
  nonResourceURLs?: string[]
}
export interface RoleConfiguration {
  rules: PolicyRule[]
}
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid Role object')
  return value as Record<string, unknown>
}
function text(value: unknown): string {
  if (typeof value !== 'string') throw new Error('Invalid Role text')
  return value
}
function array(value: unknown): unknown[] {
  if (!Array.isArray(value)) throw new Error('Invalid Role list')
  return value
}
function texts(value: unknown): string[] | undefined {
  return value == null ? undefined : array(value).map(text)
}
export function roleConfiguration(
  content: string,
  name: string,
  namespace: string,
): RoleConfiguration {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('Role budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid Role YAML')
  const root = object(doc.toJS({ maxAliasCount: 50 }))
  const metadata = object(root.metadata)
  if (
    root.kind !== 'Role' ||
    root.apiVersion !== 'rbac.authorization.k8s.io/v1' ||
    metadata.name !== name ||
    metadata.namespace !== namespace
  )
    throw new Error('Role identity mismatch')
  return {
    rules: array(root.rules ?? []).map((entry) => {
      const r = object(entry)
      return {
        apiGroups: texts(r.apiGroups),
        resources: texts(r.resources),
        verbs: texts(r.verbs) ?? [],
        resourceNames: texts(r.resourceNames),
        nonResourceURLs: texts(r.nonResourceURLs),
      }
    }),
  }
}
