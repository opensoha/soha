import { Button, Card, Descriptions, Empty, Select, Space, Tabs, TabPane, Typography } from '@douyinfe/semi-ui'
import ReactECharts from 'echarts-for-react'
import { AdminTable } from '@/components/admin-table'
import { StatGrid } from '@/components/stat-grid'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { MetricSeries, MetricsSnapshot } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'

const { Text } = Typography

interface MetricPointRow {
  timestamp: string
  value: number
}

function formatBytes(value: number) {
  if (!Number.isFinite(value)) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let current = value
  let index = 0
  while (current >= 1024 && index < units.length - 1) {
    current /= 1024
    index += 1
  }
  return `${current >= 10 ? current.toFixed(0) : current.toFixed(1)} ${units[index]}`
}

function formatMetricValue(value: number, unit: string) {
  if (!Number.isFinite(value)) return '-'
  switch (unit) {
    case 'bytes':
      return formatBytes(value)
    case 'bytes/s':
      return `${formatBytes(value)}/s`
    case 'cores':
      return value >= 1 ? `${value.toFixed(2)} cores` : `${(value * 1000).toFixed(0)} mCPU`
    case 'count':
      return `${value.toFixed(0)}`
    default:
      return `${value.toFixed(2)} ${unit}`.trim()
  }
}

function summarizeSeries(series: MetricSeries) {
  const values = (series.points ?? []).map((point) => point.value).filter((value) => Number.isFinite(value))
  if (values.length === 0) {
    return {
      min: '-',
      max: '-',
      avg: '-',
      samples: 0,
    }
  }
  const min = Math.min(...values)
  const max = Math.max(...values)
  const avg = values.reduce((sum, value) => sum + value, 0) / values.length
  return {
    min: formatMetricValue(min, series.unit),
    max: formatMetricValue(max, series.unit),
    avg: formatMetricValue(avg, series.unit),
    samples: values.length,
  }
}

function buildSeriesChartOption(series: MetricSeries, localeCode: 'zh_CN' | 'en_US') {
  const points = series.points ?? []
  return {
    grid: {
      left: 56,
      right: 24,
      top: 24,
      bottom: 36,
    },
    tooltip: {
      trigger: 'axis',
      valueFormatter: (value: number) => formatMetricValue(Number(value), series.unit),
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: points.map((item) => formatDateTime(item.timestamp)),
      axisLabel: {
        hideOverlap: true,
      },
    },
    yAxis: {
      type: 'value',
      name: localeCode === 'zh_CN' ? `单位: ${series.unit}` : `Unit: ${series.unit}`,
      nameTextStyle: {
        color: 'var(--semi-color-text-2)',
      },
      axisLabel: {
        formatter: (value: number) => formatMetricValue(Number(value), series.unit),
      },
      splitLine: {
        lineStyle: {
          color: 'var(--semi-color-border)',
          type: 'dashed',
        },
      },
    },
    series: [
      {
        name: series.label,
        type: 'line',
        smooth: true,
        showSymbol: false,
        areaStyle: {
          opacity: 0.08,
        },
        lineStyle: {
          width: 2,
        },
        data: points.map((item) => item.value),
      },
    ],
  }
}

function getMetricsHint(message: string | undefined, localeCode: 'zh_CN' | 'en_US') {
  const normalized = (message || '').toLowerCase()
  if (!normalized) return ''
  if (normalized.includes('no such host') || normalized.includes('lookup')) {
    return localeCode === 'zh_CN'
      ? 'Prometheus 地址当前不可解析，请检查集群监控地址、DNS 或网络连通性。'
      : 'The Prometheus address cannot be resolved. Check the monitoring URL, DNS, or network reachability.'
  }
  if (normalized.includes('connection refused') || normalized.includes('timeout')) {
    return localeCode === 'zh_CN'
      ? 'Prometheus 当前不可达，请检查服务可用性和网络连通性。'
      : 'Prometheus is currently unreachable. Check service availability and network connectivity.'
  }
  return ''
}

