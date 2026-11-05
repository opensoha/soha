import { parseDocument } from 'yaml'
export interface IngressClassParameters {
  apiGroup?: string
  kind: string
  name: string
  scope: 'Cluster' | 'Namespace'
  namespace?: string
}
export function ingressClassParameters(
  content: string,
  name: string,
): IngressClassParameters | null {
  const document = parseDocument(content)
  if (document.errors.length) throw new Error('Invalid IngressClass YAML')
  const value = document.toJS({ maxAliasCount: 30 })
  if (
    value?.kind !== 'IngressClass' ||
    value?.apiVersion !== 'networking.k8s.io/v1' ||
    value?.metadata?.name !== name ||
    value?.metadata?.namespace
  )
    throw new Error('IngressClass YAML identity mismatch')
  const p = value?.spec?.parameters
  if (!p) return null
  if (
    typeof p !== 'object' ||
    typeof p.kind !== 'string' ||
    typeof p.name !== 'string' ||
    !p.kind.trim() ||
    !p.name.trim() ||
    (p.scope !== undefined && !['Cluster', 'Namespace'].includes(p.scope)) ||
    (p.apiGroup !== undefined && typeof p.apiGroup !== 'string') ||
    (p.scope === 'Namespace' && (typeof p.namespace !== 'string' || !p.namespace.trim()))
  )
    throw new Error('Invalid IngressClass parameters reference')
  return {
    kind: p.kind,
    name: p.name,
    apiGroup: p.apiGroup,
    scope: p.scope || 'Cluster',
    namespace: p.scope === 'Namespace' ? p.namespace : undefined,
  }
}
