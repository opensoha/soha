import {
  emptyPayloadMap,
  isObservabilityPayloadMap,
  parseObservabilityJson,
  toText,
} from '../shared/json'
import type {
  AlertRule,
  AlertRuleDatasourceSelector,
  AlertRuleFormValues,
  AlertRulePayload,
} from './types'

const simpleOperators = new Set(['gt', 'gte', 'lt', 'lte', 'eq'])
const simpleReducers = new Set(['latest', 'average', 'max', 'min', 'sum', 'count'])
const metricLabels: Record<string, string> = {
  cpu_usage: 'CPU 使用率',
  memory_usage: '内存使用率',
  restart_rate: '重启次数',
  error_rate: '错误率',
  latency_p95: 'P95 延迟',
}
const reducerLabels: Record<string, string> = {
  latest: '最新值',
  average: '平均值',
  max: '最大值',
  min: '最小值',
  sum: '总和',
  count: '样本数',
}
const operatorLabels: Record<string, string> = {
  gt: '大于',
  gte: '大于等于',
  lt: '小于',
  lte: '小于等于',
  eq: '等于',
}
const severityLabels: Record<string, string> = {
  critical: '严重',
  warning: '警告',
  info: '提示',
}

function payloadMapFromField(value: unknown) {
  if (typeof value === 'string') return parseObservabilityJson(value, emptyPayloadMap())
  if (isObservabilityPayloadMap(value)) return value
  return emptyPayloadMap()
}

function stringListFromField(value: unknown) {
  if (Array.isArray(value)) {
    return value.map((item) => String(item).trim()).filter(Boolean)
  }
  if (typeof value === 'string') {
    return value
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean)
  }
  return []
}

export function prettyObservabilityJson(value: unknown) {
  if (value == null) return ''
  return JSON.stringify(value, null, 2)
}

export function alertRuleConditionSummary(values: Partial<AlertRuleFormValues>) {
  const metricKey = toText(values.metricKey || 'cpu_usage')
  const reducer = toText(values.reducer || 'latest')
  const operator = toText(values.operator || 'gt')
  const severity = toText(values.severity || 'warning')
  const threshold = Number(values.thresholdValue ?? 80)
  const windowMinutes = Number(values.windowMinutes ?? 60)
  const forSeconds = Number(values.forSeconds ?? 60)
  const scope = [
    ['集群', values.clusterId],
    ['命名空间', values.namespace],
    ['工作负载', values.workload],
  ]
    .map(([label, value]) => [label, toText(value).trim()] as const)
    .filter(([, value]) => value)
    .map(([label, value]) => `${label} ${value}`)
  const duration = Number.isFinite(forSeconds) && forSeconds > 0
    ? `条件持续 ${forSeconds} 秒后`
    : '条件成立后立即'
  const summary = `最近 ${Number.isFinite(windowMinutes) ? windowMinutes : 60} 分钟内，${metricLabels[metricKey] || metricKey || '指标'}的${reducerLabels[reducer] || reducer || '取值'}${operatorLabels[operator] || operator || '满足条件'} ${Number.isFinite(threshold) ? threshold : 80}；${duration}触发${severityLabels[severity] || severity || '警告'}告警。`
  return scope.length > 0 ? `${summary}范围：${scope.join('、')}。` : summary
}

export function alertRuleDashboardDraft(params: URLSearchParams): AlertRuleFormValues | null {
  if (params.get('create') !== 'dashboard-panel') return null
  const query = (params.get('query') ?? '').trim().slice(0, 8192)
  if (!query) return null
  const dataSourceId = (params.get('dataSourceId') ?? '').trim().slice(0, 200)
  const windowMinutes = boundedNumber(params.get('windowMinutes'), 60, 1, 1440)
  const stepSeconds = boundedNumber(params.get('stepSeconds'), 60, 1, 3600)
  const datasourceSelector: AlertRuleDatasourceSelector = { sourceKind: 'metrics' }
  if (dataSourceId) datasourceSelector.datasourceIds = [dataSourceId]
  return {
    ...alertRuleFormValues(null),
    name: (params.get('name') ?? 'Dashboard 指标告警').trim().slice(0, 200),
    mode: 'advanced',
    ruleType: 'metrics',
    datasourceSelector: prettyObservabilityJson(datasourceSelector),
    querySpec: prettyObservabilityJson({ query, windowMinutes, stepSeconds }),
    thresholdSpec: prettyObservabilityJson({ operator: 'gt', reducer: 'latest', value: 0 }),
    labels: prettyObservabilityJson({ severity: 'warning' }),
    annotations: prettyObservabilityJson({ summary: 'Dashboard 面板阈值告警' }),
  }
}

function boundedNumber(value: string | null, fallback: number, minimum: number, maximum: number) {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? Math.min(maximum, Math.max(minimum, parsed)) : fallback
}

function mapText(value: unknown, key: string) {
  if (!isObservabilityPayloadMap(value)) return ''
  return typeof value[key] === 'string' ? value[key] : ''
}

function mapNumber(value: unknown, key: string, fallback: number) {
  if (!isObservabilityPayloadMap(value)) return fallback
  const result = Number(value[key])
  return Number.isFinite(result) ? result : fallback
}

function hasOnlyKeys(value: unknown, allowed: string[]) {
  return (
    !isObservabilityPayloadMap(value) || Object.keys(value).every((key) => allowed.includes(key))
  )
}

