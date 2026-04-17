import { useQuery } from '@tanstack/react-query'
import { api } from '@/services/api-client'
import { useAuthStore } from '@/stores/auth-store'
import type { ApiResponse, PermissionSnapshot } from '@/types'
import { routeMeta } from '@/routes/meta'

const allPermissionKeys = [
  'overview.view',
  'platform.nodes.view',
  'platform.namespaces.view',
  'platform.workloads.view',
  'platform.configuration.view',
  'platform.network.view',
  'platform.storage.view',
  'platform.extensions.view',
  'platform.helm.view',
  'platform.clusters.view',
  'platform.deployment.restart',
  'platform.deployment.scale',
  'platform.deployment.rollback',
  'delivery.applications.view',
  'delivery.application.create',
  'delivery.application.update',
  'delivery.application.delete',
  'delivery.business-lines.view',
  'delivery.business-lines.manage',
  'delivery.environments.view',
  'delivery.environments.manage',
  'delivery.application-environments.view',
  'delivery.application-environments.manage',
  'delivery.workflow-templates.view',
  'delivery.workflow-templates.manage',
  'delivery.release-board.view',
  'delivery.workflows.view',
  'delivery.workflows.trigger',
  'delivery.releases.view',
  'delivery.releases.trigger',
  'delivery.registries.view',
  'delivery.registries.manage',
  'observe.monitoring.view',
  'observe.alerts.view',
  'observe.alerts.ack',
  'observe.alerts.assign',
  'observe.notifications.view',
  'observe.notifications.manage',
  'observe.oncall.view',
  'observe.events.view',
  'observe.ai.view',
  'observe.ai.chat',
  'observe.ai.root-cause.run',
  'observe.ai.inspection.manage',
  'observe.ai.inspection.run',
  'access.users.view',
  'access.roles.view',
  'access.groups.view',
  'access.policies.view',
  'system.online-users.view',
  'system.online-users.manage',
  'system.announcements.view',
  'system.announcements.manage',
  'system.menus.view',
  'system.menus.manage',
  'system.audit.view',
  'system.operations.view',
  'settings.identity.view',
  'settings.identity.manage',
  'settings.monitoring.view',
  'settings.monitoring.manage',
  'settings.ai.view',
  'settings.ai.manage',
] as const

