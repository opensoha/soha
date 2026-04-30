import { useMemo } from 'react'
import { useLocation } from '@umijs/max'
import { useQuery } from '@tanstack/react-query'
import {
  AccessCenterPage,
  AccessPoliciesPage,
  AccessRolesPage,
  AccessTeamsPage,
  AccessUsersPage,
} from '@/features/access/access-pages'
import { AccessScopeGrantsPage } from '@/features/access/scope-grants-page'
import {
  accessRolesQueryOptions,
  accessTeamsQueryOptions,
  accessUsersQueryOptions,
} from '@/features/access/queries'
import { usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { NonPlatformWorkspace } from '@/pages/nonplatform-workspace'

const accessItems = [
  { key: 'users', path: '/access/users', label: '用户', description: '管理用户账号、角色绑定与用户组绑定。', permissionKey: 'access.users.view' },
  { key: 'roles', path: '/access/roles', label: '角色', description: '维护角色及 permissionKeys 绑定。', permissionKey: 'access.roles.view' },
  { key: 'teams', path: '/access/teams', label: '用户组', description: '维护用户组与成员范围。', permissionKey: 'access.groups.view' },
  { key: 'policies', path: '/access/policies', label: '策略', description: '查看和管理授权策略。', permissionKey: 'access.policies.view' },
  { key: 'scope-grants', path: '/access/scope-grants', label: '授权范围', description: '维护业务线、环境和应用级别的授权。', permissionKey: 'access.scope-grants.view' },
] as const

function renderAccessContent(pathname: string) {
  if (pathname.startsWith('/access/users')) return <AccessUsersPage />
  if (pathname.startsWith('/access/roles')) return <AccessRolesPage />
  if (pathname.startsWith('/access/teams')) return <AccessTeamsPage />
  if (pathname.startsWith('/access/policies')) return <AccessPoliciesPage />
  if (pathname.startsWith('/access/scope-grants')) return <AccessScopeGrantsPage />
  return <AccessCenterPage />
}

export default function AccessPage() {
  const { pathname } = useLocation()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data

  const usersQuery = useQuery(accessUsersQueryOptions())
  const rolesQuery = useQuery(accessRolesQueryOptions())
  const groupsQuery = useQuery(accessTeamsQueryOptions())

  const stats = useMemo(() => [
    { label: '可见分区', value: String(accessItems.filter((item) => !item.permissionKey || snapshot?.permissionKeys.includes(item.permissionKey)).length), hint: '当前账号在访问控制下可打开的入口数量。' },
    { label: '用户', value: String(usersQuery.data?.data?.length ?? 0), hint: '后端真实用户列表条目数。' },
    { label: '角色', value: String(rolesQuery.data?.data?.length ?? 0), hint: '用于快照授权的角色总数。' },
    { label: '用户组', value: String(groupsQuery.data?.data?.length ?? 0), hint: '参与策略匹配的用户组数量。' },
  ], [groupsQuery.data?.data?.length, rolesQuery.data?.data?.length, snapshot?.permissionKeys, usersQuery.data?.data?.length])

  return (
    <NonPlatformWorkspace
      rootPath="/access"
      title="访问控制工作区"
      description="用户、角色、用户组、策略与范围授权的统一 Pro 入口。"
      workspaceLabel="Access"
      workspaceSummary="访问控制入口页现在直接提供分区导航和权限面板摘要，保留现有后端 schema、权限键和 scope grant 语义不变。用户仍通过同一批 feature 页面执行 CRUD，只是入口组织从单纯转发改成了工作区式布局。"
      items={[...accessItems]}
      stats={stats}
    >
      {renderAccessContent(pathname)}
    </NonPlatformWorkspace>
  )
}
