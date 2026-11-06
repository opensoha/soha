import { parseDocument } from 'yaml'

const object = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
const text = (value: unknown) => (typeof value === 'string' ? value : undefined)
const number = (value: unknown) =>
  typeof value === 'number' && Number.isFinite(value) ? value : undefined
const objects = (value: unknown) => (Array.isArray(value) ? value.map(object) : [])
const strings = (value: unknown) =>
  Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : []
function document(content: string) {
  if (!content.trim() || new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('StatefulSet YAML is unavailable or exceeds the size limit')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid StatefulSet YAML')
  return object(doc.toJS({ maxAliasCount: 100 }))
}
export interface StatefulSetConfiguration {
  uid: string
  generation?: number
  observedGeneration?: number
  updatedReplicas?: number
  availableReplicas?: number
  strategy: string
  partition?: number
  ordinalStart: number
  podManagementPolicy: string
  serviceName: string
  revisionHistoryLimit?: number
  currentRevision?: string
  updateRevision?: string
  retention: { whenDeleted: string; whenScaled: string }
  containers: { name: string; image: string; init: boolean }[]
  claims: {
    name: string
    requested?: string
    storageClass?: string
    accessModes: string[]
    volumeMode?: string
  }[]
  referencedClaims: string[]
}
export function readStatefulSetConfiguration(
  content: string,
  name: string,
  namespace: string,
): StatefulSetConfiguration {
  const value = document(content),
    meta = object(value.metadata),
    spec = object(value.spec),
    status = object(value.status)
  if (
    value.kind !== 'StatefulSet' ||
    meta.name !== name ||
    meta.namespace !== namespace ||
    !value.spec
  )
    throw new Error('StatefulSet YAML identity does not match the selected resource')
  const strategy = object(spec.updateStrategy),
    retention = object(spec.persistentVolumeClaimRetentionPolicy),
    template = object(object(spec.template).spec)
  return {
    uid: text(meta.uid) || '',
    generation: number(meta.generation),
    observedGeneration: number(status.observedGeneration),
    updatedReplicas: number(status.updatedReplicas),
    availableReplicas: number(status.availableReplicas),
    strategy: text(strategy.type) || 'RollingUpdate',
    partition: number(object(strategy.rollingUpdate).partition),
    ordinalStart: number(object(spec.ordinals).start) ?? 0,
    podManagementPolicy: text(spec.podManagementPolicy) || 'OrderedReady',
    serviceName: text(spec.serviceName) || '',
    revisionHistoryLimit: number(spec.revisionHistoryLimit),
    currentRevision: text(status.currentRevision),
    updateRevision: text(status.updateRevision),
    retention: {
      whenDeleted: text(retention.whenDeleted) || 'Retain',
      whenScaled: text(retention.whenScaled) || 'Retain',
    },
    containers: [
      ...objects(template.initContainers).map((v) => ({
        name: text(v.name) || '—',
        image: text(v.image) || '—',
        init: true,
      })),
      ...objects(template.containers).map((v) => ({
        name: text(v.name) || '—',
        image: text(v.image) || '—',
        init: false,
      })),
    ],
    claims: objects(spec.volumeClaimTemplates).map((claim) => {
      const s = object(claim.spec)
      return {
        name: text(object(claim.metadata).name) || '—',
        requested: text(object(object(s.resources).requests).storage),
        storageClass: text(s.storageClassName),
        accessModes: strings(s.accessModes),
        volumeMode: text(s.volumeMode),
      }
    }),
    referencedClaims: objects(template.volumes)
      .map((v) => text(object(v.persistentVolumeClaim).claimName))
      .filter((v): v is string => Boolean(v)),
  }
}
export { readControllerRevision as readStatefulSetRevision } from '../shared/controller-revision'
