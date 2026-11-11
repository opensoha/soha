import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useAIGlobalAssistant } from '@/features/copilot'
import { useKubernetesResourceStream } from '@/features/platform/shared/resource-stream'
import { ResourceStreamStatus } from '@/features/platform/shared/resource-stream-status'
import { systemQueries } from '@/features/system'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { ArrowRightOutlined, RobotOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button, Card, Collapse, Progress, Space, Spin, Typography } from 'antd'
import { useEffect, useMemo, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { nodeQueries } from '../cluster-resources/queries'
import { clusterQueries } from '../clusters/queries'
import { configurationQueries } from '../configuration/shared/queries'
import type { ConfigurationResourceRecord } from '../configuration/shared/types'
import { networkCoreQueries } from '../network/resources/queries'
import type { NetworkPolicy } from '../network/resources/types'
import type { useWorkloadRefresh } from '../workloads/shared/workload-refresh'
import {
  buildActivityTimeline,
  buildOperationsAIEvidence,
  formatPlatformOverviewCapability,
  formatPlatformOverviewText,
  summarizeCapabilityGovernance,
  summarizeNodeCapacity,
} from './model'
import { platformOverviewQueries } from './queries'

const { Text } = Typography

interface OperationsPermissions {
  audit: boolean
  events: boolean
  hpas: boolean
  mutatingWebhooks: boolean
  networkPolicies: boolean
  nodes: boolean
  operations: boolean
  podDisruptionBudgets: boolean
  validatingWebhooks: boolean
}

interface PlatformOperationsPanelProps {
  clusterId: string | null
  connectionMode?: string
  localeCode: 'zh_CN' | 'en_US'
  permissions: OperationsPermissions
  refresh?: ReturnType<typeof useWorkloadRefresh>
  refreshToken?: number
  onRefreshStateChange?: (state: { isFetching: boolean; isError: boolean }) => void
}

function CapacityMetric({
  label,
  value,
  missingLabel,
}: {
  label: string
  value: number | undefined
  missingLabel: string
}) {
  const rounded = value == null ? undefined : Math.round(value)
  return (
    <div className="soha-platform-ops-capacity-item">
      <div className="soha-platform-ops-row-head">
        <Text>{label}</Text>
        <Text strong>{rounded == null ? missingLabel : `${rounded}%`}</Text>
      </div>
      {rounded != null ? (
        <Progress
          percent={Math.min(100, rounded)}
          showInfo={false}
          size="small"
          status={rounded >= 90 ? 'exception' : 'normal'}
        />
      ) : null}
    </div>
  )
}

export function PlatformOperationsPanel({
  clusterId,
  connectionMode,
  localeCode,
  permissions,
  refresh,
  refreshToken,
  onRefreshStateChange,
}: PlatformOperationsPanelProps) {
  const navigate = useNavigate()
  const assistant = useAIGlobalAssistant()
  const scope = { clusterId, namespace: null }
  const nodesQuery = useQuery({
    ...nodeQueries.list(scope),
    enabled: permissions.nodes && Boolean(clusterId),
    refetchInterval: refresh?.interval,
  })
  const visibleNodesQueryData =
    permissions.nodes && Boolean(clusterId) ? readableQueryData(nodesQuery) : undefined
  const eventsQuery = useQuery({
    ...platformOverviewQueries.events(clusterId, permissions.events, 12),
    refetchInterval: refresh?.interval,
  })
  const visibleEventsQueryData =
    permissions.events && Boolean(clusterId) ? readableQueryData(eventsQuery) : undefined
  const auditQuery = useQuery({
    ...systemQueries.audit('system', { clusterId: clusterId ?? undefined, limit: 8 }),
    enabled: permissions.audit && Boolean(clusterId),
    refetchInterval: refresh?.interval,
  })
  const visibleAuditQueryData =
    permissions.audit && Boolean(clusterId) ? readableQueryData(auditQuery) : undefined
  const operationQuery = useQuery({
    ...systemQueries.operationLogs({ clusterId: clusterId ?? undefined, limit: 8 }),
    enabled: permissions.operations && Boolean(clusterId),
    refetchInterval: refresh?.interval,
  })
  const visibleOperationQueryData =
    permissions.operations && Boolean(clusterId) ? readableQueryData(operationQuery) : undefined
  const capabilitiesQuery = useQuery({
    ...clusterQueries.capabilities(),
    enabled: Boolean(clusterId),
    refetchInterval: refresh?.interval,
  })
  const visibleCapabilitiesQueryData = clusterId ? readableQueryData(capabilitiesQuery) : undefined
  const networkPoliciesQuery = useQuery({
    ...networkCoreQueries.list<NetworkPolicy>('networkpolicies', scope),
    enabled: permissions.networkPolicies && Boolean(clusterId),
    refetchInterval: refresh?.interval,
  })
  const visibleNetworkPoliciesQueryData =
    permissions.networkPolicies && Boolean(clusterId)
      ? readableQueryData(networkPoliciesQuery)
      : undefined
  const hpasQuery = useQuery({
    ...configurationQueries.list<ConfigurationResourceRecord>('hpas', scope),
    enabled: permissions.hpas && Boolean(clusterId),
    refetchInterval: refresh?.interval,
  })
  const visibleHpasQueryData =
    permissions.hpas && Boolean(clusterId) ? readableQueryData(hpasQuery) : undefined
  const pdbsQuery = useQuery({
    ...configurationQueries.list<ConfigurationResourceRecord>('poddisruptionbudgets', scope),
    enabled: permissions.podDisruptionBudgets && Boolean(clusterId),
    refetchInterval: refresh?.interval,
  })
  const visiblePdbsQueryData =
    permissions.podDisruptionBudgets && Boolean(clusterId)
      ? readableQueryData(pdbsQuery)
      : undefined
  const mutatingWebhooksQuery = useQuery({
    ...configurationQueries.list<ConfigurationResourceRecord>(
      'mutatingwebhookconfigurations',
      scope,
    ),
    enabled: permissions.mutatingWebhooks && Boolean(clusterId),
    refetchInterval: refresh?.interval,
  })
  const visibleMutatingWebhooksQueryData =
    permissions.mutatingWebhooks && Boolean(clusterId)
      ? readableQueryData(mutatingWebhooksQuery)
      : undefined
  const validatingWebhooksQuery = useQuery({
    ...configurationQueries.list<ConfigurationResourceRecord>(
      'validatingwebhookconfigurations',
      scope,
    ),
    enabled: permissions.validatingWebhooks && Boolean(clusterId),
    refetchInterval: refresh?.interval,
  })
  const visibleValidatingWebhooksQueryData =
    permissions.validatingWebhooks && Boolean(clusterId)
      ? readableQueryData(validatingWebhooksQuery)
      : undefined
  const enabledQueries = [
    nodesQuery,
    eventsQuery,
    auditQuery,
    operationQuery,
    capabilitiesQuery,
    networkPoliciesQuery,
    hpasQuery,
    pdbsQuery,
    mutatingWebhooksQuery,
    validatingWebhooksQuery,
  ].filter((query) => query.isEnabled)
  const isFetching = enabledQueries.some((query) => query.isFetching)
  const isError = enabledQueries.some((query) => query.isError)
  useEffect(() => {
    onRefreshStateChange?.({ isFetching, isError })
  }, [isFetching, isError, onRefreshStateChange])

  const previousRefreshToken = useRef(refreshToken)
  useEffect(() => {
    if (previousRefreshToken.current === refreshToken) return
    previousRefreshToken.current = refreshToken
    void Promise.allSettled([
      nodesQuery.isEnabled ? nodesQuery.refetch() : Promise.resolve(),
      eventsQuery.isEnabled ? eventsQuery.refetch() : Promise.resolve(),
      auditQuery.isEnabled ? auditQuery.refetch() : Promise.resolve(),
      operationQuery.isEnabled ? operationQuery.refetch() : Promise.resolve(),
      capabilitiesQuery.isEnabled ? capabilitiesQuery.refetch() : Promise.resolve(),
      networkPoliciesQuery.isEnabled ? networkPoliciesQuery.refetch() : Promise.resolve(),
      hpasQuery.isEnabled ? hpasQuery.refetch() : Promise.resolve(),
      pdbsQuery.isEnabled ? pdbsQuery.refetch() : Promise.resolve(),
      mutatingWebhooksQuery.isEnabled ? mutatingWebhooksQuery.refetch() : Promise.resolve(),
      validatingWebhooksQuery.isEnabled ? validatingWebhooksQuery.refetch() : Promise.resolve(),
    ])
  }, [
    refreshToken,
    nodesQuery.isEnabled,
    nodesQuery.refetch,
    eventsQuery.isEnabled,
    eventsQuery.refetch,
    auditQuery.isEnabled,
    auditQuery.refetch,
    operationQuery.isEnabled,
    operationQuery.refetch,
    capabilitiesQuery.isEnabled,
    capabilitiesQuery.refetch,
    networkPoliciesQuery.isEnabled,
    networkPoliciesQuery.refetch,
    hpasQuery.isEnabled,
    hpasQuery.refetch,
    pdbsQuery.isEnabled,
    pdbsQuery.refetch,
    mutatingWebhooksQuery.isEnabled,
    mutatingWebhooksQuery.refetch,
    validatingWebhooksQuery.isEnabled,
    validatingWebhooksQuery.refetch,
  ])
  const streamKinds = useMemo(
    () => [permissions.nodes ? 'Node' : '', permissions.events ? 'Event' : ''].filter(Boolean),
    [permissions.events, permissions.nodes],
  )
  const refreshLiveEvidence = () => {
    if (refresh && (!refresh.enabled || refresh.hidden)) return Promise.resolve()
    return Promise.all([
      nodesQuery.isEnabled && !nodesQuery.isError ? nodesQuery.refetch() : Promise.resolve(),
      eventsQuery.isEnabled && !eventsQuery.isError ? eventsQuery.refetch() : Promise.resolve(),
    ])
  }
  const evidenceStream = useKubernetesResourceStream({
    clusterId,
    kinds: streamKinds,
    enabled:
      Boolean(clusterId && streamKinds.length > 0) &&
      (!refresh || (refresh.enabled && !refresh.hidden)),
    onEvent: () => void refreshLiveEvidence(),
    onFallback: () => void refreshLiveEvidence(),
    onResyncRequired: refreshLiveEvidence,
  })

  const capacity = useMemo(
    () => summarizeNodeCapacity(visibleNodesQueryData ?? []),
    [visibleNodesQueryData],
  )
  const governance = useMemo(
    () => summarizeCapabilityGovernance(visibleCapabilitiesQueryData ?? [], connectionMode),
    [visibleCapabilitiesQueryData, connectionMode],
  )
  const timeline = useMemo(
    () =>
      buildActivityTimeline(
        visibleEventsQueryData ?? [],
        visibleAuditQueryData ?? [],
        visibleOperationQueryData ?? [],
      ),
    [visibleAuditQueryData, visibleEventsQueryData, visibleOperationQueryData],
  )
  const canViewTimeline = permissions.events || permissions.audit || permissions.operations
  const admissionLoading =
    (permissions.mutatingWebhooks && mutatingWebhooksQuery.isLoading) ||
    (permissions.validatingWebhooks && validatingWebhooksQuery.isLoading)
  const admissionError =
    (permissions.mutatingWebhooks && mutatingWebhooksQuery.isError) ||
    (permissions.validatingWebhooks && validatingWebhooksQuery.isError)
  const governanceCoverage = [
    {
      key: 'network-policy',
      label: 'NetworkPolicy',
      count: permissions.networkPolicies ? visibleNetworkPoliciesQueryData?.length : undefined,
      path: '/network/networkpolicies',
    },
    {
      key: 'admission',
      label: 'Admission Webhook',
      count:
        (permissions.mutatingWebhooks || permissions.validatingWebhooks) &&
        !admissionLoading &&
        !admissionError
          ? (visibleMutatingWebhooksQueryData?.length ?? 0) +
            (visibleValidatingWebhooksQueryData?.length ?? 0)
          : undefined,
      path: permissions.validatingWebhooks
        ? '/configuration/validatingwebhookconfigurations'
        : '/configuration/mutatingwebhookconfigurations',
    },
    {
      key: 'pdb',
      label: 'PodDisruptionBudget',
      count: permissions.podDisruptionBudgets ? visiblePdbsQueryData?.length : undefined,
      path: '/configuration/poddisruptionbudgets',
    },
    {
      key: 'hpa',
      label: 'HorizontalPodAutoscaler',
      count: permissions.hpas ? visibleHpasQueryData?.length : undefined,
      path: '/configuration/hpas',
    },
  ]

  if (!clusterId) {
    return (
      <Card
        className="soha-overview-runtime-card"
        title={localeCode === 'zh_CN' ? '运行证据与治理' : 'Operations Evidence'}
      >
        <ManagementState
          bordered={false}
          compact
          kind="select-scope"
          title={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'}
        />
      </Card>
    )
  }

  return (
    <div className="soha-platform-operations-stack">
      <div className="soha-overview-summary-grid">
        <Card
          className="soha-overview-panel-card"
          title={localeCode === 'zh_CN' ? '节点容量' : 'Node Capacity'}
          extra={
            <Space size={8} wrap>
              {permissions.nodes ? (
                <Button
                  type="text"
                  icon={<ArrowRightOutlined />}
                  onClick={() => navigate('/cluster-resources/nodes')}
                >
                  {localeCode === 'zh_CN' ? '查看节点' : 'Open Nodes'}
                </Button>
              ) : null}
              {streamKinds.length > 0 ? (
                <ResourceStreamStatus
                  lastEventAt={evidenceStream.lastEventAt}
                  localeCode={localeCode}
                  status={evidenceStream.status}
                />
              ) : null}
            </Space>
          }
        >
          <section className="soha-platform-ops-section" aria-label="capacity">
            {nodesQuery.isError && readableQueryData(nodesQuery) !== undefined ? (
              <ReadFeedback canRead={nodesQuery.isEnabled} query={nodesQuery} label="节点容量" />
            ) : null}
            <div className="soha-platform-ops-row-head">
              <Text strong>
                {localeCode === 'zh_CN' ? '各项节点最高占比' : 'Peak Percentage by Node'}
              </Text>
              <Text type="secondary" className="text-xs">
                {visibleNodesQueryData === undefined
                  ? '—'
                  : `${capacity.readyNodes}/${capacity.totalNodes}`}{' '}
                {localeCode === 'zh_CN' ? '就绪' : 'Ready'}
              </Text>
            </div>
            {!permissions.nodes ? (
              <ManagementState
                bordered={false}
                compact
                kind="no-permission"
                title={localeCode === 'zh_CN' ? '无权限查看节点容量' : 'No node capacity access'}
              />
            ) : nodesQuery.isLoading ? (
              <div className="soha-platform-ops-loading">
                <Spin size="small" />
              </div>
            ) : nodesQuery.isError && readableQueryData(nodesQuery) === undefined ? (
              <ReadFeedback canRead={nodesQuery.isEnabled} query={nodesQuery} label="节点容量" />
            ) : (
              <div className="soha-platform-ops-capacity-list">
                <div className="soha-platform-capacity-resource">
                  <Text strong>CPU</Text>
                  <CapacityMetric
                    label={localeCode === 'zh_CN' ? '实际使用' : 'Actual Usage'}
                    value={capacity.cpuPercent}
                    missingLabel={localeCode === 'zh_CN' ? '未提供' : 'Unavailable'}
                  />
                  <CapacityMetric
                    label={localeCode === 'zh_CN' ? 'Requests 分配' : 'Requests Allocation'}
                    value={capacity.cpuRequestPercent}
                    missingLabel={localeCode === 'zh_CN' ? '未提供' : 'Unavailable'}
                  />
                </div>
                <div className="soha-platform-capacity-resource">
                  <Text strong>{localeCode === 'zh_CN' ? '内存' : 'Memory'}</Text>
                  <CapacityMetric
                    label={localeCode === 'zh_CN' ? '实际使用' : 'Actual Usage'}
                    value={capacity.memoryPercent}
                    missingLabel={localeCode === 'zh_CN' ? '未提供' : 'Unavailable'}
                  />
                  <CapacityMetric
                    label={localeCode === 'zh_CN' ? 'Requests 分配' : 'Requests Allocation'}
                    value={capacity.memoryRequestPercent}
                    missingLabel={localeCode === 'zh_CN' ? '未提供' : 'Unavailable'}
                  />
                </div>
                <div className="soha-platform-capacity-resource">
                  <Text strong>{localeCode === 'zh_CN' ? 'Pods 容量' : 'Pod Capacity'}</Text>
                  <CapacityMetric
                    label={localeCode === 'zh_CN' ? '节点 Pod 占比' : 'Node Pod Occupancy'}
                    value={capacity.podPercent}
                    missingLabel={localeCode === 'zh_CN' ? '未提供' : 'Unavailable'}
                  />
                </div>
                <div className="soha-platform-ops-inline-meta">
                  <span>
                    {localeCode === 'zh_CN' ? '资源信号' : 'Signals'}: {capacity.signalNodes}/
                    {capacity.totalNodes}
                  </span>
                  <span>
                    {localeCode === 'zh_CN' ? '不可调度' : 'Unschedulable'}:{' '}
                    {capacity.unschedulableNodes}
                  </span>
                </div>
              </div>
            )}
          </section>
        </Card>

        <Card
          className="soha-overview-panel-card"
          title={localeCode === 'zh_CN' ? '最近运行证据' : 'Recent Operations Evidence'}
          extra={
            <Space size={4}>
              <Button
                type="text"
                icon={<RobotOutlined />}
                disabled={!assistant}
                onClick={() =>
                  void assistant?.launchAssistant({
                    action: 'analyze-page',
                    contextOverride: {
                      sourceWorkbench: 'platform',
                      sourceTitle:
                        localeCode === 'zh_CN' ? 'K8s 工作台运行证据' : 'K8s operations evidence',
                      entityKind: 'kubernetes.cluster',
                      entityName: clusterId,
                      clusterId,
                      timeRangeMinutes: 60,
                      pinnedData: buildOperationsAIEvidence({
                        capacity,
                        connectionMode,
                        governance,
                        coverage: governanceCoverage,
                        timeline,
                      }),
                      promptHint:
                        localeCode === 'zh_CN'
                          ? '结合节点压力、治理覆盖和最近运行证据，按影响与证据强度排序排查。'
                          : 'Prioritize investigation by impact and evidence strength across capacity, governance, and recent operations.',
                    },
                  })
                }
              >
                {localeCode === 'zh_CN' ? 'AI 排障' : 'AI troubleshoot'}
              </Button>
              {permissions.operations ? (
                <Button
                  type="text"
                  icon={<ArrowRightOutlined />}
                  onClick={() => navigate('/system/operations')}
                >
                  {localeCode === 'zh_CN' ? '操作日志' : 'Operation Logs'}
                </Button>
              ) : null}
            </Space>
          }
        >
          <ReadFeedback canRead={eventsQuery.isEnabled} query={eventsQuery} label="资源事件" />
          <ReadFeedback canRead={auditQuery.isEnabled} query={auditQuery} label="审计记录" />
          <ReadFeedback
            canRead={operationQuery.isEnabled}
            query={operationQuery}
            label="操作记录"
          />
          {!canViewTimeline ? (
            <ManagementState
              bordered={false}
              compact
              kind="no-permission"
              title={localeCode === 'zh_CN' ? '无权限查看运行证据' : 'No evidence access'}
            />
          ) : eventsQuery.isLoading || auditQuery.isLoading || operationQuery.isLoading ? (
            <div className="soha-platform-ops-loading">
              <Spin size="small" />
            </div>
          ) : timeline.length === 0 &&
            [eventsQuery, auditQuery, operationQuery].some(
              (query) => query.isEnabled && query.isError,
            ) ? null : timeline.length === 0 ? (
            <ManagementState
              bordered={false}
              compact
              title={localeCode === 'zh_CN' ? '当前没有运行记录' : 'No recent operations'}
            />
          ) : (
            <div className="soha-platform-activity-list">
              {timeline.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  className="soha-platform-activity-row"
                  onClick={() => navigate(item.path)}
                >
                  <span className={`soha-platform-activity-source is-${item.source}`}>
                    {item.source === 'kubernetes'
                      ? 'K8s'
                      : item.source === 'audit'
                        ? localeCode === 'zh_CN'
                          ? '审计'
                          : 'Audit'
                        : localeCode === 'zh_CN'
                          ? '操作'
                          : 'Operation'}
                  </span>
                  <span className="soha-platform-activity-copy">
                    <Text strong ellipsis>
                      {formatPlatformOverviewText(item.title, localeCode)}
                    </Text>
                    <span>
                      {item.description
                        ? formatPlatformOverviewText(item.description, localeCode)
                        : '-'}
                    </span>
                  </span>
                  <span className="soha-platform-activity-meta">
                    <StatusTag
                      label={formatPlatformOverviewText(item.status, localeCode)}
                      value={item.status}
                    />
                    <span>{formatDateTime(item.timestamp)}</span>
                  </span>
                </button>
              ))}
            </div>
          )}
        </Card>
      </div>
      <Collapse
        className="soha-platform-governance-collapse"
        defaultActiveKey={[]}
        items={[
          {
            key: 'governance',
            label: localeCode === 'zh_CN' ? '治理能力' : 'Governance Capabilities',
            children: (
              <>
                <section className="soha-platform-ops-section" aria-label="governance">
                  {capabilitiesQuery.isError &&
                  readableQueryData(capabilitiesQuery) !== undefined ? (
                    <ReadFeedback
                      canRead={capabilitiesQuery.isEnabled}
                      query={capabilitiesQuery}
                      label="治理能力"
                    />
                  ) : null}
                  <div className="soha-platform-ops-row-head">
                    <Text strong>
                      {localeCode === 'zh_CN' ? '高风险与审批能力' : 'Risk & Approval Capabilities'}
                    </Text>
                    <Text type="secondary" className="text-xs">
                      {localeCode === 'zh_CN'
                        ? visibleCapabilitiesQueryData === undefined
                          ? '—'
                          : `共 ${governance.approvalRequired} 项能力需审批`
                        : visibleCapabilitiesQueryData === undefined
                          ? '—'
                          : `${governance.approvalRequired} approval-gated`}
                    </Text>
                  </div>
                  {capabilitiesQuery.isLoading ? (
                    <div className="soha-platform-ops-loading">
                      <Spin size="small" />
                    </div>
                  ) : capabilitiesQuery.isError &&
                    readableQueryData(capabilitiesQuery) === undefined ? (
                    <ReadFeedback
                      canRead={capabilitiesQuery.isEnabled}
                      query={capabilitiesQuery}
                      label="治理能力"
                    />
                  ) : (
                    <div className="soha-platform-ops-risk-list">
                      {governance.items.map((item) => (
                        <div key={item.key} className="soha-platform-ops-risk-row">
                          <div>
                            <Text>
                              {formatPlatformOverviewCapability(item.key, item.label, localeCode)}
                            </Text>
                            <div className="soha-overview-inline-caption">{item.key}</div>
                          </div>
                          <div className="soha-platform-ops-risk-tags">
                            {item.requiresApproval ? (
                              <StatusTag
                                label={formatPlatformOverviewText('approval', localeCode)}
                                value="approval"
                              />
                            ) : null}
                            <StatusTag
                              label={formatPlatformOverviewText(item.riskLevel, localeCode)}
                              value={item.riskLevel}
                            />
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </section>
                <div className="soha-detail-stack">
                  <ReadFeedback
                    canRead={networkPoliciesQuery.isEnabled}
                    query={networkPoliciesQuery}
                    label="NetworkPolicy"
                  />
                  <ReadFeedback canRead={hpasQuery.isEnabled} query={hpasQuery} label="HPA" />
                  <ReadFeedback
                    canRead={pdbsQuery.isEnabled}
                    query={pdbsQuery}
                    label="PodDisruptionBudget"
                  />
                  <ReadFeedback
                    canRead={mutatingWebhooksQuery.isEnabled}
                    query={mutatingWebhooksQuery}
                    label="变更 Webhook"
                  />
                  <ReadFeedback
                    canRead={validatingWebhooksQuery.isEnabled}
                    query={validatingWebhooksQuery}
                    label="校验 Webhook"
                  />
                </div>
                <div className="soha-platform-governance-coverage">
                  {governanceCoverage.map((item) => (
                    <button
                      key={item.key}
                      type="button"
                      className="soha-platform-governance-item"
                      onClick={() => navigate(item.path)}
                    >
                      <span>{item.label}</span>
                      <strong>{item.count ?? '-'}</strong>
                    </button>
                  ))}
                </div>
              </>
            ),
          },
        ]}
      />
    </div>
  )
}
