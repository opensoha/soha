import { ManagementIconButton, ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { podQueries } from '@/features/platform'
import { ApiError } from '@/services/api-client'
import { toScopeKey } from '@/types'
import { downloadText } from '@/utils/download'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import {
  ClockCircleOutlined,
  CodeOutlined,
  DatabaseOutlined,
  DeleteOutlined,
  DownOutlined,
  DownloadOutlined,
  ExportOutlined,
  FilterOutlined,
  LinkOutlined,
  ReloadOutlined,
  RightOutlined,
  SearchOutlined,
  StopOutlined,
} from '@ant-design/icons'
import type { LogEntry, LogQuery, LogStreamEvent } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery } from '@tanstack/react-query'
import {
  Alert,
  App,
  Button,
  Card,
  Checkbox,
  DatePicker,
  Flex,
  Form,
  Input,
  Popover,
  Segmented,
  Select,
  Space,
  Spin,
  Switch,
  Typography,
} from 'antd'
import type { Dayjs } from 'dayjs'
import dayjs from 'dayjs'
import type { ReactNode } from 'react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { buildLogStreamURL, issueLogStreamTicket, logTargetKey, type LogTarget } from './api'
import {
  buildDockerRuntimeLogQuery,
  buildDurableLogQuery,
  buildLogExplorerPath,
  buildRuntimeLogQuery,
  buildSohaQLExpression,
  formatLogExport,
  formatLogSource,
  mergeLogEntries,
  parseSohaQLExpression,
  type LogExplorerPreset,
  type RuntimeLogFilters,
} from './model'
import { observabilityLogQueries } from './queries'
import './styles.css'

const { Text } = Typography
const MAX_RECONNECT_ATTEMPTS = 5
const RECONNECT_DELAYS_MS = [1000, 2000, 5000, 10000, 15000]
const ALL_CONTAINERS_VALUE = '__all_containers__'

interface LogExplorerProps {
  autoStart?: boolean
  clusterId?: string | null
  embedded?: boolean
  namespace?: string | null
  preset?: LogExplorerPreset
  scopeControl?: ReactNode
  syncURL?: boolean
  target?: LogTarget
}

interface SubmittedLogQuery {
  mode: 'live' | 'history'
  query: LogQuery
  target: LogTarget
}

type QueryEditorMode = 'query' | 'builder'
type LogTimeRange = [Dayjs, Dayjs]
type LogTimeMode = number | 'absolute'

const LOG_TIME_OPTIONS = [
  { label: '最近 5 分钟', value: 300 },
  { label: '最近 15 分钟', value: 900 },
  { label: '最近 1 小时', value: 3600 },
  { label: '最近 6 小时', value: 21600 },
]

const LOG_FIELDS = [
  { value: 'timestamp', label: '时间' },
  { value: 'severity', label: '级别' },
  { value: 'source', label: '来源' },
  { value: 'stream', label: '流' },
  { value: 'traceId', label: 'Trace ID' },
  { value: 'message', label: '消息' },
]
const DEFAULT_LOG_FIELDS = ['timestamp', 'severity', 'source', 'message']

function logFieldLabel(field: string) {
  return (
    LOG_FIELDS.find((option) => option.value === field)?.label ?? field.replace('attribute:', '')
  )
}

function logFieldValue(entry: LogEntry, field: string) {
  if (field.startsWith('attribute:')) return entry.attributes?.[field.slice(10)] ?? '-'
  switch (field) {
    case 'timestamp':
      return formatDateTime(entry.timestamp)
    case 'severity':
      return entry.severity ?? '-'
    case 'source':
      return formatLogSource(entry)
    case 'stream':
      return entry.stream ?? '-'
    case 'traceId':
      return entry.traceId ?? '-'
    default:
      return entry.message
  }
}

function logFieldClassName(field: string) {
  return field.startsWith('attribute:') ? 'attribute' : field
}

function logSeverityClassName(severity?: string) {
  const value = severity?.toLowerCase() ?? ''
  if (value.includes('error') || value.includes('fatal') || value === 'err') return 'is-error'
  if (value.includes('warn')) return 'is-warning'
  if (value.includes('info')) return 'is-info'
  return 'is-muted'
}

function logEntryContext(entry: LogEntry) {
  const source = entry.source
  const values: Array<[string, string | undefined]> = [
    ['记录时间', entry.timestamp],
    ['采集时间', entry.observedAt],
    ['日志域', source.domain],
    ['集群', source.clusterId],
    ['命名空间', source.namespace],
    ['工作负载类型', source.workloadKind],
    ['工作负载', source.workloadName],
    ['Pod', source.podName],
    ['容器', source.containerName],
    ['应用', source.applicationId],
    ['环境', source.environmentKey],
    ['Docker 项目', source.dockerProjectId],
    ['Docker 服务', source.dockerService],
    ['流', entry.stream],
    ['Trace ID', entry.traceId],
    ['Span ID', entry.spanId],
    ['来源模式', entry.sourceMode],
    ...Object.entries(entry.attributes ?? {}).map(
      ([key, value]) => [`attributes.${key}`, value] as [string, string],
    ),
  ]
  return values.filter((value): value is [string, string] => Boolean(value[1]))
}

function initialFilters(preset?: LogExplorerPreset): RuntimeLogFilters {
  return {
    dataSourceId: preset?.dataSourceId ?? undefined,
    workloadKind: preset?.workloadKind,
    workloadName: preset?.workloadName,
    podNames: preset?.podNames,
    containers: preset?.containers,
    labelSelector: preset?.labelSelector,
    text: preset?.text,
    traceId: preset?.traceId,
    spanId: preset?.spanId,
    from: preset?.from,
    to: preset?.to,
    sinceSeconds: preset?.sinceSeconds ?? 900,
    tail: preset?.tail ?? 200,
    allContainers: preset?.allContainers ?? !preset?.containers?.length,
    previous: preset?.previous ?? false,
  }
}

