import { parseDocument } from 'yaml'

export interface PvSource {
  kind: string
  fields: { label: string; value: string }[]
}
export interface PvNodeRequirement {
  key: string
  operator: string
  values?: string[]
}
export interface PvConfiguration {
  sources: PvSource[]
  storageClassName?: string
  mountOptions?: string[]
  requiredNodeTerms?: {
    matchExpressions: PvNodeRequirement[]
    matchFields: PvNodeRequirement[]
  }[]
  reason?: string
  message?: string
  lastPhaseTransitionTime?: string
}

function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid PV object')
  return value as Record<string, unknown>
}
function text(value: unknown): string | undefined {
  if (value == null) return undefined
  if (typeof value !== 'string') throw new Error('Invalid PV text')
  return value
}
function list(value: unknown): unknown[] {
  if (value == null) return []
  if (!Array.isArray(value)) throw new Error('Invalid PV list')
  return value
}
function texts(value: unknown): string[] {
  return list(value).map((entry) => {
    const result = text(entry)
    if (result === undefined) throw new Error('Invalid PV list entry')
    return result
  })
}
function requirements(value: unknown): PvNodeRequirement[] {
  return list(value).map((entry) => {
    const item = object(entry)
    const key = text(item.key)
    const operator = text(item.operator)
    if (!key || !operator) throw new Error('Invalid PV node requirement')
    return { key, operator, ...(item.values == null ? {} : { values: texts(item.values) }) }
  })
}

// Show non-credential source fields. Secret references are identities, never fetched here.
const sourceFields: Record<string, string[]> = {
  csi: ['driver', 'volumeHandle', 'fsType', 'readOnly'],
  nfs: ['server', 'path', 'readOnly'],
  hostPath: ['path', 'type'],
  local: ['path', 'fsType'],
  awsElasticBlockStore: ['volumeID', 'fsType', 'partition', 'readOnly'],
  gcePersistentDisk: ['pdName', 'fsType', 'partition', 'readOnly'],
  azureDisk: ['diskName', 'diskURI', 'kind', 'cachingMode', 'fsType', 'readOnly'],
  azureFile: ['shareName', 'readOnly', 'secretName', 'secretNamespace'],
  vsphereVolume: ['volumePath', 'fsType', 'storagePolicyName', 'storagePolicyID'],
  cinder: ['volumeID', 'fsType', 'readOnly'],
  iscsi: ['targetPortal', 'iqn', 'lun', 'iscsiInterface', 'fsType', 'readOnly', 'portals'],
  fc: ['targetWWNs', 'lun', 'fsType', 'readOnly', 'wwids'],
  rbd: ['monitors', 'image', 'fsType', 'pool', 'user', 'readOnly'],
  cephfs: ['monitors', 'path', 'user', 'readOnly'],
  flexVolume: ['driver', 'fsType', 'readOnly'],
  glusterfs: ['endpoints', 'endpointsNamespace', 'path', 'readOnly'],
  portworxVolume: ['volumeID', 'fsType', 'readOnly'],
  scaleIO: [
    'gateway',
    'system',
    'protectionDomain',
    'storagePool',
    'storageMode',
    'volumeName',
    'fsType',
    'readOnly',
  ],
  storageos: ['volumeName', 'volumeNamespace', 'fsType', 'readOnly'],
  quobyte: ['registry', 'volume', 'readOnly', 'user', 'group', 'tenant'],
  photonPersistentDisk: ['pdID', 'fsType'],
  flocker: ['datasetName', 'datasetUUID'],
}
function source(kind: string, value: unknown): PvSource {
  const raw = object(value)
  const fields: PvSource['fields'] = []
  for (const label of sourceFields[kind]) {
    const value = raw[label]
    if (value == null) continue
    if (Array.isArray(value)) fields.push({ label, value: texts(value).join(', ') })
    else if (['string', 'number', 'boolean'].includes(typeof value))
      fields.push({ label, value: String(value) })
    else throw new Error('Invalid PV source field')
  }
  if (raw.volumeAttributes != null && kind === 'csi') {
    for (const [key, value] of Object.entries(object(raw.volumeAttributes))) {
      const item = text(value)
      if (item === undefined) throw new Error('Invalid CSI attribute')
      fields.push({ label: `volumeAttributes.${key}`, value: item })
    }
  }
  for (const label of [
    'controllerPublishSecretRef',
    'nodeStageSecretRef',
    'nodePublishSecretRef',
    'controllerExpandSecretRef',
    'nodeExpandSecretRef',
    'secretRef',
  ]) {
    if (raw[label] == null) continue
    const reference = object(raw[label])
    const name = text(reference.name)
    const namespace = text(reference.namespace)
    if (!name) throw new Error('Invalid PV Secret reference')
    fields.push({ label, value: namespace ? `${namespace}/${name}` : name })
  }
  return { kind, fields }
}

/** Supplemental facts must belong to this cluster-scoped PV and stay within the read budget. */
export function pvConfiguration(content: string, name: string): PvConfiguration {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('PV read budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid PV YAML')
  const root = object(doc.toJS({ maxAliasCount: 50 }))
  const metadata = object(root.metadata)
  if (
    root.apiVersion !== 'v1' ||
    root.kind !== 'PersistentVolume' ||
    metadata.name !== name ||
    (metadata.namespace != null && metadata.namespace !== '')
  )
    throw new Error('PV identity does not match the requested resource')
  const spec = root.spec == null ? {} : object(root.spec)
  const status = root.status == null ? {} : object(root.status)
  const affinity = spec.nodeAffinity == null ? undefined : object(spec.nodeAffinity)
  const required = affinity?.required == null ? undefined : object(affinity.required)
  return {
    sources: Object.keys(sourceFields)
      .filter((kind) => spec[kind] != null)
      .map((kind) => source(kind, spec[kind])),
    storageClassName: text(spec.storageClassName),
    mountOptions: spec.mountOptions == null ? undefined : texts(spec.mountOptions),
    requiredNodeTerms:
      required == null
        ? undefined
        : list(required.nodeSelectorTerms).map((entry) => {
            const term = object(entry)
            return {
              matchExpressions: requirements(term.matchExpressions),
              matchFields: requirements(term.matchFields),
            }
          }),
    reason: text(status.reason),
    message: text(status.message),
    lastPhaseTransitionTime: text(status.lastPhaseTransitionTime),
  }
}
