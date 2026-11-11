import type { LogEntry, LogQuery } from '@opensoha/contracts/gen/ts/sohaapi'

export interface LogExplorerPreset {
  dataSourceId?: string | null
  source?: 'kubernetes' | 'docker' | 'delivery'
  clusterId?: string | null
  namespace?: string | null
  dockerProjectId?: string | null
  dockerService?: string | null
  applicationId?: string | null
  environmentId?: string | null
  /** Correlation context only; durable log selectors still use workload/trace fields. */
  service?: string | null
  workloadKind?: string
  workloadName?: string
  podNames?: string[]
  containers?: string[]
  labelSelector?: string
  text?: string
  traceId?: string
  spanId?: string
  from?: string
  to?: string
  sinceSeconds?: number
  tail?: number
  allContainers?: boolean
  previous?: boolean
}

export interface RuntimeLogFilters {
  dataSourceId?: string
  workloadKind?: string
  workloadName?: string
  podNames?: string[]
  containers?: string[]
  labelSelector?: string
  text?: string
  traceId?: string
  spanId?: string
  from?: string
  to?: string
  sinceSeconds?: number
  tail?: number
  allContainers?: boolean
  previous?: boolean
}

const MAX_BROWSER_ENTRIES = 10000
const SOHAQL_MAX_LENGTH = 2048
const SOHAQL_SELECTOR_KEYS = new Set(['workload_kind', 'workload', 'pod', 'container'])

function clean(value?: string | null) {
  return value?.trim() || undefined
}

function cleanList(values?: string[]) {
  const normalized = values?.map((value) => value.trim()).filter(Boolean) ?? []
  return normalized.length > 0 ? [...new Set(normalized)] : undefined
}

function absoluteRange(from?: string | null, to?: string | null) {
  const start = Date.parse(from ?? '')
  const end = Date.parse(to ?? '')
  return Number.isFinite(start) && Number.isFinite(end) && start <= end
    ? [new Date(start).toISOString(), new Date(end).toISOString()]
    : undefined
}

function escapeSohaQLValue(value: string) {
  return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')
}

