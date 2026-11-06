import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useClusterCapabilityForCluster } from '@/features/platform/cluster-capabilities'
import { useI18n } from '@/i18n'
import type { Pod, ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Select, Spin } from 'antd'
import { lazy, Suspense } from 'react'
import { useSearchParams } from 'react-router-dom'
import { podQueries } from '../pods/queries'
const Viewer = lazy(async () => ({
  default: (await import('@/components/pod-log-viewer')).PodLogViewer,
}))

export function JobDetailLogs({ pods, scope }: { pods: Pod[]; scope: ScopeKey }) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead =
    hasPermission(permissions, 'platform.pods.view') &&
    hasPermission(permissions, 'platform.pods.logs')
  const capability = useClusterCapabilityForCluster('pod.logs', localeCode, scope.clusterId)
  const [params, setParams] = useSearchParams()
  const name = params.get('pod') || pods[0]?.name || ''
  const selected = pods.find((p) => p.name === name && p.namespace === scope.namespace)
  const options = podQueries.detail(scope, selected?.name || '')
  const query = useQuery({ ...options, enabled: Boolean(canRead && selected && options.enabled) })
  const raw = canRead ? readableQueryData(query) : undefined
  const detail = raw?.name === name && raw.namespace === scope.namespace ? raw : undefined
  const containers = detail?.containers?.map((c) => ({ label: c.name, value: c.name })) || []
  const container = params.get('container') || containers[0]?.value
  const change = (key: string, value: string) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set(key, value)
      if (key === 'pod') next.delete('container')
      return next
    })
  if (!canRead)
    return (
      <ManagementState
        compact
        kind="no-permission"
        title={zh ? '无权查看 Pod 日志' : 'No permission to view Pod logs'}
      />
    )
  if (!pods.length)
    return (
      <ManagementState
        compact
        title={zh ? '没有可读取日志的 Pod' : 'No Pod available for logs'}
        description={
          zh
            ? '此 Job 未创建 Pod，或执行实例已清理；没有持久日志时无法恢复。'
            : 'The Job has no retained Pod. Logs cannot be recovered without persistent storage.'
        }
      />
    )
  return (
    <div className="soha-job-logs">
      <div className="soha-k8s-detail-toolbar">
        <Select
          aria-label={zh ? '日志 Pod' : 'Log Pod'}
          value={selected?.name}
          showSearch={{ optionFilterProp: 'label' }}
          placeholder={zh ? '选择执行 Pod' : 'Select a Pod'}
          options={pods.map((p) => ({ value: p.name, label: `${p.name} · ${p.phase}` }))}
          onChange={(v) => change('pod', v)}
        />
      </div>
      {!selected ? (
        <ManagementState
          compact
          title={zh ? '所选 Pod 已不属于当前 Job' : 'Selected Pod is no longer part of this Job'}
        />
      ) : (
        <>
          <ReadFeedback canRead query={query} label={zh ? '日志 Pod' : 'Log Pod'} />
          {query.isLoading ? (
            <Spin />
          ) : detail &&
            (!hasAllowedAction(detail.allowedActions, 'logs') || capability.disabled) ? (
            <ManagementState
              compact
              kind="unsupported"
              title={zh ? '日志不可用' : 'Logs unavailable'}
              description={capability.reason}
            />
          ) : detail && !containers.some((c) => c.value === container) ? (
            <ManagementState
              compact
              title={zh ? '所选容器不存在' : 'Selected container is unavailable'}
            />
          ) : detail ? (
            <Suspense fallback={<Spin />}>
              <Viewer
                key={`${scope.clusterId}/${scope.namespace}/${name}`}
                clusterId={scope.clusterId}
                namespace={scope.namespace}
                podName={name}
                container={container}
                containerOptions={containers}
                onContainerChange={(v) => change('container', v)}
                streamingDisabledReason={
                  capability.status === 'partial'
                    ? capability.reason
                    : ['Succeeded', 'Failed'].includes(detail.phase)
                      ? zh
                        ? 'Pod 已结束，读取保留日志'
                        : 'Pod has finished; reading retained logs'
                      : undefined
                }
              />
            </Suspense>
          ) : null}
        </>
      )}
    </div>
  )
}
