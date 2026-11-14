import { parseDocument } from 'yaml'
import type { WorkloadCondition } from '@/types'
import type { PodDisruptionBudgetResource } from './types'
const object = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
const count = (value: unknown) =>
  typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? value : undefined
const text = (value: unknown) => (typeof value === 'string' ? value : undefined)
const limit = (value: unknown) =>
  typeof value === 'string' ? value : count(value) === undefined ? undefined : String(value)
export function pdbConfiguration(content: string, detail: PodDisruptionBudgetResource) {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('PDB read budget exceeded')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid PDB YAML')
  const root = object(doc.toJS({ maxAliasCount: 50 })),
    metadata = object(root.metadata),
    spec = object(root.spec),
    status = object(root.status)
  if (
    root.kind !== 'PodDisruptionBudget' ||
    !['policy/v1', 'policy/v1beta1'].includes(String(root.apiVersion)) ||
    metadata.name !== detail.name ||
    metadata.namespace !== detail.namespace
  )
    throw new Error('PDB identity does not match the requested resource')
  if (spec.selector != null && (typeof spec.selector !== 'object' || Array.isArray(spec.selector)))
    throw new Error('Invalid PDB selector')
  const selector = object(spec.selector),
    labels = object(selector.matchLabels)
  if (
    Object.values(labels).some((value) => typeof value !== 'string') ||
    (selector.matchExpressions !== undefined && !Array.isArray(selector.matchExpressions))
  )
    throw new Error('Invalid PDB selector')
  const terms = Object.entries(labels).map(([key, value]) => key + '=' + String(value))
  for (const raw of (selector.matchExpressions || []) as unknown[]) {
    const term = object(raw)
    if (
      typeof term.key !== 'string' ||
      typeof term.operator !== 'string' ||
      (term.values !== undefined &&
        (!Array.isArray(term.values) || term.values.some((value) => typeof value !== 'string')))
    )
      throw new Error('Invalid PDB selector expression')
    terms.push(
      term.key +
        ' ' +
        term.operator +
        (Array.isArray(term.values) ? ' (' + term.values.join(', ') + ')' : ''),
    )
  }
  const empty = terms.length === 0
  const selectorMode =
    spec.selector == null || (root.apiVersion === 'policy/v1beta1' && empty)
      ? 'none'
      : empty
        ? 'all'
        : 'labels'
  const conditions = (Array.isArray(status.conditions) ? status.conditions : [])
    .map(object)
    .filter((item) => typeof item.type === 'string' && typeof item.status === 'string')
    .map((item) => ({
      type: String(item.type),
      status: String(item.status),
      reason: text(item.reason),
      message: text(item.message),
      lastTransitionTime: text(item.lastTransitionTime),
    })) as WorkloadCondition[]
  const generation = count(metadata.generation),
    observedGeneration = count(status.observedGeneration)
  return {
    apiVersion: String(root.apiVersion),
    selectorMode,
    terms,
    minAvailable: limit(spec.minAvailable),
    maxUnavailable: limit(spec.maxUnavailable),
    unhealthyPodEvictionPolicy: text(spec.unhealthyPodEvictionPolicy) || 'IfHealthyBudget',
    policyDefaulted: spec.unhealthyPodEvictionPolicy == null,
    currentHealthy: count(status.currentHealthy),
    desiredHealthy: count(status.desiredHealthy),
    disruptionsAllowed: count(status.disruptionsAllowed),
    expectedPods: count(status.expectedPods),
    generation,
    observedGeneration,
    observed: generation !== undefined && observedGeneration === generation,
    conditions,
    disruptedPods: Object.entries(object(status.disruptedPods)).map(([name, time]) => ({
      name,
      time: text(time),
    })),
  }
}
export type PDBConfiguration = ReturnType<typeof pdbConfiguration>
export function pdbBudgetStatus(configuration: PDBConfiguration | undefined, zh: boolean) {
  if (!configuration) return { value: 'unknown', label: zh ? '预算待确认' : 'Budget unconfirmed' }
  if (!configuration.observed)
    return { value: 'checking', label: zh ? '等待预算更新' : 'Waiting for budget update' }
  if (configuration.conditions.some((c) => c.reason === 'SyncFailed'))
    return { value: 'warning', label: zh ? '预算计算失败' : 'Budget calculation failed' }
  if (configuration.disruptionsAllowed === undefined)
    return { value: 'unknown', label: zh ? '预算未返回' : 'Budget not returned' }
  return configuration.disruptionsAllowed > 0
    ? { value: 'success', label: zh ? '有可用预算' : 'Budget available' }
    : { value: 'unknown', label: zh ? '无可用预算' : 'No budget available' }
}
