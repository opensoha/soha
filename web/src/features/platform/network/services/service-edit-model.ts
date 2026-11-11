import type {
  KubernetesManifest,
  ServiceFormValues,
} from '@/features/platform/resource-creation/forms'

type UnknownRecord = Record<string, unknown>

const serviceTypes = new Set<ServiceFormValues['type']>([
  'ClusterIP',
  'NodePort',
  'LoadBalancer',
  'ExternalName',
])
const protocols = new Set<ServiceFormValues['ports'][number]['protocol']>(['TCP', 'UDP', 'SCTP'])

function asRecord(value: unknown, label: string): UnknownRecord {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`${label} must be an object`)
  }
  return value as UnknownRecord
}

function optionalRecord(value: unknown): UnknownRecord {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as UnknownRecord) : {}
}

function requiredString(value: unknown, label: string) {
  const result = typeof value === 'string' ? value.trim() : ''
  if (!result) throw new Error(`${label} is required`)
  return result
}

function recordEntries(value: unknown) {
  return Object.entries(optionalRecord(value))
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, item]) => ({ key, value: String(item ?? '') }))
}

function serviceType(value: unknown): ServiceFormValues['type'] {
  return serviceTypes.has(value as ServiceFormValues['type'])
    ? (value as ServiceFormValues['type'])
    : 'ClusterIP'
}

function serviceProtocol(value: unknown): ServiceFormValues['ports'][number]['protocol'] {
  const normalized = typeof value === 'string' ? value.toUpperCase() : 'TCP'
  return protocols.has(normalized as ServiceFormValues['ports'][number]['protocol'])
    ? (normalized as ServiceFormValues['ports'][number]['protocol'])
    : 'TCP'
}

function servicePort(value: unknown, index: number): ServiceFormValues['ports'][number] {
  const port = asRecord(value, `spec.ports[${index}]`)
  if (typeof port.port !== 'number' || !Number.isInteger(port.port)) {
    throw new Error(`spec.ports[${index}].port must be an integer`)
  }
  const targetPort =
    typeof port.targetPort === 'string' || typeof port.targetPort === 'number'
      ? port.targetPort
      : port.port
  return {
    ...(typeof port.name === 'string' && port.name.trim() ? { name: port.name } : {}),
    ...(typeof port.nodePort === 'number' && port.nodePort > 0 ? { nodePort: port.nodePort } : {}),
    port: port.port,
    protocol: serviceProtocol(port.protocol),
    targetPort,
  }
}

export function serviceFormValuesFromManifest(manifest: unknown): ServiceFormValues {
  const resource = asRecord(manifest, 'Service manifest')
  if (resource.kind && resource.kind !== 'Service') {
    throw new Error('YAML kind must be Service')
  }
  const metadata = asRecord(resource.metadata, 'metadata')
  const spec = optionalRecord(resource.spec)
  const type = serviceType(spec.type)

  return {
    annotations: recordEntries(metadata.annotations),
    labels: recordEntries(metadata.labels),
    name: requiredString(metadata.name, 'metadata.name'),
    namespace: requiredString(metadata.namespace, 'metadata.namespace'),
    type,
    ...(typeof spec.externalName === 'string' ? { externalName: spec.externalName } : {}),
    ports:
      type === 'ExternalName' ? [] : Array.isArray(spec.ports) ? spec.ports.map(servicePort) : [],
    selector: recordEntries(spec.selector),
  }
}

function replaceMetadataMap(
  target: UnknownRecord,
  source: KubernetesManifest['metadata'],
  key: 'annotations' | 'labels',
) {
  if (source[key]) target[key] = source[key]
  else delete target[key]
}

const loadBalancerFields = [
  'allocateLoadBalancerNodePorts',
  'healthCheckNodePort',
  'loadBalancerClass',
  'loadBalancerIP',
  'loadBalancerSourceRanges',
]

export function mergeServiceManifest(source: unknown, edited: KubernetesManifest): UnknownRecord {
  const resource = asRecord(source, 'Service manifest')
  if (resource.kind && resource.kind !== 'Service') {
    throw new Error('YAML kind must be Service')
  }
  const sourceMetadata = asRecord(resource.metadata, 'metadata')
  const editedMetadata = asRecord(edited.metadata, 'edited metadata')
  const sourceSpec = optionalRecord(resource.spec)
  const editedSpec = asRecord(edited.spec, 'edited spec')
  const previousType = serviceType(sourceSpec.type)
  const nextType = serviceType(editedSpec.type)
  const metadata = { ...sourceMetadata }

  metadata.name = sourceMetadata.name || editedMetadata.name
  metadata.namespace = sourceMetadata.namespace || editedMetadata.namespace
  replaceMetadataMap(metadata, edited.metadata, 'labels')
  replaceMetadataMap(metadata, edited.metadata, 'annotations')

  let spec: UnknownRecord
  if (nextType === 'ExternalName' && previousType !== 'ExternalName') {
    spec = { type: nextType, externalName: editedSpec.externalName }
  } else {
    spec = { ...sourceSpec }
    for (const key of ['externalName', 'ports', 'selector', 'type']) delete spec[key]
    Object.assign(spec, editedSpec)
    if (Array.isArray(sourceSpec.ports) && Array.isArray(editedSpec.ports)) {
      const previousPorts = sourceSpec.ports.map(optionalRecord)
      const editedPorts = editedSpec.ports.map(optionalRecord)
      spec.ports = editedPorts.map((port) => {
        const sameProtocol = previousPorts.filter(
          (previous) => serviceProtocol(previous.protocol) === serviceProtocol(port.protocol),
        )
        const previousByName =
          typeof port.name === 'string' && port.name
            ? sameProtocol.find((candidate) => candidate.name === port.name)
            : undefined
        const previous =
          previousByName ??
          sameProtocol.find(
            (candidate) =>
              candidate.port === port.port &&
              !editedPorts.some(
                (other) =>
                  candidate.name &&
                  other.name === candidate.name &&
                  serviceProtocol(other.protocol) === serviceProtocol(candidate.protocol),
              ),
          )
        return previous && typeof previous.appProtocol === 'string'
          ? { appProtocol: previous.appProtocol, ...port }
          : port
      })
    }
    if (nextType !== 'ExternalName') delete spec.externalName

    if (nextType !== previousType && nextType !== 'LoadBalancer') {
      for (const key of loadBalancerFields) delete spec[key]
    }
    if (nextType !== previousType && nextType === 'ClusterIP') {
      delete spec.externalTrafficPolicy
    }
  }

  return {
    ...resource,
    apiVersion: resource.apiVersion || edited.apiVersion,
    kind: 'Service',
    metadata,
    spec,
  }
}
