import { parseDocument } from 'yaml'
import type { GatewayCondition } from './gateway-configuration'
import type { GatewayRouteBackend, HTTPRouteDetail } from './types'
import { grpcRouteMatches } from './grpcroute-matches'

export type GatewayRouteKind = 'HTTPRoute' | 'GRPCRoute'

const object = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
const text = (value: unknown) => (typeof value === 'string' ? value : '')
const list = (value: unknown): unknown[] => (Array.isArray(value) ? value : [])
const number = (value: unknown) =>
  typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? value : undefined
const json = (value: unknown) =>
  typeof value === 'string' ? value : (JSON.stringify(value) ?? '—')

export interface HTTPRouteReference {
  name: string
  namespace: string
  kind: string
  group: string
  sectionName?: string
  port?: number
  sourceLabel: string
}
export interface HTTPRouteFilter {
  type: string
  details: string[]
}
export interface HTTPRouteBackend extends GatewayRouteBackend {
  key: string
  group?: string
  native: boolean
  weightDefaulted: boolean
  filters: HTTPRouteFilter[]
}
export interface HTTPRouteRuleView {
  key: string
  index: number
  name?: string
  native: boolean
  matches: string[][]
  filters: HTTPRouteFilter[]
  backends: HTTPRouteBackend[]
  hasBackendRefs?: boolean
  settings: { label: string; value: string }[]
}
export interface HTTPRouteParentView {
  key: string
  label: string
  ref?: HTTPRouteReference
  controllerName?: string
  conditions: GatewayCondition[]
}
export interface HTTPRouteConfiguration {
  kind: GatewayRouteKind
  generation?: number
  createdAt?: string
  parents: HTTPRouteReference[]
  parentStatuses: HTTPRouteParentView[]
  rules: Record<string, unknown>[]
}

