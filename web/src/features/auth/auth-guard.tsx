import { useEffect, useState } from 'react'
import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { Spin } from 'antd'
import { ManagementState } from '@/components/management-list'
import { refreshAuthSession } from '@/features/auth/auth-api'
import { permissionSnapshotQueryKey, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { canAccessRoute, findFirstAccessiblePath, findPreferredWorkspace, getRouteMeta } from '@/routes/meta'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { useQueryClient } from '@tanstack/react-query'

const EMPTY_ROLES: string[] = []

export function AuthGuard() {
  const location = useLocation()
  const queryClient = useQueryClient()
  const accessToken = useAuthStore((s) => s.accessToken)
  const isAuthenticated = Boolean(accessToken)
  const roles = useAuthStore((s) => s.user?.roles ?? EMPTY_ROLES)
  const currentWorkspace = usePreferencesStore((state) => state.currentWorkspace)
  const [isRestoringAuth, setIsRestoringAuth] = useState(!isAuthenticated)
  const permissionSnapshotQuery = usePermissionSnapshot()

  useEffect(() => {
    if (isAuthenticated) {
      setIsRestoringAuth(false)
      return undefined
    }

    let cancelled = false
    setIsRestoringAuth(true)
    void refreshAuthSession().finally(() => {
      if (!cancelled) {
        setIsRestoringAuth(false)
      }
    })

    return () => {
      cancelled = true
    }
  }, [isAuthenticated])

  if (!isAuthenticated) {
    if (isRestoringAuth) {
      return <div className="flex items-center justify-center h-screen"><Spin size="large" /></div>
    }
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
