import type { AlertEvent } from '@/features/observability'
import type { LocaleCode } from '@/i18n'
import type { AuditLog, OperationLog } from '@/features/system'
import type { Cluster, ClusterCapabilityMatrixEntry } from '@/types'
import type { KubernetesClusterEvent } from '@opensoha/contracts/gen/ts/sohaapi'
import type { ClusterNode } from '../cluster-resources/types'
import type {
  OverviewActivityItem,
  OverviewCapacitySummary,
  OverviewCapabilityGovernance,
  OverviewFleetReadiness,
} from './types'

const capabilityZhLabels: Record<string, string> = {
  'cluster.inventory': '集群清单',
  'namespace.lifecycle': '命名空间生命周期',
  'workload.read': '工作负载读取',
  'workload.mutations': '工作负载变更',
  'resource.yaml.view': 'YAML 查看',
  'resource.yaml.apply': 'YAML 应用与删除',
  'resource.creation': '资源创建',
  'configuration.inventory': '配置清单',
  'rbac.inventory': 'RBAC 清单',
  'network.inventory': '网络清单',
  'port.forward': '端口转发',
  'storage.inventory': '存储清单',
  'custom.resources': '自定义资源',
  'helm.releases': 'Helm 发布',
  'delivery.actions': '交付操作',
  'pod.logs': 'Pod 日志',
  'logs.runtime.snapshot': '运行日志快照',
  'logs.runtime.stream': '运行日志流',
  'logs.runtime.aggregate': '运行日志聚合',
  'pod.exec': 'Pod 执行与终端',
  metrics: '指标',
}

const overviewZhLabels: Record<string, string> = {
  approval: '需审批',
  mutate: '变更',
  execute: '执行',
  high: '高风险',
  read: '只读',
  analyze: '分析',
  critical: '严重',
  warning: '警告',
  info: '信息',
  success: '成功',
  succeeded: '成功',
  running: '运行中',
  pending: '等待中',
  failed: '失败',
  failure: '失败',
  error: '错误',
  healthy: '健康',
  unhealthy: '异常',
  unknown: '未知',
  available: '可用',
  partial: '部分可用',
  unsupported: '不支持',
  direct_kubeconfig: 'Kubeconfig 直连',
  'identity.provider.delete': '删除身份提供商',
  'docker.host.delete': '删除 Docker 主机',
  logs: '集群日志',
  'logs.query': '日志查询',
  'platform.resource_creation.batch': '资源创建批次',
  'platform.resource_creation.resource': '资源创建',
  'resource creation batch succeeded': '资源创建批次成功',
  'resource creation batch running': '资源创建批次运行中',
  'resource creation batch failed': '资源创建批次失败',
  'created resource from manifest': '通过 Manifest 创建资源',
  'deleted docker host': '已删除 Docker 主机',
  'deleted identity provider': '已删除身份提供商',
}

export function formatPlatformOverviewCapability(
  key: string,
  fallback: string,
  localeCode: LocaleCode,
) {
  return localeCode === 'zh_CN' ? capabilityZhLabels[key] || fallback : fallback
}

export function formatPlatformOverviewText(value: string, localeCode: LocaleCode): string {
  if (localeCode !== 'zh_CN') return value
  const normalized = value.trim()
  if (!normalized) return value

  const governance = normalized.match(
    /^Governance (audit|operation) (success|succeeded|running|failed|failure|error):\s*(.+)$/i,
  )
  if (governance) {
    const source = governance[1].toLowerCase() === 'audit' ? '审计' : '操作'
    const result = formatPlatformOverviewText(governance[2], localeCode)
    const target = formatPlatformOverviewText(governance[3], localeCode)
    return `治理${source}${result}：${target}`
  }

  if (normalized.includes(' · ')) {
    return normalized
      .split(' · ')
      .map((part) => formatPlatformOverviewText(part, localeCode))
      .join(' · ')
  }

  return overviewZhLabels[normalized.toLowerCase()] || value
}

