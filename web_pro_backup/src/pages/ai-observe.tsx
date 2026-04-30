import { useMemo } from 'react'
import { useLocation } from '@umijs/max'
import { useQuery } from '@tanstack/react-query'
import {
  InspectionCenterPage,
  PerformanceAnalysisPage,
  RootCauseAnalysisPage,
} from '@/features/copilot/analysis-pages'
import { ChatPage } from '@/features/copilot/chat-page'
import {
  copilotInspectionTasksQueryOptions,
  copilotRootCauseRunsQueryOptions,
  copilotSessionsQueryOptions,
} from '@/features/copilot/queries'
import { NonPlatformWorkspace } from '@/pages/nonplatform-workspace'

const aiObserveItems = [
  { key: 'root-cause', path: '/ai-observe/root-cause', label: '根因分析', description: '基于告警、日志和集群信号输出根因假设。', permissionKey: 'observe.ai.view' },
  { key: 'performance', path: '/ai-observe/performance', label: '性能分析', description: '聚合性能信号与 AI 建议。', permissionKey: 'observe.ai.view' },
  { key: 'chat', path: '/ai-observe/chat', label: 'AI Chat', description: '围绕平台上下文发起分析会话。', permissionKey: 'observe.ai.chat' },
  { key: 'inspection', path: '/ai-observe/inspection', label: '智能巡检', description: '维护巡检任务并查看执行结果。', permissionKey: 'observe.ai.view' },
] as const

function renderAIObserveContent(pathname: string) {
  if (pathname.startsWith('/ai-observe/performance')) return <PerformanceAnalysisPage embedded />
  if (pathname.startsWith('/ai-observe/chat')) return <ChatPage />
  if (pathname.startsWith('/ai-observe/inspection')) return <InspectionCenterPage embedded />
  return <RootCauseAnalysisPage embedded />
}

export default function AIObservePage() {
  const { pathname } = useLocation()
  const runsQuery = useQuery(copilotRootCauseRunsQueryOptions())
  const inspectionTasksQuery = useQuery(copilotInspectionTasksQueryOptions())
  const sessionsQuery = useQuery(copilotSessionsQueryOptions())

  const stats = useMemo(() => {
    const runs = runsQuery.data?.data ?? []
    const tasks = inspectionTasksQuery.data?.data ?? []
    const sessions = sessionsQuery.data?.data ?? []
    return [
      { label: '分析运行', value: String(runs.length), hint: `${runs.filter((item) => item.status === 'running').length} 个分析仍在执行。` },
      { label: '高优先级线索', value: String(runs.filter((item) => item.severity === 'critical' || item.severity === 'warning').length), hint: '当前分析记录中的高风险结果数量。' },
      { label: '巡检任务', value: String(tasks.length), hint: `${tasks.filter((item) => item.enabled).length} 个任务已启用。` },
      { label: '会话数', value: String(sessions.length), hint: '可回看的 AI Chat 会话总数。' },
    ]
  }, [inspectionTasksQuery.data?.data, runsQuery.data?.data, sessionsQuery.data?.data])

  return (
    <NonPlatformWorkspace
      rootPath="/ai-observe"
      title="AI 观测分析中心"
      description="根因分析、性能分析、AI Chat 与巡检任务的统一入口。"
      workspaceLabel="AI Observe"
      workspaceSummary="AI 观测分析入口现在承担独立的工作区职责，直接暴露分析面板导航和当前 AI 运行态势。这样保留原有 copilot API 行为的同时，把入口页提升为真正的 Pro-native section workspace。"
      items={[...aiObserveItems]}
      stats={stats}
      highlights={[
        { title: 'AI 分析链路按场景拆开', description: '根因分析、性能分析、AI Chat 和巡检任务共用一个入口，但每个分区都有单独的操作意图，不再只是并列标签页。' },
        { title: '运行态势前置可见', description: '入口页先暴露分析运行数、高优先级线索、巡检任务和会话规模，减少进入子页后才发现当前负载的情况。' },
      ]}
      actions={[
        { label: '调查单次异常', description: '优先进入根因分析或性能分析，围绕活跃告警、集群状态和 AI 建议缩小排查面。' },
        { label: '建立持续巡检', description: '当问题模式稳定后，再切到智能巡检，把检查项和执行节奏固化为任务。' },
      ]}
    >
      {renderAIObserveContent(pathname)}
    </NonPlatformWorkspace>
  )
}
