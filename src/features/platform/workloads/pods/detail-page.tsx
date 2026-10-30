import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { PodDetailEvents } from './pod-detail-events'
import { PodContainerSquare, PodDetailOverview } from './pod-detail-overview'
import { PodResourceCell } from './pod-resource-cell'
import { KubernetesResourceActionsMenu } from '@/features/platform/shared/kubernetes-resource-actions-menu'
import './pod-detail.css'
import { StatusTag } from '@/components/status-tag'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAIPageContext } from '@/features/copilot'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import { useRealtimeSessionDock } from '@/features/platform/session-dock'
import {
  buildRelatedResourcePath,
  buildVolumeDetailPath,
  formatVolumeTypeLabel,
} from '@/features/platform/workloads-model'
import '@/features/platform/workloads/styles.css'
import { useI18n } from '@/i18n'
import type { PodVolume, PodVolumeMount } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatRelativeTime } from '@/utils/time'
import {
  ArrowLeftOutlined,
  CodeOutlined,
  FileTextOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType, TabsProps } from 'antd'
import { Button, Card, Select, Space, Spin, Tag, Tooltip, Typography } from 'antd'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { Link as RouterLink, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useWorkloadDetailScope } from '../shared/detail-scope'
import { WorkloadDetailShell } from '../shared/detail-shell'
import { podQueries } from './queries'

const { Link, Text } = Typography

const PodLogViewer = lazy(async () => {
  const mod = await import('@/components/pod-log-viewer')
  return { default: mod.PodLogViewer }
})

const PodTerminal = lazy(async () => {
  const mod = await import('@/components/pod-terminal')
  return { default: mod.PodTerminal }
})

const ResourceMetricsPanel = lazy(async () => {
  const mod = await import('./pod-detail-metrics')
  return { default: mod.PodDetailMetrics }
})

const PodDetailResources = lazy(async () => {
  const mod = await import('./pod-detail-resources')
  return { default: mod.PodDetailResources }
})

/* ─── shared helpers ─── */

function renderVolumeMounts(mounts: PodVolumeMount[] | undefined) {
  if (!mounts || mounts.length === 0) {
    return <Text type="secondary">N/A</Text>
  }
  return (
    <div className="soha-volume-mount-list">
      {mounts.map((mount) => (
        <div
          key={`${mount.name}:${mount.mountPath}:${mount.subPath || '-'}`}
          className="soha-volume-mount-item"
        >
          <Text className="soha-volume-mount-name">{mount.name}</Text>
          <Text type="secondary" className="soha-volume-mount-path">
            {mount.subPath ? `${mount.mountPath} (${mount.subPath})` : mount.mountPath}
          </Text>
          {mount.readOnly ? <Tag className="soha-volume-mount-badge">RO</Tag> : null}
        </div>
      ))}
    </div>
  )
}

function summarizeVolumeDetail(volume: PodVolume, localeCode: 'zh_CN' | 'en_US') {
  if (volume.sourceName && ['ConfigMap', 'Secret', 'PersistentVolumeClaim'].includes(volume.type)) {
    return volume.sourceName
  }
  const preferredDetail =
    (volume.details ?? []).find(
      (item) =>
        item.startsWith('Sources:') || item.startsWith('Medium:') || item.startsWith('SizeLimit:'),
    ) ?? volume.details?.[0]
  return preferredDetail || (localeCode === 'zh_CN' ? '无' : 'N/A')
}

function renderVolumeDetail(
  volume: PodVolume,
  localeCode: 'zh_CN' | 'en_US',
  detailNamespace: string | null,
  navigate: ReturnType<typeof useNavigate>,
  clusterId: string | null,
) {
  const targetPath = buildVolumeDetailPath(volume, detailNamespace, clusterId)
  const summary = summarizeVolumeDetail(volume, localeCode)
  if (!targetPath) {
    return (
      <Text type="secondary" className="soha-volume-detail-value">
        {summary}
      </Text>
    )
  }
  return (
    <Link className="soha-volume-detail-link" onClick={() => navigate(targetPath)}>
      {summary}
    </Link>
  )
}