function unescapeSohaQLValue(value: string) {
  return value.replace(/\\(["\\])/g, '$1')
}

export function buildSohaQLExpression(filters: RuntimeLogFilters) {
  const matchers: string[] = []
  const values: Array<[string, string | undefined]> = [
    ['workload_kind', clean(filters.workloadKind)],
    ['workload', clean(filters.workloadName)],
    ['pod', cleanList(filters.podNames)?.[0]],
    ['container', filters.allContainers ? undefined : cleanList(filters.containers)?.[0]],
  ]
  for (const [key, value] of values) {
    if (value) matchers.push(`${key}="${escapeSohaQLValue(value)}"`)
  }
  const text = clean(filters.text)
  return `{${matchers.join(', ')}}${text ? ` |= "${escapeSohaQLValue(text)}"` : ''}`
}

export function parseSohaQLExpression(expression: string): RuntimeLogFilters {
  const normalized = expression.trim()
  if (!normalized) return { allContainers: true }
  if (normalized.length > SOHAQL_MAX_LENGTH) throw new Error('查询语句不能超过 2048 个字符')

  const parsed = normalized.match(/^\{([\s\S]*?)\}\s*(?:\|=\s*"((?:\\["\\]|[^"\\])*)")?\s*$/)
  if (!parsed) throw new Error('查询语句格式无效')

  const selector = (parsed[1] ?? '').trim()
  const values = new Map<string, string>()
  const matcherPattern = /\s*([a-z_]+)\s*=\s*"((?:\\["\\]|[^"\\])*)"\s*(?:,|$)/gy
  let offset = 0
  while (offset < selector.length) {
    matcherPattern.lastIndex = offset
    const matcher = matcherPattern.exec(selector)
    if (!matcher || matcher.index !== offset) throw new Error('选择器格式无效')
    const key = matcher[1] ?? ''
    if (!SOHAQL_SELECTOR_KEYS.has(key)) throw new Error(`不支持的选择器：${key}`)
    if (values.has(key)) throw new Error(`选择器不能重复：${key}`)
    values.set(key, unescapeSohaQLValue(matcher[2] ?? ''))
    offset = matcherPattern.lastIndex
  }

  const container = clean(values.get('container'))
  return {
    workloadKind: clean(values.get('workload_kind')),
    workloadName: clean(values.get('workload')),
    podNames: values.has('pod') ? [values.get('pod') ?? ''] : undefined,
    containers: container ? [container] : undefined,
    text: clean(parsed[2] ? unescapeSohaQLValue(parsed[2]) : undefined),
    allContainers: !container,
  }
}

export function buildRuntimeLogQuery(namespace: string, filters: RuntimeLogFilters): LogQuery {
  const workloadKind = clean(filters.workloadKind)
  const workloadName = clean(filters.workloadName)
  if (Boolean(workloadKind) !== Boolean(workloadName)) {
    throw new Error('工作负载类型和名称必须同时填写')
  }

  const selectedContainers = cleanList(filters.containers)
  const allContainers = Boolean(filters.allContainers) || !selectedContainers
  const containers = allContainers ? undefined : selectedContainers
  return {
    sourceMode: 'runtime',
    selector: {
      namespace: clean(namespace),
      workloadKind,
      workloadName,
      podNames: cleanList(filters.podNames),
      containers,
      labelSelector: clean(filters.labelSelector),
      allContainers,
    },
    tail: filters.tail || 200,
    limit: 5000,
    direction: 'forward',
    text: clean(filters.text),
    runtimeOptions: {
      previous: Boolean(filters.previous),
      sinceSeconds: filters.sinceSeconds || undefined,
    },
  }
}

export function buildDockerRuntimeLogQuery(
  serviceName: string,
  filters: RuntimeLogFilters,
): LogQuery {
  return {
    sourceMode: 'runtime',
    selector: { dockerService: serviceName.trim() },
    tail: Math.min(filters.tail || 200, 2000),
    limit: Math.min(filters.tail || 200, 2000),
    direction: 'forward',
    text: clean(filters.text),
    runtimeOptions: { sinceSeconds: filters.sinceSeconds || undefined },
  }
}

export function buildDurableLogQuery(
  namespace: string,
  filters: RuntimeLogFilters,
  now = Date.now(),
): LogQuery {
  const workloadKind = clean(filters.workloadKind)
  const workloadName = clean(filters.workloadName)
  if (Boolean(workloadKind) !== Boolean(workloadName)) {
    throw new Error('工作负载类型和名称必须同时填写')
  }
  const podNames = cleanList(filters.podNames)
  const selectedContainers = cleanList(filters.containers)
  const allContainers = Boolean(filters.allContainers) || !selectedContainers
  const containers = allContainers ? undefined : selectedContainers
  if (clean(filters.labelSelector)) {
    throw new Error('历史日志暂不支持 Kubernetes 标签选择器')
  }
  const range = absoluteRange(filters.from, filters.to)
  const rangeSeconds = filters.sinceSeconds && filters.sinceSeconds > 0 ? filters.sinceSeconds : 900
  return {
    sourceMode: 'durable',
    dataSourceId: clean(filters.dataSourceId),
    selector: {
      namespace: clean(namespace),
      workloadKind,
      workloadName,
      podNames,
      containers,
      allContainers,
    },
    from: range?.[0] ?? new Date(now - rangeSeconds * 1000).toISOString(),
    to: range?.[1] ?? new Date(now).toISOString(),
    limit: Math.min(filters.tail || 200, 1000),
    direction: 'backward',
    text: clean(filters.text),
    traceId: clean(filters.traceId),
    spanId: clean(filters.spanId),
  }
}

export function readLogExplorerPreset(params: URLSearchParams): LogExplorerPreset {
  const numberValue = Number(params.get('range'))
  const tailValue = Number(params.get('tail'))
  const podNames = cleanList(params.getAll('pod'))
  const containers = cleanList(params.getAll('container'))
  const range = absoluteRange(params.get('from'), params.get('to'))
  return {
    dataSourceId: clean(params.get('dataSourceId')),
    source:
      params.get('source') === 'docker'
        ? 'docker'
        : params.get('source') === 'delivery'
          ? 'delivery'
          : 'kubernetes',
    clusterId: clean(params.get('cluster')),
    namespace: clean(params.get('namespace')),
    dockerProjectId: clean(params.get('dockerProject')),
    dockerService: clean(params.get('dockerService')),
    applicationId: clean(params.get('application')),
    environmentId: clean(params.get('environment')),
    service: clean(params.get('service')),
    workloadKind: clean(params.get('workloadKind')),
    workloadName: clean(params.get('workload')),
    podNames,
    containers,
    labelSelector: clean(params.get('labelSelector')),
    text: clean(params.get('text')),
    traceId: clean(params.get('traceId')),
    spanId: clean(params.get('spanId')),
    from: range?.[0],
    to: range?.[1],
    sinceSeconds:
      !range && Number.isFinite(numberValue) && numberValue > 0 ? numberValue : undefined,
    tail: Number.isFinite(tailValue) && tailValue > 0 ? tailValue : undefined,
    allContainers: params.get('allContainers') === 'true',
    previous: params.get('previous') === 'true',
  }
}

export function buildLogExplorerPath(preset: LogExplorerPreset) {
  const params = new URLSearchParams()
  const range = absoluteRange(preset.from, preset.to)
  const values: Array<[string, string | null | undefined]> = [
    ['dataSourceId', preset.dataSourceId],
    ['source', preset.source && preset.source !== 'kubernetes' ? preset.source : undefined],
    ['cluster', preset.clusterId],
    ['namespace', preset.namespace],
    ['dockerProject', preset.dockerProjectId],
    ['dockerService', preset.dockerService],
    ['application', preset.applicationId],
    ['environment', preset.environmentId],
    ['service', preset.service],
    ['workloadKind', preset.workloadKind],
    ['workload', preset.workloadName],
    ['labelSelector', preset.labelSelector],
    ['text', preset.text],
    ['traceId', preset.traceId],
    ['spanId', preset.spanId],
    ['from', range?.[0]],
    ['to', range?.[1]],
    ['range', !range && preset.sinceSeconds ? String(preset.sinceSeconds) : undefined],
    ['tail', preset.tail ? String(preset.tail) : undefined],
    ['allContainers', preset.allContainers ? 'true' : undefined],
    ['previous', preset.previous ? 'true' : undefined],
  ]
  for (const [key, value] of values) {
    if (value) params.set(key, value)
  }
  for (const podName of cleanList(preset.podNames) ?? []) params.append('pod', podName)
  for (const container of cleanList(preset.containers) ?? []) params.append('container', container)
  const search = params.toString()
  return `/monitoring-workbench/logs${search ? `?${search}` : ''}`
}

export function logEntryKey(entry: LogEntry) {
  const source = entry.source
  return [
    entry.timestamp,
    source.podUid || source.podName || source.dockerProjectId || source.applicationId,
    source.containerName || source.dockerService || source.environmentKey,
    entry.stream,
    entry.message,
  ].join('\u0000')
}

export function mergeLogEntries(
  current: LogEntry[],
  incoming: LogEntry[],
  maxEntries = MAX_BROWSER_ENTRIES,
) {
  if (incoming.length === 0) return current
  const seen = new Set(current.map(logEntryKey))
  const additions = incoming.filter((entry) => {
    const key = logEntryKey(entry)
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
  if (additions.length === 0) return current
  const merged = [...current, ...additions].sort(
    (left, right) =>
      left.timestamp.localeCompare(right.timestamp) ||
      logEntryKey(left).localeCompare(logEntryKey(right)),
  )
  return Number.isFinite(maxEntries) ? merged.slice(-maxEntries) : merged
}

export function formatLogSource(entry: LogEntry) {
  const source = entry.source
  return (
    [source.podName || source.workloadName || source.dockerService, source.containerName]
      .filter(Boolean)
      .join(' / ') || source.domain
  )
}

export function formatLogExport(entries: LogEntry[]) {
  return entries
    .map((entry) => `${entry.timestamp} ${formatLogSource(entry)} ${entry.message}`)
    .join('\n')
}
