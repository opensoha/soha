import { parseDocument } from 'yaml'
import type { WorkloadCondition } from '@/types'
import type { GatewayDetail, GatewayListener, GatewayRouteReference } from './types'

const object = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
const text = (value: unknown) => (typeof value === 'string' ? value : '')
const array = (value: unknown): unknown[] => (Array.isArray(value) ? value : [])
const count = (value: unknown) =>
  typeof value === 'number' && Number.isInteger(value) && value >= 0 ? value : undefined

export interface GatewayCondition extends WorkloadCondition {
  observedGeneration?: number
}
export interface GatewayCertificateReference {
  name: string
  namespace: string
  kind: string
  group: string
  /** Matches the server's permission-filtered reference projection. */
  sourceLabel: string
}
export interface GatewayListenerConfiguration {
  name: string
  protocol: string
  port?: number
  hostname?: string
  tlsMode?: string
  tlsConfigured: boolean
  certificateRefs: GatewayCertificateReference[]
  certificateLabels: string[]
  allowedRouteKinds?: string[]
  allowedNamespaces?: string
  namespaceSelector: string[]
  supportedKinds?: string[]
  attachedRoutes?: number
  conditions: GatewayCondition[]
  statusReturned: boolean
  configurationReturned: boolean
}
export interface GatewayConfiguration {
  generation?: number
  createdAt?: string
  requestedAddresses: string[]
  listeners: GatewayListenerConfiguration[]
  conditions: GatewayCondition[]
}

function conditions(value: unknown): GatewayCondition[] {
  return array(value).flatMap((raw) => {
    const item = object(raw)
    return text(item.type) && text(item.status)
      ? [
          {
            type: text(item.type),
            status: text(item.status),
            reason: text(item.reason),
            message: text(item.message),
            lastTransitionTime: text(item.lastTransitionTime),
            observedGeneration: count(item.observedGeneration),
          },
        ]
      : []
  })
}

function reference(value: unknown, namespace: string): GatewayCertificateReference {
  const item = object(value),
    name = text(item.name),
    explicitKind = text(item.kind),
    group = text(item.group)
  const targetNamespace = text(item.namespace) || namespace
  const label = [explicitKind, group].filter(Boolean).join('.')
  return {
    name,
    namespace: targetNamespace,
    group,
    kind: explicitKind || 'Secret',
    sourceLabel: `${targetNamespace}:${label ? `${label}/` : ''}${name}`,
  }
}

function selector(value: unknown): string[] {
  const item = object(value)
  return [
    ...Object.entries(object(item.matchLabels)).map(([key, value]) => `${key}=${String(value)}`),
    ...array(item.matchExpressions).map((raw) => {
      const expression = object(raw)
      return `${text(expression.key)} ${text(expression.operator)}${array(expression.values).length ? ` (${array(expression.values).map(String).join(', ')})` : ''}`.trim()
    }),
  ]
}

export function gatewayConfiguration(
  content: string,
  name: string,
  namespace: string,
): GatewayConfiguration {
  if (!content.trim() || new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('Gateway YAML is empty or exceeds 4 MiB')
  const document = parseDocument(content)
  if (document.errors.length) throw new Error('Invalid Gateway YAML')
  const manifest = object(document.toJS({ maxAliasCount: 50 })),
    metadata = object(manifest.metadata)
  if (
    !['gateway.networking.k8s.io/v1', 'gateway.networking.k8s.io/v1beta1'].includes(
      text(manifest.apiVersion),
    ) ||
    manifest.kind !== 'Gateway' ||
    metadata.name !== name ||
    metadata.namespace !== namespace
  )
    throw new Error('Gateway YAML identity does not match the current resource')
  const spec = object(manifest.spec),
    status = object(manifest.status)
  const statusByName = new Map(
    array(status.listeners).map((raw) => {
      const item = object(raw)
      return [text(item.name), item] as const
    }),
  )
  return {
    generation: count(metadata.generation),
    createdAt: text(metadata.creationTimestamp),
    requestedAddresses: array(spec.addresses)
      .map((raw) => text(object(raw).value))
      .filter(Boolean),
    conditions: conditions(status.conditions),
    listeners: array(spec.listeners).map((raw) => {
      const item = object(raw),
        name = text(item.name),
        state = statusByName.get(name)
      const tls = object(item.tls),
        allowed = object(item.allowedRoutes),
        namespaces = object(allowed.namespaces)
      return {
        name,
        protocol: text(item.protocol),
        port: count(item.port),
        hostname: text(item.hostname),
        tlsConfigured: item.tls !== undefined,
        tlsMode: text(tls.mode),
        certificateRefs: array(tls.certificateRefs)
          .map((raw) => reference(raw, namespace))
          .filter((r) => r.name),
        certificateLabels: [],
        allowedRouteKinds: array(allowed.kinds).map((raw) => {
          const kind = object(raw)
          return [text(kind.kind), text(kind.group)].filter(Boolean).join('.')
        }),
        allowedNamespaces: text(namespaces.from),
        namespaceSelector: selector(namespaces.selector),
        supportedKinds: state
          ? array(state.supportedKinds).map((raw) => {
              const kind = object(raw)
              return [text(kind.kind), text(kind.group)].filter(Boolean).join('.')
            })
          : undefined,
        attachedRoutes: count(state?.attachedRoutes),
        conditions: conditions(state?.conditions),
        statusReturned: Boolean(state),
        configurationReturned: true,
      }
    }),
  }
}

/** Native configuration enriches facts; the DTO retains ownership of filtered references. */
export function gatewayListeners(
  detail: GatewayDetail,
  configuration?: GatewayConfiguration,
): GatewayListenerConfiguration[] {
  if (configuration)
    return configuration.listeners.map((listener) => {
      const readableRefs = new Set(
        detail.listeners?.find((item) => item.name === listener.name)?.certificateRefs || [],
      )
      return {
        ...listener,
        certificateRefs: listener.certificateRefs.filter((ref) =>
          readableRefs.has(ref.sourceLabel),
        ),
        certificateLabels: [...readableRefs],
      }
    })
  return (detail.listeners || []).map((item: GatewayListener) => ({
    name: item.name,
    protocol: item.protocol,
    port: item.port,
    hostname: item.hostname,
    tlsMode: item.tlsMode,
    tlsConfigured: Boolean(item.tlsMode || item.certificateRefs?.length),
    certificateRefs: [],
    certificateLabels: item.certificateRefs || [],
    allowedRouteKinds: item.allowedRouteKinds,
    namespaceSelector: [],
    attachedRoutes: item.attachedRoutes > 0 ? item.attachedRoutes : undefined,
    conditions: item.conditions || [],
    statusReturned: Boolean(item.conditions?.length),
    configurationReturned: false,
  }))
}

export function gatewayRouteKey(route: GatewayRouteReference, namespace: string) {
  return JSON.stringify([route.kind, route.namespace || namespace, route.name])
}

export function staleGatewayCondition(condition: GatewayCondition, generation?: number) {
  return (
    generation !== undefined &&
    condition.observedGeneration !== undefined &&
    condition.observedGeneration < generation
  )
}
