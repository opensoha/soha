import { AdminTable } from '@/components/admin-table'
import {
  ManagementIconButton,
  ManagementQueryField,
  ManagementState,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useAIPageContext } from '@/features/copilot'
import { ApiError } from '@/services/api-error'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { FileSearchOutlined } from '@ant-design/icons'
import type {
  ObservabilityTraceQueryInput,
  ObservabilityTraceSpan,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Form, Input, InputNumber, Typography } from 'antd'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { signalSearchParams, traceWaterfallRows } from './model'
import { observabilitySignalQueries } from './queries'
import { SignalQueryForm, SignalState, traceInput, type SignalFilters } from './shared'

const { Text } = Typography

function TraceWaterfall({ spans, traceId }: { spans: ObservabilityTraceSpan[]; traceId: string }) {
  const rows = traceWaterfallRows(spans, traceId)
  return (
    <div aria-label={`Trace ${traceId} Span 瀑布`} className="soha-trace-waterfall" role="list">
      {rows.map(({ leftPercent, span, widthPercent }) => (
        <div
          aria-label={`${span.service} ${span.operation} ${span.durationMs.toFixed(1)} ms${span.error ? ' 错误' : ' 正常'}`}
          className="soha-trace-waterfall-row"
          key={span.spanId}
          role="listitem"
          tabIndex={0}
        >
          <div className="soha-trace-waterfall-label" title={`${span.service} / ${span.operation}`}>
            <Text strong>{span.service}</Text>
            <Text type="secondary">{span.operation}</Text>
          </div>
          <div className="soha-trace-waterfall-track">
            <div
              className={`soha-trace-waterfall-bar${span.error ? ' is-error' : ''}`}
              style={{ left: `${leftPercent}%`, width: `${widthPercent}%` }}
              title={`${span.durationMs.toFixed(1)} ms`}
            />
          </div>
          <Text className="soha-trace-waterfall-duration" type="secondary">
            {span.durationMs.toFixed(1)} ms
          </Text>
        </div>
      ))}
    </div>
  )
}

