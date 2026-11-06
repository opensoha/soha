import { parseDocument } from 'yaml'
const object = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
const text = (value: unknown) => (typeof value === 'string' ? value : undefined)
const number = (value: unknown) =>
  typeof value === 'number' && Number.isFinite(value) ? value : undefined
const objects = (value: unknown) => (Array.isArray(value) ? value.map(object) : [])
export interface DaemonSetConfiguration {
  uid: string
  generation?: number
  observedGeneration?: number
  misscheduled?: number
  unavailable?: number
  strategy: string
  maxUnavailable?: string | number
  maxSurge?: string | number
  minReadySeconds?: number
  revisionHistoryLimit?: number
  nodeSelector: Record<string, string>
  tolerations: string[]
  containers: { name: string; image: string; init: boolean }[]
  conditions: { type: string; status: string; reason?: string; message?: string }[]
}
export function readDaemonSetConfiguration(
  content: string,
  name: string,
  namespace: string,
): DaemonSetConfiguration {
  if (!content.trim() || new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('DaemonSet YAML is unavailable or exceeds the size limit')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid DaemonSet YAML')
  const value = object(doc.toJS({ maxAliasCount: 100 })),
    meta = object(value.metadata),
    spec = object(value.spec),
    status = object(value.status)
  if (
    value.kind !== 'DaemonSet' ||
    meta.name !== name ||
    meta.namespace !== namespace ||
    !value.spec
  )
    throw new Error('DaemonSet YAML identity does not match the selected resource')
  const strategy = object(spec.updateStrategy),
    rolling = object(strategy.rollingUpdate),
    template = object(object(spec.template).spec)
  const intOrString = (value: unknown) => text(value) ?? number(value)
  return {
    uid: text(meta.uid) || '',
    generation: number(meta.generation),
    observedGeneration: number(status.observedGeneration),
    misscheduled: number(status.numberMisscheduled),
    unavailable: number(status.numberUnavailable),
    strategy: text(strategy.type) || 'RollingUpdate',
    maxUnavailable: intOrString(rolling.maxUnavailable),
    maxSurge: intOrString(rolling.maxSurge),
    minReadySeconds: number(spec.minReadySeconds),
    revisionHistoryLimit: number(spec.revisionHistoryLimit),
    nodeSelector: Object.fromEntries(
      Object.entries(object(template.nodeSelector)).filter(
        (entry): entry is [string, string] => typeof entry[1] === 'string',
      ),
    ),
    tolerations: objects(template.tolerations).map((v) =>
      [
        text(v.key) || '*',
        text(v.operator) || 'Equal',
        text(v.value),
        text(v.effect),
        number(v.tolerationSeconds) !== undefined ? `${number(v.tolerationSeconds)}s` : undefined,
      ]
        .filter(Boolean)
        .join(' · '),
    ),
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
    conditions: objects(status.conditions).map((v) => ({
      type: text(v.type) || '—',
      status: text(v.status) || 'Unknown',
      reason: text(v.reason),
      message: text(v.message),
    })),
  }
}
