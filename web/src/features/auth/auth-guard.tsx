import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { Spin } from 'antd'
import { ManagementState } from '@/components/management-list'
import { permissionSnapshotQueryKey, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { canAccessRoute, findFirstAccessiblePath, findPreferredWorkspace, getRouteMeta } from '@/routes/meta'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { useQueryClient } from '@tanstack/react-query'

const EMPTY_ROLES: string[] = []

export function AuthGuard() {
  const location = useLocation()
  const queryClient = useQueryClient()
  const isAuthenticated = useAuthStore((s) => Boolean(s.accessToken))
  const roles = useAuthStore((s) => s.user?.roles ?? EMPTY_ROLES)
  const currentWorkspace = usePreferencesStore((state) => state.currentWorkspace)
  const permissionSnapshotQuery = usePermissionSnapshot()

  if (!isAuthenticated) {
    return <Navigate to="/login" state={{ from: location }} replace />
  }

  if (permissionSnapshotQuery.isLoading) {
    return <div className="flex items-center justify-center h-screen"><Spin size="large" /></div>
  }

  const snapshot =
    permissionSnapshotQuery.data?.data
    ?? queryClient.getQueryData<{ data?: import('@/types').PermissionSnapshot }>(permissionSnapshotQueryKey)?.data
  const currentRoute = getRouteMeta(location.pathname)
  if (!canAccessRoute(currentRoute, snapshot)) {
    const preferredWorkspace = findPreferredWorkspace(snapshot, currentWorkspace, roles)
    const fallbackPath = findFirstAccessiblePath(snapshot, preferredWorkspace)
    if (fallbackPath && fallbackPath !== location.pathname) {
      return <Navigate to={fallbackPath} replace />
    }
    return (
      <div className="flex items-center justify-center h-screen">
        <ManagementState className="max-w-[520px]" kind="no-permission" description="当前账号没有可访问的页面权限" />
      </div>
    )
  }

  return <Outlet />
}