export function ObservabilityTracesPage({ embedded = false }: { embedded?: boolean } = {}) {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const { clusterId, namespace } = usePlatformScopeStore()
  const requestedClusterId = searchParams.get('cluster') || clusterId
  const requestedNamespace = searchParams.get('namespace') || namespace
  const [form] = Form.useForm<SignalFilters>()
  const [request, setRequest] = useState<ObservabilityTraceQueryInput>()
  const traces = useQuery(observabilitySignalQueries.traces(request))
  const result = readableQueryData(traces)
  const runTraces = useCallback(
    (input: ObservabilityTraceQueryInput) => {
      if (request && JSON.stringify(input) === JSON.stringify(request)) void traces.refetch()
      else setRequest(input)
    },
    [request, traces.refetch],
  )
  const appliedSearch = useRef<string | null>(null)
  const columns: TableColumnsType<ObservabilityTraceSpan> = [
    {
      title: '开始时间',
      dataIndex: 'startTime',
      key: 'startTime',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
    { title: '服务', dataIndex: 'service', key: 'service', width: 180 },
    { title: '操作', dataIndex: 'operation', key: 'operation' },
    {
      title: '耗时',
      dataIndex: 'durationMs',
      key: 'durationMs',
      width: 110,
      render: (value: number) => `${value.toFixed(1)} ms`,
    },
    {
      title: '状态',
      dataIndex: 'error',
      key: 'error',
      width: 90,
      render: (value: boolean) => (
        <StatusTag label={value ? '错误' : '正常'} value={value ? 'error' : 'normal'} />
      ),
    },
    {
      key: 'actions',
      render: (_, span) => (
        <ManagementIconButton
          aria-label="查看关联日志"
          icon={<FileSearchOutlined />}
          tooltip="查看关联日志"
          onClick={() => {
            const next = signalSearchParams(searchParams, {
              signal: undefined,
              compare: undefined,
              dataSourceId: undefined,
              metricKey: undefined,
              minDurationMs: undefined,
              limit: undefined,
              traceId: span.traceId,
              spanId: span.spanId,
            })
            navigate(`/monitoring-workbench/logs?${next.toString()}`)
          }}
        />
      ),
    },
  ]
  useAIPageContext(
    {
      sourceWorkbench: 'monitoring',
      sourceTitle: '链路调查',
      entityKind: 'monitoring.signal.traces',
      entityName: searchParams.get('traceId') || searchParams.get('service') || '链路',
      clusterId: requestedClusterId || undefined,
      namespace: requestedNamespace || undefined,
      service: searchParams.get('service') || undefined,
      workload: searchParams.get('workload') || undefined,
      visibleFilters: {
        dataSourceId: searchParams.get('dataSourceId') || undefined,
        traceId: searchParams.get('traceId') || undefined,
        minDurationMs: searchParams.get('minDurationMs') || undefined,
        limit: searchParams.get('limit') || undefined,
        timeFrom: searchParams.get('from') || undefined,
        timeTo: searchParams.get('to') || undefined,
      },
      pinnedData: result
        ? { serviceCount: result.services.length, spanCount: result.spans.length }
        : undefined,
      promptHint: '分析当前 Trace 的慢 Span、错误路径和关联日志，并给出可打开的证据。',
    },
    !embedded,
  )

  useEffect(() => {
    const signature = searchParams.toString()
    if (appliedSearch.current === signature) return
    appliedSearch.current = signature
    const from = Date.parse(searchParams.get('from') ?? '')
    const to = Date.parse(searchParams.get('to') ?? '')
    if (
      !searchParams.get('traceId') ||
      !Number.isFinite(from) ||
      !Number.isFinite(to) ||
      from >= to
    ) {
      setRequest(undefined)
      return
    }
    const values: SignalFilters = {
      ...form.getFieldsValue(true),
      traceId: searchParams.get('traceId') ?? undefined,
      minDurationMs: Number(searchParams.get('minDurationMs') || 0),
      limit: Number(searchParams.get('limit') || 100),
      dataSourceId: searchParams.get('dataSourceId') ?? undefined,
      service: searchParams.get('service') ?? undefined,
      workload: searchParams.get('workload') ?? undefined,
      timeFrom: searchParams.get('from') ?? undefined,
      timeTo: searchParams.get('to') ?? undefined,
    }
    form.setFieldsValue(values)
    setRequest(traceInput(values, requestedClusterId, requestedNamespace))
  }, [form, requestedClusterId, requestedNamespace, searchParams])

  return (
    <div className={`${embedded ? '' : 'soha-page '}soha-signal-page`}>
      <SignalQueryForm
        form={form}
        initialValues={{
          dataSourceId: searchParams.get('dataSourceId') ?? undefined,
          service: searchParams.get('service') ?? undefined,
          traceId: searchParams.get('traceId') ?? undefined,
          workload: searchParams.get('workload') ?? undefined,
          timeFrom: searchParams.get('from') ?? undefined,
          timeTo: searchParams.get('to') ?? undefined,
        }}
        loading={traces.isFetching}
        submitLabel="查询链路"
        onFinish={(values) => {
          const input = traceInput(values, requestedClusterId, requestedNamespace)
          const next = signalSearchParams(searchParams, {
            dataSourceId: input.dataSourceId,
            cluster: requestedClusterId,
            namespace: requestedNamespace,
            service: values.service,
            workload: values.workload,
            traceId: values.traceId,
            from: input.timeFrom,
            to: input.timeTo,
          })
          appliedSearch.current = next.toString()
          setSearchParams(next, { replace: true })
          runTraces(input)
        }}
      >
        <ManagementQueryField width={240} label="Trace ID" name="traceId">
          <Input allowClear placeholder="精确 Trace ID" />
        </ManagementQueryField>
        <ManagementQueryField width={240} label="最小耗时 (ms)" name="minDurationMs">
          <InputNumber min={0} max={3_600_000} step={100} style={{ width: '100%' }} />
        </ManagementQueryField>
        <ManagementQueryField width={240} label="最大 Span" name="limit">
          <InputNumber min={1} max={500} style={{ width: '100%' }} />
        </ManagementQueryField>
      </SignalQueryForm>
      <ReadFeedback label="链路查询" query={traces} />
      {traces.error instanceof ApiError && traces.error.status === 404 ? (
        <Text type="secondary">请确认链路查询 Provider 和链接指定的数据源仍可用。</Text>
      ) : null}
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
      {request ? (
        <SignalState error={null} idle={false} loading={traces.isFetching && !result} />
      ) : (
        <ManagementState
          bordered={false}
          compact
          description="设置范围后开始查询。OpenTelemetry 负责采集与导出，链路查询需要可用的查询 Provider。"
        />
      )}
      {result ? (
        <AdminTable
          enableDensity
          refreshing={traces.isFetching}
          onRefresh={() => {
            if (request) void traces.refetch()
          }}
          columnSettingIconOnly
          columnSettingPlacement="header"
          columns={columns}
          dataSource={result.spans}
          empty={<ManagementState bordered={false} compact description="当前范围暂无链路" />}
          expandedRowRender={(span: ObservabilityTraceSpan) => (
            <div className="soha-trace-expanded">
              <Text code>Trace {span.traceId}</Text>
              <TraceWaterfall spans={result.spans} traceId={span.traceId} />
            </div>
          )}
          pagination={false}
          rowKey={(span) => `${span.traceId}:${span.spanId}`}
          shellClassName="soha-management-table-shell"
          scroll={{ x: 'max-content' }}
        />
      ) : null}
    </div>
  )
}