export function PodDetailPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canViewPodLogs = hasPermission(permissionSnapshot, 'platform.pods.logs')
  const canExecPod = hasPermission(permissionSnapshot, 'platform.pods.exec')
  const params = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const podName = params.podName as string
  const { clusterId, namespace: detailNamespace } = useWorkloadDetailScope()
  const { openSession, closeSession, sessions = [] } = useRealtimeSessionDock()
  const [terminalShell, setTerminalShell] = useState('/bin/sh')
  const requestedTab =
    searchParams.get('tab') === 'relationships'
      ? 'related-resources'
      : searchParams.get('tab') || 'overview'
  const activeTabKey =
    [
      'overview',
      'logs',
      'terminal',
      'events',
      'metrics',
      'volumes',
      'related-resources',
      'yaml',
    ].includes(requestedTab) &&
    !(requestedTab === 'logs' && !canViewPodLogs) &&
    !(requestedTab === 'terminal' && !canExecPod)
      ? requestedTab
      : 'overview'
  const parsedRange = Number(searchParams.get('range') || 60)
  const metricsRangeMinutes = [15, 60, 360, 1440].includes(parsedRange) ? parsedRange : 60
  const [expanded, setExpanded] = useState<string[]>([])
  const [terminalConnection, setTerminalConnection] = useState('')
  function setActiveTabKey(key: string) {
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('tab', key)
      return next
    })
  }
  function setContainer(name: string) {
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('container', name)
      return next
    })
  }
  function openContainerTab(key: string, name: string) {
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('container', name)
      next.set('tab', key)
      return next
    })
  }
  function setMetricsRangeMinutes(value: number) {
    setSearchParams(
      (previous) => {
        const next = new URLSearchParams(previous)
        next.set('range', String(value))
        return next
      },
      { replace: true },
    )
  }
  const podLogsCapability = useClusterCapability('pod.logs', localeCode)
  const podExecCapability = useClusterCapability('pod.exec', localeCode)
  const logsStreamingDisabledReason =
    podLogsCapability.status === 'partial' ? podLogsCapability.reason : undefined
  const terminalPartialReason =
    localeCode === 'zh_CN'
      ? '当前连接模式仅支持非交互式 exec，暂不支持交互终端。'
      : 'The current connection mode only supports non-interactive exec, not interactive terminals.'
  const terminalLoadingReason =
    localeCode === 'zh_CN'
      ? '正在读取当前集群的终端能力。'
      : 'Reading terminal capability for the current cluster.'
  const detailScope = { clusterId, namespace: detailNamespace }
  const podDetailQuery = useQuery(podQueries.detail(detailScope, podName))
  const visiblePodDetailQueryData = readableQueryData(podDetailQuery)
  const readsMetrics = activeTabKey === 'overview' || activeTabKey === 'metrics'
  const podMetricsQueryOptions = podQueries.metrics(detailScope, podName, metricsRangeMinutes)
  const podMetricsQuery = useQuery({
    ...podMetricsQueryOptions,
    enabled: Boolean(podMetricsQueryOptions.enabled) && readsMetrics,
  })
  const visiblePodMetricsQueryData =
    Boolean(podMetricsQueryOptions.enabled) && readsMetrics
      ? readableQueryData(podMetricsQuery)
      : undefined
  const podEventsQueryOptions = podQueries.events(detailScope, podName)
  const podEventsQuery = useQuery({
    ...podEventsQueryOptions,
    enabled: Boolean(podEventsQueryOptions.enabled) && activeTabKey === 'events',
  })
  const visiblePodEventsQueryData =
    Boolean(podEventsQueryOptions.enabled) && activeTabKey === 'events'
      ? readableQueryData(podEventsQuery)
      : undefined

  const containerOptions = useMemo(
    () =>
      (visiblePodDetailQueryData?.containers ?? []).map((item) => ({
        value: item.name,
        label: item.name,
      })),
    [visiblePodDetailQueryData?.containers],
  )

  const requestedContainer = searchParams.get('container') || ''
  const container = containerOptions.some((item) => item.value === requestedContainer)
    ? requestedContainer
    : (
        visiblePodDetailQueryData?.containers?.find((item) => item.role !== 'init') ??
        visiblePodDetailQueryData?.containers?.[0]
      )?.name || ''
  const identity = `${clusterId}/${detailNamespace}/${podName}`
  const connectionKey = `${identity}/${container}/${terminalShell}`
  useEffect(() => {
    setExpanded([])
    setTerminalConnection('')
  }, [identity])
  useEffect(() => {
    if (activeTabKey !== 'terminal') setTerminalConnection('')
  }, [activeTabKey])
  const podDetail = visiblePodDetailQueryData
  const logsAllowedByPolicy = hasAllowedAction(podDetail?.allowedActions, 'logs')
  const logsDisabled =
    podLogsCapability.isLoading || podLogsCapability.disabled || !logsAllowedByPolicy
  const logsDisabledReason = podLogsCapability.isLoading
    ? localeCode === 'zh_CN'
      ? '正在读取当前集群的 Pod 日志能力。'
      : 'Reading pod log capability for the current cluster.'
    : podLogsCapability.disabled
      ? podLogsCapability.reason
      : localeCode === 'zh_CN'
        ? '当前环境或资源范围不允许查看 Pod 日志。'
        : 'Pod logs are not allowed in the current environment or resource scope.'
  const terminalAllowedByPolicy = hasAllowedAction(podDetail?.allowedActions, 'exec')
  const terminalDisabled =
    podExecCapability.isLoading ||
    podExecCapability.disabled ||
    podExecCapability.status === 'partial' ||
    !terminalAllowedByPolicy
  const terminalDisabledReason = podExecCapability.isLoading
    ? terminalLoadingReason
    : podExecCapability.status === 'partial'
      ? podExecCapability.reason || terminalPartialReason
      : podExecCapability.disabled
        ? podExecCapability.reason
        : localeCode === 'zh_CN'
          ? '当前环境或资源范围不允许打开 Pod 终端。'
          : 'Pod terminal access is not allowed in the current environment or resource scope.'

  function openLogSession() {
    if (!clusterId || !detailNamespace || !canViewPodLogs || logsDisabled) return
    openSession({
      clusterId,
      container: container || undefined,
      kind: 'logs',
      namespace: detailNamespace,
      podName,
      streamingDisabledReason: logsStreamingDisabledReason,
    })
  }

  function openTerminalSession() {
    if (
      !clusterId ||
      !detailNamespace ||
      !canExecPod ||
      terminalDisabled ||
      podDetail?.containers?.find((item) => item.name === container)?.state === 'terminated'
    )
      return
    openSession({
      clusterId,
      container: container || undefined,
      kind: 'terminal',
      namespace: detailNamespace,
      podName,
      shell: terminalShell,
    })
  }

  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: `Pod ${podName}`,
    entityKind: 'kubernetes.pod',
    entityName: podName,
    clusterId: clusterId ?? undefined,
    namespace: detailNamespace ?? undefined,
    pod: podName,
    node: podDetail?.nodeName,
    timeRangeMinutes: metricsRangeMinutes,
    pinnedData: {
      phase: podDetail?.phase,
      containers: podDetail?.containers?.length,
      readyContainers: podDetail?.containers?.filter((item) => item.ready).length,
      restarts: podDetail?.containers?.reduce((sum, item) => sum + (item.restartCount || 0), 0),
      activeTab: activeTabKey,
      container,
    },
    promptHint:
      localeCode === 'zh_CN'
        ? `排查 Pod ${podName} 的状态、容器、事件、日志、指标和节点相关问题。`
        : `Investigate status, containers, events, logs, metrics, and node issues for Pod ${podName}.`,
  })
  const podTimelineEvents = visiblePodEventsQueryData ?? []
  const podVolumes = podDetail?.volumes ?? []
  const podRelatedResources = podDetail?.relatedResources ?? []
  const containerSummary = useMemo(() => {
    const containers = podDetail?.containers ?? []
    return {
      total: containers.length,
      ready: containers.filter((item) => item.ready).length,
      restarts: containers.reduce((sum, item) => sum + (item.restartCount || 0), 0),
    }
  }, [podDetail])

  const volumeColumns: TableColumnsType<PodVolume> = [
    {
      title: localeCode === 'zh_CN' ? '卷名' : 'Volume',
      dataIndex: 'name',
      width: 220,
      render: (value: string, record: PodVolume) => {
        const targetPath = buildVolumeDetailPath(record, detailNamespace, clusterId)
        if (!targetPath) {
          return <Text className="soha-volume-name-text">{value}</Text>
        }
        return (
          <Link className="soha-volume-name-link" onClick={() => navigate(targetPath)}>
            {value}
          </Link>
        )
      },
    },
    {
      title: 'Type',
      dataIndex: 'type',
      width: 130,
      render: (value: string) => (
        <Tag className="soha-volume-type-tag">{formatVolumeTypeLabel(value)}</Tag>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '详情' : 'Details',
      width: 300,
      render: (_: unknown, record: PodVolume) =>
        renderVolumeDetail(record, localeCode, detailNamespace, navigate, clusterId),
    },
    {
      title: localeCode === 'zh_CN' ? 'Volume Mounts' : 'Volume Mounts',
      dataIndex: 'volumeMounts',
      render: (value?: PodVolumeMount[]) => renderVolumeMounts(value),
    },
  ]

  const nodeLink =
    podDetail?.nodeName &&
    !['-', '—'].includes(podDetail.nodeName) &&
    hasPermission(permissionSnapshot, 'platform.nodes.view') ? (
      <RouterLink
        to={`/cluster-resources/nodes/${encodeURIComponent(podDetail.nodeName)}?clusterId=${encodeURIComponent(clusterId || '')}`}
      >
        {podDetail.nodeName}
      </RouterLink>
    ) : (
      podDetail?.nodeName || '—'
    )
  const namespaceLink = hasPermission(permissionSnapshot, 'platform.namespaces.view') ? (
    <RouterLink
      to={`/cluster-resources/namespaces?clusterId=${encodeURIComponent(clusterId || '')}&namespace=${encodeURIComponent(detailNamespace || '')}`}
    >
      {podDetail?.namespace || detailNamespace}
    </RouterLink>
  ) : (
    podDetail?.namespace || detailNamespace || '—'
  )
  const owner = podRelatedResources.find((item) =>
    ['Deployment', 'StatefulSet', 'DaemonSet', 'Job', 'ReplicaSet'].includes(item.kind),
  )
  const ownerPath = owner ? buildRelatedResourcePath(owner, detailNamespace, clusterId) : null
  const account = podRelatedResources.find(
    (item) => item.kind === 'ServiceAccount' && item.name === podDetail?.serviceAccountName,
  )
  const accountPath = account ? buildRelatedResourcePath(account, detailNamespace, clusterId) : null
  const metricValue = (key: string) => {
    const series = visiblePodMetricsQueryData?.series?.find((item) => item.key === key)
    return series && Number.isFinite(series.latest) ? String(series.latest) : undefined
  }
  const usagePod = {
    name: podName,
    cpu: metricValue('cpu'),
    memory: metricValue('memory'),
    requests: podDetail?.requests,
    limits: podDetail?.limits,
  }
  const runtimeOverview = podDetail ? (
    <PodDetailOverview
      pod={podDetail}
      zh={localeCode === 'zh_CN'}
      expanded={expanded}
      onExpand={setExpanded}
      onContainerAction={openContainerTab}
      canLogs={canViewPodLogs && !logsDisabled}
      canExec={canExecPod && !terminalDisabled}
      logsReason={logsDisabledReason}
      execReason={terminalDisabledReason}
      nodeLink={nodeLink}
      namespaceLink={namespaceLink}
      accountLink={
        accountPath ? (
          <RouterLink to={accountPath}>{account?.name}</RouterLink>
        ) : (
          podDetail.serviceAccountName || '—'
        )
      }
      ownerLink={
        owner ? (
          <Space orientation="vertical">
            <Text type="secondary">{owner.kind}</Text>
            {ownerPath ? <RouterLink to={ownerPath}>{owner.name}</RouterLink> : owner.name}
          </Space>
        ) : null
      }
      onEvents={() => setActiveTabKey('events')}
      metrics={
        <Card
          className="soha-pod-detail-card"
          title={localeCode === 'zh_CN' ? '资源使用' : 'Resource usage'}
          extra={
            <Button type="link" size="small" onClick={() => setActiveTabKey('metrics')}>
              {localeCode === 'zh_CN' ? '查看趋势' : 'View trends'}
            </Button>
          }
        >
          <div className="soha-pod-detail-usage">
            {(['cpu', 'memory'] as const).map((resource) => (
              <div key={resource}>
                <Text>
                  {resource === 'cpu' ? 'CPU' : localeCode === 'zh_CN' ? '内存' : 'Memory'}
                </Text>
                <PodResourceCell pod={usagePod} resource={resource} localeCode={localeCode} />
              </div>
            ))}
          </div>
        </Card>
      }
    />
  ) : null
  const metricsTab: NonNullable<TabsProps['items']>[number] = {
    key: 'metrics',
    label: localeCode === 'zh_CN' ? '指标' : 'Metrics',
    children:
      activeTabKey === 'metrics' ? (
        <Suspense fallback={<Spin size="large" />}>
          <ResourceMetricsPanel
            data={visiblePodMetricsQueryData}
            loading={podMetricsQuery.isLoading}
            rangeMinutes={metricsRangeMinutes}
            onRangeChange={setMetricsRangeMinutes}
            errorMessage={
              podMetricsQuery.error instanceof Error ? podMetricsQuery.error.message : undefined
            }
            resourceRequests={podDetail?.requests}
            resourceLimits={podDetail?.limits}
            onRefresh={() => void podMetricsQuery.refetch()}
          />
        </Suspense>
      ) : null,
  }

  const eventsTab: NonNullable<TabsProps['items']>[number] = {
    key: 'events',
    label: localeCode === 'zh_CN' ? '事件' : 'Events',
    children:
      activeTabKey === 'events' ? (
        <PodDetailEvents
          events={podTimelineEvents}
          observedAt={podEventsQuery.dataUpdatedAt || Date.now()}
          loading={podEventsQuery.isLoading}
          error={
            podEventsQuery.isError
              ? podEventsQuery.error instanceof Error
                ? podEventsQuery.error.message
                : String(podEventsQuery.error)
              : undefined
          }
          onRefresh={() => void podEventsQuery.refetch()}
        />
      ) : null,
  }
  const volumesTab: NonNullable<TabsProps['items']>[number] = {
    key: 'volumes',
    label: localeCode === 'zh_CN' ? '存储与挂载' : 'Storage and mounts',
    children: (
      <Card className="soha-detail-card">
        <AdminTable
          className="soha-pod-volumes-table"
          shellClassName="soha-management-table-shell"
          columns={volumeColumns}
          dataSource={podVolumes}
          rowKey={(record) => record.name}
          pagination={false}
          enableColumnSelection={false}
          empty={
            <ManagementState
              bordered={false}
              compact
              title={localeCode === 'zh_CN' ? '当前 Pod 没有关联卷' : 'No pod volumes found'}
            />
          }
        />
      </Card>
    ),
  }

  const relatedResourcesTab: NonNullable<TabsProps['items']>[number] = {
    key: 'related-resources',
    label: localeCode === 'zh_CN' ? '关联资源' : 'Related resources',
    children: podDetail ? (
      <Suspense fallback={<Spin />}>
        <PodDetailResources
          pod={podDetail}
          scope={detailScope}
          active={activeTabKey === 'related-resources'}
        />
      </Suspense>
    ) : null,
  }
  const matchingDock = (kind: 'logs' | 'terminal') =>
    sessions.find(
      (session) =>
        session.kind === kind &&
        session.clusterId === clusterId &&
        session.namespace === detailNamespace &&
        session.podName === podName &&
        session.container === (container || undefined) &&
        (kind === 'logs' || session.shell === terminalShell),
    )
  const logDock = matchingDock('logs')
  const terminalDock = matchingDock('terminal')
  const dockNotice = (kind: 'logs' | 'terminal', id: string) => (
    <ManagementState
      compact
      title={localeCode === 'zh_CN' ? '已在实时会话栏打开' : 'Open in realtime session dock'}
      description={
        localeCode === 'zh_CN'
          ? '继续使用当前会话，或返回页面查看。'
          : 'Continue in the current session or return to this page.'
      }
      actions={
        <Space>
          <Button onClick={() => (kind === 'logs' ? openLogSession() : openTerminalSession())}>
            {localeCode === 'zh_CN' ? '查看会话' : 'Show session'}
          </Button>
          <Button
            onClick={() => {
              closeSession(id)
              setTerminalConnection('')
            }}
          >
            {localeCode === 'zh_CN' ? '返回页面' : 'Return to page'}
          </Button>
        </Space>
      }
    />
  )
  const logsTab: NonNullable<TabsProps['items']>[number] = {
    key: 'logs',
    label: logsDisabled ? (
      <Tooltip title={logsDisabledReason}>
        <span>{localeCode === 'zh_CN' ? '日志' : 'Logs'}</span>
      </Tooltip>
    ) : localeCode === 'zh_CN' ? (
      '日志'
    ) : (
      'Logs'
    ),
    disabled: logsDisabled,
    children:
      activeTabKey !== 'logs' ? null : logDock ? (
        dockNotice('logs', logDock.id)
      ) : podLogsCapability.isLoading ? (
        <ManagementState compact kind="loading" />
      ) : podLogsCapability.disabled ? (
        <ManagementState
          compact
          kind="unsupported"
          title={localeCode === 'zh_CN' ? '当前集群不支持 Pod 日志' : 'Pod logs are not supported'}
          description={podLogsCapability.reason}
        />
      ) : (
        <Suspense fallback={<Spin size="large" />}>
          <PodLogViewer
            clusterId={clusterId}
            namespace={detailNamespace}
            podName={podName}
            container={container || undefined}
            active={activeTabKey === 'logs'}
            containerOptions={containerOptions}
            onContainerChange={setContainer}
            streamingDisabledReason={logsStreamingDisabledReason}
            toolbarExtra={
              <Button icon={<FileTextOutlined />} size="small" onClick={openLogSession}>
                {localeCode === 'zh_CN' ? '打开日志会话' : 'Open log session'}
              </Button>
            }
            onOpenLogCenter={() => {
              const query = new URLSearchParams()
              if (clusterId) query.set('cluster', clusterId)
              if (detailNamespace) query.set('namespace', detailNamespace)
              query.append('pod', podName)
              if (container) query.append('container', container)
              navigate(`/monitoring-workbench/logs?${query.toString()}`)
            }}
          />
        </Suspense>
      ),
  }

  const terminalTab: NonNullable<TabsProps['items']>[number] = {
    key: 'terminal',
    label: (
      <Tooltip title={terminalDisabled ? terminalDisabledReason : undefined}>
        {localeCode === 'zh_CN' ? '终端' : 'Terminal'}
      </Tooltip>
    ),
    disabled: terminalDisabled,
    children:
      activeTabKey !== 'terminal' ? null : terminalDock ? (
        dockNotice('terminal', terminalDock.id)
      ) : (
        <div className="soha-pod-detail-terminal">
          <div className="soha-terminal-controls">
            <label className="soha-pod-detail-selector">
              {localeCode === 'zh_CN' ? '容器:' : 'Container:'}
              <Select
                aria-label={localeCode === 'zh_CN' ? '选择容器' : 'Select container'}
                value={container || undefined}
                onChange={setContainer}
                options={containerOptions}
              />
            </label>
            <label className="soha-pod-detail-selector">
              Shell:
              <Select
                aria-label="Shell"
                value={terminalShell}
                onChange={setTerminalShell}
                options={['/bin/sh', '/bin/bash', '/bin/ash'].map((value) => ({
                  value,
                  label: value,
                }))}
              />
            </label>
            <Button
              icon={<CodeOutlined />}
              size="small"
              onClick={openTerminalSession}
              disabled={
                podDetail?.containers?.find((item) => item.name === container)?.state ===
                'terminated'
              }
            >
              {localeCode === 'zh_CN' ? '打开终端会话' : 'Open terminal session'}
            </Button>
          </div>
          {terminalConnection === connectionKey ? (
            <Suspense fallback={<Spin />}>
              <PodTerminal
                clusterId={clusterId}
                namespace={detailNamespace}
                podName={podName}
                container={container || undefined}
                shell={terminalShell}
              />
            </Suspense>
          ) : (
            <ManagementState
              compact
              title={localeCode === 'zh_CN' ? '连接容器终端' : 'Connect to container terminal'}
              description={
                localeCode === 'zh_CN'
                  ? '选择容器和 Shell 后连接。'
                  : 'Select a container and shell, then connect.'
              }
              actions={
                <Button
                  type="primary"
                  disabled={
                    terminalDisabled ||
                    !container ||
                    podDetail?.containers?.find((item) => item.name === container)?.state ===
                      'terminated'
                  }
                  onClick={() => setTerminalConnection(connectionKey)}
                >
                  {localeCode === 'zh_CN' ? '连接终端' : 'Connect'}
                </Button>
              }
            />
          )}
        </div>
      ),
  }
  const header = (
    <header className="soha-pod-detail-header">
      <div className="soha-pod-detail-title-row">
        <div>
          <RouterLink
            aria-label={localeCode === 'zh_CN' ? '返回 Pods' : 'Back to Pods'}
            to={`/workloads/pods?clusterId=${encodeURIComponent(clusterId || '')}&namespace=${encodeURIComponent(detailNamespace || '')}`}
          >
            <ArrowLeftOutlined />
          </RouterLink>
          <h1>{podName}</h1>
          <Text copyable={{ text: podName }} />
        </div>
        <Space>
          <Button
            icon={<ReloadOutlined />}
            loading={podDetailQuery.isFetching}
            onClick={() => {
              void podDetailQuery.refetch()
              if (readsMetrics) void podMetricsQuery.refetch()
              if (activeTabKey === 'events') void podEventsQuery.refetch()
            }}
          >
            {localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
          </Button>
          <KubernetesResourceActionsMenu
            resourceLabel="Pod"
            name={podName}
            localeCode={localeCode}
            items={[
              ...(canViewPodLogs
                ? [
                    {
                      key: 'logs',
                      label: (
                        <Tooltip title={logsDisabled ? logsDisabledReason : undefined}>
                          {localeCode === 'zh_CN' ? '查看日志' : 'View logs'}
                        </Tooltip>
                      ),
                      disabled: logsDisabled,
                    },
                  ]
                : []),
              ...(canExecPod
                ? [
                    {
                      key: 'terminal',
                      label: (
                        <Tooltip title={terminalDisabled ? terminalDisabledReason : undefined}>
                          {localeCode === 'zh_CN' ? '打开终端' : 'Open terminal'}
                        </Tooltip>
                      ),
                      disabled: terminalDisabled,
                    },
                  ]
                : []),
              { key: 'yaml', label: 'YAML' },
            ]}
            onSelect={setActiveTabKey}
          />
        </Space>
      </div>
      <div className="soha-pod-detail-meta">
        <StatusTag value={podDetail?.phase} />
        <span className="soha-pod-detail-squares">
          {[...(podDetail?.containers ?? [])]
            .sort((a, b) => Number(b.role === 'init') - Number(a.role === 'init'))
            .map((item) => (
              <PodContainerSquare
                key={item.name}
                item={item}
                onClick={() => {
                  setExpanded([item.name])
                  openContainerTab('overview', item.name)
                }}
              />
            ))}
        </span>
        <span>
          {localeCode === 'zh_CN' ? '重启' : 'Restarts'} {containerSummary.restarts}
        </span>
        {namespaceLink}
        <span>{podDetail?.qosClass || '—'}</span>
        <Text type="secondary">{formatRelativeTime(podDetail?.createdAt)}</Text>
      </div>
    </header>
  )

  return (
    <div className={`soha-pod-detail is-tab-${activeTabKey}`}>
      <ReadFeedback canRead={podDetailQuery.isEnabled} query={podDetailQuery} label="Pod 详情" />
      <ReadFeedback canRead={podMetricsQuery.isEnabled} query={podMetricsQuery} label="Pod 指标" />
      <WorkloadDetailShell
        title="Pod"
        resource="pods"
        paramKey="podName"
        header={header}
        hideMetadataOverview
        hideRelationshipsTab
        extraOverview={runtimeOverview}
        extraTabPanes={[
          ...(canViewPodLogs ? [logsTab] : []),
          ...(canExecPod ? [terminalTab] : []),
          eventsTab,
          metricsTab,
          volumesTab,
          relatedResourcesTab,
        ]}
        activeTabKey={activeTabKey}
        onTabChange={setActiveTabKey}
        yamlLast
      />
    </div>
  )
}
