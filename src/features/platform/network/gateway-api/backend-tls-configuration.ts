import { parseDocument } from 'yaml'
import type { GatewayCondition } from './gateway-configuration'
import type { BackendTLSPolicyDetail } from './types'

const object = (v: unknown): Record<string, unknown> =>
  v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : {}
const text = (v: unknown) => (typeof v === 'string' ? v : '')
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : [])
const generation = (v: unknown) =>
  typeof v === 'number' && Number.isSafeInteger(v) && v >= 0 ? v : undefined
export interface PolicyReference {
  name: string
  namespace: string
  kind: string
  group: string
  sectionName?: string
  sourceLabel: string
}
export interface PolicyAncestor {
  key: string
  ref: PolicyReference
  controller: string
  conditions: GatewayCondition[]
}
export interface BackendTLSConfiguration {
  generation?: number
  createdAt?: string
  hostname: string
  sans: string[]
  systemCA: string
  targets: PolicyReference[]
  caRefs: PolicyReference[]
  ancestors: PolicyAncestor[]
  options: Record<string, unknown>
}
function reference(raw: unknown, namespace: string, ancestor = false): PolicyReference {
  const r = object(raw),
    explicitKind = text(r.kind),
    group = text(r.group),
    ns = text(r.namespace) || namespace
  const label = [explicitKind, group].filter(Boolean).join('.')
  return {
    name: text(r.name),
    namespace: ns,
    kind: explicitKind || (ancestor ? 'Gateway' : 'Unknown'),
    group: ancestor && r.group === undefined ? 'gateway.networking.k8s.io' : group,
    sectionName: text(r.sectionName) || undefined,
    sourceLabel: `${ns}:${label ? label + '/' : ''}${text(r.name)}`,
  }
}
export function backendTLSConfiguration(
  content: string,
  detail: BackendTLSPolicyDetail,
): BackendTLSConfiguration {
  if (!content.trim() || new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('BackendTLSPolicy YAML exceeds read budget')
  const document = parseDocument(content)
  if (document.errors.length) throw new Error('Invalid BackendTLSPolicy YAML')
  const m = object(document.toJS({ maxAliasCount: 50 })),
    metadata = object(m.metadata),
    spec = object(m.spec),
    status = object(m.status)
  if (
    m.kind !== 'BackendTLSPolicy' ||
    !['gateway.networking.k8s.io/v1', 'gateway.networking.k8s.io/v1alpha3'].includes(
      text(m.apiVersion),
    ) ||
    metadata.name !== detail.name ||
    metadata.namespace !== detail.namespace
  )
    throw new Error('BackendTLSPolicy YAML identity mismatch')
  const validation = object(spec.validation),
    targets = new Set(detail.targetRefs || []),
    caRefs = new Set(detail.caCertificateRefs || [])
  return {
    generation: generation(metadata.generation),
    createdAt: text(metadata.creationTimestamp),
    hostname: text(validation.hostname),
    sans: list(validation.subjectAltNames).map((raw) => {
      const r = object(raw)
      return `${text(r.type)} · ${text(r.hostname) || text(r.uri)}`
    }),
    systemCA: text(validation.wellKnownCACertificates),
    options: object(spec.options),
    targets: (Array.isArray(spec.targetRefs)
      ? spec.targetRefs
      : spec.targetRef
        ? [spec.targetRef]
        : []
    )
      .map((raw) => reference(raw, detail.namespace))
      .filter((ref) => targets.has(ref.sourceLabel)),
    caRefs: list(validation.caCertificateRefs)
      .map((raw) => reference(raw, detail.namespace))
      .filter((ref) => caRefs.has(ref.sourceLabel)),
    ancestors: list(status.ancestors).map((raw, index) => {
      const a = object(raw),
        ref = reference(a.ancestorRef, detail.namespace, true),
        controller = text(a.controllerName)
      return {
        key: JSON.stringify([ref.sourceLabel, ref.sectionName, controller, index]),
        ref,
        controller,
        conditions: list(a.conditions).flatMap((raw) => {
          const c = object(raw)
          return text(c.type) && text(c.status)
            ? [
                {
                  type: text(c.type),
                  status: text(c.status),
                  reason: text(c.reason),
                  message: text(c.message),
                  observedGeneration: generation(c.observedGeneration),
                  lastTransitionTime: text(c.lastTransitionTime),
                },
              ]
            : []
        }),
      }
    }),
  }
}
