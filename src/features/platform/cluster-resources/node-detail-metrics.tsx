import { Card, Progress, Typography } from 'antd'
import { useI18n } from '@/i18n'
import { formatMetricValue } from '@/components/resource-metrics-format'
import { MonitoringChart } from '../resource-monitoring/chart'
import {
  qualityLabels,
  monitoringRatio,
  alignedMetricDenominator,
} from '../resource-monitoring/model'
import type { MonitoringView } from '../resource-monitoring/types'
import { nodeMetric, nodeUsage } from './node-list-model'
import '../resource-monitoring/styles.css'

export default function NodeDetailMetrics({
  data,
  clusterId,
  nodeName,
  canRead,
  failed,
}: {
  data?: MonitoringView
  clusterId: string
  nodeName: string
  canRead: boolean
  failed: boolean
}) {
  const { localeCode, t } = useI18n()
  const zh = localeCode === 'zh_CN'
  const node = data?.nodes.find(
    (n) => n.nodeRef.name === nodeName && n.nodeRef.clusterId === clusterId,
  )
  const available = Boolean(canRead && !failed && node?.quality === 'valid')
  const empty = !canRead
    ? zh
      ? '无监控查看权限'
      : 'Monitoring permission required'
    : failed
      ? zh
        ? '监控读取失败，请重试刷新'
        : 'Monitoring read failed; retry refresh'
      : zh
        ? '未采集到有效数据'
        : 'No valid samples'
  const chart = (key: string) => {
    const metric = nodeMetric(data, clusterId, nodeName, key)
    const panel = data?.panels.find((p) => p.key === key)
    const series = panel?.series.find(
      (s) => s.nodeRef.name === nodeName && s.nodeRef.clusterId === clusterId,
    )
    return panel &&
      series?.points.some((p) => p.value !== null) &&
      ['valid', 'partial'].includes(series.quality) ? (
      <>
        <MonitoringChart panel={{ ...panel, series: [series] }} names={[nodeName]} />
        {series.quality !== 'valid' && (
          <Typography.Text type="warning">
            {t(
              `resourceMonitoring.quality.${series.quality}`,
              qualityLabels[series.quality] || series.quality,
            )}
          </Typography.Text>
        )}
      </>
    ) : (
      <div className="soha-node-metric-empty">
        {metric.quality === 'empty'
          ? empty
          : t(
              `resourceMonitoring.quality.${metric.quality}`,
              qualityLabels[metric.quality] || metric.quality,
            )}
      </div>
    )
  }
  const value = (number: number | null | undefined, unit: string) =>
    number != null && Number.isFinite(number) ? formatMetricValue(number, unit) : '—'
  const capacity = (
    used: number | null | undefined,
    total: number | null | undefined,
    unit: string,
  ) => {
    const percent = available ? monitoringRatio(used, total) : null
    return (
      <div className="soha-node-capacity">
        <strong>{available ? value(used, unit) : '—'}</strong>
        <Typography.Text type="secondary">
          {' '}
          / {available ? value(total, unit) : '—'}
        </Typography.Text>
        {percent !== null && (
          <Progress
            percent={Math.max(0, Math.min(100, percent))}
            format={() => `${percent.toFixed(1)}%`}
            strokeColor="var(--soha-primary)"
          />
        )}
        {!available && <div>{empty}</div>}
      </div>
    )
  }
  const conn = nodeMetric(data, clusterId, nodeName, 'conntrack.entries')
  const connLimit = nodeMetric(data, clusterId, nodeName, 'conntrack.limit')
  const connTotal =
    connLimit.value === null
      ? null
      : alignedMetricDenominator(data, 'conntrack.entries', 'conntrack.limit', nodeName)
  return (
    <div className="soha-node-metrics-grid">
      {(['cpu', 'memory'] as const).map((kind) => {
        const usage = nodeUsage(data, clusterId, nodeName, kind)
        return (
          <Card
            className="soha-detail-card"
            key={kind}
            title={
              kind === 'cpu' ? (zh ? '主机 CPU' : 'Host CPU') : zh ? '主机内存' : 'Host memory'
            }
          >
            <div className="soha-node-metric-summary">
              <strong>{value(usage.value, kind === 'cpu' ? 'cores' : 'bytes')}</strong>
              {usage.percent !== null && (
                <Typography.Text type="secondary">{usage.percent.toFixed(1)}%</Typography.Text>
              )}
            </div>
            {chart(`host.${kind}.used`)}
          </Card>
        )
      })}
      <Card
        className="soha-detail-card"
        title={zh ? 'CPU 分配' : 'CPU allocation'}
        extra={zh ? 'Pod 请求 / 可分配' : 'Pod requests / allocatable'}
      >
        {capacity(node?.requests.cpuCores, node?.allocatable.cpuCores, 'cores')}
      </Card>
      <Card
        className="soha-detail-card"
        title={zh ? '内存分配' : 'Memory allocation'}
        extra={zh ? 'Pod 请求 / 可分配' : 'Pod requests / allocatable'}
      >
        {capacity(node?.requests.memoryBytes, node?.allocatable.memoryBytes, 'bytes')}
      </Card>
      <Card
        className="soha-detail-card"
        title={zh ? 'Pod 容量' : 'Pod capacity'}
        extra={zh ? '非终态绑定 / 可分配' : 'Non-terminal bound / allocatable'}
      >
        {capacity(node?.boundPods, node?.allocatable.podSlots, 'count')}
        {chart('pods.bound')}
      </Card>
      <Card className="soha-detail-card" title={zh ? '连接跟踪' : 'Conntrack'}>
        <div className="soha-node-metric-summary">
          <strong>{value(conn.value, 'count')}</strong>
          <Typography.Text type="secondary"> / {value(connTotal, 'count')}</Typography.Text>
        </div>
        {chart('conntrack.entries')}
      </Card>
    </div>
  )
}
