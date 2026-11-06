import { ResourceDetailTabs, useResourceDetailTab } from '../shared/resource-detail-header'
import { WorkloadDetailHeader } from '../workloads/shared/workload-detail-header'
import { WorkloadRefresh, useWorkloadRefresh } from '../workloads/shared/workload-refresh'
import { WorkloadPodsTable } from '../workloads/shared/workload-pods-table'
import { WorkloadDetailEvents } from '../workloads/shared/detail-events'
import { podQueries } from '../workloads/pods/queries'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag } from '@/components/status-tag'
import { NodeStateTags } from '../shared/node-state-tags'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAIPageContext } from '@/features/copilot'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery, useQueryClient, useIsFetching } from '@tanstack/react-query'
import { Card, Segmented, Spin, Typography } from 'antd'
import { lazy, Suspense, useEffect, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useClusterCapabilityForCluster } from '../cluster-capabilities'
import { nodeQueries } from './queries'
import { toClusterScope } from './scope'
import { useNodeActions } from './use-node-actions'
import { useNodeMonitoring } from './use-node-monitoring'
import { NodeDetailOverview, NodeConditions } from './node-detail-overview'
import type { Pod } from '@/types'
import '../styles/base.css'
import './node-detail.css'
const NodeYAMLPanel = lazy(() => import('./node-yaml-panel'))
const NodeDetailMetrics = lazy(() => import('./node-detail-metrics'))
const tabKeys = ['overview', 'pods', 'events', 'metrics', 'conditions', 'yaml']