function initialTimeRange(filters: RuntimeLogFilters): LogTimeRange {
  if (filters.from && filters.to) {
    const start = dayjs(filters.from)
    const end = dayjs(filters.to)
    if (start.isValid() && end.isValid() && !start.isAfter(end)) return [start, end]
  }
  const now = dayjs()
  return [now.subtract(filters.sinceSeconds ?? 900, 'second'), now]
}

function initialTimeMode(filters: RuntimeLogFilters): LogTimeMode {
  return filters.from && filters.to ? 'absolute' : (filters.sinceSeconds ?? 900)
}

function formatTimeRangeLabel(mode: LogTimeMode, range: LogTimeRange) {
  if (mode === 'absolute') {
    return `${range[0].format('YYYY-MM-DD HH:mm')} - ${range[1].format('YYYY-MM-DD HH:mm')}`
  }
  return LOG_TIME_OPTIONS.find((option) => option.value === mode)?.label ?? `最近 ${mode} 秒`
}

function queryErrorDescription(error: unknown, mode: SubmittedLogQuery['mode']) {
  if (error instanceof ApiError) {
    if (error.status === 403) return '当前账号没有所选日志范围的查看权限。'
    if (error.status === 404) return '所选日志来源不存在。'
    if (error.status === 422) return '当前连接模式不支持聚合日志，或筛选条件无法解析。'
    if (error.status === 503) {
      return mode === 'history'
        ? '当前范围没有可用的持久化日志数据源，或日志后端暂时不可用。'
        : '集群日志能力暂时不可用，请检查集群或 Agent 状态。'
    }
  }
  return '日志查询失败，请检查筛选条件和集群连接状态。'
}

