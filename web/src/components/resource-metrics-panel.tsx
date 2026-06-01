import { Button, Card, Descriptions, Select, Space, Tabs, Typography } from 'antd'
import { LineChart } from '@visactor/react-vchart'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { StatGrid } from '@/components/stat-grid'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { MetricSeries, MetricsSnapshot, ResourceQuantity } from '@/types'
import type { DescriptionsProps, TableColumnsType, TabsProps } from 'antd'

const { Text } = Typography
type ColumnProps<T> = TableColumnsType<T>[number]
type TabItem = NonNullable<TabsProps['items']>[number]

const COMPACT_METRIC_CARD_HEIGHT = 300
const COMPACT_METRIC_CHART_HEIGHT = 236

export type MetricLocale = 'zh_CN' | 'en_US'

export type ChartLineStyle = 'solid' | 'dashed' | 'dotted'

export interface CompactChartLine {
  color: string
  fill?: boolean
  key: string
  label: string
  lineStyle?: ChartLineStyle
  negate?: boolean
  points?: MetricSeries['points']
  unit: string
  value?: number
}

interface CompactChartCard {
  key: string
  lines: CompactChartLine[]
  title: string
  unit: string
}

interface CompactChartRow {
  rawValue: number
  time: string
  type: string
  value: number
}

interface MetricPointRow {
  timestamp: string
  value: number
}

export const compactMetricColors: Record<string, string> = {
  connections: '#64748b',
  cpu: '#0ea5e9',
  cpuLimit: '#ef4444',
  cpuRequest: '#22c55e',
  default: '#2563eb',
  diskRead: '#0891b2',
  diskWrite: '#dc2626',
  memory: '#4f46e5',
  memoryLimit: '#ef4444',
  memoryRequest: '#22c55e',
  networkRx: '#10b981',
  networkTx: '#f97316',
}

