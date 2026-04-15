import { useQuery } from '@tanstack/react-query'
import { api } from '@/services/api-client'
import { useAuthStore } from '@/stores/auth-store'
import type { ApiResponse, PermissionSnapshot } from '@/types'

export function usePermissionSnapshot() {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated())
  return useQuery({
    queryKey: ['access/permission-snapshot'],
    queryFn: () => api.get<ApiResponse<PermissionSnapshot>>('/access/permission-snapshot'),
    enabled: isAuthenticated,
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
