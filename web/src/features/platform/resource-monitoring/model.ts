import type { MonitoringNode, MonitoringView, MonitoringViewName } from './types'
export const monitoringNodeLimit = 10
export const monitoringViews = [
  { key: 'overview', label: '总览', englishLabel: 'Overview' },
  { key: 'cpu', label: 'CPU', englishLabel: 'CPU' },
  { key: 'memory', label: '内存', englishLabel: 'Memory' },
  { key: 'conntrack', label: '连接跟踪', englishLabel: 'Conntrack' },
  { key: 'pods', label: 'Pod 容量', englishLabel: 'Pod capacity' },
] as const
export const monitoringPermission = (view: string) => `platform.resource-monitoring.${view}.view`
export const qualityLabels: Record<string, string> = {
  valid: '可用',
  partial: '部分数据',
  stale: '数据过期',
  not_configured: '未配置数据源',
  not_collected: '未采集',
  unsupported: '不支持',
  denied: '无读取权限',
  error: '读取失败',
  empty: '没有数据',
}
export const qualityStatus: Record<string, string> = {
  valid: 'healthy',
  partial: 'warning',
  stale: 'warning',
  not_configured: 'pending',
  not_collected: 'pending',
  unsupported: 'unknown',
  denied: 'blocked',
  error: 'error',
  empty: 'unknown',
}
export const reasonLabels: Record<string, string> = {
  container_coverage_unverified: '仅汇总可确认身份的容器样本，无法排除完全缺采集的容器',
  container_coverage_incomplete: '已观测容器或CPU维度存在缺失，当前总量为部分覆盖',
  cluster_identity_mismatch: '返回序列的集群标识不匹配，已排除',
  cpu_coverage_incomplete: '缺少部分 CPU 维度样本，未作为完整用量显示',
  node_snapshot_unavailable: '节点快照读取失败，请检查 Agent 连通性与 Kubernetes RBAC',
  cluster_label_required: '请配置集群指标标签；本页不会移除集群匹配条件重试',
  prometheus_not_configured: '为当前集群配置 Prometheus 数据源后，可查看历史曲线',
  metric_not_collected: '数据源没有返回该指标，请检查采集器及指标名称',
  no_verified_samples: '没有可确认节点身份的有效样本',
  no_verified_node_identity: '没有可确认的节点身份',
  upgrade_agent: '请升级 Agent 后读取当前容量快照',
  container_identity_unverified: '部分容器缺少 Pod UID，未纳入总量',
  replica_values_differ: '重复采集序列数值不同，使用固定来源并标记部分覆盖',
  insufficient_counter_samples: '5 分钟窗口内有效样本不足，事件速率未确认',
  container_allocations_only: '历史容器资源值不含初始化、Pod 级资源和 overhead，当前调度总量见快照',
  sample_gaps: '所选范围存在缺失点',
  stale_samples: '来源采样时间过期',
  node_coverage_incomplete: '部分节点没有该指标',
  linux_collector_required: '需要 Linux 节点及 node_exporter conntrack 采集器',
  node_os_unverified: '未确认节点操作系统',
  pod_snapshot_incomplete: 'Pod 快照不完整，容量分配与数量保留为空',
  point_budget: '已达到时序点预算，请选择节点或缩短时间窗口',
  response_budget: '指标响应超过预算，请缩小范围',
  query_unavailable: '指标数据源暂时不可达',
  prometheus_denied: '数据源拒绝访问',
  query_timeout: '查询超时',
  freshness_unverified: '无法确认来源采样新鲜度',
}
export function monitoringRatio(
  value: number | null | undefined,
  denominator: number | null | undefined,
) {
  return value != null && denominator != null && denominator > 0
    ? (value / denominator) * 100
    : null
}
function metricSeries(data: MonitoringView | undefined, key: string, node: string) {
  return data?.panels
    .find((panel) => panel.key === key)
    ?.series.find((series) => series.nodeRef.name === node)
}
export function latestMetric(data: MonitoringView | undefined, key: string, node: string) {
  return metricSeries(data, key, node)?.latest ?? null
}
export function alignedMetricDenominator(
  data: MonitoringView | undefined,
  numeratorKey: string,
  denominatorKey: string,
  node: string,
) {
  const numerator = metricSeries(data, numeratorKey, node)
  const denominator = metricSeries(data, denominatorKey, node)
  const timestamp = Date.parse(numerator?.sampledAt || '')
  return Number.isFinite(timestamp) && timestamp === Date.parse(denominator?.sampledAt || '')
    ? (denominator?.latest ?? null)
    : null
}
export function nodeMetricScore(
  data: MonitoringView | undefined,
  node: MonitoringNode,
  view: MonitoringViewName,
  historical: boolean,
): number | null {
  const name = node.nodeRef.name
  switch (view) {
    case 'cpu':
      return historical
        ? null
        : monitoringRatio(latestMetric(data, 'host.cpu.used', name), node.capacity.cpuCores)
    case 'memory':
      return monitoringRatio(
        latestMetric(data, 'host.memory.used', name),
        alignedMetricDenominator(data, 'host.memory.used', 'host.memory.total', name),
      )
    case 'conntrack':
      return monitoringRatio(
        latestMetric(data, 'conntrack.entries', name),
        alignedMetricDenominator(data, 'conntrack.entries', 'conntrack.limit', name),
      )
    case 'pods':
      return monitoringRatio(node.boundPods, node.allocatable.podSlots)
    default:
      return Math.max(
        ...(['cpu', 'memory', 'conntrack', 'pods'] as const).map(
          (view) => nodeMetricScore(data, node, view, historical) ?? -1,
        ),
      )
  }
}
