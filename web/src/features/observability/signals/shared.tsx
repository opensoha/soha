import type {
  ObservabilityMetricQueryInput,
  ObservabilityMetricKey,
  ObservabilityTraceQueryInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { SearchOutlined } from '@ant-design/icons'
import type { FormInstance } from 'antd'
import { Button, Form, Input, Select } from 'antd'
import {
  ManagementQueryPanel,
  ManagementQueryField,
  ManagementState,
} from '@/components/management-list'
import { PlatformScopeToolbar } from '@/components/platform-scope-toolbar'
import { observabilityScope } from './model'
import './styles.css'

export interface SignalFilters {
  dataSourceId?: string
  limit: number
  metricKey: ObservabilityMetricKey
  minDurationMs: number
  rangeMinutes: number
  service?: string
  timeFrom?: string
  timeTo?: string
  traceId?: string
  workload?: string
}

const timeOptions = [
  { label: '最近 15 分钟', value: 15 },
  { label: '最近 1 小时', value: 60 },
  { label: '最近 6 小时', value: 360 },
  { label: '最近 24 小时', value: 1440 },
]

export function queryTimes(rangeMinutes: number, requestedFrom?: string, requestedTo?: string) {
  const from = Date.parse(requestedFrom ?? '')
  const to = Date.parse(requestedTo ?? '')
  if (Number.isFinite(from) && Number.isFinite(to) && from < to) {
    return { timeFrom: new Date(from).toISOString(), timeTo: new Date(to).toISOString() }
  }
  const timeTo = new Date()
  return {
    timeFrom: new Date(timeTo.getTime() - rangeMinutes * 60_000).toISOString(),
    timeTo: timeTo.toISOString(),
  }
}

export function SignalQueryForm({
  children,
  form,
  initialValues,
  loading,
  onFinish,
  showWorkload = true,
  submitLabel,
}: {
  children?: React.ReactNode
  form: FormInstance<SignalFilters>
  initialValues?: Partial<SignalFilters>
  loading: boolean
  onFinish: (values: SignalFilters) => void
  showWorkload?: boolean
  submitLabel: string
}) {
  return (
    <ManagementQueryPanel
      form={form}
      initialValues={{ limit: 100, minDurationMs: 0, rangeMinutes: 15, ...initialValues }}
      onFinish={onFinish}
      actions={
        <Button htmlType="submit" icon={<SearchOutlined />} loading={loading} type="primary">
          {submitLabel}
        </Button>
      }
    >
      <Form.Item hidden name="timeFrom">
        <Input />
      </Form.Item>
      <Form.Item hidden name="timeTo">
        <Input />
      </Form.Item>
      <Form.Item hidden name="dataSourceId">
        <Input />
      </Form.Item>
      <ManagementQueryField label="服务" name="service" width={240}>
        <Input allowClear placeholder="service.name" />
      </ManagementQueryField>
      <ManagementQueryField label="时间范围" name="rangeMinutes" width={220}>
        <Select
          aria-label="时间范围"
          options={timeOptions}
          onChange={() => form.setFieldsValue({ timeFrom: undefined, timeTo: undefined })}
        />
      </ManagementQueryField>
      <div className="soha-management-query-field soha-signal-scope-field">
        <PlatformScopeToolbar embedded showLabel={false} />
      </div>
      {showWorkload ? (
        <ManagementQueryField label="工作负载" name="workload" width={240}>
          <Input allowClear placeholder="可选" />
        </ManagementQueryField>
      ) : null}
      {children}
    </ManagementQueryPanel>
  )
}

export function SignalState({
  error,
  idle,
  loading,
}: {
  error: Error | null
  idle: boolean
  loading: boolean
}) {
  if (loading) return <ManagementState bordered={false} compact kind="loading" title="正在查询" />
  if (error) {
    return (
      <ManagementState
        bordered={false}
        compact
        kind="error"
        title="查询失败"
        description={error.message}
      />
    )
  }
  if (idle) return <ManagementState bordered={false} compact description="设置范围后开始查询。" />
  return null
}

export function traceInput(
  values: SignalFilters,
  clusterId: string | null,
  namespace: string | null,
): ObservabilityTraceQueryInput {
  return {
    ...queryTimes(values.rangeMinutes, values.timeFrom, values.timeTo),
    dataSourceId: values.dataSourceId?.trim() || undefined,
    limit: values.limit,
    minDurationMs: values.minDurationMs,
    traceId: values.traceId?.trim() || undefined,
    scope: observabilityScope(clusterId, namespace, values.service, values.workload),
  }
}

export function metricInput(
  values: SignalFilters,
  clusterId: string | null,
  namespace: string | null,
): ObservabilityMetricQueryInput {
  return {
    ...queryTimes(values.rangeMinutes, values.timeFrom, values.timeTo),
    dataSourceId: values.dataSourceId?.trim() || undefined,
    metricKey: values.metricKey,
    scope: observabilityScope(clusterId, namespace, values.service, values.workload),
    stepSeconds: values.rangeMinutes <= 60 ? 60 : 300,
  }
}