export function alertRuleFormValues(rule?: AlertRule | null): AlertRuleFormValues {
  const isSimple =
    !rule ||
    (rule.ruleType === 'metrics' &&
      hasOnlyKeys(rule.datasourceSelector, ['sourceKind', 'clusterId', 'namespace', 'workload']) &&
      hasOnlyKeys(rule.querySpec, ['metricKey', 'windowMinutes', 'stepSeconds']) &&
      hasOnlyKeys(rule.thresholdSpec, ['operator', 'reducer', 'value']) &&
      hasOnlyKeys(rule.labels, ['severity']) &&
      hasOnlyKeys(rule.annotations, ['summary']) &&
      (rule.groupBy ?? []).every((key) => ['clusterId', 'namespace', 'workload'].includes(key)) &&
      simpleOperators.has(mapText(rule.thresholdSpec, 'operator')) &&
      simpleReducers.has(mapText(rule.thresholdSpec, 'reducer')) &&
      Number.isFinite(Number(rule.thresholdSpec?.value)))
  const defaults = rule ?? {
    id: '',
    name: '',
    ruleType: 'metrics',
    datasourceSelector: {},
    querySpec: { metricKey: 'cpu_usage', windowMinutes: 60, stepSeconds: 60 },
    thresholdSpec: { operator: 'gt', reducer: 'latest', value: 80 },
    forSeconds: 60,
    groupBy: [],
    labels: {},
    annotations: {},
    notificationPolicyId: '',
    healingPolicyIds: [],
    enabled: true,
    createdAt: '',
    updatedAt: '',
  }
  return {
    name: defaults.name,
    mode: isSimple ? 'simple' : 'advanced',
    ruleType: defaults.ruleType,
    datasourceSelector: prettyObservabilityJson(defaults.datasourceSelector),
    querySpec: prettyObservabilityJson(defaults.querySpec),
    thresholdSpec: prettyObservabilityJson(defaults.thresholdSpec),
    metricKey: mapText(defaults.querySpec, 'metricKey') || 'cpu_usage',
    operator: mapText(defaults.thresholdSpec, 'operator') || 'gt',
    thresholdValue: mapNumber(defaults.thresholdSpec, 'value', 80),
    reducer: mapText(defaults.thresholdSpec, 'reducer') || 'latest',
    windowMinutes: mapNumber(defaults.querySpec, 'windowMinutes', 60),
    stepSeconds: mapNumber(defaults.querySpec, 'stepSeconds', 60),
    clusterId: mapText(defaults.datasourceSelector, 'clusterId'),
    namespace: mapText(defaults.datasourceSelector, 'namespace'),
    workload: mapText(defaults.datasourceSelector, 'workload'),
    severity: mapText(defaults.labels, 'severity') || 'warning',
    summary: mapText(defaults.annotations, 'summary'),
    forSeconds: defaults.forSeconds,
    groupBy: (defaults.groupBy ?? []).join(', '),
    labels: prettyObservabilityJson(defaults.labels),
    annotations: prettyObservabilityJson(defaults.annotations),
    notificationPolicyId: defaults.notificationPolicyId,
    healingPolicyIds: defaults.healingPolicyIds ?? [],
    enabled: defaults.enabled,
  }
}

export function buildAlertRulePayload(
  values: Partial<AlertRuleFormValues> | Partial<AlertRule>,
): AlertRulePayload {
  if ('mode' in values && values.mode === 'simple') {
    const datasourceSelector: AlertRuleDatasourceSelector = { sourceKind: 'metrics' }
    for (const key of ['clusterId', 'namespace', 'workload'] as const) {
      const value = toText(values[key]).trim()
      if (value) datasourceSelector[key] = value
    }
    const summary = toText(values.summary).trim()
    return {
      id: typeof values.id === 'string' ? values.id : undefined,
      name: toText(values.name).trim(),
      ruleType: 'metrics',
      datasourceSelector,
      querySpec: {
        metricKey: toText(values.metricKey || 'cpu_usage'),
        windowMinutes: Number(values.windowMinutes ?? 60),
        stepSeconds: Number(values.stepSeconds ?? 60),
      },
      thresholdSpec: {
        operator: toText(values.operator || 'gt'),
        reducer: toText(values.reducer || 'latest'),
        value: Number(values.thresholdValue ?? 0),
      },
      forSeconds: Number(values.forSeconds ?? 0),
      groupBy: ['clusterId', 'namespace', 'workload'].filter((key) =>
        Boolean(datasourceSelector[key]),
      ),
      labels: { severity: toText(values.severity || 'warning') },
      annotations: summary ? { summary } : {},
      notificationPolicyId: toText(values.notificationPolicyId),
      healingPolicyIds: stringListFromField(values.healingPolicyIds),
      enabled: Boolean(values.enabled),
    }
  }
  return {
    id: typeof values.id === 'string' ? values.id : undefined,
    name: toText(values.name),
    ruleType: toText(values.ruleType || 'metrics'),
    datasourceSelector: payloadMapFromField(values.datasourceSelector),
    querySpec: payloadMapFromField(values.querySpec),
    thresholdSpec: payloadMapFromField(values.thresholdSpec),
    forSeconds: Number(values.forSeconds ?? 0),
    groupBy: stringListFromField(values.groupBy),
    labels: payloadMapFromField(values.labels),
    annotations: payloadMapFromField(values.annotations),
    notificationPolicyId: toText(values.notificationPolicyId),
    healingPolicyIds: stringListFromField(values.healingPolicyIds),
    enabled: Boolean(values.enabled),
  }
}
