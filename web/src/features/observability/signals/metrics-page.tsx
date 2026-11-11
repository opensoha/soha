import { ManagementQueryField, ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import {
  buildCompactChartSpec,
  compactMetricColors,
  formatMetricValue,
  type CompactChartLine,
} from '@/components/resource-metrics-panel'
import { useAIPageContext } from '@/features/copilot'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import type {
  ObservabilityMetricKey,
  ObservabilityMetricQueryInput,
  ObservabilityMetricSeries,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery } from '@tanstack/react-query'
import { LineChart } from '@visactor/react-vchart'
import { Card, Form, Select, Typography } from 'antd'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { signalSearchParams } from './model'
import { observabilitySignalQueries } from './queries'
import { metricInput, SignalQueryForm, SignalState, type SignalFilters } from './shared'

const { Text } = Typography

const metricLabels: Record<ObservabilityMetricKey, string> = {
  cpu_usage: 'CPU 使用率',
  memory_usage: '内存使用率',
  restart_rate: '重启率',
  error_rate: '错误率',
  latency_p95: 'P95 延迟',
}

export function ObservabilityMetricsPage({ embedded = false }: { embedded?: boolean } = {}) {
  const { clusterId, namespace } = usePlatformScopeStore()
  const [searchParams, setSearchParams] = useSearchParams()
  const [form] = Form.useForm<SignalFilters>()
  const catalog = useQuery(observabilitySignalQueries.metricCatalog())
  const [request, setRequest] = useState<ObservabilityMetricQueryInput>()
  const metrics = useQuery(observabilitySignalQueries.metrics(request))
  const result = readableQueryData(metrics)
  const runMetrics = useCallback(
    (input: ObservabilityMetricQueryInput) => {
      if (request && JSON.stringify(input) === JSON.stringify(request)) void metrics.refetch()
      else setRequest(input)
    },
    [request, metrics.refetch],
  )
  const appliedSearch = useRef<string | null>(null)
  const requestedClusterId = searchParams.get('cluster') || clusterId
  const requestedNamespace = searchParams.get('namespace') || namespace
  const requestedMetricKey = searchParams.get('metricKey')
  const initialMetricKey =
    requestedMetricKey && Object.prototype.hasOwnProperty.call(metricLabels, requestedMetricKey)
      ? (requestedMetricKey as ObservabilityMetricKey)
      : 'cpu_usage'
  const metricOptions = (readableQueryData(catalog) ?? []).map((item) => ({
    disabled: !item.available,
    label: metricLabels[item.key as ObservabilityMetricKey] ?? item.label,
    value: item.key as ObservabilityMetricKey,
  }))
  useAIPageContext(
    {
      sourceWorkbench: 'monitoring',
      sourceTitle: '指标调查',
      entityKind: 'monitoring.signal.metrics',
      entityName: metricLabels[initialMetricKey],
      clusterId: requestedClusterId || undefined,
      namespace: requestedNamespace || undefined,
      service: searchParams.get('service') || undefined,
      workload: searchParams.get('workload') || undefined,
      visibleFilters: {
        dataSourceId: searchParams.get('dataSourceId') || undefined,
        metricKey: initialMetricKey,
        timeFrom: searchParams.get('from') || undefined,
        timeTo: searchParams.get('to') || undefined,
      },
      pinnedData: result ? { seriesCount: result.series.length } : undefined,
      promptHint: '分析当前指标的趋势、异常时间段、影响范围，并给出可打开的关联证据。',
    },
    !embedded,
  )

  function submit(values: SignalFilters) {
    const input = metricInput(values, requestedClusterId, requestedNamespace)
    const next = signalSearchParams(searchParams, {
      dataSourceId: input.dataSourceId,
      cluster: requestedClusterId,
      namespace: requestedNamespace,
      service: values.service,
      workload: values.workload,
      metricKey: values.metricKey,
      from: input.timeFrom,
      to: input.timeTo,
    })
    appliedSearch.current = next.toString()
    setSearchParams(next, { replace: true })
    runMetrics(input)
  }

  useEffect(() => {
    const signature = searchParams.toString()
    if (appliedSearch.current === signature) return
    appliedSearch.current = signature
    const from = Date.parse(searchParams.get('from') ?? '')
    const to = Date.parse(searchParams.get('to') ?? '')
    if (!requestedMetricKey || !Number.isFinite(from) || !Number.isFinite(to) || from >= to) {
      setRequest(undefined)
      return
    }
    const values: SignalFilters = {
      ...form.getFieldsValue(true),
      metricKey: initialMetricKey,
      dataSourceId: searchParams.get('dataSourceId') ?? undefined,
      service: searchParams.get('service') ?? undefined,
      workload: searchParams.get('workload') ?? undefined,
      timeFrom: searchParams.get('from') ?? undefined,
      timeTo: searchParams.get('to') ?? undefined,
    }
    form.setFieldsValue(values)
    setRequest(metricInput(values, requestedClusterId, requestedNamespace))
  }, [
    form,
    requestedClusterId,
    requestedNamespace,
    searchParams,
    initialMetricKey,
    requestedMetricKey,
  ])

  return (
    <div className={`${embedded ? '' : 'soha-page '}soha-signal-page`}>
      <SignalQueryForm
        form={form}
        initialValues={{
          dataSourceId: searchParams.get('dataSourceId') ?? undefined,
          metricKey: initialMetricKey,
          service: searchParams.get('service') ?? undefined,
          workload: searchParams.get('workload') ?? undefined,
          timeFrom: searchParams.get('from') ?? undefined,
          timeTo: searchParams.get('to') ?? undefined,
        }}
        loading={metrics.isFetching}
        submitLabel="查询指标"
        onFinish={submit}
      >
        <ManagementQueryField width={240} label="指标" name="metricKey">
          <Select loading={catalog.isLoading} options={metricOptions} />
        </ManagementQueryField>
      </SignalQueryForm>
      <ReadFeedback label="指标目录" query={catalog} />
      <ReadFeedback label="指标查询" query={metrics} />
      {request ? (
        <Text type="secondary">
          查询范围：
          {[
            request.dataSourceId,
            request.scope?.clusterId,
            request.scope?.namespace,
            request.scope?.service,
          ]
            .filter(Boolean)
            .join(' / ') || '默认来源'}{' '}
          · {formatDateTime(request.timeFrom)} — {formatDateTime(request.timeTo)}
        </Text>
      ) : null}
      <SignalState error={null} idle={!request} loading={metrics.isFetching && !result} />
      {result ? (
        <div className="soha-signal-metric-grid">
          {result.series.map((series: ObservabilityMetricSeries) => {
            const lines: CompactChartLine[] = [
              {
                color: compactMetricColors.default,
                fill: true,
                key: series.key,
                label: series.label,
                points: series.points,
                unit: series.unit ?? '',
              },
            ]
            return (
              <Card
                className="soha-signal-metric-card"
                key={series.key}
                size="small"
                title={series.label}
                extra={
                  <Text type="secondary">
                    最新 {formatMetricValue(series.latest, series.unit ?? '')}
                  </Text>
                }
              >
                <Text className="soha-signal-metric-summary" type="secondary">
                  区间共 {series.points.length} 个数据点
                </Text>
                <div aria-hidden="true" className="soha-signal-metric-chart">
                  <LineChart
                    spec={{
                      ...buildCompactChartSpec(lines, series.unit ?? '', 'zh_CN'),
                      background: 'transparent',
                    }}
                  />
                </div>
                <MetricSeriesDataDetails series={series} />
              </Card>
            )
          })}
          {result.series.length === 0 ? (
            <ManagementState bordered={false} compact description="当前范围暂无指标数据" />
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

function MetricSeriesDataDetails({ series }: { series: ObservabilityMetricSeries }) {
  const [visible, setVisible] = useState(false)
  return (
    <details
      className="soha-signal-data-details"
      onToggle={(event) => event.currentTarget.open && setVisible(true)}
    >
      <summary>查看数据表</summary>
      {visible ? (
        <div className="soha-signal-data-table-wrap">
          <table aria-label={`${series.label}数据表`}>
            <thead>
              <tr>
                <th scope="col">时间</th>
                <th scope="col">数值</th>
              </tr>
            </thead>
            <tbody>
              {series.points.map((point, index) => (
                <tr key={`${point.timestamp}:${index}`}>
                  <td>{formatDateTime(point.timestamp)}</td>
                  <td>{formatMetricValue(point.value, series.unit ?? '')}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </details>
  )
}