export function formatPlatformOverviewAlertScope(
  alert: Pick<AlertEvent, 'clusterId' | 'namespace' | 'summary'>,
  clusterName: string | undefined,
  localeCode: LocaleCode,
) {
  const cluster = clusterName?.trim() || alert.clusterId?.trim()
  const namespace = alert.namespace?.trim()
  const parts =
    localeCode === 'zh_CN'
      ? [cluster && `集群：${cluster}`, namespace && `命名空间：${namespace}`]
      : [cluster && `Cluster: ${cluster}`, namespace && `Namespace: ${namespace}`]
  return parts.filter(Boolean).join(' · ') || formatPlatformOverviewText(alert.summary, localeCode)
}

function validPercent(value: number | undefined) {
  return Number.isFinite(value) ? Math.max(0, value!) : undefined
}

function maxNodePercent(
  nodes: ClusterNode[],
  key: 'cpu' | 'memory' | 'pods',
  source: 'usagePercentages' | 'requestPercentages',
  fallbackSource?: 'requestPercentages',
): number | undefined {
  const values = nodes
    .map(
      (node) =>
        validPercent(node.resources?.[source]?.[key]) ??
        (fallbackSource ? validPercent(node.resources?.[fallbackSource]?.[key]) : undefined),
    )
    .filter((value): value is number => value != null)
  return values.length > 0 ? Math.max(...values) : undefined
}

export function summarizeNodeCapacity(nodes: ClusterNode[]): OverviewCapacitySummary {
  return {
    cpuPercent: maxNodePercent(nodes, 'cpu', 'usagePercentages'),
    memoryPercent: maxNodePercent(nodes, 'memory', 'usagePercentages'),
    cpuRequestPercent: maxNodePercent(nodes, 'cpu', 'requestPercentages'),
    memoryRequestPercent: maxNodePercent(nodes, 'memory', 'requestPercentages'),
    podPercent: maxNodePercent(nodes, 'pods', 'usagePercentages', 'requestPercentages'),
    readyNodes: nodes.filter((node) => node.status.toLowerCase() === 'ready').length,
    totalNodes: nodes.length,
    unschedulableNodes: nodes.filter((node) => node.unschedulable).length,
    signalNodes: nodes.filter(
      (node) =>
        node.resources?.usagePercentages?.cpu != null ||
        node.resources?.requestPercentages?.cpu != null,
    ).length,
  }
}

function eventTimestamp(event: KubernetesClusterEvent, now: number) {
  const explicit = Date.parse(event.lastTimestamp ?? '')
  return Number.isFinite(explicit)
    ? new Date(explicit).toISOString()
    : new Date(now - Math.max(0, event.ageSeconds) * 1000).toISOString()
}

function eventPath(event: KubernetesClusterEvent) {
  const namespace = event.namespace?.trim()
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''
  switch (event.involvedKind?.toLowerCase()) {
    case 'pod':
      return event.involvedName
        ? `/workloads/pods/${encodeURIComponent(event.involvedName)}${query}`
        : `/workloads/pods${query}`
    case 'deployment':
      return event.involvedName
        ? `/workloads/deployments/${encodeURIComponent(event.involvedName)}${query}`
        : `/workloads/deployments${query}`
    case 'service':
      return event.involvedName
        ? `/network/services/${encodeURIComponent(event.involvedName)}${query}`
        : `/network/services${query}`
    default:
      return `/workloads/overview${query}`
  }
}

function recordString(record: Record<string, unknown> | undefined, key: string) {
  const value = record?.[key]
  return typeof value === 'string' ? value.trim() : ''
}

function operationResourceLabel(record: Record<string, unknown> | undefined) {
  const kind = recordString(record, 'resourceKind') || recordString(record, 'kind')
  const name = recordString(record, 'resourceName') || recordString(record, 'name')
  const namespace = recordString(record, 'namespace')
  if (!kind && !name) return ''
  const identity = namespace && name ? `${namespace}/${name}` : name
  return [kind, identity].filter(Boolean).join(' ')
}

