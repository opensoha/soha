import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { restoreAuthSession } from '@/features/auth/auth-api'
import {
  permissionSnapshotQueryKey,
  usePermissionSnapshot,
} from '@/features/auth/permission-snapshot'
import {
  canAccessRoute,
  findFirstAccessiblePath,
  findPreferredWorkspace,
  getRouteMeta,
} from '@/routes/meta'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { queryAccessDenied } from '@/utils/query-feedback'
import { useQueryClient } from '@tanstack/react-query'
import { Spin } from 'antd'
import { useEffect, useState } from 'react'
import { Navigate, Outlet, useLocation } from 'react-router-dom'

const EMPTY_ROLES: string[] = []
const AUTH_RESTORE_RETRY_MS = 2_000

function AuthRestoringState({ unavailable }: { unavailable: boolean }) {
  return (
    <div className="flex items-center justify-center h-screen">
      <ManagementState
        bordered={false}
        kind="loading"
        title="正在恢复登录状态"
        description={unavailable ? '后端服务暂时不可用，正在自动重试。' : '正在恢复当前登录会话。'}
      />
    </div>
  )
}

export function AuthGuard() {
  const location = useLocation()
  const queryClient = useQueryClient()
  const accessToken = useAuthStore((s) => s.accessToken)
  const isAuthenticated = Boolean(accessToken)
  const roles = useAuthStore((s) => s.user?.roles ?? EMPTY_ROLES)
  const currentWorkspace = usePreferencesStore((state) => state.currentWorkspace)
  const [isRestoringAuth, setIsRestoringAuth] = useState(!isAuthenticated)
  const [isRestoreUnavailable, setIsRestoreUnavailable] = useState(false)
  const permissionSnapshotQuery = usePermissionSnapshot()

  useEffect(() => {
    if (isAuthenticated) {
      setIsRestoringAuth(false)
      setIsRestoreUnavailable(false)
      return undefined
    }

    let cancelled = false
    let retryTimer: number | undefined

    const restore = async () => {
      setIsRestoringAuth(true)
      const result = await restoreAuthSession()
      if (cancelled) {
        return
      }

      if (result === 'authenticated') {
        setIsRestoreUnavailable(false)
        return
      }

      if (result === 'unavailable') {
        setIsRestoreUnavailable(true)
        retryTimer = window.setTimeout(restore, AUTH_RESTORE_RETRY_MS)
        return
      }

      setIsRestoreUnavailable(false)
      setIsRestoringAuth(false)
    }

    setIsRestoringAuth(true)
    void restore()

    return () => {
      cancelled = true
      if (retryTimer !== undefined) {
        window.clearTimeout(retryTimer)
      }
    }
  }, [isAuthenticated])

  if (!isAuthenticated) {
    if (isRestoringAuth) {
      return <AuthRestoringState unavailable={isRestoreUnavailable} />
    }
    return <Navigate to="/login" state={{ from: location }} replace />
  }

  if (permissionSnapshotQuery.isLoading) {
    return (
      <div className="flex items-center justify-center h-screen">
        <Spin size="large" />
      </div>
    )
  }

  if (queryAccessDenied(permissionSnapshotQuery)) {
    return (
      <div className="soha-page">
        <ReadFeedback query={permissionSnapshotQuery} label="权限" />
      </div>
    )
  }

  const snapshot =
    permissionSnapshotQuery.data?.data ??
    queryClient.getQueryData<{ data?: import('@/types').PermissionSnapshot }>(
      permissionSnapshotQueryKey,
    )?.data
  const currentRoute = getRouteMeta(location.pathname)
  if (!canAccessRoute(currentRoute, snapshot)) {
    const preferredWorkspace = findPreferredWorkspace(snapshot, currentWorkspace, roles)
    const fallbackPath = findFirstAccessiblePath(snapshot, preferredWorkspace)
    if (fallbackPath && fallbackPath !== location.pathname) {
      return <Navigate to={fallbackPath} replace />
    }
    return (
      <div className="flex items-center justify-center h-screen">
        <ManagementState
          className="max-w-[520px]"
          kind="no-permission"
          description="当前账号没有可访问的页面权限"
        />
      </div>
    )
  }

  return <Outlet />
}
