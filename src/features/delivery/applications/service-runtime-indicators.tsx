import { useId } from 'react'
import { Button } from 'antd'
import { useQueries, type QueryFunctionContext } from '@tanstack/react-query'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { podQueries } from '@/features/platform'
import { formatMetricValue } from '@/components/resource-metrics-format'
import { requestErrorMessage, useInlineApiError } from '@/components/request-feedback'
import { localeText, useI18n } from '@/i18n'
import { toScopeKey } from '@/types'
import type { ResourceMetrics } from '@/types'
import { deliveryQueries } from '../queries'
import type { ApplicationRuntimeWorkload, Pod } from '../types'
import './service-runtime-indicators.css'

const MAX_SOURCES = 4
const MAX_METRIC_PODS = 8
const workloadKey = (item: ApplicationRuntimeWorkload) =>
  JSON.stringify([
    item.applicationEnvironmentId,
    item.clusterId,
    item.namespace,
    item.workloadKind,
    item.workloadName,
  ])
const readableSource = (item: ApplicationRuntimeWorkload) =>
  !item.sourceStatus || ['linked', 'changed'].includes(item.sourceStatus)

type PodRow = { pod: Pod; clusterId: string }

export function ServiceRuntimeIndicators({
  applicationId,
  workloads,
  podWorkloads,
  enabled,
  onOpenPods,
  onOpenMetrics,
}: {
  applicationId: string
  workloads: ApplicationRuntimeWorkload[]
  podWorkloads: ApplicationRuntimeWorkload[]
  enabled: boolean
  onOpenPods: () => void
  onOpenMetrics: () => void
}) {
  const verificationMount = useId()
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => localeText(localeCode, zh, en)
  const permissionQuery = usePermissionSnapshot()
  const permissions = permissionQuery.data?.data
  const allowed =
    enabled &&
    !permissionQuery.isError &&
    hasPermission(permissions, 'delivery.applications.view') &&
    hasPermission(permissions, 'platform.pods.view')
  const all = [...new Map(workloads.map((item) => [workloadKey(item), item])).values()]
  // shortcut: bound first-screen reads; larger services show explicit coverage until a batch API exists.
  const sources = [...new Map(podWorkloads.map((item) => [workloadKey(item), item])).values()]
    .filter(
      (item) =>
        item.workloadKind === 'Deployment' &&
        readableSource(item) &&
        all.some(
          (candidate) => workloadKey(candidate) === workloadKey(item) && readableSource(candidate),
        ),
    )
    .slice(0, MAX_SOURCES)
  const runtime = useQueries({
    queries: sources.map((workload) => ({
      ...deliveryQueries.workloads.runtime(
        {
          applicationId,
          applicationEnvironmentId: workload.applicationEnvironmentId,
          workloadName: workload.workloadName,
        },
        allowed,
        30_000,
      ),
      staleTime: 0,
      refetchOnMount: 'always' as const,
      refetchIntervalInBackground: false,
    })),
  })
  // Shared runtime polling must retain verified Pods rather than remount their metric observers.
  const snapshots = runtime.flatMap((query, index) => {
    const value = query.data
    const source = sources[index]
    return allowed &&
      query.isSuccess &&
      query.isFetchedAfterMount &&
      value &&
      value.workload &&
      readableSource(value.workload) &&
      workloadKey(value.workload) === workloadKey(source) &&
      Array.isArray(value.pods)
      ? [value]
      : []
  })
  const rows: PodRow[] = [
    ...new Map(
      snapshots
        .flatMap((snapshot) =>
          (snapshot.pods ?? []).map((pod) => ({ pod, clusterId: snapshot.workload.clusterId })),
        )
        .map((row) => [
          JSON.stringify([row.clusterId, row.pod.namespace, row.pod.name, row.pod.uid]),
          row,
        ]),
    ).values(),
  ]
  const complete =
    all.length > 0 && all.length === sources.length && snapshots.length === all.length
  const sampledRows = rows.filter(({ pod }) => Boolean(pod.uid)).slice(0, MAX_METRIC_PODS)
  const metrics = useQueries({
    queries: sampledRows.map(({ pod, clusterId }) => {
      const scope = toScopeKey(clusterId, pod.namespace)
      const options = podQueries.metrics(scope, pod.name, 60, pod.uid)
      const detail = podQueries.detail(scope, pod.name)
      return {
        ...options,
        // Isolate verified responses from name-based metrics observers and their cache writes.
        queryKey: [...options.queryKey, 'service-runtime-verified', verificationMount],
        queryFn: async (context: QueryFunctionContext) => {
          const readDetail = detail.queryFn
          const readMetrics = options.queryFn
          if (typeof readDetail !== 'function' || typeof readMetrics !== 'function') {
            throw new Error('Pod metric readers unavailable')
          }
          const verify = async () => {
            if (context.signal.aborted) throw new Error('Pod metric observation cancelled')
            const current = await readDetail({ ...context, queryKey: detail.queryKey })
            if (context.signal.aborted) throw new Error('Pod metric observation cancelled')
            if (
              current.uid !== pod.uid ||
              current.name !== pod.name ||
              current.namespace !== pod.namespace
            ) {
              throw new Error(
                text(
                  '读取指标期间 Pod 身份已变化，请刷新后重试',
                  'Pod identity changed during metric observation. Refresh and retry.',
                ),
              )
            }
          }
          // At most 8 metric reads and 16 fresh detail GETs per round; these GETs are not atomic.
          await verify()
          const reading = await readMetrics({ ...context, queryKey: options.queryKey })
          await verify()
          if (reading.configured && reading.message?.trim()) throw new Error(reading.message)
          return reading
        },
        enabled: allowed && Boolean(options.enabled) && Boolean(detail.enabled),
        staleTime: 0,
        gcTime: 0,
        retry: false,
        refetchOnMount: 'always' as const,
        refetchInterval: 30_000,
        refetchIntervalInBackground: false,
      }
    }),
  })
  const readings = metrics.map((query) =>
    allowed && query.isSuccess && query.isFetchedAfterMount ? query.data : undefined,
  )
  const metricsFailed = metrics.some((query) => query.isError)
  const loading = runtime.some((query) => query.isFetching)
  const failed = runtime.some((query) => query.isError)
  const localError = [...runtime, ...metrics].find((query) => query.isError)?.error
  useInlineApiError(allowed ? localError : null)
  const scopeNote = !allowed
    ? text('无查看权限', 'No permission')
    : loading
      ? snapshots.length
        ? text('刷新中', 'Refreshing')
        : text('读取中', 'Loading')
      : failed
        ? text('运行数据读取失败', 'Runtime unavailable')
        : complete
          ? text('当前 Pod 快照', 'Current Pod snapshot')
          : text(
              '待观测 · 此来源暂无完整 Pod 摘要',
              'Awaiting observation · no complete Pod summary',
            )
  const ready = rows.filter(({ pod }) => {
    const [count, total] = (pod.readyContainers || '').split('/').map(Number)
    return pod.phase === 'Running' && total > 0 && count === total
  }).length
  const restartsKnown = rows.every(({ pod }) => Number.isFinite(pod.restarts) && pod.restarts >= 0)
  function metric(key: string, unit: string) {
    const values = readings.flatMap((reading: ResourceMetrics | undefined) => {
      const series = reading?.configured
        ? reading.series?.find((item) => item.key === key && item.unit === unit)
        : undefined
      return series && Number.isFinite(series.latest) && series.latest >= 0 ? [series.latest] : []
    })
    const full = complete && rows.length > 0 && values.length === rows.length
    const unconfigured =
      readings.length > 0 && readings.every((reading) => reading && !reading.configured)
    return {
      value: full
        ? formatMetricValue(
            values.reduce((sum, value) => sum + value, 0),
            unit,
          )
        : '—',
      note:
        !allowed || !rows.length
          ? scopeNote
          : unconfigured
            ? text('未接入监控', 'Monitoring not configured')
            : `${metricsFailed ? text('指标读取失败 · ', 'Metrics unavailable · ') : ''}${text('指标覆盖', 'Metric coverage')} ${values.length}/${rows.length} Pod${complete ? '' : text(' · 部分来源', ' · partial sources')}${rows.length > MAX_METRIC_PODS ? text(' · 首屏采样上限 8 Pod', ' · first-screen sample capped at 8 Pods') : ''}`,
    }
  }
  const cpu = metric('cpu', 'cores')
  const memory = metric('memory', 'bytes')
  const items = [
    {
      label: text('Pod 就绪', 'Ready Pods'),
      value: complete ? `${ready}/${rows.length}` : '—',
      note: scopeNote,
      open: onOpenPods,
    },
    {
      label: text('累计重启', 'Total restarts'),
      value:
        complete && restartsKnown
          ? String(rows.reduce((sum, { pod }) => sum + pod.restarts, 0))
          : '—',
      note:
        complete && !restartsKnown ? text('重启数据不完整', 'Restart data incomplete') : scopeNote,
      open: onOpenPods,
    },
    { label: 'CPU', ...cpu, open: onOpenMetrics },
    { label: text('内存', 'Memory'), ...memory, open: onOpenMetrics },
  ]
  return (
    <section
      className="soha-service-runtime-indicators"
      aria-label={text('服务运行指标', 'Service runtime indicators')}
    >
      <dl>
        {items.map((item) => (
          <div className="soha-service-runtime-indicator" key={item.label}>
            <dt>
              <Button type="link" size="small" disabled={!allowed} onClick={item.open}>
                {item.label}
              </Button>
            </dt>
            <dd>
              <strong>{item.value}</strong>
              <span>{item.note}</span>
            </dd>
          </div>
        ))}
      </dl>
      {allowed && (failed || metricsFailed) ? (
        <div>
          <p role="alert">{requestErrorMessage(localError, localeCode)}</p>
          <Button
            size="small"
            onClick={() => {
              for (const query of [...runtime, ...metrics]) void query.refetch()
            }}
          >
            {text('重试运行指标', 'Retry runtime indicators')}
          </Button>
        </div>
      ) : null}
    </section>
  )
}