export function NodeDetailPage() {
  const { localeCode, t } = useI18n()
  const zh = localeCode === 'zh_CN'
  const navigate = useNavigate()
  const { nodeName = '' } = useParams()
  const [params] = useSearchParams()
  const store = usePlatformScopeStore()
  const requestedClusterId = params.get('clusterId')
  const clusterId = requestedClusterId || store.clusterId
  const scope = toClusterScope(clusterId)
  const queryClient = useQueryClient()
  const yamlKey = nodeQueries.yaml(scope, nodeName).queryKey
  const yamlFetching = useIsFetching({ queryKey: yamlKey })
  const [tab, setTab] = useResourceDetailTab()
  const activeTab = tabKeys.includes(tab) ? tab : 'overview'
  const [range, setRange] = useState(15)
  const refresh = useWorkloadRefresh()
  const permissionQuery = usePermissionSnapshot()
  const permissions = readableQueryData(permissionQuery)?.data
  const canRead = hasPermission(permissions, 'platform.nodes.view')
  const detailQuery = useQuery({
    ...nodeQueries.detail(scope, nodeName),
    enabled: Boolean(clusterId && nodeName && canRead),
    refetchInterval: refresh.interval,
  })
  const node = canRead ? readableQueryData(detailQuery) : undefined
  const actions = useNodeActions({
    scope,
    nodes: node ? [node] : [],
    onDeleted: () =>
      navigate(`/cluster-resources/nodes?clusterId=${encodeURIComponent(clusterId ?? '')}`),
  })
  const monitor = useNodeMonitoring(clusterId, nodeName, range, refresh.interval)
  const canReadPods = canRead && hasPermission(permissions, 'platform.pods.view')
  const podsQuery = useQuery({
    ...podQueries.list(scope),
    enabled: Boolean(clusterId && canReadPods && ['overview', 'pods'].includes(activeTab)),
    refetchInterval: refresh.interval,
  })
  const fullPods = canReadPods ? readableQueryData(podsQuery) : undefined
  const pods: Pod[] =
    !canReadPods || podsQuery.isError
      ? []
      : fullPods
        ? fullPods.filter((p) => p.nodeName === nodeName)
        : (node?.pods ?? []).map((p) => ({ ...p, nodeName, allowedActions: [] }))
  const canReadEvents = canRead && hasPermission(permissions, 'platform.workloads.overview.view')
  const eventsQuery = useQuery({
    ...nodeQueries.events(scope, nodeName, 100),
    enabled: Boolean(clusterId && canReadEvents && activeTab === 'events'),
    refetchInterval: refresh.interval,
  })
  const events =
    readableQueryData(eventsQuery)?.filter(
      (e) => e.involvedKind === 'Node' && e.involvedName === nodeName,
    ) ?? []
  const yamlCapability = useClusterCapabilityForCluster('resource.yaml.view', localeCode, clusterId)
  const applyCapability = useClusterCapabilityForCluster(
    'resource.yaml.apply',
    localeCode,
    clusterId,
  )
  const yamlAvailable = !yamlCapability.disabled && !yamlCapability.isLoading
  const canApply =
    canRead &&
    hasPermission(permissions, 'platform.nodes.update') &&
    hasAllowedAction(node?.allowedActions, 'update') &&
    !applyCapability.disabled &&
    !applyCapability.isLoading
  useEffect(() => {
    if (requestedClusterId && requestedClusterId !== store.clusterId)
      store.setClusterId(requestedClusterId)
  }, [requestedClusterId, store.clusterId, store.setClusterId])
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: zh ? '节点详情' : 'Node detail',
    entityKind: 'Node',
    entityName: nodeName,
    clusterId: clusterId || undefined,
    node: nodeName,
  })
  if (!clusterId)
    return (
      <ManagementState compact kind="select-scope" title={zh ? '请选择集群' : 'Select a cluster'} />
    )
  if (permissionQuery.isPending || detailQuery.isLoading) return <Spin size="large" />
  if (!canRead)
    return (
      <ManagementState
        compact
        kind="error"
        title={zh ? '需要节点查看权限' : 'Node view permission required'}
      />
    )
  if (!node)
    return (
      <>
        <ReadFeedback query={detailQuery} canRead label={zh ? '节点详情' : 'Node detail'} />
        {!detailQuery.isError && (
          <ManagementState compact kind="not-found" title={zh ? '节点未找到' : 'Node not found'} />
        )}
      </>
    )
  const activeQueries = [
    detailQuery,
    ...(monitor.canRead ? [monitor.query] : []),
    ...(podsQuery.isEnabled ? [podsQuery] : []),
    ...(eventsQuery.isEnabled ? [eventsQuery] : []),
  ]
  const yamlActive = activeTab === 'yaml' && yamlAvailable
  const refreshQueries = [
    ...activeQueries,
    ...(yamlActive
      ? [
          {
            isFetching: yamlFetching > 0,
            isError: queryClient.getQueryState(yamlKey)?.status === 'error',
          },
        ]
      : []),
  ]
  const reload = () =>
    Promise.all([
      ...activeQueries.map((q) => q.refetch()),
      ...(yamlActive ? [queryClient.refetchQueries({ queryKey: yamlKey })] : []),
    ])
  const podsTable = (
    <>
      <ReadFeedback canRead={canReadPods} query={podsQuery} label="Pods" />
      {canReadPods && !podsQuery.isError && (
        <WorkloadPodsTable
          key={`${clusterId}:${nodeName}`}
          pods={pods}
          scope={scope}
          factColumns={[
            {
              title: zh ? '命名空间' : 'Namespace',
              dataIndex: 'namespace',
              key: 'namespace',
              width: 180,
              render: (ns: string) => <MetadataTag tone="blue" label={ns} />,
            },
          ]}
        />
      )}
    </>
  )
  return (
    <div className="soha-page soha-workload-detail-page soha-workload-detail-frame soha-node-detail">
      <ReadFeedback query={detailQuery} canRead label={zh ? '节点详情' : 'Node detail'} />
      {podsQuery.isEnabled && activeTab === 'overview' && (
        <ReadFeedback canRead={canReadPods} query={podsQuery} label="Pods" />
      )}
      {actions.feedback}
      <WorkloadDetailHeader
        kind="Node"
        name={node.name}
        status={
          <NodeStateTags
            status={node.status}
            cordoned={node.unschedulable}
            conditions={node.conditions}
            draining={actions.isDraining(node.name)}
          />
        }
        facts={
          <>
            {(node.roles ?? []).map((role) => (
              <MetadataTag key={role} label={role} />
            ))}
            <span>{node.version || '—'}</span>
            <span>{node.internalIp || '—'}</span>
            <Typography.Text type="secondary">{formatAgeSeconds(node.ageSeconds)}</Typography.Text>
          </>
        }
        actions={
          <>
            <WorkloadRefresh
              settings={refresh}
              queries={refreshQueries}
              onRefresh={reload}
              resourceLabel="Node"
              beforeRefresh={actions.buttons(node)}
            />
            {actions.buttons(node, true)}
          </>
        }
      />
      <ResourceDetailTabs
        className="soha-resource-tabs soha-workload-detail-tabs"
        activeKey={activeTab}
        onChange={setTab}
        size="small"
        tabBarGutter={32}
        items={[
          {
            key: 'overview',
            destroyOnHidden: true,
            label: zh ? '概览' : 'Overview',
            children: (
              <NodeDetailOverview
                node={node}
                scope={scope}
                pods={pods}
                canReadPods={canReadPods && !podsQuery.isError}
                monitoring={monitor.data}
                canReadMetrics={monitor.canRead}
                metricsFailed={monitor.query.isError}
                onTabChange={setTab}
              />
            ),
          },
          { key: 'pods', label: 'Pods', children: podsTable },
          {
            key: 'events',
            destroyOnHidden: true,
            label: zh ? '事件' : 'Events',
            children: canReadEvents ? (
              <WorkloadDetailEvents
                events={events}
                observedAt={eventsQuery.dataUpdatedAt}
                loading={eventsQuery.isLoading}
                error={
                  eventsQuery.isError
                    ? zh
                      ? '节点事件读取失败'
                      : 'Node events could not be read'
                    : undefined
                }
                onRefresh={() => void eventsQuery.refetch()}
                resourceLabel="Node"
              />
            ) : (
              <ManagementState
                compact
                title={zh ? '需要事件查看权限' : 'Event view permission required'}
              />
            ),
          },
          {
            key: 'metrics',
            destroyOnHidden: true,
            label: zh ? '指标' : 'Metrics',
            children: (
              <>
                <div className="soha-node-metrics-toolbar">
                  <Segmented
                    aria-label={zh ? '指标时间范围' : 'Metric time range'}
                    value={range}
                    onChange={(v) => setRange(Number(v))}
                    options={[
                      { value: 15, label: zh ? '15 分钟' : '15 min' },
                      { value: 60, label: zh ? '1 小时' : '1 hour' },
                      { value: 360, label: zh ? '6 小时' : '6 hours' },
                      { value: 1440, label: zh ? '24 小时' : '24 hours' },
                    ]}
                  />
                </div>
                <Suspense fallback={<Spin />}>
                  <NodeDetailMetrics
                    data={monitor.data}
                    clusterId={clusterId}
                    nodeName={nodeName}
                    canRead={monitor.canRead}
                    failed={monitor.query.isError}
                  />
                </Suspense>
              </>
            ),
          },
          {
            key: 'conditions',
            destroyOnHidden: true,
            label: zh ? '运行条件' : 'Conditions',
            children: <NodeConditions conditions={node.conditions} />,
          },
          {
            key: 'yaml',
            destroyOnHidden: false,
            label: t('common.yaml', 'YAML'),
            children: yamlAvailable ? (
              <Suspense
                fallback={
                  <Card>
                    <Spin />
                  </Card>
                }
              >
                <NodeYAMLPanel
                  key={`${clusterId}:${nodeName}`}
                  scope={scope}
                  nodeName={nodeName}
                  active={activeTab === 'yaml'}
                  canApply={canApply}
                  interval={refresh.interval}
                />
              </Suspense>
            ) : (
              <ManagementState
                compact
                title={yamlCapability.reason || (zh ? 'YAML 暂不可用' : 'YAML unavailable')}
              />
            ),
          },
        ]}
      />
      {actions.dialogs}
    </div>
  )
}
