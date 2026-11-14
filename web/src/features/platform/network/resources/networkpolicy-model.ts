import { parseDocument } from 'yaml'
import type { NetworkPolicyDetail } from './types'
export type PolicyObject = Record<string, unknown>
const object = (v: unknown): PolicyObject =>
  v && typeof v === 'object' && !Array.isArray(v) ? (v as PolicyObject) : {}
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : [])
export interface PolicyRuleView {
  key: string
  peers: PolicyObject[]
  ports: PolicyObject[]
}
export interface PolicyDirection {
  name: 'Ingress' | 'Egress'
  isolated: boolean
  rules: PolicyRuleView[]
}
export interface PolicyConfiguration {
  selector: PolicyObject
  directions: PolicyDirection[]
}
export function policyConfiguration(
  content: string,
  detail: NetworkPolicyDetail,
): PolicyConfiguration {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('NetworkPolicy read budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid NetworkPolicy YAML')
  const root = object(doc.toJS({ maxAliasCount: 50 })),
    meta = object(root.metadata),
    spec = object(root.spec)
  if (
    root.kind !== 'NetworkPolicy' ||
    root.apiVersion !== 'networking.k8s.io/v1' ||
    meta.name !== detail.name ||
    meta.namespace !== detail.namespace ||
    !spec.podSelector ||
    typeof spec.podSelector !== 'object'
  )
    throw new Error('NetworkPolicy identity or selector missing')
  for (const key of ['policyTypes', 'ingress', 'egress'])
    if (spec[key] !== undefined && !Array.isArray(spec[key])) throw new Error('Invalid policy list')
  const types =
    spec.policyTypes === undefined
      ? ['Ingress', ...(list(spec.egress).length ? ['Egress'] : [])]
      : list(spec.policyTypes)
  return {
    selector: object(spec.podSelector),
    directions: (['Ingress', 'Egress'] as const).map((name) => ({
      name,
      isolated: types.includes(name),
      rules: list(spec[name.toLowerCase()]).map((raw, i) => {
        const r = object(raw),
          field = name === 'Ingress' ? 'from' : 'to'
        if (
          (r[field] !== undefined && !Array.isArray(r[field])) ||
          (r.ports !== undefined && !Array.isArray(r.ports))
        )
          throw new Error('Invalid policy rule')
        return {
          key: `${name}/${i}`,
          peers: list(r[field]).map(object),
          ports: list(r.ports).map(object),
        }
      }),
    })),
  }
}
export function policySelectorTerms(selector: PolicyObject): string[] {
  return [
    ...Object.entries(object(selector.matchLabels)).map(([k, v]) => `${k} = ${String(v)}`),
    ...list(selector.matchExpressions).map((raw) => {
      const e = object(raw)
      return `${String(e.key)} ${String(e.operator)}${Array.isArray(e.values) && e.values.length ? ` (${e.values.join(', ')})` : ''}`
    }),
  ]
}
