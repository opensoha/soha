import { parseDocument } from 'yaml'

type ObjectValue = Record<string, unknown>
const object = (value: unknown): ObjectValue =>
  value && typeof value === 'object' && !Array.isArray(value) ? (value as ObjectValue) : {}
const objects = (value: unknown) => (Array.isArray(value) ? value.map(object) : [])
const text = (value: unknown) => (typeof value === 'string' ? value : undefined)
const quantity = (value: unknown) =>
  text(value) ?? (typeof value === 'number' && Number.isFinite(value) ? String(value) : undefined)
const integer = (value: unknown) =>
  typeof value === 'number' && Number.isSafeInteger(value) ? value : undefined
const strings = (value: unknown) =>
  Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
const quantities = (value: unknown) =>
  Object.fromEntries(
    Object.entries(object(value)).flatMap(([key, value]) =>
      quantity(value) === undefined ? [] : [[key, quantity(value)!]],
    ),
  )

export interface ContainerReference {
  kind: 'ConfigMap' | 'Secret' | 'Field' | 'Resource' | 'Unknown'
  name?: string
  key?: string
  optional?: boolean
  detail?: string
}
export interface ContainerRuntime {
  type: 'running' | 'waiting' | 'terminated' | 'unknown'
  reason?: string
  message?: string
  exitCode?: number
  signal?: number
  startedAt?: string
  finishedAt?: string
}
export interface ContainerProbe {
  type: string
  target: string
  command?: string[]
  headers: { name: string; value: string }[]
  settings: Record<string, string>
}
export interface ContainerConfiguration {
  name: string
  imagePullPolicy?: string
  command: string[]
  args: string[]
  requests: Record<string, string>
  limits: Record<string, string>
  env: { name: string; value?: string; source?: ContainerReference }[]
  envFrom: { source: ContainerReference; prefix?: string }[]
  ports: { name?: string; port?: number; protocol?: string; hostPort?: number; hostIP?: string }[]
  mounts: {
    name: string
    path: string
    subPath?: string
    subPathExpr?: string
    readOnly: boolean
  }[]
  probes: { name: 'liveness' | 'readiness' | 'startup'; probe: ContainerProbe }[]
  current: ContainerRuntime
  previous: ContainerRuntime
}
export interface PodContainerConfiguration {
  containers: ContainerConfiguration[]
}

function reference(value: unknown): ContainerReference {
  const source = object(value)
  for (const [key, kind] of [
    ['secretKeyRef', 'Secret'],
    ['configMapKeyRef', 'ConfigMap'],
    ['secretRef', 'Secret'],
    ['configMapRef', 'ConfigMap'],
  ] as const) {
    if (!source[key]) continue
    const ref = object(source[key])
    return { kind, name: text(ref.name), key: text(ref.key), optional: ref.optional === true }
  }
  if (source.fieldRef) return { kind: 'Field', detail: text(object(source.fieldRef).fieldPath) }
  if (source.resourceFieldRef) {
    const ref = object(source.resourceFieldRef)
    return {
      kind: 'Resource',
      detail: [
        text(ref.resource),
        ref.containerName && `container=${ref.containerName}`,
        ref.divisor && `divisor=${ref.divisor}`,
      ]
        .filter(Boolean)
        .join(' · '),
    }
  }
  return { kind: 'Unknown' }
}

function runtime(value: unknown): ContainerRuntime {
  const state = object(value)
  for (const type of ['terminated', 'waiting', 'running'] as const) {
    if (!state[type]) continue
    const record = object(state[type])
    return {
      type,
      reason: text(record.reason),
      message: text(record.message),
      exitCode: integer(record.exitCode),
      signal: integer(record.signal),
      startedAt: text(record.startedAt),
      finishedAt: text(record.finishedAt),
    }
  }
  return { type: 'unknown' }
}