export function ResourceMetricsPanel({
  title,
  data,
  loading,
  rangeMinutes,
  onRangeChange,
}: {
  title: string
  data?: MetricsSnapshot
  loading?: boolean
  rangeMinutes?: number
  onRangeChange?: (rangeMinutes: number) => void
}) {
  const { localeCode } = useI18n()

  if (loading) {
    return <Card className="kc-detail-card" loading />
  }

  if (!data) {
    return (
      <Card className="kc-detail-card" title={title}>
        <Empty description={localeCode === 'zh_CN' ? '暂无指标数据' : 'No metrics data'} />
      </Card>
    )
  }

  const series = data.series ?? []
  const stats = series.map((item) => ({
    label: item.label,
    value: formatMetricValue(item.latest, item.unit),
  }))
  const metricsHint = getMetricsHint(data.message, localeCode)

  const pointColumns: ColumnProps<MetricPointRow>[] = [
    {
      ...tableColumnPresets.datetime,
      title: localeCode === 'zh_CN' ? '时间' : 'Timestamp',
      dataIndex: 'timestamp',
      render: (value: string) => formatDateTime(value),
    },
    {
      title: localeCode === 'zh_CN' ? '值' : 'Value',
      dataIndex: 'value',
      render: (value: number, _record: MetricPointRow, index: number) => {
        const currentSeries = series[index]
        return currentSeries ? formatMetricValue(value, currentSeries.unit) : value
      },
    },
  ]

  return (
    <div className="kc-page-section">
      <Card
        className="kc-detail-card"
        title={title}
        headerExtraContent={(
          <Space>
            {onRangeChange ? (
              <Select
                value={String(rangeMinutes ?? data.rangeMinutes)}
                onChange={(value) => onRangeChange(Number(value))}
                style={{ width: 180 }}
                optionList={[
                  { value: '15', label: localeCode === 'zh_CN' ? '最近 15 分钟' : 'Last 15 min' },
                  { value: '60', label: localeCode === 'zh_CN' ? '最近 1 小时' : 'Last 1 hour' },
                  { value: '360', label: localeCode === 'zh_CN' ? '最近 6 小时' : 'Last 6 hours' },
                  { value: '1440', label: localeCode === 'zh_CN' ? '最近 24 小时' : 'Last 24 hours' },
                ]}
              />
            ) : null}
            {data.grafanaBaseUrl ? (
              <Button theme="light" type="primary" onClick={() => window.open(data.grafanaBaseUrl, '_blank', 'noopener,noreferrer')}>
                {localeCode === 'zh_CN' ? '打开 Grafana' : 'Open Grafana'}
              </Button>
            ) : null}
          </Space>
        )}
      >
        <Descriptions
          data={[
            { key: localeCode === 'zh_CN' ? '状态' : 'Status', value: data.configured ? (localeCode === 'zh_CN' ? '已配置' : 'Configured') : (localeCode === 'zh_CN' ? '未配置' : 'Not configured') },
            { key: localeCode === 'zh_CN' ? '来源' : 'Source', value: data.source || '-' },
            { key: localeCode === 'zh_CN' ? '生成时间' : 'Generated At', value: formatDateTime(data.generatedAt) },
            { key: localeCode === 'zh_CN' ? '查询范围' : 'Range', value: `${data.rangeMinutes} min` },
            { key: localeCode === 'zh_CN' ? '采样步长' : 'Step', value: `${data.stepSeconds}s` },
          ]}
        />
        {data.message ? (
          <div style={{ marginTop: 12 }}>
            <Text type="tertiary" size="small" style={{ display: 'block' }}>
              {data.message}
            </Text>
            {metricsHint ? (
              <Text size="small" style={{ display: 'block', marginTop: 6, color: 'var(--semi-color-warning)' }}>
                {metricsHint}
              </Text>
            ) : null}
          </div>
        ) : null}
      </Card>

      {series.length > 0 ? (
        <>
          <StatGrid items={stats} />
          <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '时序明细' : 'Series Detail'}>
            <Tabs type="card">
              {series.map((item) => {
                const summary = summarizeSeries(item)
                const rows = [...(item.points ?? [])]
                  .slice(-20)
                  .reverse()
                  .map((point) => ({
                    timestamp: point.timestamp,
                    value: point.value,
                  }))

                const columns: ColumnProps<MetricPointRow>[] = [
                  pointColumns[0],
                  {
                    title: localeCode === 'zh_CN' ? '值' : 'Value',
                    dataIndex: 'value',
                    render: (value: number) => formatMetricValue(value, item.unit),
                  },
                ]

                const chartOption = buildSeriesChartOption(item, localeCode)

                return (
                  <TabPane tab={item.label} itemKey={item.key} key={item.key}>
                    <Descriptions
                      data={[
                        { key: localeCode === 'zh_CN' ? '最新值' : 'Latest', value: formatMetricValue(item.latest, item.unit) },
                        { key: localeCode === 'zh_CN' ? '最小值' : 'Min', value: summary.min },
                        { key: localeCode === 'zh_CN' ? '最大值' : 'Max', value: summary.max },
                        { key: localeCode === 'zh_CN' ? '平均值' : 'Average', value: summary.avg },
                        { key: localeCode === 'zh_CN' ? '样本数' : 'Samples', value: summary.samples },
                      ]}
                    />
                    <div style={{ marginTop: 16 }}>
                      <ReactECharts option={chartOption} style={{ height: 280 }} notMerge lazyUpdate />
                    </div>
                    <div style={{ marginTop: 16 }}>
                      <AdminTable
                        columns={columns}
                        dataSource={rows}
                        rowKey={(record) => record.timestamp}
                        pageSize={10}
                        enableColumnSelection={false}
                      />
                    </div>
                  </TabPane>
                )
              })}
            </Tabs>
          </Card>
        </>
      ) : (
        <Card className="kc-detail-card">
          <Empty description={metricsHint || data.message || (localeCode === 'zh_CN' ? '当前范围没有可展示的指标序列' : 'No metrics series available for the current range')} />
        </Card>
      )}
    </div>
  )
}