export function formatBytes(value: number) {
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

export function formatMetricValue(value: number, unit: string) {
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
    return { min: '-', max: '-', avg: '-', samples: 0 }
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

function parseCPUQuantity(value?: string) {
  const normalized = value?.trim()
  if (!normalized) return null
  if (normalized.endsWith('m')) {
    const parsed = Number.parseFloat(normalized.slice(0, -1))
    return Number.isFinite(parsed) ? parsed / 1000 : null
  }
  const parsed = Number.parseFloat(normalized)
  return Number.isFinite(parsed) ? parsed : null
}

function parseMemoryQuantity(value?: string) {
  const normalized = value?.trim()
  if (!normalized) return null
  const match = normalized.match(/^([0-9]*\.?[0-9]+)([A-Za-z]+)?$/)
  if (!match) return null
  const amount = Number.parseFloat(match[1])
  if (!Number.isFinite(amount)) return null
  const unit = match[2] ?? ''
  const binaryUnits: Record<string, number> = {
    Ei: 1024 ** 6, Gi: 1024 ** 3, Ki: 1024, Mi: 1024 ** 2, Pi: 1024 ** 5, Ti: 1024 ** 4,
  }
  const decimalUnits: Record<string, number> = {
    E: 1000 ** 6, G: 1000 ** 3, K: 1000, M: 1000 ** 2, P: 1000 ** 5, T: 1000 ** 4,
  }
  if (unit in binaryUnits) return amount * binaryUnits[unit]
  if (unit in decimalUnits) return amount * decimalUnits[unit]
  return amount
}

function compareMetricTimestamps(left: string, right: string) {
  const leftTime = Date.parse(left)
  const rightTime = Date.parse(right)
  if (Number.isFinite(leftTime) && Number.isFinite(rightTime)) return leftTime - rightTime
  if (Number.isFinite(leftTime)) return -1
  if (Number.isFinite(rightTime)) return 1
  return left.localeCompare(right)
}

function buildResourceBaseline(
  resource: 'cpu' | 'memory',
  resourceRequests?: ResourceQuantity,
  resourceLimits?: ResourceQuantity,
) {
  const requestRaw = resource === 'cpu' ? resourceRequests?.cpu : resourceRequests?.memory
  const limitRaw = resource === 'cpu' ? resourceLimits?.cpu : resourceLimits?.memory
  const parseValue = resource === 'cpu' ? parseCPUQuantity : parseMemoryQuantity
  const unit = resource === 'cpu' ? 'cores' : 'bytes'
  const request = parseValue(requestRaw)
  const limit = parseValue(limitRaw)
  return {
    limit,
    limitDisplay: limit != null ? formatMetricValue(limit, unit) : '-',
    request,
    requestDisplay: request != null ? formatMetricValue(request, unit) : '-',
  }
}

function buildChartValues(lines: CompactChartLine[]) {
  const timestampSet = new Set<string>()
  lines.forEach((line) => {
    (line.points ?? []).forEach((point) => timestampSet.add(point.timestamp))
  })
  const timestamps = Array.from(timestampSet).sort(compareMetricTimestamps)

  const rows: CompactChartRow[] = []
  lines.forEach((line) => {
    if (line.value != null) {
      const constantValue = line.value
      const anchorTimestamps = timestamps.length > 0 ? timestamps : (line.points ?? []).map((p) => p.timestamp)
      anchorTimestamps.forEach((ts) => {
        rows.push({
          rawValue: constantValue,
          time: formatDateTime(ts),
          type: line.label,
          value: line.negate ? -Math.abs(constantValue) : constantValue,
        })
      })
    } else {
      (line.points ?? []).forEach((point) => {
        rows.push({
          rawValue: point.value,
          time: formatDateTime(point.timestamp),
          type: line.label,
          value: line.negate ? -Math.abs(point.value) : point.value,
        })
      })
    }
  })
  return rows
}

function formatAxisMetricValue(value: number, unit: string) {
  return formatMetricValue(Math.abs(value), unit)
}

function resolveMirroredAxisDomain(rows: CompactChartRow[]) {
  if (rows.length === 0) return undefined
  const maxAbs = rows.reduce((current, row) => Math.max(current, Math.abs(row.value)), 0)
  const bound = maxAbs > 0 ? maxAbs : 1
  return { max: bound, min: -bound }
}

function resolveMetricSeriesLabel(seriesKey: string, fallback: string, localeCode: MetricLocale) {
  const dictionary: Record<string, { en_US: string; zh_CN: string }> = {
    connections: { en_US: 'Connections', zh_CN: '连接数' },
    cpu: { en_US: 'CPU Usage', zh_CN: 'CPU 使用' },
    disk_read: { en_US: 'Disk Read', zh_CN: '磁盘读' },
    disk_write: { en_US: 'Disk Write', zh_CN: '磁盘写' },
    memory: { en_US: 'Memory Working Set', zh_CN: '内存工作集' },
    network_rx: { en_US: 'Network In', zh_CN: '网络入' },
    network_tx: { en_US: 'Network Out', zh_CN: '网络出' },
  }
  return dictionary[seriesKey]?.[localeCode] ?? fallback
}

export function buildCompactChartSpec(lines: CompactChartLine[], unit: string, _localeCode: MetricLocale): any {
  const values = buildChartValues(lines)
  const mirroredAxisDomain = lines.some((line) => line.negate) ? resolveMirroredAxisDomain(values) : undefined
  const colorDomain: string[] = []
  const colorRange: string[] = []
  const dashMap: Record<string, number[] | undefined> = {}
  lines.forEach((line) => {
    colorDomain.push(line.label)
    colorRange.push(line.color)
    if (line.lineStyle === 'dashed') dashMap[line.label] = [6, 4]
    else if (line.lineStyle === 'dotted') dashMap[line.label] = [2, 4]
  })

  const hasFill = lines.some((line) => line.fill)
  const showLegend = lines.length > 1

  return {
    type: 'line',
    data: { values },
    interactions: [
      {
        type: 'element-highlight-by-group',
        trigger: 'pointermove',
        triggerOff: 'pointerleave',
      },
    ],
    xField: 'time',
    yField: 'value',
    seriesField: 'type',
    color: {
      type: 'ordinal' as const,
      domain: colorDomain,
      range: colorRange,
    },
    line: {
      style: {
        lineWidth: (datum: any) => (dashMap[datum?.type] ? 1.6 : 2.4),
        lineDash: (datum: any) => dashMap[datum?.type] ?? [0],
        strokeOpacity: 0.95,
      },
      state: {
        highlight: {
          lineWidth: 3.4,
          strokeOpacity: 1,
        },
        blur: {
          strokeOpacity: 0.22,
        },
        dimension_hover: {
          lineWidth: 3.2,
          strokeOpacity: 1,
        },
        dimension_hover_reverse: {
          strokeOpacity: 0.24,
        },
      },
    },
    point: {
      visible: false,
      state: {
        highlight: {
          fillOpacity: 1,
          lineWidth: 2,
          size: 68,
          stroke: 'var(--ant-color-bg-container)',
          visible: true,
        },
        blur: {
          fillOpacity: 0.18,
        },
        dimension_hover: {
          fillOpacity: 1,
          lineWidth: 2,
          size: 64,
          stroke: 'var(--ant-color-bg-container)',
          visible: true,
        },
      },
    },
    area: hasFill
      ? {
          visible: true,
          style: {
            fillOpacity: 0.15,
            visible: (datum: any) => !dashMap[datum?.type],
          },
          state: {
            highlight: {
              fillOpacity: 0.26,
            },
            blur: {
              fillOpacity: 0.05,
            },
            dimension_hover: {
              fillOpacity: 0.24,
            },
            dimension_hover_reverse: {
              fillOpacity: 0.06,
            },
          },
        }
      : { visible: false },
    axes: [
      {
        orient: 'bottom',
        type: 'band',
        label: {
          style: { fontSize: 10, fill: '#8a91a5' },
          autoLimit: true,
          autoHide: true,
        },
        domainLine: { style: { stroke: 'var(--ant-color-border-secondary)' } },
        tick: { visible: false },
      },
      {
        orient: 'left',
        type: 'linear',
        zero: true,
        nice: true,
        min: mirroredAxisDomain?.min,
        max: mirroredAxisDomain?.max,
        expand: { min: 0.08, max: 0.08 },
        label: {
          style: { fontSize: 10, fill: '#8a91a5' },
          formatMethod: (value: unknown) => formatAxisMetricValue(Number(value), unit),
        },
        grid: {
          visible: true,
          style: {
            stroke: 'var(--ant-color-border-secondary)',
            lineDash: [3, 3],
            strokeOpacity: 0.6,
          },
        },
        domainLine: { visible: false },
        tick: { visible: false },
      },
    ],
    tooltip: {
      visible: true,
      mark: { visible: false },
      dimension: {
        content: [
          {
            key: (datum: any) => datum?.type ?? '',
            value: (datum: any) => formatMetricValue(Math.abs(Number(datum?.rawValue ?? datum?.value)), unit),
          },
        ],
      },
    },
    legends: showLegend
      ? {
          visible: true,
          orient: 'top' as const,
          position: 'start' as const,
          padding: { bottom: 4 },
          item: {
            label: { style: { fontSize: 11, fill: 'var(--ant-color-text)' } },
          },
        }
      : { visible: false },
    padding: {
      top: showLegend ? 26 : 10,
      right: 10,
      bottom: 28,
      left: mirroredAxisDomain ? 54 : 48,
    },
    animation: false,
  }
}

function buildPlaceholderCard(key: string, title: string, unit: string): CompactChartCard {
  return { key, title, unit, lines: [] }
}

function buildCompactChartCards(
  series: MetricSeries[],
  localeCode: MetricLocale,
  resourceRequests?: ResourceQuantity,
  resourceLimits?: ResourceQuantity,
) {
  const seriesMap = new Map(series.map((item) => [item.key, item]))
  const cards: CompactChartCard[] = []

  const buildUsageCard = (resource: 'cpu' | 'memory', title: string) => {
    const usageSeries = seriesMap.get(resource)
    const unit = usageSeries?.unit ?? (resource === 'cpu' ? 'cores' : 'bytes')
    if (!usageSeries || (usageSeries.points?.length ?? 0) === 0) {
      return buildPlaceholderCard(resource, title, unit)
    }
    const baseline = buildResourceBaseline(resource, resourceRequests, resourceLimits)
    const requestLabel = localeCode === 'zh_CN' ? '请求' : 'Request'
    const limitLabel = localeCode === 'zh_CN' ? '限制' : 'Limit'
    const lines: CompactChartLine[] = [
      {
        color: compactMetricColors[resource],
        fill: true,
        key: usageSeries.key,
        label: resolveMetricSeriesLabel(usageSeries.key, usageSeries.label, localeCode),
        points: usageSeries.points,
        unit: usageSeries.unit,
      },
    ]
    if (baseline.request != null) {
      lines.push({
        color: compactMetricColors[resource === 'cpu' ? 'cpuRequest' : 'memoryRequest'],
        key: `${resource}-request`,
        label: requestLabel,
        lineStyle: 'dashed',
        unit: usageSeries.unit,
        value: baseline.request,
      })
    }
    if (baseline.limit != null) {
      lines.push({
        color: compactMetricColors[resource === 'cpu' ? 'cpuLimit' : 'memoryLimit'],
        key: `${resource}-limit`,
        label: limitLabel,
        lineStyle: 'dotted',
        unit: usageSeries.unit,
        value: baseline.limit,
      })
    }
    return { key: usageSeries.key, title, unit: usageSeries.unit, lines }
  }

  const buildCombinedCard = (
    key: string,
    members: Array<{ color: string; negate?: boolean; seriesKey: string }>,
    title: string,
    fallbackUnit: string,
  ) => {
    const lines: CompactChartLine[] = []
    let resolvedUnit = fallbackUnit
    members.forEach(({ color, negate, seriesKey }) => {
      const current = seriesMap.get(seriesKey)
      if (!current || (current.points?.length ?? 0) === 0) return
      resolvedUnit = current.unit
      lines.push({
        color,
        key: current.key,
        label: resolveMetricSeriesLabel(current.key, current.label, localeCode),
        negate,
        points: current.points,
        unit: current.unit,
      })
    })
    return { key, title, unit: resolvedUnit, lines }
  }

  const buildSingleCard = (seriesKey: string, colorKey: string, fallbackTitle: string, fallbackUnit: string) => {
    const current = seriesMap.get(seriesKey)
    if (!current || (current.points?.length ?? 0) === 0) {
      return buildPlaceholderCard(seriesKey, fallbackTitle, fallbackUnit)
    }
    return {
      key: current.key,
      title: fallbackTitle,
      unit: current.unit,
      lines: [
        {
          color: compactMetricColors[colorKey] ?? compactMetricColors.default,
          fill: false,
          key: current.key,
          label: resolveMetricSeriesLabel(current.key, current.label, localeCode),
          points: current.points,
          unit: current.unit,
        },
      ],
    }
  }

  cards.push(buildUsageCard('cpu', localeCode === 'zh_CN' ? 'CPU 使用' : 'CPU Usage'))
  cards.push(buildUsageCard('memory', localeCode === 'zh_CN' ? '内存使用' : 'Memory Usage'))
  cards.push(buildCombinedCard(
    'network',
    [
      { color: compactMetricColors.networkRx, seriesKey: 'network_rx' },
      { color: compactMetricColors.networkTx, negate: true, seriesKey: 'network_tx' },
    ],
    localeCode === 'zh_CN' ? '网络吞吐' : 'Network Traffic',
    'bytes/s',
  ))
  cards.push(buildCombinedCard(
    'disk',
    [
      { color: compactMetricColors.diskRead, seriesKey: 'disk_read' },
      { color: compactMetricColors.diskWrite, negate: true, seriesKey: 'disk_write' },
    ],
    localeCode === 'zh_CN' ? '磁盘吞吐' : 'Disk I/O',
    'bytes/s',
  ))
  cards.push(buildSingleCard('connections', 'connections', localeCode === 'zh_CN' ? '连接数' : 'Connections', 'count'))

  return cards
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

function CompactMetricCard({ card, localeCode }: { card: CompactChartCard; localeCode: MetricLocale }) {
  const hasData = card.lines.length > 0
  return (
    <div
      className="rounded-lg border border-[var(--ant-color-border-secondary)] bg-[var(--ant-color-bg-container)] p-4"
      style={{ display: 'flex', flexDirection: 'column', height: COMPACT_METRIC_CARD_HEIGHT }}
    >
      <div style={{ marginBottom: 8 }}>
        <Text strong>{card.title}</Text>
      </div>
      {hasData ? (
        <div style={{ height: COMPACT_METRIC_CHART_HEIGHT }}>
          <LineChart spec={buildCompactChartSpec(card.lines, card.unit, localeCode)} />
        </div>
      ) : (
        <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <ManagementState bordered={false} compact title={localeCode === 'zh_CN' ? '暂无数据' : 'No data'} />
        </div>
      )}
    </div>
  )
}

export function ResourceMetricsPanel({
  title,
  data,
  loading,
  rangeMinutes,
  onRangeChange,
  errorMessage,
  resourceRequests,
  resourceLimits,
  compact = false,
}: {
  title: string
  data?: MetricsSnapshot
  loading?: boolean
  rangeMinutes?: number
  onRangeChange?: (rangeMinutes: number) => void
  errorMessage?: string
  resourceRequests?: ResourceQuantity
  resourceLimits?: ResourceQuantity
  compact?: boolean
}) {
  const { localeCode } = useI18n()

  if (loading) {
    return <Card className="soha-detail-card" loading />
  }

  if (errorMessage) {
    return (
      <Card className="soha-detail-card" title={title}>
        <ManagementState bordered={false} compact kind="error" title={errorMessage} />
      </Card>
    )
  }

  if (!data) {
    return (
      <Card className="soha-detail-card" title={title}>
        <ManagementState bordered={false} compact title={localeCode === 'zh_CN' ? '暂无指标数据' : 'No metrics data'} />
      </Card>
    )
  }

  const series = data.series ?? []
  const stats = series.map((item) => ({
    label: item.label,
    value: formatMetricValue(item.latest, item.unit),
  }))
  const metricsHint = getMetricsHint(data.message, localeCode)
  const emptyDescription = metricsHint || data.message || (localeCode === 'zh_CN' ? '当前范围没有可展示的指标序列' : 'No metrics series available for the current range')
  const rangeSelector = onRangeChange ? (
    <Select
      value={String(rangeMinutes ?? data.rangeMinutes)}
      onChange={(value) => onRangeChange(Number(value))}
      style={{ width: 180 }}
      options={[
        { value: '15', label: localeCode === 'zh_CN' ? '最近 15 分钟' : 'Last 15 min' },
        { value: '60', label: localeCode === 'zh_CN' ? '最近 1 小时' : 'Last 1 hour' },
        { value: '360', label: localeCode === 'zh_CN' ? '最近 6 小时' : 'Last 6 hours' },
        { value: '1440', label: localeCode === 'zh_CN' ? '最近 24 小时' : 'Last 24 hours' },
      ]}
    />
  ) : null
  const grafanaButton = data.grafanaBaseUrl ? (
    <Button type="primary" onClick={() => window.open(data.grafanaBaseUrl, '_blank', 'noopener,noreferrer')}>
      {localeCode === 'zh_CN' ? '打开 Grafana' : 'Open Grafana'}
    </Button>
  ) : null
  const headerExtraContent = rangeSelector || grafanaButton ? (
    <Space>
      {rangeSelector}
      {grafanaButton}
    </Space>
  ) : null

  if (compact) {
    const compactCards = buildCompactChartCards(series, localeCode, resourceRequests, resourceLimits)
    return (
      <div className="soha-page-section">
        <Card className="soha-detail-card" title={title} extra={headerExtraContent}>
          <div
            className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3"
            style={{ gridAutoRows: `${COMPACT_METRIC_CARD_HEIGHT}px` }}
          >
            {compactCards.map((card) => (
              <CompactMetricCard key={card.key} card={card} localeCode={localeCode} />
            ))}
          </div>
        </Card>
      </div>
    )
  }

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
    <div className="soha-page-section">
      <Card className="soha-detail-card" title={title} extra={headerExtraContent}>
        <Descriptions
          items={[
            { key: 'status', label: localeCode === 'zh_CN' ? '状态' : 'Status', children: data.configured ? (localeCode === 'zh_CN' ? '已配置' : 'Configured') : (localeCode === 'zh_CN' ? '未配置' : 'Not configured') },
            { key: 'source', label: localeCode === 'zh_CN' ? '来源' : 'Source', children: data.source || '-' },
            { key: 'generated-at', label: localeCode === 'zh_CN' ? '生成时间' : 'Generated At', children: formatDateTime(data.generatedAt) },
            { key: 'range', label: localeCode === 'zh_CN' ? '查询范围' : 'Range', children: `${data.rangeMinutes} min` },
            { key: 'step', label: localeCode === 'zh_CN' ? '采样步长' : 'Step', children: `${data.stepSeconds}s` },
          ] satisfies DescriptionsProps['items']}
        />
        {data.message ? (
          <div style={{ marginTop: 12 }}>
            <Text type="secondary" style={{ display: 'block', fontSize: 12 }}>
              {data.message}
            </Text>
            {metricsHint ? (
              <Text style={{ display: 'block', marginTop: 6, fontSize: 12, color: 'var(--ant-color-warning)' }}>
                {metricsHint}
              </Text>
            ) : null}
          </div>
        ) : null}
      </Card>

      {series.length > 0 ? (
        <>
          <StatGrid items={stats} />
          <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '时序明细' : 'Series Detail'}>
            <Tabs
              type="card"
              items={series.map((item): TabItem => {
                const summary = summarizeSeries(item)
                const rows = [...(item.points ?? [])]
                  .slice(-20)
                  .reverse()
                  .map((point) => ({ timestamp: point.timestamp, value: point.value }))

                const columns: ColumnProps<MetricPointRow>[] = [
                  pointColumns[0],
                  {
                    title: localeCode === 'zh_CN' ? '值' : 'Value',
                    dataIndex: 'value',
                    render: (value: number) => formatMetricValue(value, item.unit),
                  },
                ]

                const cardLines: CompactChartLine[] = [
                  {
                    color: compactMetricColors[item.key] ?? compactMetricColors.default,
                    fill: true,
                    key: item.key,
                    label: item.label,
                    points: item.points,
                    unit: item.unit,
                  },
                ]

                return {
                  label: item.label,
                  key: item.key,
                  children: (
                    <>
                      <Descriptions
                        items={[
                          { key: 'latest', label: localeCode === 'zh_CN' ? '最新值' : 'Latest', children: formatMetricValue(item.latest, item.unit) },
                          { key: 'min', label: localeCode === 'zh_CN' ? '最小值' : 'Min', children: summary.min },
                          { key: 'max', label: localeCode === 'zh_CN' ? '最大值' : 'Max', children: summary.max },
                          { key: 'avg', label: localeCode === 'zh_CN' ? '平均值' : 'Average', children: summary.avg },
                          { key: 'samples', label: localeCode === 'zh_CN' ? '样本数' : 'Samples', children: summary.samples },
                        ] satisfies DescriptionsProps['items']}
                      />
                      <div style={{ marginTop: 16, height: 280 }}>
                        <LineChart spec={buildCompactChartSpec(cardLines, item.unit, localeCode)} />
                      </div>
                      <div style={{ marginTop: 16 }}>
                        <AdminTable
                          shellClassName="soha-management-table-shell"
                          columns={columns}
                          dataSource={rows}
                          rowKey={(record) => record.timestamp}
                          pageSize={10}
                          enableColumnSelection={false}
                        />
                      </div>
                    </>
                  ),
                }
              })}
            />
          </Card>
        </>
      ) : (
        <Card className="soha-detail-card">
          <ManagementState bordered={false} compact title={emptyDescription} />
        </Card>
      )}
    </div>
  )
}
