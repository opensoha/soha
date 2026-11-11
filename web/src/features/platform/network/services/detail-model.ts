import { parseDocument } from 'yaml'

function object(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}
function text(value: unknown) {
  return typeof value === 'string' && value.trim() ? value : undefined
}
function strings(value: unknown) {
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : []
}

/** Read-only projection; never use this reduced view to write a Service manifest. */
export function serviceConfiguration(content: string, name: string, namespace: string) {
  if (!content.trim() || new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('Service YAML is empty or exceeds 4 MiB')
  const document = parseDocument(content)
  if (document.errors.length) throw new Error(document.errors[0].message)
  const value = object(document.toJS({ maxAliasCount: 100 }))
  const metadata = object(value.metadata)
  if (
    value.apiVersion !== 'v1' ||
    value.kind !== 'Service' ||
    metadata.name !== name ||
    metadata.namespace !== namespace
  )
    throw new Error('Service YAML identity does not match the current resource')
  const spec = object(value.spec)
  const ingress = object(object(value.status).loadBalancer).ingress
  return {
    headless: spec.clusterIP === 'None',
    clusterIPs: strings(spec.clusterIPs).length
      ? strings(spec.clusterIPs)
      : text(spec.clusterIP)
        ? [String(spec.clusterIP)]
        : [],
    externalName: text(spec.externalName),
    externalIPs: strings(spec.externalIPs),
    loadBalancerAddresses: Array.isArray(ingress)
      ? ingress.flatMap((v) => {
          const entry = object(v)
          return [text(entry.ip), text(entry.hostname)].filter((s): s is string => Boolean(s))
        })
      : [],
    ipFamilies: strings(spec.ipFamilies),
    ipFamilyPolicy: text(spec.ipFamilyPolicy),
    externalTrafficPolicy: text(spec.externalTrafficPolicy),
    internalTrafficPolicy: text(spec.internalTrafficPolicy),
    sessionAffinity: text(spec.sessionAffinity),
    publishNotReadyAddresses:
      typeof spec.publishNotReadyAddresses === 'boolean'
        ? spec.publishNotReadyAddresses
        : undefined,
  }
}
