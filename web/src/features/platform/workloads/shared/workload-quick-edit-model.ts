import type { KeyValueEntry } from '@/features/platform/resource-creation/forms'
import type { WorkloadKind } from './types'

type UnknownRecord = Record<string, unknown>

export type QuickEditableWorkloadKind = Extract<
  WorkloadKind,
  'deployments' | 'statefulsets' | 'daemonsets' | 'cronjobs'
>

export interface WorkloadQuickEditValues {
  containerName: string
  image: string
  replicas?: number
  cpuRequest?: string
  cpuLimit?: string
  memoryRequest?: string
  memoryLimit?: string
  env: KeyValueEntry[]
  nodeSelector: KeyValueEntry[]
  serviceAccountName?: string
  schedule?: string
  suspend?: boolean
  concurrencyPolicy?: 'Allow' | 'Forbid' | 'Replace'
  timeZone?: string
  successfulJobsHistoryLimit?: number
  failedJobsHistoryLimit?: number
}

function asRecord(value: unknown): UnknownRecord {
  return value != null && typeof value === 'object' && !Array.isArray(value)
    ? (value as UnknownRecord)
    : {}
}

function ensureRecord(parent: UnknownRecord, key: string) {
  const current = asRecord(parent[key])
  parent[key] = current
  return current
}

function recordEntries(value: unknown): KeyValueEntry[] {
  return Object.entries(asRecord(value)).map(([key, item]) => ({
    key,
    value: item == null ? '' : String(item),
  }))
}

function entriesRecord(entries: KeyValueEntry[] | undefined) {
  return Object.fromEntries(
    (entries ?? [])
      .map((item) => [item.key.trim(), item.value] as const)
      .filter(([key]) => Boolean(key)),
  )
}

function controllerSpec(root: UnknownRecord) {
  return ensureRecord(root, 'spec')
}

function podSpec(root: UnknownRecord, kind: QuickEditableWorkloadKind) {
  const spec = controllerSpec(root)
  if (kind === 'cronjobs') {
    const jobTemplate = ensureRecord(spec, 'jobTemplate')
    const jobSpec = ensureRecord(jobTemplate, 'spec')
    return ensureRecord(ensureRecord(jobSpec, 'template'), 'spec')
  }
  return ensureRecord(ensureRecord(spec, 'template'), 'spec')
}

function containers(root: UnknownRecord, kind: QuickEditableWorkloadKind) {
  const value = podSpec(root, kind).containers
  return Array.isArray(value) ? value.map(asRecord) : []
}

function selectedContainer(
  root: UnknownRecord,
  kind: QuickEditableWorkloadKind,
  containerName?: string,
) {
  const items = containers(root, kind)
  return items.find((item) => item.name === containerName) ?? items[0]
}

function resourceValue(container: UnknownRecord, group: 'requests' | 'limits', key: string) {
  const value = asRecord(asRecord(container.resources)[group])[key]
  return value == null ? undefined : String(value)
}

function literalEnvironment(container: UnknownRecord): KeyValueEntry[] {
  const env = Array.isArray(container.env) ? container.env : []
  return env
    .map(asRecord)
    .filter((item) => typeof item.name === 'string' && 'value' in item)
    .map((item) => ({
      key: String(item.name),
      value: item.value == null ? '' : String(item.value),
    }))
}

export function workloadContainerNames(manifest: unknown, kind: QuickEditableWorkloadKind) {
  return containers(asRecord(manifest), kind)
    .map((item) => (typeof item.name === 'string' ? item.name : ''))
    .filter(Boolean)
}

