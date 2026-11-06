import { Alert, Spin } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAuthStore } from '@/stores/auth-store'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { monitoringQueries } from '../resource-monitoring/queries'
import { reasonLabels } from '../resource-monitoring/model'
import type { MonitoringView } from '../resource-monitoring/types'

export function prometheusCoverage(data: MonitoringView | undefined, clusterId: string) {
  const nodes =
    data?.clusterId === clusterId
      ? data.nodes.filter(
          (n) => n.nodeRef.clusterId === clusterId && n.nodeRef.kind === 'Node' && n.nodeRef.uid,
        )
      : []
  const count = (key: string) => {
    const panel = data?.panels.find((p) => p.key === key)
    return nodes.filter(
      (n) =>
        n.quality === 'valid' &&
        panel?.series.some(
          (s) =>
            s.nodeRef.clusterId === clusterId &&
            s.nodeRef.uid === n.nodeRef.uid &&
            s.nodeRef.name === n.nodeRef.name &&
            s.quality === 'valid' &&
            s.latest !== null &&
            Number.isFinite(s.latest),
        ),
    ).length
  }
  const cpu = count('host.cpu.used')
  const memory = count('host.memory.used')
  const reason =
    data?.clusterId !== clusterId
      ? 'cluster_identity_mismatch'
      : data.snapshotQuality !== 'valid'
        ? 'node_snapshot_unavailable'
        : ['host.cpu.used', 'host.memory.used']
            .flatMap((key) => {
              const p = data.panels.find((panel) => panel.key === key)
              return [p?.reasonCode, ...(p?.series.map((s) => s.reasonCode) ?? [])]
            })
            .find(Boolean) ||
          (cpu < nodes.length || memory < nodes.length ? 'metric_not_collected' : '')
  return {
    total: nodes.length,
    cpu,
    memory,
    reason,
    complete: Boolean(
      nodes.length &&
      data?.snapshotQuality === 'valid' &&
      cpu === nodes.length &&
      memory === nodes.length &&
      !data.truncated,
    ),
  }
}

export function useClusterPrometheusCheck(clusterId: string | undefined, active: boolean) {
  const principal = useAuthStore((s) => s.user?.userId || '')
  const permissions = usePermissionSnapshot()
  const snapshot = readableQueryData(permissions)?.data
  const canCheck =
    hasPermission(snapshot, 'platform.nodes.view') &&
    hasPermission(snapshot, 'platform.resource-monitoring.overview.view')
  const signature = `${(snapshot?.permissionKeys ?? []).slice().sort().join(',')}@${permissions.dataUpdatedAt}`
  const query = useQuery({
    ...monitoringQueries.view(principal, signature, clusterId ?? '', {
      view: 'overview',
      rangeMinutes: 15,
      stepSeconds: 60,
    }),
    enabled: Boolean(clusterId && active && canCheck),
    retry: false,
  })
  return {
    query,
    canCheck,
    data: active && canCheck && !query.isError ? readableQueryData(query) : undefined,
  }
}

export function PrometheusCheckResult({
  check,
  clusterId,
}: {
  check: ReturnType<typeof useClusterPrometheusCheck>
  clusterId: string
}) {
  const { localeCode, t } = useI18n()
  const zh = localeCode === 'zh_CN'
  if (!check.canCheck)
    return (
      <Alert
        showIcon
        type="info"
        title={
          zh
            ? '配置可保存，当前账号无主机监控校验权限'
            : 'Configuration can be saved; host monitoring permission is required to check it'
        }
      />
    )
  if (check.query.isPending || check.query.isFetching)
    return (
      <div role="status">
        <Spin size="small" /> {zh ? '正在校验主机指标…' : 'Checking host metrics…'}
      </div>
    )
  if (check.query.isError)
    return (
      <ReadFeedback
        canRead
        query={check.query}
        label={zh ? 'Prometheus 接入校验' : 'Prometheus check'}
      />
    )
  if (!check.data) return null
  const result = prometheusCoverage(check.data, clusterId)
  const reason =
    result.reason === 'metric_not_collected'
      ? zh
        ? '未匹配到部分主机指标，请核对集群标签名和值，以及 node-exporter 采集。'
        : 'Some host metrics did not match. Check the cluster label name/value and node-exporter collection.'
      : t(
          `resourceMonitoring.reason.${result.reason}`,
          reasonLabels[result.reason] || result.reason,
        )
  return (
    <Alert
      showIcon
      type={result.complete ? 'success' : 'warning'}
      title={
        result.complete
          ? zh
            ? '主机指标已接通'
            : 'Host metrics are available'
          : zh
            ? '主机指标尚未全部接通'
            : 'Host metrics are not fully available'
      }
      description={
        <>
          <div>
            {zh
              ? `CPU ${result.cpu}/${result.total} 个节点 · 内存 ${result.memory}/${result.total} 个节点`
              : `CPU ${result.cpu}/${result.total} nodes · Memory ${result.memory}/${result.total} nodes`}
          </div>
          {!result.complete && (
            <div>
              {reason ||
                (zh
                  ? '未获得完整节点快照或有效样本。'
                  : 'A complete node snapshot or valid samples are unavailable.')}
            </div>
          )}
        </>
      }
    />
  )
}
