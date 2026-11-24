import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import {
  Button,
  Drawer,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Typography,
} from 'antd'
import { DeploymentUnitOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import {
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { ManagementRefreshButton, ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag, MetadataTag } from '@/components/status-tag'
import { OverviewChartSlot } from '@/components/overview/overview-chart-slot'
import { formatMetricValue } from '@/components/resource-metrics-format'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  podQueries,
  podKeys,
  deploymentMutations,
  useClusterCapabilityForCluster,
  useRealtimeSessionDock,
  buildClusterScopedPath,
  buildRelatedResourcePath,
  RelatedResourceYaml,
} from '@/features/platform'
import { useI18n, localeText } from '@/i18n'
import { toScopeKey } from '@/types'
import { formatAgeSeconds } from '@/utils/time'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { deliveryQueries } from '../queries'
import { deliveryMutations } from '../mutations'
import { deliveryKeys } from '../keys'
import { deliveryApi } from '../api'
import type {
  ApplicationRuntimeWorkload,
  ApplicationServicePods,
  ApplicationWorkloadRuntimeDetail,
  Pod,
  ResourceMetrics,
} from '../types'
import {
  ResourceRelationWorkbench,
  relationDisplayNodes,
  type RelationDisplayNode,
  type RelationWorkbenchData,
} from './resource-relation-workbench'
import { ServiceAddresses } from '../applications/service-addresses'
import { createPodPreviewRequestLimit } from './pod-preview-requests'
import './workload-detail-page.css'
import './service-pod-workspace.css'

const { Text } = Typography
const PodInspection = lazy(async () => ({
  default: (await import('./pod-inspection')).VerifiedPodInspection,
}))
const ResourceMetricsPanel = lazy(async () => ({
  default: (await import('@/components/resource-metrics-panel')).ResourceMetricsPanel,
}))
type PodRow = {
  pod: Pod
  workload: Pick<ApplicationRuntimeWorkload, 'clusterId' | 'workloadKind' | 'workloadName'>
}
const rowKey = ({ pod, workload }: PodRow) =>
  JSON.stringify([workload.clusterId, pod.namespace, pod.name, pod.uid])
const maxPodMetricPreviews = 4
const runPodMetricPreview = createPodPreviewRequestLimit(maxPodMetricPreviews)
function useText() {
  const { localeCode } = useI18n()
  return (zh: string, en: string) => localeText(localeCode, zh, en)
}

function usePodMetricPreviews(candidates: PodRow[]) {
  const listRef = useRef<HTMLDivElement>(null)
  const [visible, setVisible] = useState<{ scope: string; keys: Set<string> }>()
  const scope = JSON.stringify(candidates.map(rowKey))
  const observe = typeof IntersectionObserver !== 'undefined'
  useEffect(() => {
    if (!observe || !listRef.current) return
    const observer = new IntersectionObserver((entries) => {
      setVisible((previous) => {
        const keys = new Set(previous?.scope === scope ? previous.keys : undefined)
        for (const entry of entries) {
          const key = entry.target.getAttribute('data-pod-metric-key')
          if (!key) continue
          if (entry.isIntersecting) keys.add(key)
          else keys.delete(key)
        }
        return { scope, keys }
      })
    })
    for (const target of listRef.current.querySelectorAll('[data-pod-metric-key]'))
      observer.observe(target)
    return () => observer.disconnect()
  }, [observe, scope])
  const active = candidates
    .filter(
      (row) =>
        row.pod.uid && (!observe || (visible?.scope === scope && visible.keys.has(rowKey(row)))),
    )
    .slice(0, maxPodMetricPreviews)
  const queries = useQueries({
    queries: active.map(({ pod, workload }) => {
      const options = podQueries.metrics(
        toScopeKey(workload.clusterId, pod.namespace),
        pod.name,
        60,
        pod.uid,
      )
      return {
        ...options,
        // Preview observers own transport cancellation independently of full metric views.
        queryKey: podKeys.deliveryPreview(options.queryKey),
        queryFn: ({ signal }: { signal: AbortSignal }) =>
          runPodMetricPreview(signal, () =>
            deliveryApi.workloads.podMetricsPreview(
              workload.clusterId,
              pod.namespace,
              pod.name,
              pod.uid!,
              signal,
            ),
          ),
        refetchInterval: 30_000,
        refetchIntervalInBackground: false,
      }
    }),
  })
  return { listRef, previews: new Map(active.map((row, index) => [rowKey(row), queries[index]])) }
}

function PodMetricsPreview({
  row,
  query,
  onOpen,
}: {
  row: PodRow
  query?: UseQueryResult<ResourceMetrics>
  onOpen: () => void
}) {
  const text = useText()
  return (
    <button
      type="button"
      className="soha-pod-card-metrics"
      data-pod-metric-key={rowKey(row)}
      aria-label={text('查看监控 ', 'View metrics ') + row.pod.name}
      onClick={onOpen}
    >
      {['cpu', 'memory'].map((key) => {
        const series =
          row.pod.uid && !query?.isError && query?.data?.configured
            ? query.data.series?.find((item) => item.key === key)
            : undefined
        const points = (series?.points ?? [])
          .filter((point) => Number.isFinite(point.value))
          .map((point) => point.value)
        return (
          <span className="soha-pod-card-metric" key={key}>
            <span className="soha-pod-card-metric-label">
              {key === 'cpu' ? 'CPU' : text('内存', 'Memory')}
            </span>
            <strong>{series ? formatMetricValue(series.latest, series.unit) : '—'}</strong>
            <span
              className="soha-pod-card-chart"
              aria-label={key === 'cpu' ? 'CPU 趋势' : text('内存趋势', 'Memory trend')}
            >
              {points.length > 1 ? (
                <OverviewChartSlot chart={{ kind: 'line', data: points }} />
              ) : (
                <span className="soha-pod-card-chart-empty">
                  {query?.isPending ? text('读取中', 'Loading') : text('暂无数据', 'No data')}
                </span>
              )}
            </span>
          </span>
        )
      })}
      {query || !row.pod.uid ? (
        <span className="soha-pod-card-metrics-note">
          {!row.pod.uid
            ? text('Pod 身份待核对', 'Pod identity unverified')
            : query?.isError
              ? text('指标读取失败', 'Metrics unavailable')
              : query?.isPending
                ? text('读取指标', 'Loading metrics')
                : !query?.data?.configured
                  ? text('未接入监控', 'Monitoring not configured')
                  : query.data.series?.length
                    ? text('近 1 小时', 'Last hour')
                    : text('暂无 UID 已确认的指标', 'No UID-confirmed metrics')}
        </span>
      ) : null}
    </button>
  )
}

export function PodMetricsDetail({
  row,
  range,
  onRangeChange,
}: {
  row: {
    pod: Pick<PodRow['pod'], 'namespace' | 'name' | 'uid' | 'containerStatuses'>
    workload: Pick<PodRow['workload'], 'clusterId'>
  }
  range: number
  onRangeChange: (range: number) => void
}) {
  const [selection, setSelection] = useState<{ uid: string; container?: string }>()
  const containers = [...new Set((row.pod.containerStatuses ?? []).map(({ name }) => name))]
  const container =
    selection &&
    selection.uid === row.pod.uid &&
    selection.container &&
    containers.includes(selection.container)
      ? selection.container
      : undefined
  const options = podQueries.metrics(
    toScopeKey(row.workload.clusterId, row.pod.namespace),
    row.pod.name,
    range,
    row.pod.uid,
    container,
  )
  const query = useQuery({
    ...options,
    enabled: options.enabled && Boolean(row.pod.uid),
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })

  if (!row.pod.uid)
    return <ManagementState compact kind="empty" title="Pod 缺少 UID，无法核对历史样本" />
  return (
    <Suspense fallback={<ManagementState compact kind="loading" />}>
      {containers.length ? (
        <Select
          aria-label="指标容器"
          value={container ?? ''}
          onChange={(value) => setSelection({ uid: row.pod.uid!, container: value })}
          options={[
            { value: '', label: '全部容器' },
            ...containers.map((name) => ({ value: name, label: name })),
          ]}
        />
      ) : null}
      <ResourceMetricsPanel
        title="Pod 指标"
        data={query.isFetching || query.isError ? undefined : query.data}
        loading={query.isPending || query.isFetching}
        rangeMinutes={range}
        onRangeChange={onRangeChange}
        errorMessage={query.error instanceof Error ? query.error.message : undefined}
        compact
      />
    </Suspense>
  )
}

function SessionOpener({
  row,
  kind,
  container,
  onClose,
}: {
  row: PodRow
  kind: 'logs' | 'terminal'
  container?: string
  onClose: () => void
}) {
  const { openSession } = useRealtimeSessionDock()
  const opened = useRef(false)
  useEffect(() => {
    if (opened.current) return
    opened.current = true
    if (!row.pod.uid) return
    openSession({
      kind,
      clusterId: row.workload.clusterId,
      namespace: row.pod.namespace,
      podName: row.pod.name,
      podUid: row.pod.uid,
      container,
      ...(kind === 'terminal' ? { shell: '/bin/sh' } : {}),
    })
    onClose()
  }, [container, kind, onClose, openSession, row])
  return null
}

export function ServiceRuntimeSummary({
  applicationId,
  workloads,
}: {
  applicationId: string
  workloads: ApplicationRuntimeWorkload[]
}) {
  const permissions = usePermissionSnapshot().data?.data
  if (!hasPermission(permissions, 'delivery.applications.view')) return null
  return (
    <div className="soha-service-runtime-summary">
      {workloads.map((workload) => (
        <ServiceAddresses
          key={`${workload.clusterId}/${workload.namespace}/${workload.workloadName}`}
          applicationId={applicationId}
          workload={workload}
        />
      ))}
    </div>
  )
}

export function ServiceRuntimeActions({
  applicationId,
  workloads: inputWorkloads,
  labelTargets = false,
}: {
  labelTargets?: boolean
  applicationId: string
  workloads: ApplicationRuntimeWorkload[]
}) {
  const workloads = inputWorkloads.filter(
    (workload) => workload.workloadKind === 'Deployment' && !workload.sourceStatus,
  )
  const text = useText()
  const { localeCode } = useI18n()
  const [scaleIndex, setScaleIndex] = useState<number | null>(null)
  const [replicas, setReplicas] = useState<number | null>(null)
  const scaleTarget = scaleIndex === null ? undefined : workloads[scaleIndex]
  const capability = useClusterCapabilityForCluster(
    'workload.mutations',
    localeCode,
    scaleTarget?.clusterId,
  )
  const permissions = usePermissionSnapshot().data?.data
  const canReadRuntime = hasPermission(permissions, 'delivery.applications.view')
  const canRestart = hasPermission(permissions, 'delivery.application.update')
  const client = useQueryClient()
  const restart = useMutation(deliveryMutations.workloads.restart(client))
  const scale = useMutation(deploymentMutations.scale(client))
  const queries = useQueries({
    queries: workloads.map((workload) =>
      deliveryQueries.workloads.runtime(
        {
          applicationId,
          applicationEnvironmentId: workload.applicationEnvironmentId,
          workloadName: workload.workloadName,
        },
        canReadRuntime,
      ),
    ),
  })
  if (!canReadRuntime || !workloads.length) return null
  return (
    <div className="soha-service-runtime-actions">
      {workloads.map((workload, index) => (
        <div
          key={[workload.clusterId, workload.namespace, workload.workloadName].join('/')}
          className="soha-service-runtime-action-group"
        >
          {canRestart && workload.workloadKind === 'Deployment' ? (
            <Popconfirm
              title={
                (queries[index].data?.environment?.isProduction
                  ? text('确认重启生产实例 ', 'Restart production workload ')
                  : text('确认重启 ', 'Restart ')) +
                workload.workloadName +
                '？'
              }
              description={
                (queries[index].data?.environment?.isProduction ? 'PROD · ' : '') +
                (queries[index].data?.environment?.name || workload.applicationEnvironmentId) +
                ' · ' +
                workload.clusterId +
                '/' +
                workload.namespace
              }
              okText={
                queries[index].data?.environment?.isProduction
                  ? text('重启生产实例', 'Restart production workload')
                  : text('重启', 'Restart')
              }
              onConfirm={() =>
                restart.mutate({
                  clusterId: workload.clusterId,
                  namespace: workload.namespace,
                  workloadName: workload.workloadName,
                })
              }
            >
              <Button
                icon={<ReloadOutlined />}
                loading={restart.isPending}
                disabled={queries[index].isPending || queries[index].isError}
              >
                {text('重启', 'Restart')}
                {labelTargets || workloads.length > 1 ? ` ${workload.workloadName}` : ''}
              </Button>
            </Popconfirm>
          ) : null}
          {canRestart && workload.workloadKind === 'Deployment' ? (
            <Button
              disabled={
                queries[index].isPending ||
                queries[index].isError ||
                !queries[index].data?.deployment?.allowedActions?.includes('scale')
              }
              onClick={() => {
                setScaleIndex(index)
                setReplicas(
                  queries[index].data?.deployment?.desiredReplicas ?? workload.desiredReplicas,
                )
                scale.reset()
              }}
            >
              {text('扩缩容', 'Scale')}
              {labelTargets || workloads.length > 1 ? ` ${workload.workloadName}` : ''}
            </Button>
          ) : null}
          {queries[index].isError ? (
            <ManagementState
              compact
              kind="error"
              actions={
                <Button onClick={() => void queries[index].refetch()}>
                  {text('重试', 'Retry')}
                </Button>
              }
            />
          ) : null}
        </div>
      ))}
      {restart.isError ? (
        <ManagementState compact kind="error" description={restart.error.message} />
      ) : null}
      <Modal
        title={
          scaleIndex !== null && queries[scaleIndex]?.data?.environment?.isProduction
            ? text('生产环境扩缩容', 'Scale production workload')
            : text('扩缩容', 'Scale')
        }
        open={scaleIndex !== null}
        onCancel={() => setScaleIndex(null)}
        confirmLoading={scale.isPending}
        okText={text('确认扩缩容', 'Confirm scale')}
        okButtonProps={{
          disabled:
            !scaleTarget ||
            capability.disabled ||
            replicas === null ||
            !Number.isInteger(replicas) ||
            replicas < 0 ||
            scaleIndex === null ||
            queries[scaleIndex].isError ||
            !queries[scaleIndex].data?.deployment?.allowedActions?.includes('scale'),
        }}
        onOk={() => {
          if (
            !scaleTarget ||
            capability.disabled ||
            replicas === null ||
            !Number.isInteger(replicas) ||
            replicas < 0 ||
            scaleIndex === null ||
            queries[scaleIndex].isError ||
            !queries[scaleIndex].data?.deployment?.allowedActions?.includes('scale')
          )
            return
          scale.mutate(
            {
              scope: toScopeKey(scaleTarget.clusterId, scaleTarget.namespace),
              name: scaleTarget.workloadName,
              replicas,
            },
            {
              onSuccess: () => {
                setScaleIndex(null)
                void client.invalidateQueries({ queryKey: deliveryKeys.workloads.all })
                void client.invalidateQueries({ queryKey: deliveryKeys.applications.all })
              },
            },
          )
        }}
      >
        <p>
          {scaleTarget?.workloadName} · {scaleTarget?.clusterId} / {scaleTarget?.namespace}
        </p>
        <p>{scaleIndex === null ? '' : queries[scaleIndex]?.data?.environment?.name}</p>
        <label htmlFor="service-scale-replicas">{text('副本数', 'Replicas')} </label>
        <InputNumber
          id="service-scale-replicas"
          aria-label={text('副本数', 'Replicas')}
          min={0}
          precision={0}
          value={replicas}
          onChange={setReplicas}
        />
        {replicas === 0 ? (
          <p>
            {text(
              '缩容为 0 将停止此工作负载的所有实例。',
              'Scaling to zero stops all instances of this workload.',
            )}
          </p>
        ) : null}
        {capability.disabled ? <p>{capability.reason}</p> : null}
        {scale.isError ? (
          <ManagementState compact kind="error" description={scale.error.message} />
        ) : null}
      </Modal>
    </div>
  )
}

function RuntimeRelationships({
  snapshots,
  onOpenTool,
}: {
  snapshots: ApplicationWorkloadRuntimeDetail[]
  onOpenTool: (row: PodRow, tool: string) => void
}) {
  const [selectedKey, setSelectedKey] = useState('')
  const [yaml, setYaml] = useState<RelationDisplayNode>()
  const permissions = usePermissionSnapshot().data?.data
  const snapshot = selectedKey
    ? snapshots.find(
        (s) =>
          `${s.workload.clusterId}/${s.workload.namespace}/${s.workload.workloadName}` ===
          selectedKey,
      )
    : snapshots[0]
  if (!snapshot)
    return selectedKey ? (
      <ManagementState
        compact
        kind="not-found"
        title="所选工作负载不在当前运行范围"
        actions={
          <Button
            onClick={() => {
              setSelectedKey('')
              setYaml(undefined)
            }}
          >
            重新选择当前资源
          </Button>
        }
      />
    ) : null
  const workload = snapshot.workload
  const root = workload.source?.resource || {
    clusterId: workload.clusterId,
    namespace: workload.namespace,
    apiVersion: 'apps/v1',
    kind: workload.workloadKind,
    name: workload.workloadName,
    scopeMode: 'namespace' as const,
  }
  const data: RelationWorkbenchData = {
    root,
    nodes: [
      { resource: root, role: 'root', depth: 0 },
      ...(snapshot.pods || [])
        .filter((p) => p.uid)
        .map((p) => ({
          resource: {
            clusterId: workload.clusterId,
            namespace: p.namespace,
            apiVersion: 'v1',
            kind: 'Pod',
            name: p.name,
            uid: p.uid,
            scopeMode: 'namespace' as const,
          },
          role: 'reference' as const,
          depth: 1,
        })),
    ],
    edges: [],
  }
  const visibleYaml = Boolean(
    yaml &&
    relationDisplayNodes(data, snapshot.deployment?.relatedResources).some(
      (node) => node.key === yaml.key && node.resource.uid === yaml.resource.uid,
    ),
  )
  return (
    <section className="soha-runtime-relationships" aria-label="工作负载运行关系">
      {snapshots.length > 1 ? (
        <Select
          aria-label="工作负载关系范围"
          value={`${workload.clusterId}/${workload.namespace}/${workload.workloadName}`}
          options={snapshots.map((s) => ({
            value: `${s.workload.clusterId}/${s.workload.namespace}/${s.workload.workloadName}`,
            label: `${s.workload.workloadKind} / ${s.workload.workloadName}`,
          }))}
          onChange={(value) => {
            setSelectedKey(value)
            setYaml(undefined)
          }}
        />
      ) : null}
      <p>
        <Text type="secondary">
          来自当前工作负载运行快照。未提供 owner UID 链的 Pod 只标为运行关联，不推断 ReplicaSet
          或独占拥有关系。
        </Text>
      </p>
      <ResourceRelationWorkbench
        runtimeSnapshot
        key={`${root.clusterId}/${root.namespace}/${root.name}`}
        data={data}
        references={snapshot.deployment?.relatedResources}
        renderActions={(node) => {
          const pod = snapshot.pods?.find((p) => p.uid && p.uid === node.resource.uid)
          if (pod)
            return (
              <Space wrap>
                {['details', 'logs', 'terminal', 'events', 'yaml', 'metrics']
                  .filter((tool) =>
                    hasPermission(
                      permissions,
                      tool === 'logs'
                        ? 'platform.pods.logs'
                        : tool === 'terminal'
                          ? 'platform.pods.exec'
                          : 'platform.pods.view',
                    ),
                  )
                  .map((tool) => (
                    <Button key={tool} onClick={() => onOpenTool({ pod, workload }, tool)}>
                      {
                        {
                          details: '详情',
                          logs: '日志',
                          terminal: '终端',
                          events: '事件',
                          yaml: 'YAML',
                          metrics: '指标',
                        }[tool]
                      }
                    </Button>
                  ))}
              </Space>
            )
          return node.resource.kind === 'Secret' ? (
            <Text type="secondary">仅展示 Secret 引用身份，不回显数据</Text>
          ) : (
            <Button onClick={() => setYaml(node)}>
              {node.resource.uid ? '查看资源 YAML' : '按名称查看当前 YAML'}
            </Button>
          )
        }}
      />
      <Drawer
        open={visibleYaml}
        onClose={() => setYaml(undefined)}
        title={yaml ? `${yaml.resource.kind} / ${yaml.resource.name}` : '资源 YAML'}
        destroyOnHidden
        size="min(880px, 100vw)"
      >
        {visibleYaml && yaml ? (
          <>
            {!yaml.resource.uid ? (
              <p>
                此引用未固定
                UID；按名称重新读取当前同名对象，不代表原关联对象的配置，也不会在此处执行变更。
              </p>
            ) : null}
            <RelatedResourceYaml
              fresh
              target={{
                ...yaml.resource,
                namespace: yaml.resource.namespace || workload.namespace,
              }}
              expectedUid={yaml.resource.uid}
            />
          </>
        ) : null}
      </Drawer>
    </section>
  )
}

export function ServicePodWorkspace({
  applicationId,
  workloads,
  view,
  showRelationships = true,
  deploymentPodsOnly = false,
  podScope,
  onPodCount,
  projection,
  onOpenRelations,
}: {
  showRelationships?: boolean
  deploymentPodsOnly?: boolean
  applicationId: string
  workloads: ApplicationRuntimeWorkload[]
  view: 'runtime' | 'pods' | 'metrics' | 'summary' | 'related-resources'
  podScope?: string
  projection?: UseQueryResult<ApplicationServicePods>
  onOpenRelations?: () => void
  onPodCount?: (summary: {
    scope: string
    count: number | undefined
    ready?: number
    restarts?: number
  }) => void
}) {
  const text = useText()
  const [searchParams, setSearchParams] = useSearchParams()
  const [search, setSearch] = useState('')
  const [phase, setPhase] = useState('all')
  const permissions = usePermissionSnapshot().data?.data
  const canReadRuntime = hasPermission(permissions, 'delivery.applications.view')
  const canView = hasPermission(permissions, 'platform.pods.view')
  const canLogs = hasPermission(permissions, 'platform.pods.logs')
  const canExec = hasPermission(permissions, 'platform.pods.exec')
  const queries = useQueries({
    queries: (projection ? [] : workloads).map((workload) =>
      deliveryQueries.workloads.runtime(
        {
          applicationId,
          applicationEnvironmentId: workload.applicationEnvironmentId,
          workloadName: workload.workloadName,
        },
        canReadRuntime,
        view === 'summary' ? false : 5_000,
      ),
    ),
  })
  const snapshots = queries.flatMap((query) => {
    const snapshot = readableQueryData(query)
    return snapshot ? [snapshot] : []
  })
  const projectionData =
    projection && !projection.isError ? readableQueryData(projection) : undefined
  const rows = projection
    ? (projectionData?.items ?? []).map(({ pod, ...workload }) => ({ pod, workload }))
    : [
        ...new Map(
          snapshots
            .flatMap((runtime) =>
              (runtime.pods ?? []).map((pod) => ({ pod, workload: runtime.workload })),
            )
            .map((row) => [rowKey(row), row]),
        ).values(),
      ]
  const filtered = rows.filter(
    ({ pod }) =>
      (phase === 'all' || (phase === 'restarted' ? pod.restarts > 0 : pod.phase === phase)) &&
      [pod.name, pod.nodeName, pod.podIp].some((value) =>
        value?.toLowerCase().includes(search.toLowerCase()),
      ),
  )
  const { listRef, previews } = usePodMetricPreviews(
    view === 'pods' && canReadRuntime && canView && (!projection || projection.isEnabled)
      ? filtered.filter((row) => row.pod.uid)
      : [],
  )
  const readyCount = rows.filter(({ pod }) => {
    const [ready, total] = (pod.readyContainers || '').split('/').map(Number)
    return pod.phase === 'Running' && total > 0 && ready === total
  }).length
  const podCount =
    canReadRuntime &&
    canView &&
    (projection
      ? projectionData?.status === 'available'
      : queries.length > 0 && queries.every((query) => query.isSuccess))
      ? rows.length
      : undefined
  const restarts = rows.every(({ pod }) => Number.isFinite(pod.restarts) && pod.restarts >= 0)
    ? rows.reduce((total, { pod }) => total + pod.restarts, 0)
    : undefined
  useEffect(() => {
    if (podScope)
      onPodCount?.({
        scope: podScope,
        count: podCount,
        ready: podCount === undefined ? undefined : readyCount,
        restarts: podCount === undefined ? undefined : restarts,
      })
  }, [onPodCount, podCount, podScope, readyCount, restarts])
  const tool = searchParams.get('tool') || 'details'
  const selected = rows.filter(
    ({ pod, workload }) =>
      Boolean(pod.uid) &&
      pod.name === searchParams.get('pod') &&
      (projection || searchParams.has('podUid') ? pod.uid === searchParams.get('podUid') : true) &&
      (!searchParams.get('podCluster') || workload.clusterId === searchParams.get('podCluster')) &&
      (!searchParams.get('podNamespace') || pod.namespace === searchParams.get('podNamespace')),
  )
  const selectedRow = selected.length === 1 ? selected[0] : undefined
  const openTool = (row: PodRow, nextTool: string) => {
    if (!row.pod.uid) return
    const next = new URLSearchParams(searchParams)
    if (nextTool === 'metrics') {
      next.set('serviceTab', 'metrics')
      next.set('metricPod', row.pod.name)
      next.set('metricCluster', row.workload.clusterId)
      next.set('metricNamespace', row.pod.namespace)
      next.set('metricUid', row.pod.uid)
      next.delete('metricRoot')
      for (const key of ['pod', 'podUid', 'podCluster', 'podNamespace', 'tool', 'container'])
        next.delete(key)
      setSearchParams(next)
      return
    }
    if (view === 'runtime') next.set('serviceTab', 'pods')
    next.set('pod', row.pod.name)
    next.set('podUid', row.pod.uid)
    next.set('podCluster', row.workload.clusterId)
    next.set('podNamespace', row.pod.namespace)
    next.set('tool', nextTool)
    setSearchParams(next)
  }
  const closeTool = () => {
    const next = new URLSearchParams(searchParams)
    ;['pod', 'podUid', 'podCluster', 'podNamespace', 'tool', 'container'].forEach((key) =>
      next.delete(key),
    )
    setSearchParams(next)
  }
  const requestedPod = searchParams.get('pod')
  const allowed = tool === 'logs' ? canLogs : tool === 'terminal' ? canExec : canView
  const waiting = projection ? projection.isPending : queries.some((query) => query.isPending)
  const failed = projection ? projection.isError : queries.some((query) => query.isError)
  const retry = () => {
    if (projection?.isEnabled) void projection.refetch()
    else queries.forEach((query) => void query.refetch())
  }
  const session = tool === 'logs' || tool === 'terminal'
  useEffect(() => {
    if (tool !== 'metrics' || !requestedPod) return
    const next = new URLSearchParams(searchParams)
    next.set('serviceTab', 'metrics')
    next.set('metricPod', requestedPod)
    next.delete('metricUid')
    next.delete('metricRoot')
    for (const [source, target] of [
      ['podCluster', 'metricCluster'],
      ['podNamespace', 'metricNamespace'],
      ['podUid', 'metricUid'],
    ]) {
      const value = next.get(source)
      if (value) next.set(target, value)
    }
    for (const key of ['pod', 'podUid', 'podCluster', 'podNamespace', 'tool', 'container'])
      next.delete(key)
    setSearchParams(next, { replace: true })
  }, [requestedPod, searchParams, setSearchParams, tool])
  const labels: Record<string, string> = {
    details: text('详情', 'Details'),
    metrics: text('监控', 'Metrics'),
    events: text('事件', 'Events'),
    yaml: 'YAML',
    logs: text('日志', 'Logs'),
    terminal: text('终端', 'Terminal'),
  }
  if (view === 'summary') return null
  if (!canReadRuntime)
    return showRelationships || view !== 'runtime' ? (
      <ManagementState compact kind="no-permission" />
    ) : null
  if (projection && (!projection.isEnabled || !canView || queryAccessDenied(projection)))
    return (
      <ManagementState
        compact
        kind="no-permission"
        title={text('无 Pod 查看权限', 'No permission to view Pods')}
      />
    )
  const projectionStatus = projectionData?.status
  const observationReason = {
    forbidden: text(
      '无法读取一个或多个运行来源。',
      'Access was denied for one or more runtime sources.',
    ),
    timeout: text(
      '部分运行来源的观测超时。',
      'Observation timed out for one or more runtime sources.',
    ),
    identity_mismatch: text(
      '源资源身份已变化，请核对资源关系。',
      'Source identity changed. Check its resource relationships.',
    ),
    ownership_unverified: text(
      '尚未证实这些实例属于当前服务。',
      'Pod ownership has not been verified for this service.',
    ),
    provider_unsupported: text(
      '当前连接方式无法验证 Pod 归属。',
      'The connection cannot verify Pod ownership.',
    ),
    observation_limit: text('本次观测达到读取上限。', 'The observation limit was reached.'),
    source_unavailable: text(
      '一个或多个运行来源暂不可用。',
      'One or more runtime sources are unavailable.',
    ),
  } as Record<string, string>
  const relationFeedback =
    projectionStatus && projectionStatus !== 'available' ? (
      <ManagementState
        compact
        bordered={false}
        kind={projectionStatus === 'unavailable' ? 'error' : 'not-configured'}
        description={observationReason[projectionData?.reason || '']}
        title={
          {
            partial: text('仅显示已验证归属的 Pod', 'Showing only Pods with verified ownership'),
            relation_only: text(
              '此资源的 Pod 关联见资源关系',
              'See resource relationships for Pod ownership',
            ),
            unconfigured: text(
              '当前环境尚未部署此服务',
              'This service is not configured in this environment',
            ),
            unavailable: text('Pod 观测暂不可用', 'Pod observation unavailable'),
          }[projectionStatus]
        }
        actions={
          <Space wrap>
            {projectionStatus === 'unavailable' || projectionStatus === 'partial' ? (
              <Button onClick={retry}>{text('重试', 'Retry')}</Button>
            ) : null}
            {onOpenRelations ? (
              <Button onClick={onOpenRelations}>
                {text('查看资源关系', 'View resource relationships')}
              </Button>
            ) : null}
          </Space>
        }
      />
    ) : null
  if (relationFeedback && !rows.length) return relationFeedback
  if (view === 'metrics') {
    if (!canView) return <ManagementState compact kind="no-permission" />
    const requested = searchParams.get('metricPod')
    const candidates = requested
      ? rows.filter(
          ({ pod, workload }) =>
            Boolean(pod.uid) &&
            pod.name === requested &&
            (projection || searchParams.has('metricUid')
              ? pod.uid === searchParams.get('metricUid')
              : true) &&
            (!searchParams.get('metricCluster') ||
              workload.clusterId === searchParams.get('metricCluster')) &&
            (!searchParams.get('metricNamespace') ||
              pod.namespace === searchParams.get('metricNamespace')),
        )
      : []
    const row = candidates.length === 1 ? candidates[0] : undefined
    const requestedRange = Number(searchParams.get('metricRange') || 60)
    const range = [15, 60, 360, 1440].includes(requestedRange) ? requestedRange : 60
    return (
      <section
        className="soha-service-metrics"
        aria-label={text('当前服务指标', 'Current service metrics')}
      >
        {relationFeedback}
        <Space wrap>
          <span>{text('资源范围', 'Resource')}</span>
          <Select
            aria-label={text('指标资源范围', 'Metrics resource')}
            className="soha-service-metrics__target"
            value={row ? rowKey(row) : undefined}
            placeholder={text('选择 Pod', 'Select a Pod')}
            options={rows.map((item) => ({
              value: rowKey(item),
              label: `${item.workload.workloadName} / ${item.pod.name}`,
            }))}
            onChange={(value) => {
              const target = rows.find((item) => rowKey(item) === value)
              if (target) openTool(target, 'metrics')
            }}
          />
        </Space>
        {waiting && !row ? (
          <ManagementState compact kind="loading" />
        ) : !row ? (
          <ManagementState
            compact
            kind={failed ? 'error' : requested ? 'not-found' : 'empty'}
            title={
              failed
                ? text('运行资源读取失败', 'Runtime resources unavailable')
                : requested
                  ? text('所选 Pod 不存在或无法唯一定位', 'Selected Pod missing or ambiguous')
                  : rows.length
                    ? text('请选择 Pod 查看指标', 'Select a Pod to view metrics')
                    : text(
                        '当前服务暂无可读取的 Pod 指标',
                        'No Pod metrics available for this service',
                      )
            }
            actions={failed ? <Button onClick={retry}>{text('重试', 'Retry')}</Button> : undefined}
          />
        ) : (
          <>
            <Text type="secondary">
              {row.workload.clusterId} / {row.pod.namespace} / {row.pod.name}
            </Text>
            <Text type="secondary">
              {text(
                '当前数据源提供 Pod 级指标；容器级指标以数据源实际能力为准。',
                'This data source provides Pod metrics; container metrics depend on the source capabilities.',
              )}
            </Text>
            <PodMetricsDetail
              key={rowKey(row)}
              row={row}
              range={range}
              onRangeChange={(value) => {
                const next = new URLSearchParams(searchParams)
                next.set('metricRange', String(value))
                setSearchParams(next)
              }}
            />
          </>
        )}
      </section>
    )
  }
  return (
    <div className="soha-service-pods">
      {relationFeedback}
      {!projection &&
      failed &&
      (rows.length > 0 || (view === 'runtime' && showRelationships && snapshots.length > 0)) ? (
        <ReadFeedback query={queries.find((query) => query.isError)!} label="Pod 运行数据" />
      ) : null}
      {view === 'related-resources' ? (
        <div className="soha-pod-related-list">
          {waiting ? (
            <ManagementState compact kind="loading" />
          ) : failed && !rows.length ? (
            <ManagementState
              compact
              kind="error"
              actions={
                <Button onClick={() => queries.forEach((query) => void query.refetch())}>
                  {text('重试', 'Retry')}
                </Button>
              }
            />
          ) : null}
          {Array.from(
            new Map(
              queries
                .flatMap((query, index) =>
                  (query.data?.deployment?.relatedResources ?? []).map((resource) => ({
                    resource: {
                      ...resource,
                      namespace: resource.namespace || query.data?.workload.namespace,
                    },
                    cluster: workloads[index].clusterId,
                  })),
                )
                .map((item) => [
                  [
                    item.cluster,
                    item.resource.kind,
                    item.resource.namespace,
                    item.resource.name,
                  ].join('/'),
                  item,
                ]),
            ).values(),
          ).map(({ resource, cluster }) => {
            const path = buildRelatedResourcePath(resource, resource.namespace || null, cluster)
            return (
              <div key={[cluster, resource.kind, resource.namespace, resource.name].join('/')}>
                <MetadataTag label={resource.kind} />
                {path ? (
                  <Link
                    to={buildClusterScopedPath(
                      cluster,
                      path.split('?')[0].replace(/^\//, ''),
                      resource.namespace,
                    )}
                  >
                    {resource.name}
                  </Link>
                ) : (
                  <Text>{resource.name}</Text>
                )}
                <Text type="secondary">{resource.namespace}</Text>
              </div>
            )
          })}
          {!waiting &&
          !failed &&
          queries.every((query) => !query.data?.deployment?.relatedResources?.length) ? (
            <ManagementState
              compact
              kind="empty"
              title={text('暂无关联资源', 'No related resources')}
            />
          ) : null}
        </div>
      ) : view === 'runtime' ? (
        showRelationships ? (
          waiting && !snapshots.length ? (
            <ManagementState compact kind="loading" />
          ) : failed && !snapshots.length ? (
            <ManagementState
              compact
              kind="error"
              actions={
                <Button onClick={() => queries.forEach((query) => void query.refetch())}>
                  {text('重试', 'Retry')}
                </Button>
              }
            />
          ) : (
            <RuntimeRelationships snapshots={snapshots} onOpenTool={openTool} />
          )
        ) : null
      ) : (
        <>
          <div
            className="soha-pod-list-filter"
            role="group"
            aria-label={text('Pod 筛选', 'Pod filters')}
          >
            <div className="soha-pod-list-filter__summary">
              {deploymentPodsOnly ? <Text type="secondary">Deployment</Text> : null}
              {podCount !== undefined || (!projection && rows.length > 0) ? (
                <Text type="secondary">
                  {podCount === undefined ? text('已读取 ', 'Loaded ') : ''}
                  {rows.length} {text('个 Pod', 'Pods')} · {readyCount} {text('个就绪', 'ready')}
                  {search || phase !== 'all'
                    ? ` · ${filtered.length} ${text('个匹配', 'matching')}`
                    : ''}
                </Text>
              ) : null}
            </div>
            <Input
              aria-label={text('搜索 Pod', 'Search Pods')}
              prefix={<SearchOutlined />}
              placeholder={text('搜索名称、节点或 IP', 'Search name, node or IP')}
              value={search}
              allowClear
              onChange={(event) => setSearch(event.target.value)}
            />
            <Select
              aria-label={text('Pod 状态', 'Pod status')}
              value={phase}
              onChange={setPhase}
              options={[
                { value: 'all', label: text('全部状态', 'All statuses') },
                { value: 'restarted', label: text('有重启记录', 'Has restarts') },
                ...Array.from(new Set(rows.map(({ pod }) => pod.phase))).map((value) => ({
                  value,
                  label: value,
                })),
              ]}
            />
            <ManagementRefreshButton
              aria-label={text('刷新 Pods', 'Refresh Pods')}
              tooltip={text('刷新 Pods', 'Refresh Pods')}
              loading={
                projection ? projection.isFetching : queries.some((query) => query.isFetching)
              }
              onClick={retry}
            />
          </div>
          {waiting && !rows.length ? (
            <ManagementState compact kind="loading" />
          ) : failed && !rows.length ? (
            <ManagementState
              compact
              kind="error"
              title={text('Pod 列表读取失败', 'Could not load Pods')}
              actions={<Button onClick={retry}>{text('重试', 'Retry')}</Button>}
            />
          ) : !filtered.length ? (
            <ManagementState
              compact
              bordered={false}
              kind="empty"
              title={
                rows.length
                  ? text('没有匹配的 Pod', 'No matching Pods')
                  : text('当前服务暂无 Pod', 'No Pods for this service')
              }
            />
          ) : (
            <div
              className="soha-pod-list-scroll"
              ref={listRef}
              data-testid={projection ? 'application-service-pod-list' : undefined}
              role="region"
              aria-label={text('Pod 列表', 'Pod list')}
              tabIndex={0}
            >
              <ul className="soha-pod-card-list">
                {filtered.map((row) => (
                  <li
                    key={rowKey(row)}
                    className="soha-pod-card"
                    data-testid={projection ? 'application-service-pod-card' : undefined}
                  >
                    <div className="soha-pod-card-heading">
                      <DeploymentUnitOutlined aria-hidden="true" />
                      <Text strong className="soha-pod-card-name">
                        {row.pod.name}
                      </Text>
                      <StatusTag
                        value={
                          row.pod.phase === 'Running' &&
                          row.pod.readyContainers &&
                          row.pod.readyContainers.split('/')[0] !==
                            row.pod.readyContainers.split('/')[1]
                            ? 'pending'
                            : row.pod.phase
                        }
                        label={
                          row.pod.phase === 'Running' &&
                          row.pod.readyContainers &&
                          row.pod.readyContainers.split('/')[0] !==
                            row.pod.readyContainers.split('/')[1]
                            ? text('未就绪', 'Not ready')
                            : undefined
                        }
                      />
                    </div>
                    <div className="soha-pod-card-facts">
                      <span>
                        <span>{text('就绪', 'Ready')}</span>{' '}
                        <strong>{row.pod.readyContainers || '—'}</strong>
                      </span>
                      <span>
                        <span>{text('重启', 'Restarts')}</span> <strong>{row.pod.restarts}</strong>
                      </span>
                      <span>
                        <span>{text('节点', 'Node')}</span>{' '}
                        <strong>{row.pod.nodeName || '—'}</strong>
                      </span>
                      <span>
                        <span>IP</span> <strong>{row.pod.podIp || '—'}</strong>
                      </span>
                      <span>
                        <span>{text('时长', 'Age')}</span>{' '}
                        <strong>{formatAgeSeconds(row.pod.ageSeconds)}</strong>
                      </span>
                    </div>
                    {projection ? (
                      <Text type="secondary">
                        {row.workload.workloadKind} / {row.workload.workloadName} · UID{' '}
                        {row.pod.uid?.slice(0, 8)}
                      </Text>
                    ) : null}
                    {row.pod.containerStatuses?.length ? (
                      <ul
                        className="soha-pod-card-container-states"
                        aria-label={text('容器状态', 'Container states')}
                      >
                        {row.pod.containerStatuses.map((container) => (
                          <li key={container.name}>
                            <Text strong>{container.name}</Text>{' '}
                            <Text type="secondary">
                              {container.kind === 'container'
                                ? text('主容器', 'Container')
                                : container.kind === 'init'
                                  ? 'Init'
                                  : 'Sidecar'}{' '}
                              · {container.ready ? text('就绪', 'Ready') : container.state}
                              {container.reason ? ` · ${container.reason}` : ''}
                              {container.exitCode != null ? ` · Exit ${container.exitCode}` : ''}
                              {container.lastTerminationReason ||
                              container.lastTerminationExitCode != null
                                ? ` · ${text('上次退出', 'Previous exit')}${container.lastTerminationReason ? ` ${container.lastTerminationReason}` : ''}${container.lastTerminationExitCode != null ? ` · Exit ${container.lastTerminationExitCode}` : ''}`
                                : ''}
                            </Text>
                          </li>
                        ))}
                      </ul>
                    ) : null}
                    {canView && row.pod.uid ? (
                      <PodMetricsPreview
                        row={row}
                        query={previews.get(rowKey(row))}
                        onOpen={() => openTool(row, 'metrics')}
                      />
                    ) : null}
                    <Space size={8} wrap className="soha-pod-card-actions">
                      {row.pod.uid
                        ? ['details', 'logs', 'terminal', 'events', 'yaml']
                            .filter((item) =>
                              item === 'logs' ? canLogs : item === 'terminal' ? canExec : canView,
                            )
                            .map((item) => (
                              <Button
                                autoInsertSpace={false}
                                color="primary"
                                variant="filled"
                                size="small"
                                key={item}
                                onClick={() => openTool(row, item)}
                              >
                                {labels[item]}
                              </Button>
                            ))
                        : null}
                    </Space>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
      {requestedPod && session && selectedRow && allowed ? (
        <SessionOpener
          key={rowKey(selectedRow) + tool}
          row={selectedRow}
          kind={tool as 'logs' | 'terminal'}
          container={searchParams.get('container') || undefined}
          onClose={closeTool}
        />
      ) : (
        <Drawer
          open={Boolean(requestedPod)}
          destroyOnHidden
          size="min(1040px, 100vw)"
          rootClassName="soha-pod-drawer"
          closable={{ placement: 'end' }}
          classNames={{
            header: 'soha-pod-drawer-header',
            body: 'soha-pod-drawer-body',
            title: 'soha-pod-drawer-title-slot',
          }}
          title={
            <div className="soha-pod-drawer-title">
              <strong>
                {tool === 'yaml'
                  ? 'Pod YAML'
                  : tool === 'events'
                    ? text('Pod事件', 'Pod events')
                    : text('Pod详情', 'Pod details')}
              </strong>
              <span className="soha-pod-drawer-identity">
                <Text type="secondary" copyable={{ text: requestedPod || '' }}>
                  {requestedPod}
                </Text>
                {selectedRow?.pod.namespace || searchParams.get('podNamespace') ? (
                  <span> · {selectedRow?.pod.namespace || searchParams.get('podNamespace')}</span>
                ) : null}
              </span>
            </div>
          }
          onClose={closeTool}
        >
          {requestedPod ? (
            !allowed ? (
              <ManagementState compact kind="no-permission" />
            ) : waiting ? (
              <ManagementState compact kind="loading" />
            ) : !selectedRow ? (
              <ManagementState
                compact
                kind="not-found"
                title={text('Pod 不存在或无法唯一定位', 'Pod missing or ambiguous')}
                description={text(
                  '请关闭后重新选择，操作不会自动切换到其他 Pod。',
                  'Close and select a Pod again. Actions will not switch targets automatically.',
                )}
              />
            ) : (
              <Suspense fallback={<ManagementState compact kind="loading" />}>
                <dl className="soha-pod-drawer-context">
                  {[
                    ['Namespace', selectedRow.pod.namespace],
                    ['Pod IP', selectedRow.pod.podIp || '—'],
                    [text('节点', 'Node'), selectedRow.pod.nodeName || '—'],
                    [text('集群', 'Cluster'), selectedRow.workload.clusterId],
                  ].map(([label, value]) => (
                    <div key={label}>
                      <dt>{label}</dt>
                      <dd>{value}</dd>
                    </div>
                  ))}
                </dl>
                <PodInspection
                  clusterId={selectedRow.workload.clusterId}
                  namespace={selectedRow.pod.namespace}
                  podName={selectedRow.pod.name}
                  expectedUid={selectedRow.pod.uid}
                  mode={tool === 'events' || tool === 'yaml' ? tool : 'details'}
                />
              </Suspense>
            )
          ) : null}
        </Drawer>
      )}
    </div>
  )
}
