import { parseDocument } from 'yaml'

export interface BindingSubject {
  kind: string
  name: string
  namespace?: string
  apiGroup?: string
}
export interface BindingConfiguration {
  roleRef: { kind: string; name: string; apiGroup: string }
  subjects: BindingSubject[]
}
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid binding object')
  return value as Record<string, unknown>
}
function text(value: unknown): string {
  if (typeof value !== 'string') throw new Error('Invalid binding text')
  return value
}
export function bindingConfiguration(content: string, name: string): BindingConfiguration {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('Binding budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid binding YAML')
  const root = object(doc.toJS({ maxAliasCount: 50 }))
  const metadata = object(root.metadata)
  if (
    root.kind !== 'ClusterRoleBinding' ||
    root.apiVersion !== 'rbac.authorization.k8s.io/v1' ||
    metadata.name !== name ||
    (metadata.namespace != null && metadata.namespace !== '')
  )
    throw new Error('Binding identity mismatch')
  const ref = object(root.roleRef)
  if (
    ref.kind !== 'ClusterRole' ||
    ref.apiGroup !== 'rbac.authorization.k8s.io' ||
    !text(ref.name).trim()
  )
    throw new Error('Invalid cluster role reference')
  if (root.subjects != null && !Array.isArray(root.subjects))
    throw new Error('Invalid binding subjects')
  return {
    roleRef: { kind: text(ref.kind), name: text(ref.name), apiGroup: text(ref.apiGroup) },
    subjects: (root.subjects ?? []).map((entry: unknown) => {
      const subject = object(entry)
      return {
        kind: text(subject.kind),
        name: text(subject.name),
        namespace: subject.namespace == null ? undefined : text(subject.namespace),
        apiGroup: subject.apiGroup == null ? undefined : text(subject.apiGroup),
      }
    }),
  }
}
