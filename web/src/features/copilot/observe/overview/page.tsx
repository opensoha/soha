import { ManagementState } from '@/components/management-list'
import {
  OverviewChip,
  OverviewMetricCard,
  type OverviewChipItem,
  type OverviewMetricItem,
} from '@/components/overview-visuals'
import { ReadFeedback } from '@/components/request-feedback'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import {
  ApiOutlined,
  AuditOutlined,
  BookOutlined,
  ExperimentOutlined,
  PlayCircleOutlined,
  RobotOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button } from 'antd'
import { Link } from 'react-router-dom'
import '../../copilot-pages.css'
import { evaluationQueries } from '../../evaluation/queries'
import { gatewayQueries } from '../../gateway/queries'
import { knowledgeQueries } from '../../knowledge/queries'
import { aiProductionOperationsQueries } from '../../production-operations/queries'
import { getAIWorkbenchPathForMode } from '../../workbench/navigation'
import { workbenchQueries } from '../../workbench/queries'

const pendingApprovalFilters = {
  id: '',
  status: 'pending',
  actor: '',
  aiClientId: '',
  toolName: '',
  riskLevel: '',
  strategy: '',
  from: '',
  to: '',
}

function domainHelper(allowed: boolean, error: boolean, detail: string) {
  if (!allowed) return '无查看权限'
  return error ? '服务暂时不可用，进入对应页面重试' : detail
}

function operationalSummary(
  allowed: boolean,
  loading: boolean,
  error: boolean,
  value: number,
  helper: string,
): Pick<OverviewChipItem, 'value' | 'helper' | 'tone'> {
  if (!allowed) return { value: '-', helper: '无查看权限' }
  if (loading) return { value: '-', helper: '加载中' }
  if (error) return { value: '-', helper: '加载失败，进入页面重试', tone: 'danger' }
  return { value, helper, tone: value > 0 ? 'warning' : 'default' }
}

