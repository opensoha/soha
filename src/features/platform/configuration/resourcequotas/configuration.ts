import { parseDocument } from 'yaml'
import type { ResourceQuotaResource } from './types'
const object = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}

export function quotaConfiguration(content: string, detail: ResourceQuotaResource) {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('ResourceQuota read budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid ResourceQuota YAML')
  const root = object(doc.toJS({ maxAliasCount: 50 })),
    meta = object(root.metadata),
    spec = object(root.spec)
  if (
    root.kind !== 'ResourceQuota' ||
    root.apiVersion !== 'v1' ||
    meta.name !== detail.name ||
    meta.namespace !== detail.namespace
  )
    throw new Error('ResourceQuota identity does not match the requested resource')
  if (!spec.hard || typeof spec.hard !== 'object' || Array.isArray(spec.hard))
    throw new Error('ResourceQuota hard limits were not returned')
  const hard = Object.fromEntries(
    Object.entries(object(spec.hard)).map(([key, value]) => {
      if (typeof value !== 'string' && (typeof value !== 'number' || !Number.isFinite(value)))
        throw new Error('Invalid ResourceQuota quantity')
      return [key, String(value)]
    }),
  )
  if (
    spec.scopes !== undefined &&
    (!Array.isArray(spec.scopes) || spec.scopes.some((value) => typeof value !== 'string'))
  )
    throw new Error('Invalid ResourceQuota scopes')
  if (
    spec.scopeSelector != null &&
    (typeof spec.scopeSelector !== 'object' || Array.isArray(spec.scopeSelector))
  )
    throw new Error('Invalid ResourceQuota scope selector')
  const selector = object(spec.scopeSelector)
  if (selector.matchExpressions !== undefined && !Array.isArray(selector.matchExpressions))
    throw new Error('Invalid ResourceQuota scope selector')
  const expressions = ((selector.matchExpressions || []) as unknown[]).map((raw) => {
    const term = object(raw)
    if (
      typeof term.scopeName !== 'string' ||
      typeof term.operator !== 'string' ||
      (term.values !== undefined &&
        (!Array.isArray(term.values) || term.values.some((value) => typeof value !== 'string')))
    )
      throw new Error('Invalid quota scope expression')
    return {
      scopeName: term.scopeName,
      operator: term.operator,
      values: (term.values || []) as string[],
    }
  })
  return { hard, scopes: (spec.scopes || []) as string[], expressions }
}
