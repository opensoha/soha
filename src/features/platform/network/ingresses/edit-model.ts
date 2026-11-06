import { array, object, text, ingressBackend, readIngressManifest } from './configuration'
import type { KeyValueEntry } from '@/features/platform/resource-creation/forms'
export interface IngressEditPath {
  sourceIndex?: number
  path: string
  pathType: string
  serviceName: string
  servicePort: string
  resourceLabel?: string
}
export interface IngressEditRule {
  sourceIndex?: number
  host: string
  paths: IngressEditPath[]
}
export interface IngressEditValues {
  className: string
  rules: IngressEditRule[]
  tls: { sourceIndex?: number; hosts: string; secretName: string }[]
  defaultEnabled: boolean
  defaultBackend: IngressEditPath
  labels: KeyValueEntry[]
  annotations: KeyValueEntry[]
}
const entries = (v: unknown) =>
  Object.entries(object(v)).map(([key, value]) => ({ key, value: String(value) }))
const pathValues = (v: unknown, sourceIndex?: number): IngressEditPath => {
  const p = object(v),
    ref = ingressBackend(p.backend)
  return {
    sourceIndex,
    path: text(p.path),
    pathType: text(p.pathType) || 'Prefix',
    serviceName: ref.serviceName || '',
    servicePort: ref.servicePort || '',
    resourceLabel: ref.resource
      ? `${ref.resource.apiGroup}/${ref.resource.kind}/${ref.resource.name}`
      : undefined,
  }
}
export function ingressEditValues(manifest: Record<string, unknown>): IngressEditValues {
  const spec = object(manifest.spec),
    metadata = object(manifest.metadata)
  return {
    className: text(spec.ingressClassName),
    labels: entries(metadata.labels),
    annotations: entries(metadata.annotations),
    rules: array(spec.rules).map((r, i) => ({
      sourceIndex: i,
      host: text(object(r).host),
      paths: array(object(object(r).http).paths).map((p, j) => pathValues(p, j)),
    })),
    tls: array(spec.tls).map((t, i) => ({
      sourceIndex: i,
      hosts: array(object(t).hosts).join(', '),
      secretName: text(object(t).secretName),
    })),
    defaultEnabled: Boolean(spec.defaultBackend),
    defaultBackend: pathValues({ backend: spec.defaultBackend }),
  }
}
const keyValues = (entries: KeyValueEntry[]) =>
  Object.fromEntries(entries.filter((e) => e.key.trim()).map((e) => [e.key.trim(), e.value]))
function backend(original: unknown, value: IngressEditPath) {
  const prev = object(original)
  if (value.resourceLabel) {
    if (!prev.resource) throw new Error('Resource backend cannot be reconstructed')
    return prev
  }
  if (!value.serviceName.trim() || !value.servicePort.trim())
    throw new Error('Service name and port are required')
  const port = value.servicePort.trim(),
    numeric = /^\d+$/.test(port)
  if (numeric && (Number(port) < 1 || Number(port) > 65535))
    throw new Error('Service port must be 1–65535')
  const preservedPort = { ...object(object(prev.service).port) }
  delete preservedPort.name
  delete preservedPort.number
  const result = {
    ...prev,
    service: {
      ...object(prev.service),
      name: value.serviceName.trim(),
      port: numeric ? { ...preservedPort, number: Number(port) } : { ...preservedPort, name: port },
    },
  }
  delete (result as Record<string, unknown>).resource
  return result
}
export function mergeIngressEdit(
  content: string,
  name: string,
  namespace: string,
  values: IngressEditValues,
) {
  const result = structuredClone(readIngressManifest(content, name, namespace)),
    metadata = object(result.metadata),
    spec = object(result.spec)
  result.metadata = {
    ...metadata,
    name,
    namespace,
    labels: keyValues(values.labels),
    annotations: keyValues(values.annotations),
  }
  if (!values.labels.length) delete object(result.metadata).labels
  if (!values.annotations.length) delete object(result.metadata).annotations
  if (values.className.trim()) spec.ingressClassName = values.className.trim()
  else delete spec.ingressClassName
  const originalRules = array(spec.rules)
  spec.rules = values.rules.map((rule) => {
    const old = object(
        rule.sourceIndex === undefined ? undefined : originalRules[rule.sourceIndex],
      ),
      http = object(old.http),
      paths = array(http.paths)
    const next = {
      ...old,
      http: {
        ...http,
        paths: rule.paths.map((path) => {
          const oldPath = object(
            path.sourceIndex === undefined ? undefined : paths[path.sourceIndex],
          )
          return {
            ...oldPath,
            path: path.path,
            pathType: path.pathType,
            backend: backend(oldPath.backend, path),
          }
        }),
      },
    } as Record<string, unknown>
    if (!old.http && !rule.paths.length) delete next.http
    if (rule.host.trim()) next.host = rule.host.trim()
    else delete next.host
    return next
  })
  if (!values.rules.length) delete spec.rules
  const originalTls = array(spec.tls)
  spec.tls = values.tls.map((t) => {
    const next = {
      ...object(t.sourceIndex === undefined ? undefined : originalTls[t.sourceIndex]),
      hosts: t.hosts.split(/[\s,]+/).filter(Boolean),
    }
    if (
      !next.hosts.length &&
      !Object.prototype.hasOwnProperty.call(
        object(t.sourceIndex === undefined ? undefined : originalTls[t.sourceIndex]),
        'hosts',
      )
    )
      delete (next as Record<string, unknown>).hosts
    if (t.secretName.trim()) (next as Record<string, unknown>).secretName = t.secretName.trim()
    else delete (next as Record<string, unknown>).secretName
    return next
  })
  if (!values.tls.length) delete spec.tls
  if (values.defaultEnabled)
    spec.defaultBackend = backend(spec.defaultBackend, values.defaultBackend)
  else delete spec.defaultBackend
  result.spec = spec
  return result
}