function reference(value: unknown, namespace: string, parent: boolean): HTTPRouteReference {
  const ref = object(value)
  const explicitKind = text(ref.kind),
    group = text(ref.group),
    name = text(ref.name)
  const ns = text(ref.namespace) || namespace
  const label = [explicitKind, group].filter(Boolean).join('.')
  return {
    name,
    namespace: ns,
    kind: explicitKind || (parent ? 'Gateway' : 'Service'),
    group: ref.group === undefined && parent ? 'gateway.networking.k8s.io' : group,
    sectionName: text(ref.sectionName) || undefined,
    port: number(ref.port),
    sourceLabel: `${ns}:${label ? `${label}/` : ''}${name}`,
  }
}
export function httpRouteReferenceKey(ref: HTTPRouteReference) {
  return JSON.stringify([ref.group, ref.kind, ref.namespace, ref.name, ref.sectionName, ref.port])
}
export function projectedGatewayReference(
  value: string,
  fallbackNamespace = '',
): HTTPRouteReference | undefined {
  const parts = value.split('/')
  if (parts.length !== 2 || !parts.every(Boolean) || parts.some((part) => part.includes(':')))
    return undefined
  return {
    name: parts[1],
    namespace: parts[0] || fallbackNamespace,
    kind: 'Gateway',
    group: 'gateway.networking.k8s.io',
    sourceLabel: value,
  }
}
function projectedParentReference(
  value: string,
  namespace: string,
): HTTPRouteReference | undefined {
  const colon = value.indexOf(':')
  const ns = colon >= 0 ? value.slice(0, colon) : namespace
  const raw = colon >= 0 ? value.slice(colon + 1) : value
  const parts = raw.split('/')
  if (parts.length > 2 || !ns || !parts.every(Boolean)) return undefined
  if (
    parts.length === 2 &&
    !['Gateway', 'Gateway.gateway.networking.k8s.io', 'gateway.networking.k8s.io'].includes(
      parts[0],
    )
  )
    return undefined
  return {
    name: parts.at(-1)!,
    namespace: ns,
    kind: 'Gateway',
    group: 'gateway.networking.k8s.io',
    sourceLabel: value,
  }
}
function conditions(value: unknown): GatewayCondition[] {
  return list(value).flatMap((raw) => {
    const item = object(raw)
    return text(item.type) && text(item.status)
      ? [
          {
            type: text(item.type),
            status: text(item.status),
            reason: text(item.reason),
            message: text(item.message),
            lastTransitionTime: text(item.lastTransitionTime),
            observedGeneration: number(item.observedGeneration),
          },
        ]
      : []
  })
}
export function httpRouteConfiguration(
  content: string,
  name: string,
  namespace: string,
  kind: GatewayRouteKind = 'HTTPRoute',
): HTTPRouteConfiguration {
  if (!content.trim() || new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error(`${kind} YAML is empty or exceeds 4 MiB`)
  const document = parseDocument(content)
  if (document.errors.length) throw new Error(`Invalid ${kind} YAML`)
  const manifest = object(document.toJS({ maxAliasCount: 50 })),
    metadata = object(manifest.metadata)
  if (
    manifest.kind !== kind ||
    ![
      'gateway.networking.k8s.io/v1',
      kind === 'GRPCRoute'
        ? 'gateway.networking.k8s.io/v1alpha2'
        : 'gateway.networking.k8s.io/v1beta1',
    ].includes(text(manifest.apiVersion)) ||
    metadata.name !== name ||
    metadata.namespace !== namespace
  )
    throw new Error(`${kind} YAML identity mismatch`)
  const spec = object(manifest.spec),
    status = object(manifest.status)
  return {
    kind,
    generation: number(metadata.generation),
    createdAt: text(metadata.creationTimestamp),
    parents: list(spec.parentRefs)
      .map((raw) => reference(raw, namespace, true))
      .filter((ref) => ref.name),
    rules: list(spec.rules).map(object),
    parentStatuses: list(status.parents).map((raw, index) => {
      const parent = object(raw),
        ref = reference(parent.parentRef, namespace, true)
      const controllerName = text(parent.controllerName)
      return {
        key: JSON.stringify([httpRouteReferenceKey(ref), controllerName, index]),
        ref,
        label: ref.sourceLabel,
        controllerName,
        conditions: conditions(parent.conditions),
      }
    }),
  }
}

function filters(value: unknown): HTTPRouteFilter[] {
  return list(value).map((raw) => {
    const filter = object(raw),
      type = text(filter.type) || 'Unknown'
    const details: string[] = []
    for (const [key, payload] of Object.entries(filter)) {
      if (key === 'type') continue
      // Reference identity stays with the permission-filtered DTO, which has no mirror/extension projection.
      if (key === 'extensionRef') {
        details.push('ExtensionRef · YAML')
        continue
      }
      if (key === 'requestMirror') {
        for (const [field, v] of Object.entries(object(payload)))
          if (field !== 'backendRef') details.push(`${field}: ${json(v)}`)
        details.push('backendRef · YAML')
        continue
      }
      details.push(`${key}: ${json(payload)}`)
    }
    return { type, details }
  })
}
function matches(value: unknown): string[][] {
  const values = list(value)
  return (values.length ? values : [{}]).map((raw) => {
    const match = object(raw),
      path = object(match.path)
    const parts = [`${text(path.type) || 'PathPrefix'} ${text(path.value) || '/'}`]
    if (text(match.method)) parts.push(`Method ${text(match.method)}`)
    for (const [key, label] of [
      ['headers', 'Header'],
      ['queryParams', 'Query'],
    ] as const)
      for (const rawValue of list(match[key])) {
        const item = object(rawValue)
        parts.push(
          `${label} ${text(item.type) || 'Exact'} · ${text(item.name)} = ${text(item.value)}`,
        )
      }
    for (const [key, v] of Object.entries(match))
      if (!['path', 'method', 'headers', 'queryParams'].includes(key))
        parts.push(`${key}: ${json(v)}`)
    return parts
  })
}

/** Enrich only references retained by the detail DTO; YAML never restores filtered resources. */
export function httpRouteModel(detail: HTTPRouteDetail, config?: HTTPRouteConfiguration) {
  const allowedParents = new Set(detail.parentRefs || [])
  const parents = config
    ? config.parents.filter(
        (ref) => ref.kind === 'Gateway' && allowedParents.has(`${ref.namespace}/${ref.name}`),
      )
    : (detail.parentRefs || []).flatMap((label) => {
        const ref = projectedGatewayReference(label, detail.namespace)
        return ref ? [ref] : []
      })
  const statuses: HTTPRouteParentView[] = config
    ? config.parentStatuses.filter((item) =>
        detail.parentStatuses?.some(
          (dto) =>
            dto.parentRef === item.label &&
            (dto.controllerName || '') === (item.controllerName || ''),
        ),
      )
    : (detail.parentStatuses || []).map((item, index) => ({
        ...item,
        key: JSON.stringify([item.parentRef, item.controllerName, index]),
        label: item.parentRef,
        ref: projectedParentReference(item.parentRef, detail.namespace),
        conditions: item.conditions || [],
      }))
  const rules: HTTPRouteRuleView[] = (detail.rules || []).map((dto, index) => {
    const native = config?.rules[index]
    const remaining = list(native?.backendRefs).map(object)
    const key = String(index)
    const backends: HTTPRouteBackend[] = (dto.backends || []).map((backend, offset) => {
      const found = remaining.findIndex((raw) => {
        const ref = reference(raw, detail.namespace, false)
        return (
          ref.kind === (backend.kind || 'Service') &&
          ref.name === backend.name &&
          ref.namespace === (backend.namespace || detail.namespace) &&
          (ref.port ?? 0) === (backend.port ?? 0)
        )
      })
      const raw = found >= 0 ? remaining.splice(found, 1)[0] : undefined
      const coreService = Boolean(
        raw && !text(raw.group) && (text(raw.kind) || 'Service') === 'Service',
      )
      return {
        ...backend,
        endpoints: coreService ? backend.endpoints : undefined,
        backendPods: coreService ? backend.backendPods : undefined,
        namespace: backend.namespace || detail.namespace,
        group: raw ? text(raw.group) : undefined,
        key: JSON.stringify([
          key,
          backend.kind,
          backend.namespace,
          backend.name,
          backend.port,
          offset,
        ]),
        port: raw ? number(raw.port) : backend.port && backend.port > 0 ? backend.port : undefined,
        weight: raw
          ? (number(raw.weight) ?? 1)
          : backend.weight && backend.weight > 0
            ? backend.weight
            : undefined,
        weightDefaulted: Boolean(raw && raw.weight === undefined),
        native: Boolean(raw),
        filters: filters(raw?.filters),
      }
    })
    return {
      key,
      index,
      name: text(native?.name) || undefined,
      native: Boolean(native),
      matches: native
        ? config?.kind === 'GRPCRoute'
          ? grpcRouteMatches(native.matches)
          : matches(native.matches)
        : (dto.matches || []).filter(Boolean).map((label) => [label]),
      filters: native
        ? filters(native.filters)
        : (dto.filters || []).map((type) => ({ type, details: [] })),
      backends,
      hasBackendRefs: native ? list(native.backendRefs).length > 0 : undefined,
      settings: native
        ? Object.entries(native)
            .filter(([key]) => !['name', 'matches', 'filters', 'backendRefs'].includes(key))
            .map(([label, v]) => ({ label, value: json(v) }))
        : [],
    }
  })
  return { parents, statuses, rules }
}