export function LogExplorer({
  autoStart = false,
  clusterId,
  embedded = false,
  namespace,
  preset,
  scopeControl,
  syncURL = false,
  target,
}: LogExplorerProps) {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const { message } = App.useApp()
  const permissionSnapshot = usePermissionSnapshot().data?.data
  const canViewDataSources = hasPermission(permissionSnapshot, 'observe.log-data-sources.view')
  const resolvedTarget = useMemo<LogTarget>(
    () => target ?? { kind: 'cluster', clusterId: clusterId ?? '', namespace: namespace ?? '' },
    [target, clusterId, namespace],
  )
  const targetIdentity = logTargetKey(resolvedTarget)
  const isKubernetes = resolvedTarget.kind !== 'docker'
  const supportsDurable = resolvedTarget.kind !== 'docker'
  const targetScoped =
    resolvedTarget.kind === 'docker'
      ? Boolean(resolvedTarget.projectId && resolvedTarget.serviceName)
      : resolvedTarget.kind === 'delivery'
        ? Boolean(resolvedTarget.applicationId && resolvedTarget.environmentId)
        : Boolean(resolvedTarget.clusterId && (!embedded || resolvedTarget.namespace))
  const mode = embedded ? 'live' : 'history'
  const showQueryControls = !embedded || resolvedTarget.kind !== 'docker'
  const targetReady = targetScoped && (embedded || supportsDurable)
  const [form] = Form.useForm<RuntimeLogFilters>()
  const serializedDefaults = JSON.stringify(initialFilters(preset))
  const defaults = useMemo(
    () => JSON.parse(serializedDefaults) as RuntimeLogFilters,
    [serializedDefaults],
  )
  const [queryEditorMode, setQueryEditorMode] = useState<QueryEditorMode>('builder')
  const [queryExpression, setQueryExpression] = useState(() => buildSohaQLExpression(defaults))
  const [timeMode, setTimeMode] = useState<LogTimeMode>(() => initialTimeMode(defaults))
  const [timeRange, setTimeRange] = useState<LogTimeRange>(() => initialTimeRange(defaults))
  const [timePopoverOpen, setTimePopoverOpen] = useState(false)
  const [absoluteTimeOpen, setAbsoluteTimeOpen] = useState(false)
  const [submitted, setSubmitted] = useState<SubmittedLogQuery | null>(null)
  const [cachedEntries, setEntries] = useState<LogEntry[]>([])
  const [streamState, setStreamState] = useState('idle')
  const [streamMessage, setStreamMessage] = useState('')
  const [streamError, setStreamError] = useState<Error | null>(null)
  const [streamEnabled, setStreamEnabled] = useState(false)
  const [streamGeneration, setStreamGeneration] = useState(0)
  const [sourceErrors, setSourceErrors] = useState(0)
  const [autoScroll, setAutoScroll] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [visibleLogFields, setVisibleLogFields] = useState(DEFAULT_LOG_FIELDS)
  const allContainers = Form.useWatch('allContainers', form)
  const podNames = Form.useWatch('podNames', form)
  const selectedContainers = Form.useWatch('containers', form)
  const traceId = Form.useWatch('traceId', form)
  const spanId = Form.useWatch('spanId', form)
  const workloadScope = toScopeKey(
    resolvedTarget.kind === 'cluster' ? resolvedTarget.clusterId : null,
    resolvedTarget.kind === 'docker' ? null : resolvedTarget.namespace,
  )
  const selectedPodName = podNames?.length === 1 ? (podNames[0]?.trim() ?? '') : ''
  const podContainersQuery = useQuery({
    ...podQueries.detail(workloadScope, selectedPodName),
    enabled:
      resolvedTarget.kind === 'cluster' &&
      Boolean(selectedPodName && workloadScope.clusterId && workloadScope.namespace),
  })
  const containerOptions = useMemo(() => {
    const names = new Set<string>()
    for (const value of selectedContainers ?? []) {
      if (value.trim()) names.add(value.trim())
    }
    for (const container of readableQueryData(podContainersQuery)?.containers ?? []) {
      if (container.name.trim()) names.add(container.name.trim())
    }
    return [
      { value: ALL_CONTAINERS_VALUE, label: '全部容器' },
      ...Array.from(names)
        .sort((left, right) => left.localeCompare(right))
        .map((value) => ({ value, label: value })),
    ]
  }, [podContainersQuery.data?.containers, podContainersQuery.error, selectedContainers])
  const scrollerRef = useRef<HTMLDivElement>(null)
  const reconnectAttemptRef = useRef(0)
  const autoStartedRef = useRef('')
  const loadingMoreRef = useRef(false)

  useEffect(() => {
    form.setFieldsValue(defaults)
    setQueryExpression(buildSohaQLExpression(defaults))
    setTimeMode(initialTimeMode(defaults))
    setTimeRange(initialTimeRange(defaults))
  }, [defaults, form])

  const snapshotQuery = useQuery(
    observabilityLogQueries.snapshot(
      submitted?.target ?? resolvedTarget,
      submitted?.query ?? { selector: {} },
      Boolean(submitted),
    ),
  )

  const refusedSnapshotAt = useRef<number | null>(null)
  const streamDenied = queryAccessDenied({ error: streamError })
  const entries = queryAccessDenied(snapshotQuery) || streamDenied ? [] : cachedEntries
  useEffect(() => {
    if (!queryAccessDenied(snapshotQuery) && !streamDenied) return
    if (streamDenied) refusedSnapshotAt.current = snapshotQuery.dataUpdatedAt
    setEntries((current) => (current.length ? [] : current))
    setStreamEnabled(false)
  }, [snapshotQuery.error, snapshotQuery.dataUpdatedAt, streamDenied])

  function syncSearch(filters: RuntimeLogFilters) {
    if (!syncURL) return
    const path = buildLogExplorerPath({
      dataSourceId: filters.dataSourceId,
      source:
        resolvedTarget.kind === 'docker'
          ? 'docker'
          : resolvedTarget.kind === 'delivery'
            ? 'delivery'
            : 'kubernetes',
      clusterId: resolvedTarget.kind === 'cluster' ? resolvedTarget.clusterId : undefined,
      namespace: resolvedTarget.kind === 'docker' ? undefined : resolvedTarget.namespace,
      dockerProjectId: resolvedTarget.kind === 'docker' ? resolvedTarget.projectId : undefined,
      dockerService: resolvedTarget.kind === 'docker' ? resolvedTarget.serviceName : undefined,
      applicationId:
        resolvedTarget.kind === 'delivery' ? resolvedTarget.applicationId : preset?.applicationId,
      environmentId:
        resolvedTarget.kind === 'delivery' ? resolvedTarget.environmentId : preset?.environmentId,
      service: preset?.service,
      workloadKind: filters.workloadKind,
      workloadName: filters.workloadName,
      podNames: filters.podNames,
      containers: filters.containers,
      labelSelector: filters.labelSelector,
      text: filters.text,
      traceId: filters.traceId,
      spanId: filters.spanId,
      from: filters.from,
      to: filters.to,
      sinceSeconds: filters.sinceSeconds,
      tail: filters.tail,
      allContainers: filters.allContainers,
      previous: filters.previous,
    })
    const next = new URLSearchParams(searchParams)
    if (filters.dataSourceId) next.delete('dataSourceId')
    for (const key of [
      'source',
      'cluster',
      'namespace',
      'dockerProject',
      'dockerService',
      'application',
      'environment',
      'service',
      'workloadKind',
      'workload',
      'pod',
      'container',
      'labelSelector',
      'text',
      'traceId',
      'spanId',
      'from',
      'to',
      'range',
      'tail',
      'allContainers',
      'previous',
    ]) {
      next.delete(key)
    }
    for (const [key, value] of new URLSearchParams(path.split('?')[1] ?? '')) {
      next.append(key, value)
    }
    setSearchParams(next, { replace: true })
  }

  function submitLiveQuery(filters: RuntimeLogFilters) {
    if (!targetReady) {
      void message.warning('请先选择完整的日志范围')
      return
    }
    try {
      const query =
        resolvedTarget.kind === 'docker'
          ? buildDockerRuntimeLogQuery(resolvedTarget.serviceName, filters)
          : buildRuntimeLogQuery(
              resolvedTarget.kind === 'delivery'
                ? (resolvedTarget.namespace ?? '')
                : resolvedTarget.namespace,
              filters,
            )
      setEntries([])
      setSourceErrors(0)
      setStreamMessage('')
      reconnectAttemptRef.current = 0
      setSubmitted({ target: resolvedTarget, mode: 'live', query })
      setStreamState('connecting')
      setStreamEnabled(true)
      syncSearch(filters)
    } catch (error) {
      void message.error(error instanceof Error ? error.message : '筛选条件无效')
    }
  }

  function handleSubmit(filters: RuntimeLogFilters) {
    let resolvedFilters = { ...defaults, ...filters }
    if (!embedded && queryEditorMode === 'query') {
      try {
        resolvedFilters = {
          ...parseSohaQLExpression(queryExpression),
          dataSourceId: defaults.dataSourceId,
          traceId: filters.traceId,
          spanId: filters.spanId,
          sinceSeconds: filters.sinceSeconds,
          tail: filters.tail,
          previous: filters.previous,
        }
      } catch (error) {
        void message.error(error instanceof Error ? error.message : '查询语句无效')
        return
      }
    }
    if (mode === 'history') {
      resolvedFilters =
        timeMode === 'absolute'
          ? {
              ...resolvedFilters,
              from: timeRange[0].toISOString(),
              to: timeRange[1].toISOString(),
              sinceSeconds: undefined,
            }
          : {
              ...resolvedFilters,
              from: undefined,
              to: undefined,
              sinceSeconds: timeMode,
            }
      if (!supportsDurable) {
        void message.warning('当前日志来源暂不支持持久化查询')
        return
      }
      if (!targetReady) {
        void message.warning('请先选择完整的日志范围')
        return
      }
      try {
        const query = buildDurableLogQuery(resolvedTarget.namespace ?? '', resolvedFilters)
        if (
          submitted?.mode === 'history' &&
          logTargetKey(submitted.target) === targetIdentity &&
          JSON.stringify(submitted.query) === JSON.stringify(query)
        ) {
          void snapshotQuery.refetch()
          syncSearch(resolvedFilters)
          return
        }
        setEntries([])
        setSourceErrors(0)
        setStreamMessage('')
        setStreamEnabled(false)
        loadingMoreRef.current = false
        setLoadingMore(false)
        setSubmitted({ target: resolvedTarget, mode: 'history', query })
        syncSearch(resolvedFilters)
      } catch (error) {
        void message.error(error instanceof Error ? error.message : '筛选条件无效')
      }
      return
    }
    submitLiveQuery(resolvedFilters)
  }

  function handleContainerChange(values: string[]) {
    const containers = values.filter((value) => value !== ALL_CONTAINERS_VALUE)
    if (values.includes(ALL_CONTAINERS_VALUE) && !allContainers) {
      form.setFieldsValue({ allContainers: true, containers: undefined })
      return
    }
    if (values.includes(ALL_CONTAINERS_VALUE) && allContainers && containers.length > 0) {
      form.setFieldsValue({ allContainers: false, containers })
      return
    }
    form.setFieldsValue({
      allContainers: containers.length === 0,
      containers: containers.length > 0 ? containers : undefined,
    })
  }

  function handleQueryEditorModeChange(value: string | number) {
    const nextMode = value as QueryEditorMode
    try {
      if (nextMode === 'query') {
        const filters = form.getFieldsValue()
        if (
          (filters.podNames?.length ?? 0) > 1 ||
          (!filters.allContainers && (filters.containers?.length ?? 0) > 1)
        ) {
          void message.warning('高级语句暂不支持多个 Pod 或容器，请先缩小筛选范围')
          return
        }
        setQueryExpression(buildSohaQLExpression(filters))
      } else {
        const filters = parseSohaQLExpression(queryExpression)
        form.setFieldsValue({
          workloadKind: filters.workloadKind,
          workloadName: filters.workloadName,
          podNames: filters.podNames,
          containers: filters.containers,
          labelSelector: undefined,
          text: filters.text,
          allContainers: filters.allContainers,
        })
      }
      setQueryEditorMode(nextMode)
    } catch (error) {
      void message.error(error instanceof Error ? error.message : '查询语句无效')
    }
  }

  useEffect(() => {
    if (!autoStart || autoStartedRef.current === targetIdentity || !targetReady || mode !== 'live')
      return
    autoStartedRef.current = targetIdentity
    submitLiveQuery(defaults)
  }, [autoStart, targetIdentity, targetReady])

  useEffect(() => {
    const page = readableQueryData(snapshotQuery)
    // Reconnecting must not refill the cleared buffer from the refused snapshot.
    if (
      !page ||
      streamDenied ||
      (refusedSnapshotAt.current !== null &&
        snapshotQuery.dataUpdatedAt <= refusedSnapshotAt.current)
    )
      return
    setEntries((current) => {
      const existing = mode === 'history' ? [...current].reverse() : current
      const merged = mergeLogEntries(
        existing,
        page.entries,
        mode === 'history' ? Infinity : undefined,
      )
      return mode === 'history' ? merged.reverse() : merged
    })
  }, [snapshotQuery.data, snapshotQuery.dataUpdatedAt, snapshotQuery.error, streamDenied])

  useEffect(() => {
    if (snapshotQuery.isFetching) return
    loadingMoreRef.current = false
    setLoadingMore(false)
  }, [snapshotQuery.isFetching])

  useEffect(() => {
    if (!autoScroll || !scrollerRef.current) return
    scrollerRef.current.scrollTop = scrollerRef.current.scrollHeight
  }, [autoScroll, entries])

  useEffect(() => {
    if (!submitted || submitted.mode !== 'live' || !streamEnabled) return
    let cancelled = false
    let endedByServer = false
    let socket: WebSocket | undefined
    let reconnectTimer: number | undefined

    const scheduleReconnect = () => {
      if (cancelled || endedByServer) return
      const attempt = reconnectAttemptRef.current
      if (attempt >= MAX_RECONNECT_ATTEMPTS) {
        setStreamState('degraded')
        setStreamMessage('自动重连已停止，请手动重连。')
        return
      }
      reconnectAttemptRef.current += 1
      setStreamState('reconnecting')
      setStreamMessage(`连接中断，${RECONNECT_DELAYS_MS[attempt] / 1000} 秒后重试。`)
      reconnectTimer = window.setTimeout(
        () => setStreamGeneration((value) => value + 1),
        RECONNECT_DELAYS_MS[attempt],
      )
    }

    setStreamState(reconnectAttemptRef.current > 0 ? 'reconnecting' : 'connecting')
    void issueLogStreamTicket(submitted.target, submitted.query)
      .then((ticket) => {
        if (cancelled) return
        setStreamError(null)
        socket = new WebSocket(buildLogStreamURL(submitted.target, ticket.ticket))
        socket.onopen = () => {
          reconnectAttemptRef.current = 0
          setStreamState('live')
          setStreamMessage('')
        }
        socket.onmessage = (event) => {
          let streamEvent: LogStreamEvent
          try {
            streamEvent = JSON.parse(String(event.data)) as LogStreamEvent
          } catch {
            setStreamState('degraded')
            setStreamMessage('收到无法解析的日志事件。')
            return
          }
          if (streamEvent.type === 'entry' && streamEvent.entry) {
            setEntries((current) => mergeLogEntries(current, [streamEvent.entry as LogEntry]))
          } else if (streamEvent.type === 'source_error') {
            setSourceErrors((value) => value + 1)
            setStreamState('degraded')
            setStreamMessage('部分日志来源暂时不可用。')
          } else if (streamEvent.type === 'status' && streamEvent.status) {
            setStreamState(streamEvent.status.state)
            setStreamMessage(streamEvent.status.message ?? '')
          } else if (streamEvent.type === 'end') {
            endedByServer = true
            setStreamState('ended')
            setStreamMessage('实时会话已结束。')
            socket?.close()
          }
        }
        socket.onerror = () => {
          setStreamState('degraded')
          setStreamMessage('实时日志连接异常。')
        }
        socket.onclose = () => scheduleReconnect()
      })
      .catch((error) => {
        if (cancelled) return
        if (error instanceof ApiError && [401, 403].includes(error.status)) {
          endedByServer = true
          setEntries([])
          setStreamEnabled(false)
          setStreamState('denied')
          setStreamError(error)
          setStreamMessage('')
          return
        }
        scheduleReconnect()
      })

    return () => {
      cancelled = true
      if (reconnectTimer) window.clearTimeout(reconnectTimer)
      socket?.close()
    }
  }, [streamEnabled, streamGeneration, submitted])

  function handleReconnect() {
    if (!submitted) {
      form.submit()
      return
    }
    reconnectAttemptRef.current = 0
    setStreamEnabled(true)
    setStreamGeneration((value) => value + 1)
  }

  function handleNextPage() {
    if (
      !submitted ||
      submitted.mode !== 'history' ||
      !readableQueryData(snapshotQuery)?.nextCursor ||
      snapshotQuery.isFetching ||
      loadingMoreRef.current
    )
      return
    loadingMoreRef.current = true
    setLoadingMore(true)
    setSubmitted({
      ...submitted,
      query: { ...submitted.query, cursor: readableQueryData(snapshotQuery)?.nextCursor },
    })
  }

  function handleHistoryScroll(event: React.UIEvent<HTMLDivElement>) {
    const element = event.currentTarget
    if (element.scrollHeight - element.scrollTop - element.clientHeight <= 120) handleNextPage()
  }

  function handleExport() {
    if (!submitted) return
    downloadText(`soha-runtime-logs-${Date.now()}.txt`, formatLogExport(entries))
  }

  function handleOpenInCenter() {
    const filters = form.getFieldsValue()
    navigate(
      buildLogExplorerPath({
        ...preset,
        ...filters,
        source:
          resolvedTarget.kind === 'docker'
            ? 'docker'
            : resolvedTarget.kind === 'delivery'
              ? 'delivery'
              : 'kubernetes',
        clusterId: resolvedTarget.kind === 'cluster' ? resolvedTarget.clusterId : undefined,
        namespace: resolvedTarget.kind === 'docker' ? undefined : resolvedTarget.namespace,
        dockerProjectId: resolvedTarget.kind === 'docker' ? resolvedTarget.projectId : undefined,
        dockerService: resolvedTarget.kind === 'docker' ? resolvedTarget.serviceName : undefined,
        applicationId:
          resolvedTarget.kind === 'delivery' ? resolvedTarget.applicationId : undefined,
        environmentId:
          resolvedTarget.kind === 'delivery' ? resolvedTarget.environmentId : undefined,
        labelSelector: undefined,
        previous: false,
      }),
    )
  }

  const page = readableQueryData(snapshotQuery)
  const logFieldOptions = useMemo(
    () => [
      ...LOG_FIELDS,
      ...Array.from(new Set(entries.flatMap((entry) => Object.keys(entry.attributes ?? {}))))
        .sort()
        // ponytail: keep the field rail bounded; add search/virtualization if schemas exceed 20 fields.
        .slice(0, 20)
        .map((key) => ({ value: `attribute:${key}`, label: key })),
    ],
    [entries],
  )

  return (
    <div className={embedded ? 'soha-log-explorer is-embedded' : 'soha-log-explorer'}>
      {showQueryControls ? (
        <Card className="soha-log-query-card">
          {mode === 'history' && !supportsDurable ? (
            <Alert
              className="soha-log-inline-alert"
              showIcon
              type="warning"
              title="当前 Docker 日志仅支持在运行时页面实时查看"
            />
          ) : null}
          <Form<RuntimeLogFilters>
            form={form}
            initialValues={defaults}
            layout="vertical"
            onFinish={handleSubmit}
          >
            <Form.Item name="allContainers" hidden valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="containers" hidden>
              <Select mode="multiple" />
            </Form.Item>
            <Form.Item name="traceId" hidden>
              <Input />
            </Form.Item>
            <Form.Item name="spanId" hidden>
              <Input />
            </Form.Item>
            {!embedded ? (
              <Flex className="soha-log-query-mode-row" align="center" gap={12} wrap>
                <Segmented
                  aria-label="日志查询模式"
                  options={[
                    { icon: <FilterOutlined />, label: '筛选查询', value: 'builder' },
                    { icon: <CodeOutlined />, label: '高级语句', value: 'query' },
                  ]}
                  value={queryEditorMode}
                  onChange={handleQueryEditorModeChange}
                />
                {resolvedTarget.kind === 'cluster' && scopeControl ? (
                  <div className="soha-log-query-scope-controls">{scopeControl}</div>
                ) : (
                  <Space className="soha-log-query-scope" size={8} wrap>
                    {queryEditorMode === 'query' ? (
                      <MetadataTag label="SohaQL" tone="blue" />
                    ) : null}
                    <Text type="secondary">
                      {resolvedTarget.kind === 'cluster'
                        ? resolvedTarget.namespace || '未选择命名空间'
                        : resolvedTarget.kind === 'delivery'
                          ? `${resolvedTarget.applicationId || '-'} / ${resolvedTarget.environmentId || '-'}`
                          : `${resolvedTarget.projectId || '-'} / ${resolvedTarget.serviceName || '-'}`}
                    </Text>
                  </Space>
                )}
                {traceId ? <MetadataTag label={`Trace ${traceId}`} tone="cyan" /> : null}
                {spanId ? <MetadataTag label={`Span ${spanId}`} tone="blue" /> : null}
                <div className="soha-log-query-top-actions">
                  <Popover
                    content={
                      <div className="soha-log-time-popover">
                        <div className="soha-log-time-relative-section">
                          <Text strong>相对时间</Text>
                          <div className="soha-log-time-relative-list">
                            {LOG_TIME_OPTIONS.map((option) => (
                              <Button
                                aria-pressed={timeMode === option.value}
                                block
                                key={option.value}
                                type={timeMode === option.value ? 'primary' : 'text'}
                                onClick={() => {
                                  setTimeMode(option.value)
                                  setTimePopoverOpen(false)
                                }}
                              >
                                {option.label}
                              </Button>
                            ))}
                          </div>
                        </div>
                        <div className="soha-log-time-absolute-section">
                          <Button
                            aria-expanded={absoluteTimeOpen}
                            className="soha-log-time-absolute-toggle"
                            type="text"
                            onClick={() => setAbsoluteTimeOpen((current) => !current)}
                          >
                            <span>绝对时间</span>
                            <DownOutlined />
                          </Button>
                          {absoluteTimeOpen ? (
                            <DatePicker.RangePicker
                              allowClear={false}
                              aria-label="绝对时间范围"
                              className="soha-log-query-range-picker"
                              format="YYYY-MM-DD HH:mm:ss"
                              showTime
                              value={timeRange}
                              onChange={(value) => {
                                if (!value?.[0] || !value[1]) return
                                setTimeRange([value[0], value[1]])
                                setTimeMode('absolute')
                                setTimePopoverOpen(false)
                              }}
                            />
                          ) : null}
                        </div>
                      </div>
                    }
                    onOpenChange={(open) => {
                      if (open) {
                        setAbsoluteTimeOpen(false)
                        if (timeMode !== 'absolute') {
                          const now = dayjs()
                          setTimeRange([now.subtract(timeMode, 'second'), now])
                        }
                      }
                      setTimePopoverOpen(open)
                    }}
                    open={timePopoverOpen}
                    placement="bottomRight"
                    rootClassName="soha-log-time-popover-overlay"
                    trigger="click"
                  >
                    <Button aria-label="时间范围" className="soha-log-time-trigger">
                      <ClockCircleOutlined />
                      <span>{formatTimeRangeLabel(timeMode, timeRange)}</span>
                      <DownOutlined />
                    </Button>
                  </Popover>
                  <Form.Item name="tail" noStyle>
                    <Select
                      aria-label="行数"
                      className="soha-log-query-limit-select"
                      options={[200, 500, 1000].map((value) => ({
                        value,
                        label: `${value} 行`,
                      }))}
                    />
                  </Form.Item>
                  {queryEditorMode === 'query' ? (
                    <Button
                      htmlType="submit"
                      icon={<SearchOutlined />}
                      type="primary"
                      disabled={!targetReady}
                    >
                      查询日志
                    </Button>
                  ) : null}
                  {canViewDataSources ? (
                    <ManagementIconButton
                      aria-label="数据源"
                      icon={<DatabaseOutlined />}
                      tooltip="数据源"
                      onClick={() => navigate('/monitoring-workbench/log-data-sources')}
                    />
                  ) : null}
                </div>
              </Flex>
            ) : null}

            {!embedded && queryEditorMode === 'query' ? (
              <Input.TextArea
                aria-label="SohaQL 高级查询语句"
                className="soha-log-query-editor"
                maxLength={2048}
                rows={2}
                spellCheck={false}
                value={queryExpression}
                onChange={(event) => setQueryExpression(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) form.submit()
                }}
              />
            ) : (
              <div className={`soha-log-filter-grid${!embedded ? ' is-standalone' : ''}`}>
                {isKubernetes ? (
                  <>
                    <Form.Item label="Pod（可选）" name="podNames">
                      <Select
                        allowClear
                        aria-label="Pod"
                        mode="tags"
                        tokenSeparators={[',']}
                        placeholder="全部 Pod"
                      />
                    </Form.Item>
                    <Form.Item label="容器（可选）">
                      <Select
                        allowClear
                        aria-label="容器"
                        loading={podContainersQuery.isFetching}
                        maxTagCount="responsive"
                        mode="tags"
                        options={containerOptions}
                        showSearch={{ optionFilterProp: 'label' }}
                        tokenSeparators={[',']}
                        value={allContainers ? [ALL_CONTAINERS_VALUE] : (selectedContainers ?? [])}
                        onChange={handleContainerChange}
                      />
                    </Form.Item>
                    {embedded ? (
                      <Form.Item label="标签选择器" name="labelSelector">
                        <Input placeholder="app=api,tier=backend" />
                      </Form.Item>
                    ) : null}
                  </>
                ) : null}
                {embedded ? (
                  <Form.Item label="文本筛选" name="text">
                    <Input allowClear placeholder="服务端文本匹配" />
                  </Form.Item>
                ) : null}
                {embedded ? (
                  <>
                    <Form.Item label="时间范围" name="sinceSeconds">
                      <Select
                        options={[
                          { value: 0, label: '全部可用日志' },
                          { value: 300, label: '最近 5 分钟' },
                          { value: 900, label: '最近 15 分钟' },
                          { value: 3600, label: '最近 1 小时' },
                          { value: 21600, label: '最近 6 小时' },
                        ]}
                      />
                    </Form.Item>
                    <Form.Item label="每个来源读取行数" name="tail">
                      <Select
                        options={(isKubernetes
                          ? [200, 500, 1000, 5000]
                          : [200, 500, 1000, 2000]
                        ).map((value) => ({ value, label: String(value) }))}
                      />
                    </Form.Item>
                  </>
                ) : null}
              </div>
            )}

            {embedded ? (
              <Flex
                className="soha-log-query-actions"
                align="end"
                gap={12}
                justify="space-between"
                wrap
              >
                <Space size={16} wrap>
                  {isKubernetes && mode === 'live' ? (
                    <Form.Item name="previous" noStyle valuePropName="checked">
                      <Switch checkedChildren="上次实例" unCheckedChildren="当前实例" />
                    </Form.Item>
                  ) : null}
                </Space>
                <Space size={8} wrap>
                  <Button icon={<ExportOutlined />} onClick={handleOpenInCenter}>
                    在日志中心打开
                  </Button>
                  <Button
                    htmlType="submit"
                    icon={<SearchOutlined />}
                    type="primary"
                    disabled={!targetReady}
                  >
                    查询并连接
                  </Button>
                </Space>
              </Flex>
            ) : null}

            {!embedded && queryEditorMode === 'builder' ? (
              <div className="soha-log-primary-query">
                <Form.Item name="text" noStyle>
                  <Input.Search
                    allowClear
                    aria-label="日志全文搜索"
                    className="soha-log-primary-search"
                    enterButton={
                      <Button
                        aria-label="查询日志"
                        disabled={!targetReady}
                        icon={<SearchOutlined />}
                        type="primary"
                      >
                        查询日志
                      </Button>
                    }
                    maxLength={2048}
                    placeholder={
                      resolvedTarget.kind === 'cluster'
                        ? podNames?.length
                          ? '搜索所选 Pod 的日志内容'
                          : '搜索当前命名空间全部 Pod 的日志内容'
                        : '搜索日志内容'
                    }
                    prefix={<SearchOutlined />}
                    onSearch={() => {
                      if (targetReady) form.submit()
                    }}
                  />
                </Form.Item>
              </div>
            ) : null}
          </Form>
        </Card>
      ) : null}

      <Card
        className="soha-log-results-card"
        title={!embedded && mode === 'live' ? '运行时日志' : undefined}
      >
        <Flex className="soha-log-results-toolbar" align="center" gap={8} wrap>
          {!showQueryControls ? scopeControl : null}
          <span
            aria-atomic="true"
            aria-live="polite"
            className="soha-log-connection-status"
            role="status"
          >
            <StatusTag
              value={
                mode === 'history'
                  ? snapshotQuery.isFetching
                    ? 'querying'
                    : snapshotQuery.isError
                      ? 'error'
                      : submitted
                        ? 'ready'
                        : 'idle'
                  : streamState
              }
            />
            {mode === 'live' && streamMessage ? (
              <span className="soha-log-status-announcement">{streamMessage}</span>
            ) : null}
          </span>
          <MetadataTag
            label={
              queryAccessDenied(snapshotQuery) || streamDenied
                ? '日志不可用'
                : `${entries.length} 行`
            }
            tone="blue"
          />
          {page?.coverage ? (
            <MetadataTag
              label={`来源 ${page.coverage.successfulSources}/${page.coverage.resolvedSources}`}
              tone={page.coverage.failedSources > 0 ? 'orange' : 'cyan'}
            />
          ) : null}
          {page?.truncated ? <MetadataTag label="结果已截断" tone="orange" /> : null}
          {mode === 'live' && sourceErrors > 0 ? (
            <MetadataTag label={`${sourceErrors} 个来源错误`} tone="orange" />
          ) : null}
          <span className="soha-log-toolbar-spacer" />
          <Space size={4} wrap>
            {!showQueryControls ? (
              <Button icon={<ExportOutlined />} onClick={handleOpenInCenter}>
                在日志中心打开
              </Button>
            ) : null}
            {mode === 'live' ? (
              <>
                <Text type="secondary">自动滚动</Text>
                <Switch size="small" checked={autoScroll} onChange={setAutoScroll} />
              </>
            ) : null}
            <ManagementIconButton
              aria-label="清空日志"
              icon={<DeleteOutlined />}
              tooltip="清空日志"
              onClick={() => setEntries([])}
            />
            <ManagementIconButton
              aria-label="导出当前快照"
              disabled={!submitted}
              icon={<DownloadOutlined />}
              tooltip="导出当前快照"
              onClick={handleExport}
            />
            {mode === 'history' ? null : streamEnabled ? (
              <ManagementIconButton
                aria-label="停止实时日志"
                icon={<StopOutlined />}
                tooltip="停止实时日志"
                onClick={() => {
                  setStreamEnabled(false)
                  setStreamState('ended')
                  setStreamMessage('实时会话已手动停止。')
                }}
              />
            ) : (
              <ManagementIconButton
                aria-label="重新连接实时日志"
                icon={<ReloadOutlined />}
                tooltip="获取新 ticket 并重新连接"
                onClick={handleReconnect}
              />
            )}
          </Space>
        </Flex>

        {mode === 'live' && streamMessage ? (
          <Alert
            className="soha-log-inline-alert"
            showIcon
            type={streamState === 'degraded' ? 'warning' : 'info'}
            title={streamMessage}
          />
        ) : null}
        {page?.partial || (page?.warnings?.length ?? 0) > 0 ? (
          <Alert
            className="soha-log-inline-alert"
            showIcon
            type="warning"
            title="部分日志来源未能读取"
            description="已保留成功来源的结果；请检查对应 Pod、容器和集群连接状态。"
          />
        ) : null}
        <ReadFeedback
          label="实时日志连接"
          query={{
            data: undefined,
            error: streamError,
            isError: Boolean(streamError),
            isFetching: streamState === 'connecting',
            refetch: handleReconnect,
          }}
        />
        <ReadFeedback label="日志查询" query={snapshotQuery} />
        {snapshotQuery.error instanceof ApiError &&
        [422, 503].includes(snapshotQuery.error.status) ? (
          <Text type="secondary">
            范围/能力提示：{queryErrorDescription(snapshotQuery.error, mode)}
          </Text>
        ) : null}

        {mode === 'history' ? (
          <div className={`soha-log-results-explorer${entries.length === 0 ? ' is-empty' : ''}`}>
            {entries.length > 0 ? (
              <aside className="soha-log-results-fields" aria-label="日志字段">
                <Text strong>显示字段</Text>
                <Checkbox.Group
                  value={visibleLogFields}
                  onChange={(fields) =>
                    setVisibleLogFields([
                      ...fields.filter((field) => field !== 'message'),
                      'message',
                    ])
                  }
                >
                  {logFieldOptions.map((field) => (
                    <Checkbox
                      disabled={field.value === 'message'}
                      key={field.value}
                      value={field.value}
                    >
                      {field.label}
                    </Checkbox>
                  ))}
                </Checkbox.Group>
              </aside>
            ) : null}
            <div
              className="soha-log-results-table"
              role="region"
              aria-label="日志查询结果"
              onScroll={handleHistoryScroll}
            >
              {snapshotQuery.isLoading && entries.length === 0 ? (
                <ManagementState bordered={false} compact kind="loading" />
              ) : entries.length === 0 ? (
                <div className="soha-log-result-empty">
                  {submitted ? '当前时间范围暂无日志。' : '设置范围后查询日志。'}
                </div>
              ) : (
                <>
                  <div className="soha-log-result-header">
                    <span className="soha-log-result-disclosure" />
                    {visibleLogFields.map((field) => (
                      <span className={`soha-log-field is-${logFieldClassName(field)}`} key={field}>
                        {logFieldLabel(field)}
                      </span>
                    ))}
                  </div>
                  <div className="soha-log-result-rows">
                    {entries.map((entry) => (
                      <details
                        className="soha-log-result-row"
                        key={entry.timestamp + formatLogSource(entry) + entry.message}
                      >
                        <summary>
                          <span className="soha-log-result-disclosure" aria-hidden="true">
                            <RightOutlined />
                          </span>
                          {visibleLogFields.map((field) => (
                            <span
                              className={`soha-log-field is-${logFieldClassName(field)} ${
                                field === 'severity' ? logSeverityClassName(entry.severity) : ''
                              }`}
                              key={field}
                              title={logFieldValue(entry, field)}
                            >
                              {logFieldValue(entry, field)}
                            </span>
                          ))}
                        </summary>
                        <div className="soha-log-result-detail">
                          {entry.traceId ? (
                            <Button
                              icon={<LinkOutlined />}
                              size="small"
                              type="link"
                              onClick={() => {
                                const next = new URLSearchParams(searchParams)
                                next.delete('signal')
                                next.delete('compare')
                                next.set('traceId', entry.traceId ?? '')
                                if (entry.spanId) next.set('spanId', entry.spanId)
                                else next.delete('spanId')
                                navigate(`/monitoring-workbench/traces?${next.toString()}`)
                              }}
                            >
                              查看关联链路
                            </Button>
                          ) : null}
                          <pre>{entry.message}</pre>
                          <dl>
                            {logEntryContext(entry).map(([label, value]) => (
                              <div key={label}>
                                <dt>{label}</dt>
                                <dd>{value}</dd>
                              </div>
                            ))}
                          </dl>
                        </div>
                      </details>
                    ))}
                  </div>
                  {loadingMore ? (
                    <div className="soha-log-results-end" role="status">
                      <Spin size="small" />
                      <Text type="secondary">正在加载更多日志</Text>
                    </div>
                  ) : submitted && !page?.nextCursor ? (
                    <div className="soha-log-results-end">
                      <Text type="secondary">已加载全部日志</Text>
                    </div>
                  ) : null}
                </>
              )}
            </div>
          </div>
        ) : (
          <div ref={scrollerRef} className="soha-aggregate-log-shell">
            {snapshotQuery.isLoading && entries.length === 0 ? (
              <ManagementState bordered={false} compact kind="loading" />
            ) : entries.length === 0 ? (
              <div className="soha-aggregate-log-empty">
                {submitted
                  ? '当前范围暂无日志，实时连接会继续等待新内容。'
                  : '设置范围后开始查询实时日志。'}
              </div>
            ) : (
              entries.map((entry) => (
                <div
                  className="soha-aggregate-log-row"
                  key={entry.timestamp + formatLogSource(entry) + entry.message}
                >
                  <span className="soha-aggregate-log-time">{formatDateTime(entry.timestamp)}</span>
                  <span className="soha-aggregate-log-source">{formatLogSource(entry)}</span>
                  <span className="soha-aggregate-log-message">{entry.message}</span>
                </div>
              ))
            )}
          </div>
        )}
      </Card>
    </div>
  )
}
