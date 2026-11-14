import { parseDocument } from 'yaml'
export interface PolicyRule {
  apiGroups?: string[]
  resources?: string[]
  verbs: string[]
  resourceNames?: string[]
  nonResourceURLs?: string[]
}
export interface RoleSelector {
  matchLabels?: Record<string, string>
  matchExpressions?: { key: string; operator: string; values?: string[] }[]
}
export interface ClusterRoleConfiguration {
  rules: PolicyRule[]
  selectors: RoleSelector[]
  aggregated: boolean
}
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid ClusterRole object')
  return value as Record<string, unknown>
}
function text(value: unknown): string {
  if (typeof value !== 'string') throw new Error('Invalid ClusterRole text')
  return value
}
function array(value: unknown): unknown[] {
  if (!Array.isArray(value)) throw new Error('Invalid ClusterRole list')
  return value
}
function texts(value: unknown): string[] | undefined {
  return value == null ? undefined : array(value).map(text)
}
export function clusterRoleConfiguration(content: string, name: string): ClusterRoleConfiguration {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('ClusterRole budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid ClusterRole YAML')
  const root = object(doc.toJS({ maxAliasCount: 50 }))
  const metadata = object(root.metadata)
  if (
    root.kind !== 'ClusterRole' ||
    root.apiVersion !== 'rbac.authorization.k8s.io/v1' ||
    metadata.name !== name ||
    (metadata.namespace != null && metadata.namespace !== '')
  )
    throw new Error('ClusterRole identity mismatch')
  const aggregation = root.aggregationRule == null ? undefined : object(root.aggregationRule)
  return {
    aggregated: aggregation !== undefined,
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
    selectors: array(aggregation?.clusterRoleSelectors ?? []).map((entry) => {
      const s = object(entry)
      return {
        matchLabels:
          s.matchLabels == null
            ? undefined
            : Object.fromEntries(
                Object.entries(object(s.matchLabels)).map(([k, v]) => [k, text(v)]),
              ),
        matchExpressions:
          s.matchExpressions == null
            ? undefined
            : array(s.matchExpressions).map((entry) => {
                const e = object(entry)
                return { key: text(e.key), operator: text(e.operator), values: texts(e.values) }
              }),
      }
    }),
  }
}
