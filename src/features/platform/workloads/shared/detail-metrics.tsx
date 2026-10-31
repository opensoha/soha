import { ManagementState } from '@/components/management-list'
import { buildCompactChartSpec, type CompactChartLine } from '@/components/resource-metrics-panel'
import { formatMetricValue } from '@/components/resource-metrics-format'
import { useI18n } from '@/i18n'
import { parseCpuValue, parseMemoryValue } from '@/features/platform/workloads-model'
import type { MetricSeries, ResourceMetrics, ResourceQuantity } from '@/types'
import { InfoCircleOutlined, ReloadOutlined } from '@ant-design/icons'
import { LineChart } from '@visactor/react-vchart'
import { Button, Card, Segmented, Tooltip, Typography } from 'antd'

import './detail-observability.css'

const blue = 'var(--soha-primary)'
const teal = 'var(--soha-accent-teal)'
export function podMetricLatest(series: MetricSeries | undefined) {
  if (!series) return undefined
  return Number.isFinite(series.latest) ? series.latest : undefined
}
export function WorkloadDetailMetrics({
  data,
  loading,
  errorMessage,
  rangeMinutes,
  onRangeChange,
  resourceRequests,
  resourceLimits,
  onRefresh,
  mode = 'pod',
  emptyDescription,
  scopeDescription,
}: {
  data?: ResourceMetrics
  loading?: boolean
  errorMessage?: string
  rangeMinutes: number
  onRangeChange: (value: number) => void
  resourceRequests?: ResourceQuantity
  resourceLimits?: ResourceQuantity
  onRefresh: () => void
  mode?: 'pod' | 'workload'
  emptyDescription?: string
  scopeDescription?: string
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const map = new Map((data?.series ?? []).map((series) => [series.key, series]))
  const value = (key: string) => {
    const item = map.get(key)
    const latest = podMetricLatest(item)
    return latest === undefined ? '—' : formatMetricValue(latest, item!.unit)
  }
  const configs = [
    {
      key: 'cpu',
      title: 'CPU',
      members: [['cpu', zh ? '使用' : 'Usage', blue]],
      request: resourceRequests?.cpu,
      limit: resourceLimits?.cpu,
    },
    {
      key: 'memory',
      title: zh ? '内存' : 'Memory',
      members: [['memory', zh ? '使用' : 'Usage', blue]],
      request: resourceRequests?.memory,
      limit: resourceLimits?.memory,
    },
    {
      key: 'network',
      title: zh ? '网络吞吐' : 'Network',
      members: [
        ['network_rx', zh ? '接收' : 'Receive', blue],
        ['network_tx', zh ? '发送' : 'Transmit', teal],
      ],
    },
    {
      key: 'disk',
      title: zh ? '磁盘 I/O' : 'Disk I/O',
      members: [
        ['disk_read', zh ? '读取' : 'Read', blue],
        ['disk_write', zh ? '写入' : 'Write', teal],
      ],
    },
    {
      key: 'filesystem',
      title: zh ? '文件系统占用' : 'Filesystem',
      members: [['filesystem', zh ? '使用' : 'Usage', blue]],
    },
    {
      key: 'restarts',
      title: zh ? '容器重启' : 'Restarts',
      members: [['restarts', zh ? '累计重启' : 'Total restarts', blue]],
    },
  ]
  const visibleConfigs = mode === 'workload' ? configs.slice(0, 4) : configs
  const problem =
    errorMessage ||
    (data?.configured === false
      ? zh
        ? '尚未接入监控'
        : 'Monitoring is not configured'
      : undefined)
  return (
    <div
      className={`soha-pod-detail-metrics ${mode === 'workload' ? 'soha-workload-detail-metrics' : ''}`}
    >
      <div className="soha-pod-detail-metrics-toolbar">
        {scopeDescription && (
          <Typography.Text type="secondary" className="soha-workload-metric-scope">
            {scopeDescription}
          </Typography.Text>
        )}
        {mode === 'workload' && data?.source && (
          <Typography.Text type="secondary" title={data.generatedAt}>
            {data.source === 'prometheus' ? 'Prometheus' : data.source}
          </Typography.Text>
        )}
        <Segmented
          aria-label={zh ? '指标时间范围' : 'Metric time range'}
          value={rangeMinutes}
          onChange={(value) => onRangeChange(Number(value))}
          options={[
            { value: 15, label: zh ? '15 分钟' : '15 min' },
            { value: 60, label: zh ? '1 小时' : '1 hour' },
            { value: 360, label: zh ? '6 小时' : '6 hours' },
            { value: 1440, label: zh ? '24 小时' : '24 hours' },
          ]}
        />
        {scopeDescription && (
          <Button
            type="text"
            icon={<ReloadOutlined />}
            loading={loading}
            aria-label={zh ? '刷新指标' : 'Refresh metrics'}
            onClick={onRefresh}
          />
        )}
      </div>
      {problem ? (
        <ManagementState
          kind="error"
          title={problem}
          actions={<Button onClick={onRefresh}>{zh ? '重试' : 'Retry'}</Button>}
        />
      ) : (
        <div className="soha-pod-detail-metrics-grid">
          {visibleConfigs.map((config) => {
            const lines: CompactChartLine[] = config.members.flatMap(([key, label, color]) => {
              const item = map.get(key)
              return item?.points?.some((point) => Number.isFinite(point.value))
                ? [
                    {
                      key,
                      label,
                      color,
                      unit: item.unit,
                      points: item.points.filter((point) => Number.isFinite(point.value)),
                    },
                  ]
                : []
            })
            const limit = config.limit
              ? config.key === 'cpu'
                ? parseCpuValue(config.limit)
                : parseMemoryValue(config.limit)
              : Number.NaN
            if (lines.length && Number.isFinite(limit) && limit > 0) {
              lines.push({
                key: `${config.key}-limit`,
                label: zh ? '限制' : 'Limit',
                color: 'var(--soha-warning)',
                unit: lines[0].unit,
                value: limit,
                lineStyle: 'dashed',
              })
            }
            const chart = lines.length
              ? buildCompactChartSpec(lines, lines[0].unit, localeCode)
              : null
            if (chart) {
              chart.height = 166
              chart.legends = { visible: false }
              chart.padding = { top: 8, right: 8, bottom: 24, left: 0 }
              chart.axes[0].label.formatMethod = (label: string) =>
                rangeMinutes >= 1440 ? label : label.split(' ').at(-1) || label
              chart.axes[0].tickCount = 3
              chart.axes[1].min = 0
              chart.axes[1].expand = { min: 0, max: 0.06 }
            }
            const help =
              config.key === 'restarts'
                ? zh
                  ? '累计重启次数，不是当前时间范围内新增次数'
                  : 'Cumulative restarts, not the increase within the selected range'
                : config.key === 'filesystem'
                  ? zh
                    ? '容器文件系统使用量，PVC 容量在存储页查看'
                    : 'Container filesystem usage; see storage for PVC capacity'
                  : undefined
            return (
              <Card
                key={config.key}
                className="soha-pod-detail-metric-card"
                title={config.title}
                loading={loading}
                extra={
                  config.key === 'cpu' && mode === 'pod' ? (
                    <Tooltip title={zh ? 'CPU 限流周期占比' : 'CPU throttled periods ratio'}>
                      <Typography.Text type="secondary">
                        {zh ? '限流' : 'Throttled'} {value('cpu_throttled')}
                      </Typography.Text>
                    </Tooltip>
                  ) : config.key === 'network' && mode === 'workload' ? (
                    <Typography.Text type="secondary">
                      {zh ? '连接数' : 'Connections'}{' '}
                      {emptyDescription ? '—' : value('connections')}
                    </Typography.Text>
                  ) : config.key === 'restarts' ? (
                    <Tooltip
                      title={
                        zh
                          ? '重启及 OOM 为累计次数'
                          : 'Restarts and OOM events are cumulative counters'
                      }
                    >
                      <Typography.Text type="secondary">OOM {value('oom_events')}</Typography.Text>
                    </Tooltip>
                  ) : help ? (
                    <Tooltip title={help}>
                      <span tabIndex={0} aria-label={help}>
                        <InfoCircleOutlined />
                      </span>
                    </Tooltip>
                  ) : undefined
                }
              >
                <div className="soha-pod-detail-metric-values">
                  {config.members.map(([key, label, color]) => (
                    <div key={key}>
                      {config.members.length > 1 && (
                        <span>
                          <i style={{ background: color }} />
                          {label}
                        </span>
                      )}
                      <strong>{emptyDescription ? '—' : value(key)}</strong>
                    </div>
                  ))}
                  {config.request || config.limit ? (
                    <Typography.Text type="secondary">
                      {zh ? '请求' : 'Request'} {config.request || '—'} / {zh ? '限制' : 'Limit'}{' '}
                      {config.limit || '—'}
                    </Typography.Text>
                  ) : null}
                </div>
                {chart && !emptyDescription ? (
                  <LineChart spec={chart} />
                ) : (
                  <div className="soha-pod-detail-metric-missing">
                    <Typography.Text type="secondary">
                      {emptyDescription || (zh ? '未采集到时序数据' : 'No time series collected')}
                    </Typography.Text>
                  </div>
                )}
                <div className="soha-pod-detail-metric-legend">
                  {chart && Number.isFinite(limit) && limit > 0 && (
                    <>
                      <span>
                        <i style={{ background: blue }} />
                        {zh ? '使用' : 'Usage'}
                      </span>
                      <span>
                        <i style={{ background: 'var(--soha-warning)' }} />
                        {zh ? '限制' : 'Limit'}
                      </span>
                    </>
                  )}
                  {config.members.length > 1 &&
                    config.members.map(([key, label, color]) => (
                      <span key={key}>
                        <i style={{ background: color }} />
                        {label}
                      </span>
                    ))}
                </div>
              </Card>
            )
          })}
        </div>
      )}
      {data?.message && (
        <Typography.Text type="secondary" className="soha-pod-detail-metrics-message">
          {data.message}
        </Typography.Text>
      )}
    </div>
  )
}
