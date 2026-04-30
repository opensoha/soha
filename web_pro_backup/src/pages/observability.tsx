import { useMemo } from 'react'
import { useLocation } from '@umijs/max'
import { useQuery } from '@tanstack/react-query'
import {
  AlertsPage,
  EventsPage,
  MonitoringPage,
  NotificationsPage,
  OnCallPage,
} from '@/features/observability/monitoring-pages'
import {
  alertsQueryOptions,
  eventsQueryOptions,
  monitoringSummaryQueryOptions,
} from '@/features/observability/queries'
import { NonPlatformWorkspace } from '@/pages/nonplatform-workspace'

const observabilityItems = [
  { key: 'monitoring', path: '/observability/monitoring', label: '中心概览', description: '查看监控摘要、告警压力和通知覆盖度。', permissionKey: 'observe.monitoring.view' },
  { key: 'alerts', path: '/observability/alerts', label: '活跃告警', description: '处理当前告警、确认状态和来源。', permissionKey: 'observe.alerts.view' },
  { key: 'notifications', path: '/observability/notifications', label: '通知策略', description: '管理通知渠道和路由策略。', permissionKey: 'observe.notifications.view' },
  { key: 'oncall', path: '/observability/oncall', label: '值班协同', description: '查看值班轮换与升级协作。', permissionKey: 'observe.oncall.view' },
  { key: 'events', path: '/observability/events', label: '事件流', description: '关联系统事件时间线和上下文。', permissionKey: 'observe.events.view' },
] as const

function renderObservabilityContent(pathname: string) {
  if (pathname.startsWith('/observability/alerts')) return <AlertsPage embedded />
  if (pathname.startsWith('/observability/notifications')) return <NotificationsPage embedded />
  if (pathname.startsWith('/observability/oncall')) return <OnCallPage embedded />
  if (pathname.startsWith('/observability/events')) return <EventsPage embedded />
  return <MonitoringPage embedded />
}

export default function ObservabilityPage() {
  const { pathname } = useLocation()
  const summaryQuery = useQuery(monitoringSummaryQueryOptions())
  const alertsQuery = useQuery(alertsQueryOptions())
  const eventsQuery = useQuery(eventsQueryOptions())

  const stats = useMemo(() => {
    const summary = summaryQuery.data?.data
    const alerts = alertsQuery.data?.data ?? []
    const events = eventsQuery.data?.data ?? []
    return [
      { label: '活跃告警', value: String(summary?.firingCount ?? 0), hint: `${summary?.criticalCount ?? 0} 条为严重级别。` },
      { label: '告警总量', value: String(summary?.totalCount ?? alerts.length), hint: '用于评估当前告警面板处理压力。' },
      { label: '通知渠道', value: String(summary?.channelCount ?? 0), hint: '渠道数量反映通知覆盖基线。' },
      { label: '事件流', value: String(events.length), hint: '最近可见事件条目总数。' },
    ]
  }, [alertsQuery.data?.data, eventsQuery.data?.data, summaryQuery.data?.data])

  return (
    <NonPlatformWorkspace
      rootPath="/observability"
      title="告警与观测中心"
      description="监控摘要、活跃告警、通知协同和值班工作区。"
      workspaceLabel="Observability"
      workspaceSummary="该入口现在作为观测域的统一工作台，直接承载中心概览与分区跳转。底层页面逻辑仍复用现有 monitoring、alerts、notifications、oncall 和 events 模块，以保持既有行为和权限键不变。"
      items={[...observabilityItems]}
      stats={stats}
      highlights={[
        { title: '观测中心先给出处置上下文', description: '入口页先汇总告警压力、通知覆盖和事件流规模，再进入具体分区，避免每个分区都重复解释当前态势。' },
        { title: '预留分区仍有清晰定位', description: '值班协同继续作为占位能力存在，但现在由工作区统一说明它在告警响应链路中的位置。' },
      ]}
      actions={[
        { label: '先看告警压力', description: '从中心概览或活跃告警判断当前是否需要先做确认、分派或降噪。' },
        { label: '再核对通知与事件', description: '通知策略和事件流分区用于确认告警是否被正确路由、是否有系统级背景事件支撑。' },
      ]}
    >
      {renderObservabilityContent(pathname)}
    </NonPlatformWorkspace>
  )
}
