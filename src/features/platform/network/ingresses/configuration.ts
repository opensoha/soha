import { parseDocument } from 'yaml'

export const object = (v: unknown): Record<string, unknown> =>
  v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : {}
export const text = (v: unknown) => (typeof v === 'string' ? v : '')
export const array = (v: unknown): unknown[] => (Array.isArray(v) ? v : [])
export type IngressBackendRef = {
  serviceName?: string
  servicePort?: string
  resource?: { apiGroup: string; kind: string; name: string }
}
export type ConfiguredIngressRoute = IngressBackendRef & {
  key: string
  host: string
  path: string
  pathType: string
  tls: boolean | undefined
  isDefault: boolean
}
export function ingressBackend(value: unknown): IngressBackendRef {
  const backend = object(value),
    service = object(backend.service),
    port = object(service.port),
    resource = object(backend.resource)
  return Object.keys(resource).length
    ? {
        resource: {
          apiGroup: text(resource.apiGroup),
          kind: text(resource.kind),
          name: text(resource.name),
        },
      }
    : {
        serviceName: text(service.name),
        servicePort:
          text(port.name) || (typeof port.number === 'number' ? String(port.number) : ''),
      }
}
export function readIngressManifest(content: string, name: string, namespace: string) {
  if (!content.trim() || new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('Ingress YAML is empty or exceeds 4 MiB')
  const document = parseDocument(content)
  if (document.errors.length) throw new Error(document.errors[0].message)
  const manifest = object(document.toJS({ maxAliasCount: 100 })),
    metadata = object(manifest.metadata)
  if (
    manifest.apiVersion !== 'networking.k8s.io/v1' ||
    manifest.kind !== 'Ingress' ||
    metadata.name !== name ||
    metadata.namespace !== namespace
  )
    throw new Error('Ingress YAML identity does not match the current resource')
  return manifest
}
export function ingressConfiguration(content: string, name: string, namespace: string) {
  const manifest = readIngressManifest(content, name, namespace),
    spec = object(manifest.spec),
    metadata = object(manifest.metadata)
  const tls = array(spec.tls).map((v) => {
    const e = object(v)
    return {
      hosts: array(e.hosts).filter((h): h is string => typeof h === 'string'),
      secretName: text(e.secretName),
    }
  })
  const routes: ConfiguredIngressRoute[] = array(spec.rules).flatMap((v, i) => {
    const rule = object(v),
      host = text(rule.host)
    return array(object(rule.http).paths).map((v, j) => {
      const path = object(v)
      return {
        key: `${i}/${j}`,
        host,
        path: text(path.path),
        pathType: text(path.pathType),
        tls: tls.some((t) => t.hosts.includes(host))
          ? true
          : tls.some((t) => t.hosts.length === 0)
            ? undefined
            : false,
        isDefault: false,
        ...ingressBackend(path.backend),
      }
    })
  })
  if (spec.defaultBackend)
    routes.push({
      key: 'default',
      host: '',
      path: '',
      pathType: 'Default',
      tls: tls.length ? undefined : false,
      isDefault: true,
      ...ingressBackend(spec.defaultBackend),
    })
  return {
    routes,
    tls,
    createdAt: text(metadata.creationTimestamp),
    className: text(spec.ingressClassName),
    legacyClass: text(object(metadata.annotations)['kubernetes.io/ingress.class']),
  }
}
export function publicIngressURL(route: Pick<ConfiguredIngressRoute, 'host' | 'path' | 'tls'>) {
  if (
    route.tls === undefined ||
    !/^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$/i.test(route.host) ||
    !route.path.startsWith('/') ||
    /[\\^$+?()[\]{}|]/.test(route.path)
  )
    return undefined
  return `${route.tls ? 'https' : 'http'}://${route.host}${route.path}`
}