export function workloadQuickEditValuesFromManifest(
  manifest: unknown,
  kind: QuickEditableWorkloadKind,
  containerName?: string,
): WorkloadQuickEditValues {
  const root = asRecord(manifest)
  const spec = controllerSpec(root)
  const runtime = podSpec(root, kind)
  const container = selectedContainer(root, kind, containerName)
  if (!container || typeof container.name !== 'string') {
    throw new Error('The workload does not expose an editable container')
  }
  return {
    containerName: container.name,
    image: typeof container.image === 'string' ? container.image : '',
    replicas:
      kind === 'daemonsets' || kind === 'cronjobs'
        ? undefined
        : typeof spec.replicas === 'number'
          ? spec.replicas
          : 1,
    cpuRequest: resourceValue(container, 'requests', 'cpu'),
    cpuLimit: resourceValue(container, 'limits', 'cpu'),
    memoryRequest: resourceValue(container, 'requests', 'memory'),
    memoryLimit: resourceValue(container, 'limits', 'memory'),
    env: literalEnvironment(container),
    nodeSelector: recordEntries(runtime.nodeSelector),
    serviceAccountName:
      typeof runtime.serviceAccountName === 'string' ? runtime.serviceAccountName : undefined,
    schedule: kind === 'cronjobs' && typeof spec.schedule === 'string' ? spec.schedule : undefined,
    suspend: kind === 'cronjobs' ? Boolean(spec.suspend) : undefined,
    concurrencyPolicy:
      kind === 'cronjobs' && ['Allow', 'Forbid', 'Replace'].includes(String(spec.concurrencyPolicy))
        ? (spec.concurrencyPolicy as WorkloadQuickEditValues['concurrencyPolicy'])
        : kind === 'cronjobs'
          ? 'Allow'
          : undefined,
    timeZone: kind === 'cronjobs' && typeof spec.timeZone === 'string' ? spec.timeZone : undefined,
    successfulJobsHistoryLimit:
      kind === 'cronjobs' && typeof spec.successfulJobsHistoryLimit === 'number'
        ? spec.successfulJobsHistoryLimit
        : undefined,
    failedJobsHistoryLimit:
      kind === 'cronjobs' && typeof spec.failedJobsHistoryLimit === 'number'
        ? spec.failedJobsHistoryLimit
        : undefined,
  }
}

function setOptionalString(record: UnknownRecord, key: string, value: string | undefined) {
  const normalized = value?.trim()
  if (normalized) record[key] = normalized
  else delete record[key]
}

function setOptionalNumber(record: UnknownRecord, key: string, value: number | undefined) {
  if (value == null || !Number.isFinite(value)) delete record[key]
  else record[key] = value
}

function setResourceValue(
  container: UnknownRecord,
  group: 'requests' | 'limits',
  key: string,
  value: string | undefined,
) {
  const resources = ensureRecord(container, 'resources')
  const values = ensureRecord(resources, group)
  setOptionalString(values, key, value)
  if (Object.keys(values).length === 0) delete resources[group]
  if (Object.keys(resources).length === 0) delete container.resources
}

function mergeLiteralEnvironment(container: UnknownRecord, entries: KeyValueEntry[]) {
  const existing = Array.isArray(container.env) ? container.env.map(asRecord) : []
  const complex = existing.filter((item) => item.valueFrom != null)
  const literals = (entries ?? [])
    .filter((item) => item.key.trim())
    .map((item) => ({ name: item.key.trim(), value: item.value }))
  const merged = [...complex, ...literals]
  if (merged.length > 0) container.env = merged
  else delete container.env
}

export function mergeWorkloadQuickEditManifest(
  manifest: unknown,
  kind: QuickEditableWorkloadKind,
  values: WorkloadQuickEditValues,
) {
  const cloned = JSON.parse(JSON.stringify(asRecord(manifest))) as UnknownRecord
  const spec = controllerSpec(cloned)
  const runtime = podSpec(cloned, kind)
  const container = selectedContainer(cloned, kind, values.containerName)
  if (!container) throw new Error(`Container ${values.containerName} no longer exists`)

  if (kind !== 'daemonsets' && kind !== 'cronjobs') {
    setOptionalNumber(spec, 'replicas', values.replicas)
  }
  setOptionalString(container, 'image', values.image)
  setResourceValue(container, 'requests', 'cpu', values.cpuRequest)
  setResourceValue(container, 'limits', 'cpu', values.cpuLimit)
  setResourceValue(container, 'requests', 'memory', values.memoryRequest)
  setResourceValue(container, 'limits', 'memory', values.memoryLimit)
  mergeLiteralEnvironment(container, values.env)
  setOptionalString(runtime, 'serviceAccountName', values.serviceAccountName)
  const nextNodeSelector = entriesRecord(values.nodeSelector)
  if (Object.keys(nextNodeSelector).length > 0) runtime.nodeSelector = nextNodeSelector
  else delete runtime.nodeSelector

  if (kind === 'cronjobs') {
    setOptionalString(spec, 'schedule', values.schedule)
    spec.suspend = Boolean(values.suspend)
    setOptionalString(spec, 'concurrencyPolicy', values.concurrencyPolicy)
    setOptionalString(spec, 'timeZone', values.timeZone)
    setOptionalNumber(spec, 'successfulJobsHistoryLimit', values.successfulJobsHistoryLimit)
    setOptionalNumber(spec, 'failedJobsHistoryLimit', values.failedJobsHistoryLimit)
  }
  return cloned
}