function operationDescription(entry: OperationLog) {
  const directTarget = operationResourceLabel(entry.targetScope)
  const documents = Array.isArray(entry.metadata?.documents)
    ? entry.metadata.documents
        .map((item) =>
          item && typeof item === 'object'
            ? operationResourceLabel(item as Record<string, unknown>)
            : '',
        )
        .filter(Boolean)
    : []
  const targets = directTarget ? [directTarget] : documents.slice(0, 2)
  if (documents.length > 2 && !directTarget) targets.push(`+${documents.length - 2}`)
  return (targets.length > 0 ? targets : [entry.operationType]).join(' · ')
}

function operationLifecycleKey(entry: OperationLog) {
  const targetID =
    recordString(entry.targetScope, 'targetId') || recordString(entry.metadata, 'operationId')
  return targetID ? `${entry.operationType}:${targetID}` : entry.id
}

function operationResultRank(result: string) {
  switch (result.toLowerCase()) {
    case 'success':
    case 'succeeded':
    case 'failed':
    case 'failure':
    case 'error':
      return 2
    case 'running':
    case 'pending':
      return 1
    default:
      return 0
  }
}

function latestOperationLifecycle(entries: OperationLog[]) {
  const latest = new Map<string, OperationLog>()
  entries.forEach((entry) => {
    const key = operationLifecycleKey(entry)
    const current = latest.get(key)
    if (
      !current ||
      Date.parse(entry.createdAt) > Date.parse(current.createdAt) ||
      (entry.createdAt === current.createdAt &&
        operationResultRank(entry.result) > operationResultRank(current.result))
    ) {
      latest.set(key, entry)
    }
  })
  const items = [...latest.values()]
  const resourceCreationBatches = new Set(
    items
      .filter((entry) => entry.operationType === 'platform.resource_creation.batch')
      .map(
        (entry) =>
          recordString(entry.targetScope, 'targetId') ||
          recordString(entry.metadata, 'operationId'),
      )
      .filter(Boolean),
  )
  return items.filter(
    (entry) =>
      entry.operationType !== 'platform.resource_creation.resource' ||
      !resourceCreationBatches.has(recordString(entry.metadata, 'operationId')),
  )
}

function isReadOnlyAudit(entry: AuditLog) {
  if (entry.requestMethod?.toUpperCase() === 'GET') return true
  const action = entry.action.toLowerCase()
  const summary = entry.summary.toLowerCase()
  return (
    /(^|[._-])(get|list|read|view|watch|describe|summarize)([._-]|$)/.test(action) ||
    /^(fetched|listed|read|summarized|viewed)\b/.test(summary)
  )
}

export function buildActivityTimeline(
  events: KubernetesClusterEvent[],
  audits: AuditLog[],
  operations: OperationLog[],
  now = Date.now(),
): OverviewActivityItem[] {
  return [
    ...events.map((event) => ({
      id: `event:${event.namespace ?? ''}:${event.name}`,
      source: 'kubernetes' as const,
      title: `${event.reason} · ${event.involvedKind || 'Resource'} ${event.involvedName || event.name}`,
      description: event.message,
      status: event.type,
      timestamp: eventTimestamp(event, now),
      path: eventPath(event),
    })),
    ...audits
      .filter((entry) => !isReadOnlyAudit(entry))
      .map((entry) => ({
        id: `audit:${entry.id}`,
        source: 'audit' as const,
        title: entry.summary || entry.action,
        description: [entry.actorName || entry.actorId, entry.resourceKind, entry.resourceName]
          .filter(Boolean)
          .join(' · '),
        status: entry.result,
        timestamp: entry.createdAt,
        path: '/system/audit',
      })),
    ...latestOperationLifecycle(operations).map((entry) => ({
      id: `operation:${entry.id}`,
      source: 'operation' as const,
      title: entry.summary || entry.operationType,
      description: operationDescription(entry),
      status: entry.result,
      timestamp: entry.createdAt,
      path: '/system/operations',
    })),
  ]
    .sort((left, right) => Date.parse(right.timestamp) - Date.parse(left.timestamp))
    .slice(0, 8)
}

