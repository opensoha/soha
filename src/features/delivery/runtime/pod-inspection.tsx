import { useMemo } from 'react'
import { Button, Collapse, Descriptions, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { parseDocument } from 'yaml'
import { z } from 'zod'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { ManagementState } from '@/components/management-list'
import { podQueries, workloadQueries, WorkloadDetailEvents } from '@/features/platform'
import { localeText, useI18n } from '@/i18n'
import { toScopeKey, type WorkloadContainer } from '@/types'
import { ReadFeedback } from '@/components/request-feedback'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { formatDateTime } from '@/utils/time'
import { readableQueryData } from '@/utils/query-feedback'

const { Text } = Typography
const port = z.union([z.number(), z.string()])
const probeSchema = z.object({
  httpGet: z
    .object({
      path: z.string().optional(),
      port,
      host: z.string().optional(),
      scheme: z.string().optional(),
    })
    .optional(),
  tcpSocket: z.object({ port, host: z.string().optional() }).optional(),
  grpc: z.object({ port: z.number(), service: z.string().optional() }).optional(),
  exec: z.object({ command: z.array(z.string()) }).optional(),
  initialDelaySeconds: z.number().optional(),
  periodSeconds: z.number().optional(),
  timeoutSeconds: z.number().optional(),
  failureThreshold: z.number().optional(),
  successThreshold: z.number().optional(),
})
const containerSchema = z.object({
  name: z.string(),
  image: z.string().optional(),
  restartPolicy: z.string().optional(),
  ports: z.array(z.object({ name: z.string().optional(), containerPort: z.number() })).optional(),
  livenessProbe: probeSchema.optional(),
  startupProbe: probeSchema.optional(),
  readinessProbe: probeSchema.optional(),
})
const terminationSchema = z.object({
  reason: z.string().optional(),
  message: z.string().optional(),
  exitCode: z.number().optional(),
  signal: z.number().optional(),
  finishedAt: z.string().optional(),
})
const containerStatusSchema = z.object({
  name: z.string(),
  restartCount: z.number().optional(),
  ready: z.boolean().optional(),
  state: z
    .object({
      running: z.object({ startedAt: z.string().optional() }).optional(),
      waiting: z
        .object({ reason: z.string().optional(), message: z.string().optional() })
        .optional(),
      terminated: terminationSchema.optional(),
    })
    .optional(),
  lastState: z.object({ terminated: terminationSchema.optional() }).optional(),
})
const podSchema = z.object({
  // The typed Kubernetes GET may omit TypeMeta; identity is checked against the requested Pod.
  kind: z.literal('Pod').optional(),
  metadata: z.object({
    name: z.string(),
    namespace: z.string().optional(),
    uid: z.string().optional(),
  }),
  spec: z.object({
    containers: z.array(containerSchema),
    initContainers: z.array(containerSchema).optional(),
    ephemeralContainers: z.array(containerSchema).optional(),
  }),
  status: z
    .object({
      podIP: z.string().optional(),
      containerStatuses: z.array(containerStatusSchema).optional(),
      initContainerStatuses: z.array(containerStatusSchema).optional(),
      ephemeralContainerStatuses: z.array(containerStatusSchema).optional(),
    })
    .optional(),
})

export function parsePodInspection(content: string) {
  const document = parseDocument(content)
  if (document.errors.length) throw document.errors[0]
  return podSchema.parse(document.toJS({ maxAliasCount: 50 }))
}

export function probeEndpoint(
  probe: z.infer<typeof probeSchema>,
  container: z.infer<typeof containerSchema>,
  podIP?: string,
) {
  const resolvePort = (value: string | number) =>
    typeof value === 'number'
      ? value
      : (container.ports?.find((item) => item.name === value)?.containerPort ?? value)
  const host = (value?: string) => {
    const address = value || podIP || '<Pod IP>'
    return address.includes(':') ? `[${address}]` : address
  }
  if (probe.httpGet)
    return `${probe.httpGet.scheme || 'HTTP'}://${host(probe.httpGet.host)}:${resolvePort(probe.httpGet.port)}${probe.httpGet.path || '/'}`
  if (probe.tcpSocket)
    return `TCP ${host(probe.tcpSocket.host)}:${resolvePort(probe.tcpSocket.port)}`
  if (probe.grpc)
    return `gRPC ${host()}:${probe.grpc.port}${probe.grpc.service ? ` · ${probe.grpc.service}` : ''}`
  if (probe.exec) return `exec ${probe.exec.command.map((arg) => JSON.stringify(arg)).join(' ')}`
  return '-'
}

export function VerifiedPodInspection(props: {
  clusterId: string
  namespace: string
  podName: string
  mode: 'details' | 'events' | 'yaml'
  expectedUid?: string
}) {
  const permissions = usePermissionSnapshot().data?.data
  const allowed = hasPermission(permissions, 'platform.pods.view')
  const options = podQueries.detail(toScopeKey(props.clusterId, props.namespace), props.podName)
  const query = useQuery({
    ...options,
    enabled: allowed,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  if (!allowed) return <ManagementState compact kind="no-permission" />
  if (query.isPending || query.isFetching) return <ManagementState compact kind="loading" />
  if (query.isError) return <ReadFeedback query={query} label="Pod 身份" />
  if (!query.data?.uid || (props.expectedUid && query.data.uid !== props.expectedUid))
    return (
      <ManagementState compact kind="error" title="Pod 已重建或 UID 无法确认，请刷新后重新选择" />
    )
  return (
    <PodInspection
      {...props}
      expectedUid={query.data.uid}
      containerMetadata={query.data.containers}
    />
  )
}

export function PodInspection({
  clusterId,
  namespace,
  podName,
  mode,
  expectedUid,
  containerMetadata,
}: {
  clusterId: string
  namespace: string
  podName: string
  mode: 'details' | 'events' | 'yaml'
  expectedUid?: string
  containerMetadata?: WorkloadContainer[]
}) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => localeText(localeCode, zh, en)
  const scope = toScopeKey(clusterId, namespace)
  const yamlOptions = workloadQueries.yaml('pods', scope, podName)
  const yamlQuery = useQuery({
    ...yamlOptions,
    ...(expectedUid ? { staleTime: 0, gcTime: 0, refetchOnMount: 'always' as const } : {}),
    enabled: yamlOptions.enabled && mode !== 'events',
  })
  const eventOptions = podQueries.events(scope, podName)
  const eventsQuery = useQuery({
    ...eventOptions,
    enabled: eventOptions.enabled && mode === 'events',
  })
  const parsed = useMemo(() => {
    if (mode === 'events' || (mode !== 'details' && !expectedUid) || !yamlQuery.data?.content)
      return null
    try {
      const pod = parsePodInspection(yamlQuery.data.content)
      if (expectedUid && pod.metadata.uid !== expectedUid) return { identityMismatch: true }
      if (pod.metadata.name !== podName || pod.metadata.namespace !== namespace)
        throw new Error('Pod identity mismatch')
      return { pod }
    } catch {
      return { error: true }
    }
  }, [mode, yamlQuery.data?.content, podName, namespace, expectedUid])
  const query = mode === 'events' ? eventsQuery : yamlQuery
  if (query.isError && mode !== 'events')
    return (
      <ManagementState
        compact
        kind="error"
        title={text('Pod 数据读取失败', 'Could not load Pod data')}
        actions={<Button onClick={() => void query.refetch()}>{text('重试', 'Retry')}</Button>}
      />
    )
  if (mode !== 'events' && (query.isPending || (expectedUid && yamlQuery.isFetching)))
    return <ManagementState compact kind="loading" />
  if (mode === 'events') {
    const readableEvents = readableQueryData(eventsQuery)
    const feedback = <ReadFeedback query={eventsQuery} label={text('Pod 事件', 'Pod events')} />
    if (eventsQuery.isError && readableEvents === undefined) return feedback
    return (
      <section className="soha-pod-events" aria-label={text('Pod 事件', 'Pod events')}>
        {feedback}
        <WorkloadDetailEvents
          events={readableEvents ?? []}
          observedAt={eventsQuery.dataUpdatedAt}
          loading={eventsQuery.isPending}
          onRefresh={() => void eventsQuery.refetch()}
          scopeHint={text(
            '仅展示最近 100 条命名空间事件中属于此 Pod 的记录。历史告警不代表当前健康状态，请结合 Pod 详情判断。',
            'Shows this Pod’s records from the latest 100 namespace events. Historical warnings do not indicate current health; check Pod details for current state.',
          )}
        />
      </section>
    )
  }

  if (parsed?.identityMismatch)
    return (
      <ManagementState
        compact
        kind="error"
        title={text(
          'Pod 已重建，请刷新关系后重新选择',
          'Pod identity changed; refresh and select it again',
        )}
      />
    )
  if (mode === 'yaml' && (!expectedUid || parsed?.pod))
    return (
      <div className="soha-pod-yaml">
        <Text copyable={{ text: yamlQuery.data?.content || '' }}>
          {text('Pod YAML · 只读', 'Pod YAML · read only')}
        </Text>
        <pre className="soha-json-block" tabIndex={0} aria-label="Pod YAML">
          {yamlQuery.data?.content || text('未返回 YAML', 'No YAML returned')}
        </pre>
      </div>
    )
  if (!parsed?.pod)
    return (
      <ManagementState
        compact
        kind="error"
        title={text('无法解析当前 Pod 配置', 'Could not parse Pod configuration')}
        description={text(
          '可切换到 YAML 查看原始内容。',
          'Open YAML to inspect the original content.',
        )}
      />
    )
  const pod = parsed.pod
  const statuses = [
    ...(pod.status?.containerStatuses ?? []),
    ...(pod.status?.initContainerStatuses ?? []),
    ...(pod.status?.ephemeralContainerStatuses ?? []),
  ]
  const containers = [
    ...(pod.spec.initContainers ?? []).map((container) => ({
      ...container,
      category: container.restartPolicy === 'Always' ? 'Sidecar · restartable init' : 'Init',
    })),
    ...pod.spec.containers.map((container) => ({
      ...container,
      category: text('应用容器', 'Application container'),
    })),
    ...(pod.spec.ephemeralContainers ?? []).map((container) => ({
      ...container,
      category: text('临时', 'Ephemeral'),
    })),
  ]
  return (
    <div className="soha-pod-inspection">
      <Collapse
        ghost
        defaultActiveKey={containers.map((container) => container.name)}
        items={containers.map((container) => {
          const status = statuses.find((item) => item.name === container.name)
          const metadata = containerMetadata?.find((item) => item.name === container.name)
          const role =
            metadata?.role === 'sidecar'
              ? 'Sidecar'
              : metadata?.role === 'main'
                ? text('主容器', 'Main')
                : metadata?.role === 'init'
                  ? 'Init'
                  : container.category
          const terminated = status?.state?.terminated || status?.lastState?.terminated
          return {
            key: container.name,
            label: (
              <div className="soha-pod-container-heading">
                <Text strong>{container.name}</Text>
                <MetadataTag label={role} />
                <Text type="secondary">{container.image}</Text>
              </div>
            ),
            children: (
              <>
                <Descriptions
                  size="small"
                  column={{ xs: 1, sm: 2, md: 3 }}
                  items={[
                    {
                      key: 'state',
                      label: text('当前状态', 'State'),
                      children: (
                        <StatusTag
                          value={
                            status?.state?.waiting
                              ? 'pending'
                              : status?.state?.terminated
                                ? 'stopped'
                                : status?.state?.running
                                  ? 'running'
                                  : 'unknown'
                          }
                          label={
                            status?.state?.waiting?.reason ||
                            status?.state?.terminated?.reason ||
                            (status?.state?.running ? 'Running' : '未知')
                          }
                        />
                      ),
                    },
                    {
                      key: 'ready',
                      label: text('容器就绪', 'Container readiness'),
                      children:
                        status?.ready === undefined
                          ? '未知'
                          : status.ready
                            ? text('已就绪', 'Ready')
                            : text('未就绪', 'Not ready'),
                    },
                    {
                      key: 'restarts',
                      label: text('重启次数', 'Restarts'),
                      children: status?.restartCount ?? '-',
                    },
                    {
                      key: 'reason',
                      label: text('上次终止原因', 'Last termination reason'),
                      children: terminated?.reason || text('未记录', 'Not recorded'),
                    },
                    {
                      key: 'exit',
                      label: text('退出码', 'Exit code'),
                      children: terminated?.exitCode ?? '-',
                    },
                    ...(terminated?.signal
                      ? [
                          {
                            key: 'signal',
                            label: text('信号', 'Signal'),
                            children: terminated.signal,
                          },
                        ]
                      : []),
                    ...(terminated?.finishedAt
                      ? [
                          {
                            key: 'finished',
                            label: text('终止时间', 'Finished at'),
                            children: formatDateTime(terminated.finishedAt),
                          },
                        ]
                      : []),
                    ...(terminated?.message
                      ? [
                          {
                            key: 'message',
                            label: text('终止信息', 'Termination message'),
                            span: 3,
                            children: terminated.message,
                          },
                        ]
                      : []),
                  ]}
                />
                <div className="soha-pod-probes">
                  {(['livenessProbe', 'startupProbe', 'readinessProbe'] as const).map((key) => {
                    const probe = container[key]
                    const label = {
                      livenessProbe: 'Liveness',
                      startupProbe: 'Startup',
                      readinessProbe: 'Readiness',
                    }[key]
                    return (
                      <div key={key} className="soha-pod-probe">
                        <Text strong>{label}</Text>
                        {probe ? (
                          <>
                            <Text code>{probeEndpoint(probe, container, pod.status?.podIP)}</Text>
                            <Text type="secondary">
                              {text('延迟', 'Delay')} {probe.initialDelaySeconds ?? 0}s ·{' '}
                              {text('周期', 'Period')} {probe.periodSeconds ?? 10}s ·{' '}
                              {text('超时', 'Timeout')} {probe.timeoutSeconds ?? 1}s
                            </Text>
                            <Text type="secondary">
                              {text('成功阈值', 'Success threshold')} {probe.successThreshold ?? 1}{' '}
                              · {text('失败阈值', 'Failure threshold')}{' '}
                              {probe.failureThreshold ?? 3}
                            </Text>
                          </>
                        ) : (
                          <Text type="secondary">{text('未配置', 'Not configured')}</Text>
                        )}
                      </div>
                    )
                  })}
                </div>
              </>
            ),
          }
        })}
      />
    </div>
  )
}
