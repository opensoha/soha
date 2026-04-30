import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { Empty, Spin } from 'antd'
import { usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { canAccessRoute, findFirstAccessiblePath, getRouteMeta } from '@/routes/meta'
import { useAuthStore } from '@/stores/auth-store'

export function AuthGuard() {
  const location = useLocation()
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated())
  const permissionSnapshotQuery = usePermissionSnapshot()

  if (!isAuthenticated) {
    return <Navigate to="/login" state={{ from: location }} replace />
  }

  if (permissionSnapshotQuery.isLoading) {
    return <div className="flex items-center justify-center h-screen"><Spin size="large" /></div>
  }

  const snapshot = permissionSnapshotQuery.data?.data
  const currentRoute = getRouteMeta(location.pathname)
  if (!canAccessRoute(currentRoute, snapshot)) {
    const fallbackPath = findFirstAccessiblePath(snapshot)
    if (fallbackPath && fallbackPath !== location.pathname) {
      return <Navigate to={fallbackPath} replace />
    }
    return (
      <div className="flex items-center justify-center h-screen">
        <Empty description="当前账号没有可访问的页面权限" />
      </div>
    )
  }

  return <Outlet />
}
