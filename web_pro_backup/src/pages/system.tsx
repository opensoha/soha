import { useMemo } from 'react'
import { useLocation } from '@umijs/max'
import { useQuery } from '@tanstack/react-query'
import {
  AnnouncementsPage,
  AuditLogsPage,
  MenusPage,
  OnlineUsersPage,
  OperationLogsPage,
} from '@/features/system/system-pages'
import {
  announcementsQueryOptions,
  menusQueryOptions,
  onlineUsersQueryOptions,
} from '@/features/system/queries'
import { NonPlatformWorkspace } from '@/pages/nonplatform-workspace'

const systemItems = [
  { key: 'online-users', path: '/system/online-users', label: '在线用户', description: '查看在线会话并执行下线操作。', permissionKey: 'system.online-users.view' },
  { key: 'announcements', path: '/system/announcements', label: '公告', description: '维护控制台公告与生效窗口。', permissionKey: 'system.announcements.view' },
  { key: 'menus', path: '/system/menus', label: '菜单', description: '管理菜单结构、排序和权限绑定。', permissionKey: 'system.menus.view' },
  { key: 'audit', path: '/system/audit', label: '审计日志', description: '查看审计事件与主体行为。', permissionKey: 'system.audit.view' },
  { key: 'operations', path: '/system/operations', label: '操作日志', description: '查看系统操作记录与结果。', permissionKey: 'system.operations.view' },
] as const

function renderSystemContent(pathname: string) {
  if (pathname.startsWith('/system/announcements')) return <AnnouncementsPage embedded />
  if (pathname.startsWith('/system/menus')) return <MenusPage embedded />
  if (pathname.startsWith('/system/audit')) return <AuditLogsPage embedded />
  if (pathname.startsWith('/system/operations')) return <OperationLogsPage embedded />
  return <OnlineUsersPage embedded />
}

export default function SystemPage() {
  const { pathname } = useLocation()
  const sessionsQuery = useQuery(onlineUsersQueryOptions())
  const announcementsQuery = useQuery(announcementsQueryOptions())
  const menusQuery = useQuery(menusQueryOptions())

  const stats = useMemo(() => {
    const sessions = sessionsQuery.data?.data ?? []
    const announcements = announcementsQuery.data?.data ?? []
    const menus = menusQuery.data?.data ?? []
    return [
      { label: '在线会话', value: String(sessions.length), hint: `${sessions.filter((item) => item.status === 'active').length} 个会话处于活跃状态。` },
      { label: '公告数', value: String(announcements.length), hint: `${announcements.filter((item) => item.enabled).length} 条公告当前启用。` },
      { label: '菜单项', value: String(menus.length), hint: '包含前端可见菜单及其权限绑定。' },
      { label: '管理分区', value: String(systemItems.length), hint: '系统管理入口当前承载的工作区数量。' },
    ]
  }, [announcementsQuery.data?.data, menusQuery.data?.data, sessionsQuery.data?.data])

  return (
    <NonPlatformWorkspace
      rootPath="/system"
      title="系统管理工作区"
      description="在线用户、公告、菜单、审计与操作日志的统一入口。"
      workspaceLabel="System"
      workspaceSummary="系统管理入口已经从简单分流页提升为完整工作区，统一提供系统运维分区、基础态势摘要和当前路由上下文。现有 system feature 页保持原有行为和权限控制，入口层只负责更清晰的 Pro-native 组织。"
      items={[...systemItems]}
      stats={stats}
      highlights={[
        { title: '系统运维动作与回溯动作分区明确', description: '在线用户、公告、菜单属于直接管理面，审计和操作日志则承担复盘与追踪职责。' },
        { title: '菜单治理保留真实权限语义', description: '入口页只补齐工作台说明，不改变菜单派生与显式覆盖的现有后端契约。' },
      ]}
      actions={[
        { label: '先处理实时控制面', description: '在线用户、公告和菜单用于即时调整平台可见性与会话状态。' },
        { label: '再进入日志复盘', description: '审计日志和操作日志用于确认谁做了什么、是否命中权限与变更预期。' },
      ]}
    >
      {renderSystemContent(pathname)}
    </NonPlatformWorkspace>
  )
}
