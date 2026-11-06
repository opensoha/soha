import { parseDocument } from 'yaml'
import type { EndpointSliceDetail, EndpointSliceEndpoint } from './types'
const object = (v: unknown): Record<string, unknown> =>
  v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : {}
const text = (v: unknown) => (typeof v === 'string' ? v : '')
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : [])
export interface SliceConfiguration {
  endpointCount: number
  targetNamespaces: Record<string, string>
}
export const sliceEndpointKey = (e: EndpointSliceEndpoint) =>
  JSON.stringify([e.address, e.targetRef])
/** YAML supplements scope only; the permission-filtered DTO still owns visible references. */
export function sliceConfiguration(
  content: string,
  detail: EndpointSliceDetail,
): SliceConfiguration {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('EndpointSlice read budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid EndpointSlice YAML')
  const root = object(doc.toJS({ maxAliasCount: 50 })),
    meta = object(root.metadata)
  if (
    root.kind !== 'EndpointSlice' ||
    !['discovery.k8s.io/v1', 'discovery.k8s.io/v1beta1'].includes(text(root.apiVersion)) ||
    meta.name !== detail.name ||
    meta.namespace !== detail.namespace
  )
    throw new Error('EndpointSlice identity mismatch')
  const endpoints = list(root.endpoints),
    candidates = new Map<string, Set<string>>()
  for (const raw of endpoints) {
    const e = object(raw),
      ref = object(e.targetRef)
    if (
      ref.kind !== 'Pod' ||
      (ref.apiVersion !== 'v1' && ref.apiVersion !== undefined) ||
      !text(ref.namespace) ||
      !text(ref.name)
    )
      continue
    for (const address of list(e.addresses)) {
      if (typeof address !== 'string') continue
      const key = sliceEndpointKey({ address, targetRef: `Pod/${text(ref.name)}` })
      const namespaces = candidates.get(key) || new Set<string>()
      namespaces.add(text(ref.namespace))
      candidates.set(key, namespaces)
    }
  }
  const targetNamespaces: Record<string, string> = {}
  for (const e of detail.endpoints || []) {
    if (!e.targetRef) continue
    const key = sliceEndpointKey(e),
      ns = candidates.get(key)
    if (ns?.size === 1) targetNamespaces[key] = [...ns][0]
  }
  return { endpointCount: endpoints.length, targetNamespaces }
}