export function summarizeCapabilityGovernance(
  entries: ClusterCapabilityMatrixEntry[],
  connectionMode: string | undefined,
): OverviewCapabilityGovernance {
  const mode = connectionMode === 'agent' ? 'agent' : 'direct'
  const supported = entries.filter((entry) => entry[mode].status !== 'unsupported')
  const risky = supported
    .filter(
      (entry) =>
        entry.requiresApproval || entry.riskLevel === 'high' || entry.riskLevel === 'execute',
    )
    .sort(
      (left, right) =>
        Number(right.requiresApproval) - Number(left.requiresApproval) ||
        left.label.localeCompare(right.label),
    )

  return {
    approvalRequired: supported.filter((entry) => entry.requiresApproval).length,
    highRisk: supported.filter(
      (entry) => entry.riskLevel === 'high' || entry.riskLevel === 'execute',
    ).length,
    partial: supported.filter((entry) => entry[mode].status === 'partial').length,
    supported: supported.length,
    items: risky.slice(0, 4).map((entry) => ({
      key: entry.key,
      label: entry.label,
      riskLevel: entry.riskLevel,
      requiresApproval: entry.requiresApproval,
      status: entry[mode].status,
    })),
  }
}

export function buildOperationsAIEvidence({
  capacity,
  connectionMode,
  coverage,
  governance,
  timeline,
}: {
  capacity: OverviewCapacitySummary
  connectionMode?: string
  coverage: Array<{ key: string; count?: number }>
  governance: OverviewCapabilityGovernance
  timeline: OverviewActivityItem[]
}) {
  return {
    connectionMode: connectionMode || 'unknown',
    capacity,
    governance: {
      approvalRequired: governance.approvalRequired,
      highRisk: governance.highRisk,
      partial: governance.partial,
      supported: governance.supported,
      items: governance.items,
    },
    coverage: Object.fromEntries(
      coverage.filter((item) => item.count !== undefined).map((item) => [item.key, item.count]),
    ),
    recentEvidence: timeline.slice(0, 5).map((item) => ({
      source: item.source,
      title: item.title,
      description: item.description,
      status: item.status,
      timestamp: item.timestamp,
    })),
  }
}

export function summarizeFleetReadiness(
  clusters: Cluster[],
  now = Date.now(),
): OverviewFleetReadiness {
  const staleCutoff = now - 15 * 60 * 1000
  return {
    healthy: clusters.filter((cluster) => cluster.health?.status === 'healthy').length,
    unhealthy: clusters.filter((cluster) => cluster.health?.status !== 'healthy').length,
    stale: clusters.filter((cluster) => {
      const checkedAt = Date.parse(cluster.health?.lastChecked ?? '')
      return Number.isFinite(checkedAt) && checkedAt < staleCutoff
    }).length,
    versions: new Set(clusters.map((cluster) => cluster.version).filter(Boolean)).size,
  }
}

export function selectActiveAlerts(alerts: AlertEvent[], limit = 3) {
  return alerts
    .filter((alert) => (alert.currentState || alert.status).toLowerCase() !== 'resolved')
    .sort((left, right) => {
      const severity = { critical: 3, warning: 2, info: 1 } as Record<string, number>
      return (
        (severity[right.severity.toLowerCase()] ?? 0) -
          (severity[left.severity.toLowerCase()] ?? 0) ||
        Date.parse(right.lastSeenAt || right.updatedAt) -
          Date.parse(left.lastSeenAt || left.updatedAt)
      )
    })
    .slice(0, Math.max(0, limit))
}

export function filterAlertsForCluster(alerts: AlertEvent[], clusterId: string | null | undefined) {
  const normalizedClusterID = clusterId?.trim()
  if (!normalizedClusterID) return []
  return alerts.filter((alert) => alert.clusterId?.trim() === normalizedClusterID)
}