function probe(value: unknown): ContainerProbe {
  const record = object(value)
  const settings = quantities(
    Object.fromEntries(
      [
        'initialDelaySeconds',
        'timeoutSeconds',
        'periodSeconds',
        'successThreshold',
        'failureThreshold',
        'terminationGracePeriodSeconds',
      ].map((key) => [key, record[key]]),
    ),
  )
  if (record.httpGet) {
    const http = object(record.httpGet)
    return {
      type: 'HTTP GET',
      target: [text(http.scheme), text(http.host), quantity(http.port), text(http.path)]
        .filter((value) => value !== undefined)
        .join(' · '),
      headers: objects(http.httpHeaders).map((header) => ({
        name: text(header.name) || '—',
        value: text(header.value) || '',
      })),
      settings,
    }
  }
  if (record.exec)
    return {
      type: 'Exec',
      target: '',
      command: strings(object(record.exec).command),
      headers: [],
      settings,
    }
  if (record.tcpSocket) {
    const tcp = object(record.tcpSocket)
    return {
      type: 'TCP',
      target: [text(tcp.host), quantity(tcp.port)].filter((value) => value !== undefined).join(':'),
      headers: [],
      settings,
    }
  }
  if (record.grpc) {
    const grpc = object(record.grpc)
    return {
      type: 'gRPC',
      target: [quantity(grpc.port), text(grpc.service)]
        .filter((value) => value !== undefined)
        .join(' · '),
      headers: [],
      settings,
    }
  }
  return { type: 'Unknown', target: '', headers: [], settings }
}

/** Reads server YAML, never the editable draft or resolved Secret values. */
export function readPodContainerConfiguration(
  content: string,
  name: string,
  namespace: string,
): PodContainerConfiguration {
  if (typeof content !== 'string' || !content.trim())
    throw new Error('Pod container configuration is unavailable')
  if (
    content.length > 4 * 1024 * 1024 ||
    new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024
  )
    throw new Error('Pod YAML exceeds the container detail size limit')
  const document = parseDocument(content)
  if (document.errors.length) throw new Error('Invalid Pod YAML')
  const value = object(document.toJS({ maxAliasCount: 100 }))
  const metadata = object(value.metadata)
  if (value.kind !== 'Pod' || metadata.name !== name || metadata.namespace !== namespace)
    throw new Error('Pod YAML identity does not match the selected Pod')
  const spec = object(value.spec)
  if (!value.spec || typeof value.spec !== 'object')
    throw new Error('Pod container configuration is unavailable')
  const status = object(value.status)
  const statuses = new Map(
    [
      ...objects(status.initContainerStatuses),
      ...objects(status.containerStatuses),
      ...objects(status.ephemeralContainerStatuses),
    ].map((item) => [text(item.name), item]),
  )
  const seen = new Set<string>()
  const containers = [
    ...objects(spec.initContainers),
    ...objects(spec.containers),
    ...objects(spec.ephemeralContainers),
  ].map((item) => {
    const containerName = text(item.name)
    if (!containerName || seen.has(containerName))
      throw new Error('Pod container identity is unavailable or duplicated')
    seen.add(containerName)
    const state = statuses.get(containerName) || {}
    const resources = object(item.resources)
    return {
      name: containerName,
      imagePullPolicy: text(item.imagePullPolicy),
      command: strings(item.command),
      args: strings(item.args),
      requests: quantities(resources.requests),
      limits: quantities(resources.limits),
      env: objects(item.env).map((entry) => ({
        name: text(entry.name) || '—',
        value: text(entry.value),
        source: entry.valueFrom ? reference(entry.valueFrom) : undefined,
      })),
      envFrom: objects(item.envFrom).map((entry) => ({
        source: reference(entry),
        prefix: text(entry.prefix),
      })),
      ports: objects(item.ports).map((port) => ({
        name: text(port.name),
        port: integer(port.containerPort),
        protocol: text(port.protocol),
        hostPort: integer(port.hostPort),
        hostIP: text(port.hostIP),
      })),
      mounts: objects(item.volumeMounts).map((mount) => ({
        name: text(mount.name) || '—',
        path: text(mount.mountPath) || '—',
        subPath: text(mount.subPath),
        subPathExpr: text(mount.subPathExpr),
        readOnly: mount.readOnly === true,
      })),
      probes: (['liveness', 'readiness', 'startup'] as const).flatMap((name) =>
        item[`${name}Probe`] ? [{ name, probe: probe(item[`${name}Probe`]) }] : [],
      ),
      current: runtime(state.state),
      previous: runtime(state.lastState),
    }
  })
  return { containers }
}