const rolePermissionMap: Record<string, string[]> = {
  admin: [...allPermissionKeys],
  ops: [
    'overview.view',
    'platform.nodes.view',
    'platform.namespaces.view',
    'platform.workloads.view',
    'platform.configuration.view',
    'platform.network.view',
    'platform.storage.view',
    'platform.extensions.view',
    'platform.helm.view',
    'platform.clusters.view',
    'platform.deployment.restart',
    'platform.deployment.scale',
    'platform.deployment.rollback',
    'delivery.applications.view',
    'delivery.application.create',
    'delivery.application.update',
    'delivery.business-lines.view',
    'delivery.business-lines.manage',
    'delivery.environments.view',
    'delivery.environments.manage',
    'delivery.application-environments.view',
    'delivery.application-environments.manage',
    'delivery.workflow-templates.view',
    'delivery.workflow-templates.manage',
    'delivery.release-board.view',
    'delivery.workflows.view',
    'delivery.workflows.trigger',
    'delivery.releases.view',
    'delivery.releases.trigger',
    'delivery.registries.view',
    'delivery.registries.manage',
    'observe.monitoring.view',
    'observe.alerts.view',
    'observe.alerts.ack',
    'observe.alerts.assign',
    'observe.notifications.view',
    'observe.notifications.manage',
    'observe.oncall.view',
    'observe.events.view',
    'observe.ai.view',
    'observe.ai.chat',
    'observe.ai.root-cause.run',
    'observe.ai.inspection.manage',
    'observe.ai.inspection.run',
    'system.online-users.view',
    'system.online-users.manage',
    'system.announcements.view',
    'system.announcements.manage',
    'system.menus.view',
    'system.menus.manage',
    'system.audit.view',
    'system.operations.view',
    'settings.identity.view',
    'settings.ai.view',
    'settings.ai.manage',
  ],
  developer: [
    'overview.view',
    'platform.nodes.view',
    'platform.namespaces.view',
    'platform.workloads.view',
    'platform.configuration.view',
    'platform.network.view',
    'platform.storage.view',
    'platform.extensions.view',
    'platform.helm.view',
    'platform.deployment.restart',
    'platform.deployment.scale',
    'observe.monitoring.view',
    'observe.alerts.view',
    'observe.alerts.ack',
    'observe.events.view',
    'observe.ai.view',
    'observe.ai.chat',
    'observe.ai.root-cause.run',
    'observe.ai.inspection.run',
    'delivery.applications.view',
    'delivery.release-board.view',
    'delivery.workflows.view',
    'delivery.workflows.trigger',
    'delivery.releases.view',
    'delivery.releases.trigger',
  ],
  readonly: [
    'overview.view',
    'platform.nodes.view',
    'platform.namespaces.view',
    'platform.workloads.view',
    'platform.configuration.view',
    'platform.network.view',
    'platform.storage.view',
    'platform.extensions.view',
    'platform.helm.view',
    'platform.clusters.view',
    'delivery.applications.view',
    'delivery.release-board.view',
    'delivery.workflows.view',
    'delivery.releases.view',
    'observe.monitoring.view',
    'observe.alerts.view',
    'observe.events.view',
    'observe.ai.view',
  ],
  auditor: [
    'overview.view',
    'observe.monitoring.view',
    'observe.alerts.view',
    'observe.notifications.view',
    'observe.events.view',
    'system.audit.view',
    'system.operations.view',
  ],
}

function buildFallbackPermissionSnapshot(roles: string[] | undefined): PermissionSnapshot {
  const permissionKeys = Array.from(new Set((roles ?? []).flatMap((role) => rolePermissionMap[role] ?? []))).sort()
  const visibleMenuMap = new Map<string, { id: string; parentId?: string; path: string }>()

  routeMeta.forEach((route) => {
    const allowed = !route.permissionKey || permissionKeys.includes(route.permissionKey)
    if (!allowed || !route.menuId) {
      return
    }
    if (!visibleMenuMap.has(route.menuId)) {
      visibleMenuMap.set(route.menuId, {
        id: route.menuId,
        parentId: route.parentId,
        path: route.redirectTo ?? route.path,
      })
    }
  })

  return {
    permissionKeys,
    visibleMenuIds: Array.from(visibleMenuMap.keys()),
    visibleMenus: Array.from(visibleMenuMap.values()),
  }
}

function mergePermissionSnapshots(primary: PermissionSnapshot, fallback: PermissionSnapshot): PermissionSnapshot {
  const permissionKeys = Array.from(new Set([...primary.permissionKeys, ...fallback.permissionKeys])).sort()
  const visibleMenus = [...primary.visibleMenus]
  const seenVisibleMenus = new Set(primary.visibleMenus.map((item) => item.id))

  fallback.visibleMenus.forEach((item) => {
    if (seenVisibleMenus.has(item.id)) {
      return
    }
    visibleMenus.push(item)
    seenVisibleMenus.add(item.id)
  })

  return {
    permissionKeys,
    visibleMenuIds: visibleMenus.map((item) => item.id),
    visibleMenus,
  }
}

export function usePermissionSnapshot() {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated())
  const roles = useAuthStore((state) => state.user?.roles)
  const fallbackSnapshot = buildFallbackPermissionSnapshot(roles)
  const query = useQuery({
    queryKey: ['access/permission-snapshot'],
    queryFn: () => api.get<ApiResponse<PermissionSnapshot>>('/access/permission-snapshot'),
    enabled: isAuthenticated,
  })
  return {
    ...query,
    data: query.data
      ? { data: mergePermissionSnapshots(query.data.data, fallbackSnapshot) }
      : { data: fallbackSnapshot },
  }
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
