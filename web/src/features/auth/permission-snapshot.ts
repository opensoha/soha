import { useQuery } from '@tanstack/react-query'
import type { QueryClient } from '@tanstack/react-query'
import { api } from '@/services/api-client'
import { useAuthStore } from '@/stores/auth-store'
import type { ApiResponse, PermissionSnapshot } from '@/types'
import { permissionSnapshotStaleTime, readPermissionSnapshot } from './permission-snapshot-request'

export const permissionSnapshotQueryKey = ['access/permission-snapshot'] as const

export function usePermissionSnapshot() {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated())

  return useQuery({
    queryKey: permissionSnapshotQueryKey,
    queryFn: () =>
      readPermissionSnapshot(() =>
        api.get<ApiResponse<PermissionSnapshot>>('/access/permission-snapshot'),
      ),
    enabled: isAuthenticated,
    staleTime: permissionSnapshotStaleTime,
  })
}

export function hasPermission(snapshot: PermissionSnapshot | undefined, permissionKey?: string) {
  if (!permissionKey) {
    return true
  }
  return snapshot?.permissionKeys.includes(permissionKey) ?? false
}

export function hasVisibleMenu(snapshot: PermissionSnapshot | undefined, menuId?: string) {
  if (!menuId) {
    return true
  }
  return snapshot?.visibleMenuIds.includes(menuId) ?? false
}

export function hasAllowedAction(allowedActions: string[] | undefined, action: string) {
  return allowedActions?.includes(action) ?? false
}

export function invalidateAuthz(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: permissionSnapshotQueryKey })
}
