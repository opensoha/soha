import { ManagementState } from '@/components/management-list'
import {
  OverviewHeroCard,
  type OverviewChipItem,
  type OverviewMetricItem,
} from '@/components/overview-visuals'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import {
  ApiOutlined,
  AuditOutlined,
  BookOutlined,
  ExperimentOutlined,
  PlayCircleOutlined,
  RobotOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button, Card } from 'antd'
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
      <div className="soha-overview-bento-top">
        <OverviewHeroCard
          variant="ai"
          eyebrowIcon={<RobotOutlined />}
          eyebrowLabel="AI 智能体与网关编排中枢"
          value={overviewStats[0]?.value ?? '-'}
          valueSuffix="活跃会话 / Agent Runs"
          caption={overviewStats[0]?.helper}
          kpis={[
            {
              key: 'skills',
              label: 'Skills 注册表',
              value: catalog?.skillsRegistry?.length ?? '—',
            },
            {
              key: 'rate',
              label: '网关成功率',
              value:
                typeof relay?.successRate === 'number'
                  ? `${(relay.successRate * 100).toFixed(1)}%`
                  : '—',
            },
          ]}
          telemetryNodes={[
            {
              id: 'heya-core',
              label: `SESSIONS · ${sessions.length}`,
              title: 'Heya 对话与智能体中枢',
              metric: `会话 ${sessions.length} · Agent Runs ${runs.length}`,
              status: 'primary',
              path: overviewStats[0]?.allowed ? overviewStats[0].path : undefined,
            },
            {
              id: 'rag-kb',
              label: `RAG-KB · ${bases.length}`,
              title: '企业知识库与向量索引',
              metric:
                knowledgeAnomalies > 0
                  ? `${knowledgeAnomalies} 个知识库异常`
                  : `${bases.length} 个知识库就绪`,
              status: knowledgeAnomalies > 0 ? 'warning' : 'healthy',
              path: canKnowledge ? '/ai-workbench/knowledge' : undefined,
            },
            {
              id: 'ai-approvals',
              label: approvals.length > 0 ? `APPROVAL · ${approvals.length}` : 'APPROVAL · 0',
              title: '高风险工具审批门禁',
              metric: approvals.length > 0 ? `${approvals.length} 项待审批` : '无待办审批请求',
              status: approvals.length > 0 ? 'warning' : 'healthy',
              path: canApprovals ? '/ai-gateway/governance?tab=approvals' : undefined,
            },
            {
              id: 'agent-runs',
              label: anomalousRuns > 0 ? `RUN-ERR · ${anomalousRuns}` : `RUNS · ${runs.length}`,
              title: 'Agent 异步运行队列',
              metric: anomalousRuns > 0 ? `${anomalousRuns} 条异常运行` : '运行队列正常',
              status: anomalousRuns > 0 ? 'critical' : 'healthy',
              path: canObserve ? '/ai-workbench/agent-runs' : undefined,
            },
            {
              id: 'ai-gateway',
              label: `TOOLS · ${manifest?.summary.toolCount ?? '—'}`,
              title: 'AI Gateway 工具与模型路由',
              metric: `今日调用 ${relay?.requestsToday ?? '—'}`,
              status: 'primary',
              path: gatewayAllowed ? overviewStats[3]?.path : undefined,
            },
          ]}
          embeddedPods={overviewStats.slice(1).map(({ key, path, allowed, loading, ...item }) => ({
            key,
            label: item.label,
            value: item.value,
            helper: item.helper,
            icon: item.icon,
            tone: item.tone,
            loading,
            to: path,
            readonly: !allowed,
            ariaLabel: `查看${String(item.label)}`,
          }))}
          segments={[
            {
              key: 'runs',
              label: '运行中 Agent',
              value: runs.filter((run) => ['queued', 'running', 'claimed'].includes(run.status))
                .length,
              tone: 'primary',
            },
            {
              key: 'kb',
              label: '就绪知识库',
              value: bases.filter((base) => ['active', 'ready'].includes(base.status || '')).length,
              tone: 'success',
            },
            {
              key: 'approvals',
              label: '待审批动作',
              value: approvals.length,
              tone: 'warning',
            },
            {
              key: 'anomalies',
              label: '异常运行',
              value: anomalousRuns,
              tone: 'danger',
            },
          ]}
          ctaLabel={overviewStats[0]?.allowed ? '进入对话工作台' : undefined}
          ctaTo={overviewStats[0]?.allowed ? overviewStats[0].path : undefined}
          detailDrawerTitle="AI 工作台治理与待办明细"
          graphicDeck={[
            {
              key: 'ai-gateway-rings',
              kind: 'rings',
              title: '智能体与网关健康光谱环',
              subtitle: '推理路由 · 检索增强 · 技能执行',
              actionLabel: 'Gateway',
              actionTo: gatewayAllowed ? overviewStats[3]?.path : undefined,
              centerValue:
                typeof relay?.successRate === 'number'
                  ? `${(relay.successRate * 100).toFixed(0)}%`
                  : `${sessions.length}`,
              centerLabel: typeof relay?.successRate === 'number' ? '调用成功' : '活跃会话',
              rings: [
                {
                  key: 'kb-ready',
                  label: '就绪知识库',
                  value: `${bases.filter((b) => ['active', 'ready'].includes(b.status || '')).length}/${bases.length}`,
                  ratio:
                    bases.length > 0
                      ? bases.filter((b) => ['active', 'ready'].includes(b.status || '')).length /
                        bases.length
                      : 0.9,
                  tone: 'success',
                  to: canKnowledge ? '/ai-workbench/knowledge' : undefined,
                },
                {
                  key: 'gateway-rate',
                  label: '网关成功率',
                  value:
                    typeof relay?.successRate === 'number'
                      ? `${(relay.successRate * 100).toFixed(1)}%`
                      : '—',
                  ratio: typeof relay?.successRate === 'number' ? relay.successRate : 0.95,
                  tone: 'primary',
                  to: gatewayAllowed ? overviewStats[3]?.path : undefined,
                },
                {
                  key: 'approvals-ring',
                  label: '待审批门禁',
                  value: approvals.length,
                  ratio: approvals.length > 0 ? Math.min(1, approvals.length / 6) : 0,
                  tone: approvals.length > 0 ? 'warning' : 'neutral',
                  to: canApprovals ? '/ai-gateway/governance?tab=approvals' : undefined,
                },
                {
                  key: 'run-anomalies',
                  label: '异常 Agent Run',
                  value: anomalousRuns,
                  ratio: runs.length > 0 ? anomalousRuns / runs.length : 0,
                  tone: anomalousRuns > 0 ? 'danger' : 'neutral',
                  to: canObserve ? '/ai-workbench/agent-runs' : undefined,
                },
              ],
            },
            {
              key: 'ai-capability-spectrum',
              kind: 'spectrum',
              title: '多域智能编排能力频谱',
              subtitle: '会话 · 工具 · 知识 · 技能矩阵',
              actionLabel: 'Agent 目录',
              actionTo: canObserve ? '/ai-workbench/agent-providers' : undefined,
              bars: [
                {
                  key: 'sessions',
                  label: '对话会话',
                  value: sessions.length,
                  tone: 'primary',
                  to: overviewStats[0]?.allowed ? overviewStats[0].path : undefined,
                },
                {
                  key: 'runs',
                  label: 'Agent 运行',
                  value: runs.length,
                  tone: anomalousRuns > 0 ? 'warning' : 'primary',
                  to: canObserve ? '/ai-workbench/agent-runs' : undefined,
                },
                {
                  key: 'kb',
                  label: '知识库',
                  value: bases.length,
                  tone: 'success',
                  to: canKnowledge ? '/ai-workbench/knowledge' : undefined,
                },
                {
                  key: 'tools',
                  label: 'MCP 工具',
                  value: manifest?.summary.toolCount ?? 0,
                  tone: 'primary',
                },
                {
                  key: 'skills',
                  label: '注册技能',
                  value: catalog?.skillsRegistry?.length ?? 0,
                  tone: 'success',
                },
              ],
            },
            {
              key: 'ai-governance-radar',
              kind: 'radar',
              title: '治理审批·评测·生产动作雷达',
              subtitle: `${approvals.length + failedEvaluations + activeOperations} 项焦点信号`,
              actionLabel: '审批台',
              actionTo: canApprovals ? '/ai-gateway/governance?tab=approvals' : undefined,
              emptyLabel: '零待办审批与失败评测',
              blips: operationalActions.map((act) => ({
                key: act.key,
                label: String(act.label),
                badge: `${act.value ?? 0} · ${act.helper ?? ''}`,
                tone:
                  act.tone === 'danger'
                    ? ('danger' as const)
                    : act.tone === 'warning'
                      ? ('warning' as const)
                      : ('success' as const),
                to: act.allowed ? act.path : undefined,
              })),
            },
          ]}
        />
        <div className="soha-ai-overview-bottom-grid">
          <Card
            variant="borderless"
            className="soha-ai-overview-panel"
            title="最近 Agent 运行"
            extra={canObserve ? <Link to="/ai-workbench/agent-runs">全部运行</Link> : null}
          >
            {runs.length > 0 ? (
              <div className="soha-ai-overview-list">
                {runs.slice(0, 4).map((run) => (
                  <div className="soha-ai-overview-row" key={run.id}>
                    <div className="soha-ai-overview-row__main">
                      <strong>{run.id}</strong>
                      <span>
                        {[run.providerId, run.capabilityId].filter(Boolean).join(' · ') ||
                          'Agent 执行回执'}
                      </span>
                    </div>
                    <StatusTag value={String(run.status || 'unknown')} />
                  </div>
                ))}
              </div>
            ) : (
              <ManagementState
                bordered={false}
                compact
                title="暂无 Agent 运行"
                description="对话诊断、巡检复盘或个人搭档触发运行后会在此展示。"
              />
            )}
          </Card>

          <Card
            variant="borderless"
            className="soha-ai-overview-panel"
            title="待办审批与知识库态势"
            extra={
              canApprovals ? (
                <Link to="/ai-gateway/governance?tab=approvals">进入审批台</Link>
              ) : null
            }
          >
            {approvals.length > 0 || bases.length > 0 ? (
              <div className="soha-ai-overview-list">
                {approvals.slice(0, 2).map((approval) => (
                  <div className="soha-ai-overview-row" key={`apr-${approval.id}`}>
                    <div className="soha-ai-overview-row__main">
                      <strong>{approval.toolName || approval.id}</strong>
                      <span>{approval.summary || `审批请求 · ${approval.id}`}</span>
                    </div>
                    <StatusTag value={String(approval.status || 'pending')} />
                  </div>
                ))}
                {bases.slice(0, 2).map((base) => (
                  <div className="soha-ai-overview-row" key={`kb-${base.id}`}>
                    <div className="soha-ai-overview-row__main">
                      <strong>{base.name || base.id}</strong>
                      <span>
                        {typeof base.documentCount === 'number'
                          ? `${base.documentCount} 篇索引文档`
                          : `知识库 · ${base.id}`}
                      </span>
                    </div>
                    <StatusTag value={String(base.status || 'ready')} />
                  </div>
                ))}
              </div>
            ) : (
              <ManagementState
                bordered={false}
                compact
                title="零待办门禁异常"
                description="当前无积压审批单或异常知识库。"
              />
            )}
          </Card>
        </div>
      </div>
    </div>
  )
}
