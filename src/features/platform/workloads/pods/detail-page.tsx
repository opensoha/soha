import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { ResourceEventsTimeline } from '@/components/resource-events-timeline'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAIPageContext } from '@/features/copilot'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import { useRealtimeSessionDock } from '@/features/platform/session-dock'
import {
  buildRelatedResourcePath,
  buildVolumeDetailPath,
  conditionToTimelineEvent,
  formatContainerStateLabel,
  formatVolumeTypeLabel,
  localizeRelatedRelation,
  localizeRelatedResourceKind,
} from '@/features/platform/workloads-model'
import '@/features/platform/workloads/styles.css'
import { useI18n } from '@/i18n'
import type {
  PodRelatedResource,
  PodVolume,
  PodVolumeMount,
  WorkloadCondition,
  WorkloadContainer,
} from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import { CodeOutlined, FileTextOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType, TabsProps } from 'antd'
import { Button, Card, Descriptions, Select, Space, Spin, Tag, Tooltip, Typography } from 'antd'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useWorkloadDetailScope } from '../shared/detail-scope'
import { WorkloadDetailShell } from '../shared/detail-shell'
import { podQueries } from './queries'

const { Link, Text } = Typography

const ResourceMetricsPanel = lazy(async () => {
  const mod = await import('@/components/resource-metrics-panel')
  return { default: mod.ResourceMetricsPanel }
})

/* ─── shared helpers ─── */

