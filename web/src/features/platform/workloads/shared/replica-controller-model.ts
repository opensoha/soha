import { parseDocument } from 'yaml'
import type { WorkloadCondition } from '@/types'

const object = (v: unknown): Record<string, unknown> =>
  v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : {}
const text = (v: unknown) => (typeof v === 'string' ? v : undefined)
const number = (v: unknown) =>
  typeof v === 'number' && Number.isFinite(v) && v >= 0 ? v : undefined
const objects = (v: unknown) => (Array.isArray(v) ? v.map(object) : [])
export interface ReplicaControllerConfiguration {
  generation?: number
  observedGeneration?: number
  currentReplicas?: number
  fullyLabeledReplicas?: number
  minReadySeconds?: number
  containers: { name: string; image: string; init: boolean }[]
  conditions: WorkloadCondition[]
}
export function readReplicaControllerConfiguration(
  content: string,
  name: string,
  namespace: string,
  kind: 'ReplicaSet' | 'ReplicationController',
): ReplicaControllerConfiguration {
  if (!content.trim() || new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error(`${kind} YAML is unavailable or exceeds the size limit`)
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error(`Invalid ${kind} YAML`)
  const value = object(doc.toJS({ maxAliasCount: 100 })),
    meta = object(value.metadata),
    spec = object(value.spec),
    status = object(value.status)
  if (
    value.kind !== kind ||
    meta.name !== name ||
    meta.namespace !== namespace ||
    (kind === 'ReplicaSet' && !object(spec.template).spec)
  )
    throw new Error(`${kind} YAML identity does not match the selected resource`)
  const pod = object(object(spec.template).spec)
  return {
    generation: number(meta.generation),
    observedGeneration: number(status.observedGeneration),
    currentReplicas: number(status.replicas),
    fullyLabeledReplicas: number(status.fullyLabeledReplicas),
    minReadySeconds: number(spec.minReadySeconds),
    containers: [true, false].flatMap((init) =>
      objects(init ? pod.initContainers : pod.containers).map((c) => ({
        name: text(c.name) || '—',
        image: text(c.image) || '—',
        init,
      })),
    ),
    conditions: objects(status.conditions).map((c) => ({
      type: text(c.type) || '—',
      status: text(c.status) || 'Unknown',
      reason: text(c.reason),
      message: text(c.message),
      lastTransitionTime: text(c.lastTransitionTime),
    })),
  }
}
export function replicaControllerHealth(
  detail: {
    desiredReplicas?: number
    readyReplicas?: number
    availableReplicas?: number
    currentReplicas?: number
  },
  configuration?: ReplicaControllerConfiguration,
  requireCurrent = false,
) {
  const desired = number(detail.desiredReplicas),
    ready = number(detail.readyReplicas),
    available = number(detail.availableReplicas)
  if (configuration?.conditions.some((c) => c.type === 'ReplicaFailure' && c.status === 'True'))
    return 'failed'
  if (desired === 0) return 'inactive'
  if (
    desired === undefined ||
    ready === undefined ||
    available === undefined ||
    (requireCurrent && number(detail.currentReplicas) === undefined)
  )
    return 'unknown'
  return ready >= desired &&
    available >= desired &&
    (!requireCurrent || number(detail.currentReplicas)! >= desired)
    ? 'healthy'
    : 'warning'
}