export function AIObserveOverviewPage() {
  const permissionQuery = usePermissionSnapshot()
  const snapshot = readableQueryData(permissionQuery)?.data
  const canChat = hasPermission(snapshot, 'observe.ai.chat')
  const canObserve = hasPermission(snapshot, 'observe.ai.view')
  const canKnowledge = hasPermission(snapshot, 'ai.knowledge.view')
  const canGateway = hasPermission(snapshot, 'ai.gateway.view')
  const canRelay = hasPermission(snapshot, 'ai.gateway.relay.view')
  const canEvaluate = hasPermission(snapshot, 'ai.evaluations.view')
  const canApprovals = hasPermission(snapshot, 'ai.gateway.approvals.view')
  const canOperations = hasPermission(snapshot, 'ai.operations.view')

  const sessionsQuery = useQuery(workbenchQueries.sessions.all(canChat))
  const catalogQuery = useQuery({ ...workbenchQueries.catalog(), enabled: canObserve })
  const runsQuery = useQuery({ ...workbenchQueries.agentRuns.all(), enabled: canObserve })
  const basesQuery = useQuery(knowledgeQueries.bases(canKnowledge))
  const manifestQuery = useQuery(
    gatewayQueries.manifest({ aiClientId: '', skillId: '', source: '' }, canGateway),
  )
  const relayQuery = useQuery(gatewayQueries.relay.metrics(canRelay))
  const evaluationsQuery = useQuery({ ...evaluationQueries.runs(), enabled: canEvaluate })
  const approvalsQuery = useQuery(gatewayQueries.approvals(pendingApprovalFilters, canApprovals))
  const operationsQuery = useQuery({
    ...aiProductionOperationsQueries.snapshots(),
    enabled: canOperations,
  })

  const sessions = readableQueryData(sessionsQuery)?.data ?? []
  const catalog = readableQueryData(catalogQuery)?.data
  const runs = readableQueryData(runsQuery)?.data ?? []
  const bases = readableQueryData(basesQuery)?.data ?? []
  const manifest = readableQueryData(manifestQuery)?.data
  const relay = readableQueryData(relayQuery)?.data
  const evaluations = readableQueryData(evaluationsQuery)?.data ?? []
  const approvals = readableQueryData(approvalsQuery)?.data ?? []
  const operations = readableQueryData(operationsQuery)?.data ?? []
  const interactionAllowed = canChat || canObserve
  const interactionError = sessionsQuery.isError || runsQuery.isError
  const gatewayAllowed = canGateway || canRelay
  const gatewayError = manifestQuery.isError || relayQuery.isError
  const knowledgeAnomalies = bases.filter(
    (base) => base.status && !['active', 'ready'].includes(base.status),
  ).length
  const failedEvaluations = evaluations.filter((run) => run.status === 'failed').length
  const anomalousRuns = runs.filter((run) =>
    ['failed', 'canceled', 'cancelled', 'callback_timeout', 'timed_out'].includes(run.status),
  ).length
  const activeOperations = operations.filter((operation) =>
    ['queued', 'running', 'pending'].includes(operation.status),
  ).length

  const overviewStats: Array<
    OverviewMetricItem & { allowed: boolean; loading: boolean; path: string }
  > = [
    {
      key: 'interaction',
      label: '会话 / Agent Runs',
      value: interactionAllowed
        ? `${canChat && readableQueryData(sessionsQuery) ? sessions.length : '-'} / ${canObserve && readableQueryData(runsQuery) ? runs.length : '-'}`
        : '-',
      helper: domainHelper(
        interactionAllowed,
        interactionError,
        canObserve
          ? `运行中 ${runs.filter((run) => ['queued', 'running', 'claimed'].includes(run.status)).length}`
          : `当前会话 ${sessions.length}`,
      ),
      icon: <RobotOutlined />,
      tone: interactionError ? 'danger' : 'default',
      path: canChat ? getAIWorkbenchPathForMode('general') : '/ai-workbench/agent-runs',
      allowed: interactionAllowed,
      loading:
        permissionQuery.isLoading ||
        (canChat && sessionsQuery.isLoading) ||
        (canObserve && runsQuery.isLoading),
    },
    {
      key: 'knowledge',
      label: '知识库',
      value: canKnowledge && readableQueryData(basesQuery) ? bases.length : '-',
      helper: domainHelper(
        canKnowledge,
        basesQuery.isError,
        `可用 ${bases.filter((base) => ['active', 'ready'].includes(base.status || '')).length} · 异常 ${bases.filter((base) => base.status && !['active', 'ready'].includes(base.status)).length}`,
      ),
      icon: <BookOutlined />,
      tone: basesQuery.isError ? 'danger' : 'default',
      path: '/ai-workbench/knowledge',
      allowed: canKnowledge,
      loading: permissionQuery.isLoading || (canKnowledge && basesQuery.isLoading),
    },
    {
      key: 'providers',
      label: 'Agent Providers',
      value: canObserve ? (catalog?.agentProviders?.length ?? '-') : '-',
      helper: domainHelper(
        canObserve,
        catalogQuery.isError,
        `Skills ${catalog?.skillsRegistry?.length ?? 0} · Capabilities ${catalog?.capabilities?.length ?? 0}`,
      ),
      icon: <PlayCircleOutlined />,
      tone: catalogQuery.isError ? 'danger' : 'default',
      path: '/ai-workbench/agent-providers',
      allowed: canObserve,
      loading: permissionQuery.isLoading || (canObserve && catalogQuery.isLoading),
    },
    {
      key: 'gateway',
      label: 'Gateway Tools / 调用',
      value: gatewayAllowed
        ? `${canGateway ? (manifest?.summary.toolCount ?? '-') : '-'} / ${canRelay ? (relay?.requestsToday ?? relay?.totalCalls ?? '-') : '-'}`
        : '-',
      helper: domainHelper(
        gatewayAllowed,
        gatewayError,
        `Skills ${manifest?.summary.skillCount ?? '-'}${manifest?.version ? ` · Manifest ${manifest.version}` : ''}${typeof relay?.successRate === 'number' ? ` · 成功率 ${(relay.successRate * 100).toFixed(1)}%` : ''}`,
      ),
      icon: <ApiOutlined />,
      tone: gatewayError ? 'danger' : 'default',
      path: canRelay ? '/ai-gateway/relay' : '/ai-gateway/manifest',
      allowed: gatewayAllowed,
      loading:
        permissionQuery.isLoading ||
        (canGateway && manifestQuery.isLoading) ||
        (canRelay && relayQuery.isLoading),
    },
  ]

  const operationalActions: Array<OverviewChipItem & { path: string; allowed: boolean }> = [
    {
      key: 'knowledge',
      label: '知识库',
      ...operationalSummary(
        canKnowledge,
        basesQuery.isLoading,
        basesQuery.isError,
        knowledgeAnomalies,
        '异常知识库',
      ),
      icon: <BookOutlined />,
      path: '/ai-workbench/knowledge',
      allowed: canKnowledge,
    },
    {
      key: 'evaluations',
      label: '评测回归',
      ...operationalSummary(
        canEvaluate,
        evaluationsQuery.isLoading,
        evaluationsQuery.isError,
        failedEvaluations,
        '失败评测',
      ),
      icon: <ExperimentOutlined />,
      path: '/ai-workbench/evaluations',
      allowed: canEvaluate,
    },
    {
      key: 'approvals',
      label: '审批请求',
      ...operationalSummary(
        canApprovals,
        approvalsQuery.isLoading,
        approvalsQuery.isError,
        approvals.length,
        '待审批',
      ),
      icon: <AuditOutlined />,
      path: '/ai-gateway/governance?tab=approvals',
      allowed: canApprovals,
    },
    {
      key: 'agent-runs',
      label: 'Agent Runs',
      ...operationalSummary(
        canObserve,
        runsQuery.isLoading,
        runsQuery.isError,
        anomalousRuns,
        '异常运行',
      ),
      icon: <WarningOutlined />,
      path: '/ai-workbench/agent-runs',
      allowed: canObserve,
    },
    {
      key: 'operations',
      label: '生产操作',
      ...operationalSummary(
        canOperations,
        operationsQuery.isLoading,
        operationsQuery.isError,
        activeOperations,
        '进行中或待执行',
      ),
      icon: <PlayCircleOutlined />,
      path: '/ai-workbench/production-operations',
      allowed: canOperations,
    },
  ]

  if (permissionQuery.isError) {
    return (
      <div className="soha-page soha-overview-page soha-ai-unified-overview">
        <ReadFeedback query={sessionsQuery} canRead={canChat} label="会话" />
        <ReadFeedback query={runsQuery} canRead={canObserve} label="Agent 运行" />
        <ReadFeedback query={catalogQuery} canRead={canObserve} label="能力目录" />
        <ReadFeedback query={basesQuery} canRead={canKnowledge} label="知识库" />
        <ReadFeedback query={manifestQuery} canRead={canGateway} label="Gateway 工具" />
        <ReadFeedback query={relayQuery} canRead={canRelay} label="调用指标" />
        <ReadFeedback query={evaluationsQuery} canRead={canEvaluate} label="评测" />
        <ReadFeedback query={approvalsQuery} canRead={canApprovals} label="审批" />
        <ReadFeedback query={operationsQuery} canRead={canOperations} label="生产操作" />
        <ManagementState
          kind="error"
          actions={<Button onClick={() => void permissionQuery.refetch()}>重试</Button>}
        />
      </div>
    )
  }

  return (
    <div className="soha-page soha-overview-page soha-ai-unified-overview">
      <div className="soha-overview-metric-grid">
        {overviewStats.map(({ key, path, allowed, loading, ...item }) => {
          const card = <OverviewMetricCard {...item} loading={loading} />
          return allowed ? (
            <Link
              aria-label={`查看${String(item.label)}`}
              className="soha-overview-card-link"
              key={key}
              to={path}
            >
              {card}
            </Link>
          ) : (
            <div key={key}>{card}</div>
          )
        })}
      </div>
      <section className="soha-overview-alert-stack" aria-label="待处理事项">
        <div className="soha-overview-chip-grid">
          {operationalActions.map((action) => {
            const { key, path, allowed, ...item } = action
            const chip = <OverviewChip {...item} />
            return allowed ? (
              <Link
                aria-label={`查看${String(item.label)}`}
                className="soha-overview-card-link"
                key={key}
                to={path}
              >
                {chip}
              </Link>
            ) : (
              <div aria-disabled="true" key={key}>
                {chip}
              </div>
            )
          })}
        </div>
      </section>
    </div>
  )
}