function renderDetailTagList(values: string[] | undefined, emptyLabel = '-') {
  if (!values || values.length === 0) {
    return <Text type="secondary">{emptyLabel}</Text>
  }
  return (
    <Space size={[6, 6]} wrap>
      {values.map((item) => (
        <Tag key={item}>{item}</Tag>
      ))}
    </Space>
  )
}

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
) {
  const targetPath = buildVolumeDetailPath(volume, detailNamespace)
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
  const [searchParams] = useSearchParams()
  const podName = params.podName as string
  const { clusterId, namespace: detailNamespace } = useWorkloadDetailScope()
  const { openSession } = useRealtimeSessionDock()
  const [container, setContainer] = useState<string>('')
  const [terminalShell, setTerminalShell] = useState('/bin/sh')
  const [activeTabKey, setActiveTabKey] = useState(() =>
    searchParams.get('tab') === 'yaml' ? 'yaml' : 'overview',
  )
  const [metricsRangeMinutes, setMetricsRangeMinutes] = useState(60)
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
  const podMetricsQueryOptions = podQueries.metrics(detailScope, podName, metricsRangeMinutes)
  const podMetricsQuery = useQuery({
    ...podMetricsQueryOptions,
    enabled: Boolean(podMetricsQueryOptions.enabled) && activeTabKey === 'metrics',
  })
  const visiblePodMetricsQueryData =
    Boolean(podMetricsQueryOptions.enabled) && activeTabKey === 'metrics'
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

  useEffect(() => {
    if (container) return
    if (containerOptions.length > 0) {
      setContainer(String(containerOptions[0].value))
    }
  }, [container, containerOptions])

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
    if (!clusterId || !detailNamespace) return
    openSession({
      clusterId,
      container: container || undefined,
      kind: 'logs',
      namespace: detailNamespace,
      podName,
      podUid: podDetail?.uid,
      streamingDisabledReason: logsStreamingDisabledReason,
    })
  }

  function openTerminalSession() {
    if (!clusterId || !detailNamespace) return
    openSession({
      clusterId,
      container: container || undefined,
      kind: 'terminal',
      namespace: detailNamespace,
      podName,
      podUid: podDetail?.uid,
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
  const podTimelineEvents = useMemo(
    () =>
      visiblePodEventsQueryData?.length
        ? visiblePodEventsQueryData
        : (podDetail?.conditions ?? []).map(conditionToTimelineEvent),
    [podDetail, visiblePodEventsQueryData],
  )
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

  const containerColumns: TableColumnsType<WorkloadContainer> = [
    { title: localeCode === 'zh_CN' ? '容器' : 'Container', dataIndex: 'name' },
    {
      title: localeCode === 'zh_CN' ? '镜像' : 'Image',
      dataIndex: 'image',
      ellipsis: { showTitle: false },
      render: (value?: string) => <TableCellText value={value} />,
    },
    { title: localeCode === 'zh_CN' ? '重启次数' : 'Restarts', dataIndex: 'restartCount' },
    {
      title: (
        <Tooltip
          title={
            localeCode === 'zh_CN'
              ? '平台展示约定：初始化容器、首个普通容器为主容器，其余普通容器为辅助容器。'
              : 'Platform display convention: init containers, the first regular container as main, and remaining regular containers as sidecars.'
          }
        >
          {localeCode === 'zh_CN' ? '角色' : 'Role'}
        </Tooltip>
      ),
      dataIndex: 'role',
      render: (value?: string) => {
        const labels: Record<string, string> =
          localeCode === 'zh_CN'
            ? { init: '初始化容器', main: '主容器', sidecar: '辅助容器' }
            : { init: 'Init', main: 'Main', sidecar: 'Sidecar' }
        return <Tag>{(value && labels[value]) || value || '-'}</Tag>
      },
    },
    {
      title: localeCode === 'zh_CN' ? '就绪' : 'Ready',
      dataIndex: 'ready',
      render: (value: boolean) => (
        <BooleanTag
          value={value}
          trueLabel={localeCode === 'zh_CN' ? '是' : 'Yes'}
          falseLabel={localeCode === 'zh_CN' ? '否' : 'No'}
        />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'State',
      dataIndex: 'state',
      render: (value: string) => formatContainerStateLabel(value),
    },
    {
      title: localeCode === 'zh_CN' ? '原因' : 'Reason',
      dataIndex: 'reason',
      render: (value: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '上次状态' : 'Last State',
      dataIndex: 'lastState',
      render: (value: string) => formatContainerStateLabel(value),
    },
    {
      title: localeCode === 'zh_CN' ? '启动时间' : 'Started At',
      dataIndex: 'startedAt',
      render: (value?: string) => (value ? formatDateTime(value) : '-'),
    },
    {
      title: 'Container ID',
      dataIndex: 'containerId',
      ellipsis: { showTitle: false },
      render: (value?: string) => <TableCellText value={value} />,
    },
  ]

  const volumeColumns: TableColumnsType<PodVolume> = [
    {
      title: localeCode === 'zh_CN' ? '卷名' : 'Volume',
      dataIndex: 'name',
      width: 220,
      render: (value: string, record: PodVolume) => {
        const targetPath = buildVolumeDetailPath(record, detailNamespace)
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
        renderVolumeDetail(record, localeCode, detailNamespace, navigate),
    },
    {
      title: localeCode === 'zh_CN' ? 'Volume Mounts' : 'Volume Mounts',
      dataIndex: 'volumeMounts',
      render: (value?: PodVolumeMount[]) => renderVolumeMounts(value),
    },
  ]

  const relatedResourceColumns: TableColumnsType<PodRelatedResource> = [
    {
      title: localeCode === 'zh_CN' ? '资源类型' : 'Kind',
      dataIndex: 'kind',
      width: 150,
      render: (value: string) => <Tag>{localizeRelatedResourceKind(value, localeCode)}</Tag>,
    },
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      render: (value: string, record: PodRelatedResource) => {
        const targetPath = buildRelatedResourcePath(record, detailNamespace, clusterId)
        if (!targetPath) {
          return value
        }
        return <Link onClick={() => navigate(targetPath)}>{value}</Link>
      },
    },
    {
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 160,
      render: (value?: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '关联关系' : 'Relations',
      dataIndex: 'relations',
      render: (value?: string[]) => (
        <Space size={[6, 6]} wrap>
          {(value ?? []).map((item) => (
            <Tag key={item}>{localizeRelatedRelation(item, localeCode)}</Tag>
          ))}
        </Space>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '详情' : 'Details',
      dataIndex: 'details',
      render: (value?: string[]) =>
        renderDetailTagList(value, localeCode === 'zh_CN' ? '无' : 'None'),
    },
  ]

  const conditionColumns: TableColumnsType<WorkloadCondition> = [
    { title: localeCode === 'zh_CN' ? '条件' : 'Condition', dataIndex: 'type' },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'status',
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '原因' : 'Reason',
      dataIndex: 'reason',
      render: (value: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '消息' : 'Message',
      dataIndex: 'message',
      ellipsis: { showTitle: false },
      render: (value?: string) => <TableCellText value={value} />,
    },
    {
      ...tableColumnPresets.datetime,
      title: localeCode === 'zh_CN' ? '最近变化' : 'Last Transition',
      dataIndex: 'lastTransitionTime',
      render: (value: string) => (value ? formatDateTime(value) : '-'),
    },
  ]

  const runtimeOverview = podDetail ? (
    <div className="soha-pod-overview-stack">
      <Card
        className="soha-detail-card soha-pod-overview-card soha-pod-overview-status"
        title={localeCode === 'zh_CN' ? '容器状态' : 'Containers'}
        extra={
          <Space size={6}>
            <Tag color="blue">
              {localeCode === 'zh_CN' ? '容器' : 'Containers'} {containerSummary.total}
            </Tag>
            <Tag color="green">
              {localeCode === 'zh_CN' ? '就绪' : 'Ready'} {containerSummary.ready}
            </Tag>
            <Tag color="orange">
              {localeCode === 'zh_CN' ? '重启' : 'Restarts'} {containerSummary.restarts}
            </Tag>
          </Space>
        }
      >
        <AdminTable
          shellClassName="soha-management-table-shell"
          columns={containerColumns}
          dataSource={podDetail.containers ?? []}
          rowKey="name"
          pageSize={10}
          enableColumnSelection={false}
        />
      </Card>
      <Card
        className="soha-detail-card soha-pod-overview-card soha-pod-overview-summary"
        title={localeCode === 'zh_CN' ? '运行时概览' : 'Runtime Overview'}
      >
        <Descriptions
          column={{ xs: 1, sm: 2, lg: 3 }}
          size="small"
          items={[
            {
              key: localeCode === 'zh_CN' ? '阶段' : 'Phase',
              label: localeCode === 'zh_CN' ? '阶段' : 'Phase',
              children: <StatusTag value={podDetail.phase} />,
            },
            { key: 'Pod IP', label: 'Pod IP', children: podDetail.podIp || '-' },
            { key: 'Host IP', label: 'Host IP', children: podDetail.hostIp || '-' },
            {
              key: localeCode === 'zh_CN' ? '节点' : 'Node',
              label: localeCode === 'zh_CN' ? '节点' : 'Node',
              children: podDetail.nodeName || '-',
            },
            {
              key: localeCode === 'zh_CN' ? '服务账号' : 'ServiceAccount',
              label: localeCode === 'zh_CN' ? '服务账号' : 'ServiceAccount',
              children: podDetail.serviceAccountName || '-',
            },
            { key: 'QoS', label: 'QoS', children: podDetail.qosClass || '-' },
            {
              key: localeCode === 'zh_CN' ? '启动时间' : 'Started At',
              label: localeCode === 'zh_CN' ? '启动时间' : 'Started At',
              children: podDetail.startTime ? formatDateTime(podDetail.startTime) : '-',
            },
          ]}
        />
      </Card>
      <Card
        className="soha-detail-card soha-pod-overview-card soha-pod-overview-conditions"
        title={localeCode === 'zh_CN' ? '条件' : 'Conditions'}
      >
        <AdminTable
          shellClassName="soha-management-table-shell"
          columns={conditionColumns}
          dataSource={podDetail.conditions ?? []}
          rowKey={(record) => `${record.type}:${record.lastTransitionTime || 'na'}`}
          pageSize={10}
          enableColumnSelection={false}
        />
      </Card>
    </div>
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
            compact
          />
        </Suspense>
      ) : null,
  }

  const eventsTab: NonNullable<TabsProps['items']>[number] = {
    key: 'events',
    label: localeCode === 'zh_CN' ? '事件' : 'Events',
    children: (
      <ResourceEventsTimeline
        events={podTimelineEvents}
        loading={podEventsQuery.isLoading}
        emptyDescription={
          localeCode === 'zh_CN'
            ? '当前 Pod 暂无事件和条件变化'
            : 'No pod events or condition transitions'
        }
      />
    ),
  }

  const volumesTab: NonNullable<TabsProps['items']>[number] = {
    key: 'volumes',
    label: localeCode === 'zh_CN' ? '卷' : 'Volumes',
    children: (
      <Card className="soha-detail-card">
        <AdminTable
          className="soha-pod-volumes-table"
          shellClassName="soha-management-table-shell"
          columns={volumeColumns}
          dataSource={podVolumes}
          rowKey={(record) => record.name}
          pageSize={10}
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
    label: localeCode === 'zh_CN' ? '相关资源' : 'Related Resources',
    children: (
      <Card className="soha-detail-card">
        <AdminTable
          shellClassName="soha-management-table-shell"
          columns={relatedResourceColumns}
          dataSource={podRelatedResources}
          rowKey={(record) => `${record.kind}:${record.namespace || 'cluster'}:${record.name}`}
          pageSize={10}
          enableColumnSelection={false}
          empty={
            <ManagementState
              bordered={false}
              compact
              title={
                localeCode === 'zh_CN'
                  ? '当前 Pod 暂无可识别的关联资源'
                  : 'No related resources detected for this pod'
              }
            />
          }
        />
      </Card>
    ),
  }

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
      activeTabKey !== 'logs' ? null : podLogsCapability.isLoading ? (
        <ManagementState compact kind="loading" />
      ) : podLogsCapability.disabled ? (
        <ManagementState
          compact
          kind="unsupported"
          title={localeCode === 'zh_CN' ? '当前集群不支持 Pod 日志' : 'Pod logs are not supported'}
          description={podLogsCapability.reason}
        />
      ) : (
        <Card className="soha-detail-card">
          <Space wrap>
            <Select
              aria-label={localeCode === 'zh_CN' ? '日志容器' : 'Log container'}
              allowClear
              size="small"
              style={{ width: 220 }}
              options={containerOptions}
              value={container || undefined}
              onChange={(value) => setContainer(String(value || ''))}
            />
            <Button icon={<FileTextOutlined />} onClick={openLogSession}>
              {localeCode === 'zh_CN' ? '打开日志会话' : 'Open log session'}
            </Button>
            <Button
              onClick={() => {
                const query = new URLSearchParams()
                if (clusterId) query.set('cluster', clusterId)
                if (detailNamespace) query.set('namespace', detailNamespace)
                query.append('pod', podName)
                if (container) query.append('container', container)
                navigate(`/monitoring-workbench/logs?${query.toString()}`)
              }}
            >
              {localeCode === 'zh_CN' ? '在日志中心打开' : 'Open in log center'}
            </Button>
            <Text type="secondary">
              {localeCode === 'zh_CN'
                ? '日志与终端统一在实时会话中管理，恢复连接前重新校验 UID 与权限。'
                : 'Manage logs and terminals in live sessions; reconnect revalidates UID and permissions.'}
            </Text>
          </Space>
        </Card>
      ),
  }

  const terminalTab: NonNullable<TabsProps['items']>[number] = {
    key: 'terminal',
    label: terminalDisabled ? (
      <Tooltip title={terminalDisabledReason}>
        <span>{localeCode === 'zh_CN' ? '终端' : 'Terminal'}</span>
      </Tooltip>
    ) : localeCode === 'zh_CN' ? (
      '终端'
    ) : (
      'Terminal'
    ),
    disabled: terminalDisabled,
    children:
      activeTabKey !== 'terminal' ? null : podExecCapability.isLoading ? (
        <ManagementState compact kind="loading" />
      ) : terminalDisabled ? (
        <ManagementState
          compact
          kind="unsupported"
          title={
            localeCode === 'zh_CN'
              ? '当前集群不支持交互终端'
              : 'Interactive terminal is not supported'
          }
          description={terminalDisabledReason}
        />
      ) : (
        <Card className="soha-detail-card">
          <div className="soha-terminal-controls">
            <div className="soha-terminal-control-group">
              <Text strong className="text-xs">
                {localeCode === 'zh_CN' ? '容器:' : 'Container:'}
              </Text>
              <Select
                size="small"
                placeholder={localeCode === 'zh_CN' ? '选择容器' : 'Select container'}
                value={container}
                onChange={(value) => setContainer(String(value || ''))}
                style={{ width: 220 }}
                options={containerOptions}
                allowClear
              />
            </div>
            <div className="soha-terminal-control-group">
              <Text strong className="text-xs">
                Shell:
              </Text>
              <Select
                size="small"
                value={terminalShell}
                onChange={(value) => setTerminalShell(String(value))}
                style={{ width: 180 }}
                options={[
                  { value: '/bin/sh', label: '/bin/sh' },
                  { value: '/bin/bash', label: '/bin/bash' },
                  { value: '/bin/ash', label: '/bin/ash' },
                ]}
              />
            </div>
            <Button icon={<CodeOutlined />} size="small" onClick={openTerminalSession}>
              {localeCode === 'zh_CN' ? '打开终端会话' : 'Open terminal session'}
            </Button>
          </div>
        </Card>
      ),
  }

  return (
    <>
      <ReadFeedback canRead={podDetailQuery.isEnabled} query={podDetailQuery} label="Pod 详情" />
      <ReadFeedback canRead={podMetricsQuery.isEnabled} query={podMetricsQuery} label="Pod 指标" />
      <ReadFeedback canRead={podEventsQuery.isEnabled} query={podEventsQuery} label="Pod 事件" />
      <WorkloadDetailShell
        title="Pod"
        resource="pods"
        paramKey="podName"
        extraOverview={runtimeOverview}
        extraTabPanes={[
          ...(canViewPodLogs ? [logsTab] : []),
          ...(canExecPod ? [terminalTab] : []),
          eventsTab,
          volumesTab,
          relatedResourcesTab,
          metricsTab,
        ]}
        activeTabKey={activeTabKey}
        onTabChange={setActiveTabKey}
        yamlLast
      />
    </>
  )
}
