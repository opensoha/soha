import { parseDocument } from 'yaml'
import type { WorkloadCondition } from '@/types'
import type { HorizontalPodAutoscalerResource } from './types'

type RecordValue = Record<string, unknown>
export const object = (value: unknown): RecordValue =>
  value && typeof value === 'object' && !Array.isArray(value) ? (value as RecordValue) : {}
const array = (value: unknown): unknown[] => (Array.isArray(value) ? value : [])
export const text = (value: unknown): string | undefined =>
  typeof value === 'string'
    ? value
    : typeof value === 'number' && Number.isFinite(value)
      ? String(value)
      : undefined
export const count = (value: unknown): number | undefined =>
  typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? value : undefined
const canonical = (value: unknown): unknown =>
  Array.isArray(value)
    ? value.map(canonical)
    : value && typeof value === 'object'
      ? Object.fromEntries(
          Object.entries(object(value))
            .sort(([a], [b]) => a.localeCompare(b))
            .map(([key, item]) => [key, canonical(item)]),
        )
      : value
const metricBody = (metric: RecordValue) =>
  object(
    metric[
      (
        {
          Resource: 'resource',
          ContainerResource: 'containerResource',
          Pods: 'pods',
          Object: 'object',
          External: 'external',
        } as Record<string, string>
      )[String(metric.type)]
    ],
  )
function identity(metric: RecordValue) {
  const body = metricBody(metric)
  return JSON.stringify(
    canonical({
      type: metric.type,
      name: body.name,
      container: body.container,
      metric: body.metric,
      describedObject: body.describedObject,
    }),
  )
}
export interface HPAMetricRow {
  id: string
  type: string
  name: string
  mode: string
  target?: string
  current?: string
  selector?: string
  raw?: boolean
}
function metricRows(spec: RecordValue, status: RecordValue): HPAMetricRow[] {
  const statuses = array(status.currentMetrics).map(object)
  return array(spec.metrics).map((raw, index) => {
    const metric = object(raw),
      body = metricBody(metric),
      target = object(body.target)
    const matches = statuses.filter((item) => identity(item) === identity(metric))
    const current = matches.length === 1 ? object(metricBody(matches[0]).current) : {}
    const mode = String(target.type || '')
    const field =
      mode === 'Utilization'
        ? 'averageUtilization'
        : mode === 'AverageValue'
          ? 'averageValue'
          : mode === 'Value'
            ? 'value'
            : ''
    const name = text(body.name) || text(object(body.metric).name) || '—'
    const container = text(body.container),
      described = object(body.describedObject)
    return {
      id: identity(metric) + '/' + index,
      type: String(metric.type || 'Unknown'),
      mode,
      name: container
        ? container + '/' + name
        : described.name
          ? String(described.kind) + '/' + String(described.name) + ':' + name
          : name,
      target: field ? text(target[field]) : undefined,
      current: field ? text(current[field]) : undefined,
      selector: object(body.metric).selector
        ? JSON.stringify(canonical(object(body.metric).selector))
        : undefined,
    }
  })
}
export function hpaConfiguration(content: string, detail: HorizontalPodAutoscalerResource) {
  if (new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('HPA read budget exceeded')
  const document = parseDocument(content)
  if (document.errors.length) throw new Error('Invalid HPA YAML')
  const root = object(document.toJS({ maxAliasCount: 50 })),
    metadata = object(root.metadata),
    spec = object(root.spec),
    status = object(root.status)
  if (
    root.kind !== 'HorizontalPodAutoscaler' ||
    !['autoscaling/v1', 'autoscaling/v2'].includes(String(root.apiVersion)) ||
    metadata.name !== detail.name ||
    metadata.namespace !== detail.namespace
  )
    throw new Error('HPA identity does not match the requested resource')
  const target = object(spec.scaleTargetRef)
  if (!text(target.kind) || !text(target.name) || count(spec.maxReplicas) === undefined)
    throw new Error('HPA specification was not returned')
  const metrics = metricRows(spec, status)
  if (root.apiVersion === 'autoscaling/v1' && spec.targetCPUUtilizationPercentage !== undefined)
    metrics.push({
      id: 'cpu',
      type: 'Resource',
      name: 'cpu',
      mode: 'Utilization',
      target: text(spec.targetCPUUtilizationPercentage),
      current: text(status.currentCPUUtilizationPercentage),
    })
  return {
    target: {
      apiVersion: text(target.apiVersion) || '',
      kind: String(target.kind),
      name: String(target.name),
    },
    minReplicas: count(spec.minReplicas) ?? 1,
    minDefaulted: spec.minReplicas == null,
    maxReplicas: count(spec.maxReplicas),
    currentReplicas: count(status.currentReplicas),
    desiredReplicas: count(status.desiredReplicas),
    generation: count(metadata.generation),
    observedGeneration: count(status.observedGeneration),
    lastScaleTime: text(status.lastScaleTime),
    behavior: spec.behavior == null ? undefined : object(spec.behavior),
    conditions: array(status.conditions)
      .map(object)
      .filter((item) => typeof item.type === 'string' && typeof item.status === 'string')
      .map((item) => ({
        type: String(item.type),
        status: String(item.status),
        reason: text(item.reason),
        message: text(item.message),
        lastTransitionTime: text(item.lastTransitionTime),
      })) as WorkloadCondition[],
    metrics,
  }
}
export type HPAConfiguration = ReturnType<typeof hpaConfiguration>
export function hpaStatus(conditions: readonly WorkloadCondition[] = [], zh = true) {
  const is = (type: string, status: string) =>
    conditions.some((c) => c.type === type && c.status === status)
  if (is('AbleToScale', 'False'))
    return { value: 'error', label: zh ? '无法扩缩' : 'Unable to scale' }
  if (is('ScalingActive', 'False'))
    return { value: 'warning', label: zh ? '指标不可用' : 'Metrics unavailable' }
  if (is('ScalingLimited', 'True'))
    return { value: 'warning', label: zh ? '扩缩受限' : 'Scaling limited' }
  if (is('AbleToScale', 'True') && is('ScalingActive', 'True'))
    return { value: 'success', label: zh ? '可扩缩' : 'Scaling active' }
  return { value: 'unknown', label: zh ? '状态未知' : 'Unknown' }
}
export function hpaConditionTone(condition: WorkloadCondition) {
  if (condition.type === 'ScalingLimited')
    return condition.status === 'True'
      ? 'warning'
      : condition.status === 'False'
        ? 'success'
        : 'unknown'
  return condition.status === 'True'
    ? 'success'
    : condition.status === 'False'
      ? 'warning'
      : 'unknown'
}
