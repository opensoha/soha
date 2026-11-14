import type { ApplicationRuntimeEnvironment, ApplicationRuntimeWorkload } from '../types'
import type { OverviewTone } from '@/components/overview-visuals'

export function workloadRuntimeStatus(item: ApplicationRuntimeWorkload): {
  tone: OverviewTone
  value: string
  label: string
} {
  const status = item.healthStatus?.trim().toLowerCase() ?? ''
  const desired = Math.max(item.desiredReplicas, 0)
  const ready = Math.max(item.readyReplicas, 0)
  if (
    ['critical', 'error', 'failed', 'notready', 'not-ready', 'unavailable'].includes(status) ||
    (desired > 0 && ready === 0)
  ) {
    return { tone: 'danger', value: 'unavailable', label: '运行异常' }
  }
  if (
    desired > 0 &&
    ['deployment', 'statefulset', 'daemonset'].includes(item.workloadKind.toLowerCase()) &&
    Number.isFinite(item.updatedReplicas) &&
    item.updatedReplicas < desired
  ) {
    return { tone: 'warning', value: 'progressing', label: '滚动更新中' }
  }
  if (
    ['degraded', 'partial', 'pending', 'progressing', 'warning'].includes(status) ||
    (desired > 0 && ready < desired)
  ) {
    return { tone: 'warning', value: 'degraded', label: '部分就绪' }
  }
  if (
    status &&
    ![
      'available',
      'completed',
      'healthy',
      'normal',
      'ok',
      'ready',
      'running',
      'succeeded',
    ].includes(status)
  ) {
    return { tone: 'default', value: 'unknown', label: '状态未知' }
  }
  if (
    ['available', 'completed', 'healthy', 'normal', 'ok', 'ready', 'running', 'succeeded'].includes(
      status,
    ) ||
    (desired > 0 && ready >= desired)
  ) {
    return { tone: 'success', value: 'healthy', label: '运行正常' }
  }
  return {
    tone: 'default',
    value: 'unknown',
    label: ['cronjob', 'job'].includes(item.workloadKind.toLowerCase()) ? '按需运行' : '状态未知',
  }
}

export function manifestDeploymentRuntimeStatus(
  deployment: NonNullable<ApplicationRuntimeEnvironment['manifestDeployments']>[number],
): { tone: OverviewTone; value: string; label: string } {
  const { spec, status, generation } = deployment
  if (status.lastErrorMessage || ['failed', 'degraded'].includes(status.phase))
    return { tone: 'danger', value: 'failed', label: '部署异常' }
  if (status.phase === 'drifted' || status.drift?.drifted)
    return { tone: 'warning', value: 'drifted', label: '配置发生漂移' }
  const applied =
    status.observedGeneration >= generation && status.appliedDigest === spec.desiredDigest
  if (!applied) return { tone: 'warning', value: 'pending', label: '等待应用配置' }
  const healthy = status.conditions.some(
    (condition) =>
      condition.type === 'Healthy' &&
      condition.status === 'true' &&
      condition.observedGeneration === generation,
  )
  if (status.phase === 'converged' && healthy)
    return { tone: 'success', value: 'healthy', label: '资源已就绪' }
  return { tone: 'warning', value: 'progressing', label: '已应用，等待就绪' }
}

export function environmentRuntimeStatus(environment: ApplicationRuntimeEnvironment) {
  if (environment.status === 'unavailable')
    return { tone: 'danger', value: 'unavailable', label: '集群不可用' }
  const statuses = (environment.workloads ?? []).map((workload) => {
    const status = workload.healthStatus?.toLowerCase() || ''
    if (['pending', 'progressing', 'starting'].includes(status))
      return { tone: 'warning', value: 'pending', label: '启动中' }
    if (
      workload.desiredReplicas === 0 &&
      !['job', 'cronjob'].includes(workload.workloadKind.toLowerCase()) &&
      ['', 'healthy', 'running', 'ready', 'available'].includes(status)
    )
      return { tone: 'warning', value: 'inactive', label: '未启动' }
    return workloadRuntimeStatus(workload)
  })
  statuses.push(...(environment.manifestDeployments ?? []).map(manifestDeploymentRuntimeStatus))
  if (!statuses.length) return { tone: 'warning', value: 'pending', label: '尚未部署' }
  const danger = statuses.filter(({ tone }) => tone === 'danger').length
  const warning = statuses.filter(({ tone }) => tone === 'warning').length
  if (danger) return { tone: 'danger', value: 'unavailable', label: `${danger} 个异常` }
  if (warning) return { tone: 'warning', value: 'degraded', label: `${warning} 个需关注` }
  if (statuses.every(({ tone }) => tone === 'success'))
    return { tone: 'success', value: 'healthy', label: '运行正常' }
  return { tone: 'default', value: 'unknown', label: '状态待确认' }
}
