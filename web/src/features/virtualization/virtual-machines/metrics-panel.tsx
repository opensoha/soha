import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Alert, Card, Select, Space, Typography, theme } from 'antd'
import { LineChart } from '@visactor/react-vchart'
import {
  buildCompactChartSpec,
  compactMetricColors,
  formatMetricValue,
  type CompactChartLine,
} from '@/components/resource-metrics-panel'
import { ManagementRefreshButton, ManagementState } from '@/components/management-list'
import { localeText, useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { ApiError } from '@/services/api-error'

import { virtualizationQueries } from '../queries'
import type { VirtualizationVMMetrics } from '../virtualization-types'

const { Text } = Typography

const VM_METRIC_COLOR_MAP: Record<string, string> = {
  cpu: compactMetricColors.cpu,
  memory: compactMetricColors.memory,
  networkRx: compactMetricColors.networkRx,
  networkTx: compactMetricColors.networkTx,
}

function vmMetricColor(key: string): string {
  return VM_METRIC_COLOR_MAP[key] ?? compactMetricColors.default
}

function VMMetricsChart({ data }: { data: VirtualizationVMMetrics }) {
  const { localeCode } = useI18n()
  const { token } = theme.useToken()
  if (!data.ready) {
    return <Alert type="info" title={data.message || '当前暂无可用指标数据'} />
  }

  const series = data.series ?? []
  if (series.length === 0) {
    return (
      <ManagementState
        bordered={false}
        compact
        title="暂无指标数据"
        description="Provider 暂未返回可展示的指标序列。"
      />
    )
  }

  return (
    <div className="soha-vm-metric-grid">
      {series.map((item) => {
        const validPoints = (item.points ?? [])
          .filter(
            (point) =>
              Number.isFinite(point.value) &&
              Number.isFinite(point.timestamp) &&
              Math.abs(point.timestamp * 1000) <= 8.64e15,
          )
          .sort((a, b) => a.timestamp - b.timestamp)
        const points = validPoints.map((point) => ({
          timestamp: new Date(point.timestamp * 1000).toISOString(),
          value: point.value,
        }))
        const lines: CompactChartLine[] = [
          {
            color: vmMetricColor(item.key),
            fill: true,
            key: item.key,
            label: item.label,
            points,
            unit: item.unit,
          },
        ]
        const latest = points.length > 0 ? points[points.length - 1].value : null
        const chartSpec = {
          ...buildCompactChartSpec(lines, item.unit, localeCode),
          background: token.colorBgContainer,
        }
        if (validPoints.every((point) => point.value >= 0)) {
          chartSpec.axes[1].min = 0
          chartSpec.axes[1].expand.min = 0
        }

        const sampledAt = points.length ? points[points.length - 1].timestamp : undefined
        return (
          <Card
            key={item.key}
            size="small"
            className="soha-vm-metric"
            classNames={{ body: 'soha-vm-metric-body' }}
          >
            <div className="soha-vm-metric-label">
              <span style={{ background: vmMetricColor(item.key) }} />
              <h2>{item.label}</h2>
              <Text type="secondary">
                {item.unit
                  ? localeText(localeCode, '最新值', 'Latest value')
                  : localeText(localeCode, '单位未提供', 'Unit unavailable')}
              </Text>
            </div>
            <div className="soha-vm-metric-value">
              {latest !== null
                ? formatMetricValue(latest, item.unit === 'percent' ? '%' : item.unit)
                : '—'}
            </div>
            <Text type="secondary">
              {localeText(localeCode, '最新采样', 'Latest sample')}：
              {sampledAt
                ? formatDateTime(sampledAt)
                : localeText(localeCode, '未采集', 'Not collected')}
            </Text>
            {points.length ? (
              <div className="soha-vm-metric-chart">
                <LineChart spec={chartSpec} />
              </div>
            ) : (
              <ManagementState
                compact
                bordered={false}
                title={localeText(localeCode, '未采集到有效样本', 'No valid samples collected')}
              />
            )}
          </Card>
        )
      })}
    </div>
  )
}

interface VMMetricsPanelProps {
  range: number
  vmId: string
  onRangeChange: (range: number) => void
}

export function VMMetricsPanel({ range, vmId, onRangeChange }: VMMetricsPanelProps) {
  const { localeCode } = useI18n()
  const { token } = theme.useToken()
  const stepSeconds = range <= 60 ? 60 : 300
  const metricsQuery = useQuery({
    ...virtualizationQueries.vmMetrics(vmId, { rangeMinutes: range, stepSeconds }, Boolean(vmId)),
    retry: false,
    refetchInterval: (query) => (query.state.status === 'error' ? false : 30_000),
  })
  const forbidden =
    metricsQuery.error instanceof ApiError && [401, 403].includes(metricsQuery.error.status)
  const readableMetrics = readableQueryData(metricsQuery)

  return (
    <Card
      size="small"
      loading={metricsQuery.isLoading}
      title={localeText(localeCode, '运行监测', 'Runtime metrics')}
      className="soha-vm-metrics-panel"
      variant="borderless"
      styles={{ body: { background: token.colorBgLayout } }}
      classNames={{ header: 'soha-vm-metrics-header', body: 'soha-vm-metrics-body' }}
      extra={
        <Space wrap>
          {/* Four fixed choices use visible option nodes for keyboard and screen-reader access. */}
          <Select
            size="small"
            virtual={false}
            aria-label={localeText(localeCode, '指标时间范围', 'Metric time range')}
            value={range}
            onChange={onRangeChange}
            style={{ width: 180 }}
            options={[
              { value: 15, label: '最近 15 分钟' },
              { value: 60, label: '最近 1 小时' },
              { value: 360, label: '最近 6 小时' },
              { value: 1440, label: '最近 24 小时' },
            ]}
          />
          <ManagementRefreshButton
            tooltip={localeText(localeCode, '刷新指标', 'Refresh metrics')}
            size="small"
            loading={metricsQuery.isFetching}
            onClick={() => void metricsQuery.refetch()}
          />
        </Space>
      }
    >
      <p className="soha-vm-metrics-source">
        {localeText(localeCode, '数据来源', 'Source')}：
        {readableMetrics?.source || localeText(localeCode, '未提供', 'Unavailable')} ·{' '}
        {localeText(localeCode, '读取时间', 'Read at')}：
        {readableMetrics && metricsQuery.dataUpdatedAt
          ? formatDateTime(new Date(metricsQuery.dataUpdatedAt).toISOString())
          : '—'}{' '}
        ·{' '}
        {localeText(
          localeCode,
          `最近 ${range} 分钟，每 30 秒更新`,
          `Last ${range} minutes, refreshes every 30 seconds`,
        )}
      </p>
      {metricsQuery.isError ? (
        <Alert
          type="warning"
          showIcon
          title={localeText(
            localeCode,
            forbidden
              ? metricsQuery.error instanceof ApiError && metricsQuery.error.status === 401
                ? '登录已失效，请重新登录'
                : '无指标访问权限'
              : metricsQuery.data
                ? '指标未更新，正在展示上次读取结果'
                : '指标读取失败',
            forbidden
              ? metricsQuery.error instanceof ApiError && metricsQuery.error.status === 401
                ? 'Session expired, please sign in again'
                : 'Metric access denied'
              : metricsQuery.data
                ? 'Metrics not updated, showing last result'
                : 'Failed to read metrics',
          )}
          description={metricsQuery.error.message}
        />
      ) : null}
      {readableMetrics?.message && readableMetrics.ready ? (
        <Alert type="info" title={readableMetrics.message} />
      ) : null}
      {readableMetrics ? (
        <VMMetricsChart data={readableMetrics} />
      ) : !metricsQuery.isError && !metricsQuery.isLoading ? (
        <ManagementState
          bordered={false}
          compact
          title="暂无指标数据"
          description="指标查询完成后这里会展示 VM 运行曲线。"
        />
      ) : null}
    </Card>
  )
}
