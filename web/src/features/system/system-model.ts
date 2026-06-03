import dayjs from 'dayjs'
import { buildMenuSectionOptions, getMenuSectionOrder, normalizeMenuSection, resolveMenuSectionLabel } from '@/features/system/menu-schema'
import { getMenuWorkbenchId, getMenuWorkspace, resolveRouteMenuId, resolveRoutePermission, routeMeta, type WorkbenchId } from '@/routes/meta'
import type { WorkspaceType } from '@/types'

export function compactText(value?: string | null) {
  return typeof value === 'string' ? value.trim() : ''
}

export function isTodayDate(value?: string | null) {
  if (!value) return false
  const parsed = dayjs(value)
  return parsed.isValid() ? parsed.isSame(dayjs(), 'day') : false
}

export function buildAuditResourceLabel(kind?: string, name?: string) {
  const resourceKind = compactText(kind)
  const resourceName = compactText(name)
  return {
    primary: resourceName || resourceKind || '-',
    secondary: resourceName && resourceKind ? resourceKind : '',
  }
}

export function buildTargetScopeLabel(targetScope: Record<string, unknown>) {
  const module = compactText(String(targetScope.module || ''))
  const resourceKind = compactText(String(targetScope.resourceKind || ''))
  const resourceName = compactText(String(targetScope.resourceName || ''))
  const targetLabel = compactText(String(targetScope.targetLabel || ''))
  const clusterId = compactText(String(targetScope.clusterId || ''))
  const namespace = compactText(String(targetScope.namespace || ''))

  return {
    primary: targetLabel || resourceName || resourceKind || '-',
    secondary: [module, resourceKind, clusterId, namespace].filter(Boolean).join(' / '),
  }
}

export function prettifyAction(action: string) {
  const normalized = compactText(action)
  if (!normalized) return '-'
  return normalized.replace(/_/g, ' ')
}

export function prettifyOperationType(operationType: string) {
  const normalized = compactText(operationType)
  if (!normalized) return { primary: '-', secondary: '' }

  const operationMap: Record<string, string> = {
    'system.announcement.publish': '公告发布',
    'system.announcement.withdraw': '公告撤回',
    'system.announcement.create': '公告创建',
    'system.announcement.update': '公告更新',
    'system.announcement.delete': '公告删除',
    'system.menu.create': '菜单创建',
    'system.menu.update': '菜单更新',
    'system.menu.delete': '菜单删除',
    'system.session.revoke': '会话下线',
    'access.user.create': '用户创建',
    'access.user.update': '用户更新',
    'access.user.delete': '用户删除',
    'access.user.replace_roles': '用户角色绑定更新',
    'access.user.replace_teams': '组织绑定更新',
    'access.user.revoke_sessions': '用户会话下线',
    'access.role.create': '角色创建',
    'access.role.update': '角色更新',
    'access.role.delete': '角色删除',
    'access.team.create': '组织创建',
    'access.team.update': '组织更新',
    'access.team.delete': '组织删除',
    'access.policy.create': '策略创建',
    'access.policy.update': '策略更新',
    'access.policy.delete': '策略删除',
    'access.scope_grant.create': '范围授权创建',
    'access.scope_grant.update': '范围授权更新',
    'access.scope_grant.delete': '范围授权删除',
    'platform.deployment.restart': 'Deployment 重启',
    'platform.deployment.scale': 'Deployment 扩缩容',
    'platform.deployment.rollback': 'Deployment 回滚',
    'platform.cluster.register': '集群注册',
    'platform.cluster.update': '集群更新',
    'platform.cluster.delete': '集群删除',
    'platform.namespace.create': '命名空间创建',
    'platform.namespace.update': '命名空间更新',
    'platform.namespace.delete': '命名空间删除',
    'platform.node.update': '节点更新',
    'platform.node.delete': '节点删除',
    'platform.resource.create': '资源创建',
    'platform.resource.apply': '资源 YAML 应用',
    'platform.resource.delete': '资源删除',
    'platform.custom_resource.create': 'CRD 资源创建',
    'platform.custom_resource.apply': 'CRD 资源 YAML 应用',
    'platform.custom_resource.delete': 'CRD 资源删除',
  }

  return {
    primary: operationMap[normalized] || normalized.split('.').slice(-2).join(' / '),
    secondary: normalized,
  }
}

export function stringifyPayload(value: unknown) {
  if (!value || (typeof value === 'object' && Object.keys(value as Record<string, unknown>).length === 0)) {
    return '无'
  }
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export interface OnlineUser {
  id: string
  userId: string
  userName: string
  email: string
  providerType: string
  status: string
  loginTime: string
  lastSeenAt: string
  expiry: string
  source?: string
  sourceIp?: string
  userAgent?: string
}

export interface Announcement {
  id: string
  title: string
  summary: string
  content: string
  level: string
  status: string
  audience: string
  sticky: boolean
  startsAt?: string | null
  endsAt?: string | null
  publishedAt?: string | null
  createdBy?: string
  updatedBy?: string
  createdAt: string
  updatedAt: string
}

export function buildAnnouncementLifecycle(record: Announcement) {
  if (record.status === 'draft') return 'draft'
  if (record.startsAt && dayjs(record.startsAt).isAfter(dayjs())) return 'scheduled'
  if (record.endsAt && dayjs(record.endsAt).isBefore(dayjs())) return 'expired'
  return 'published'
}

export interface MenuItem {
  id: string
  parentId?: string
  labelZh: string
  labelEn: string
  path: string
  iconKey: string
  section: string
  sortOrder: number
  enabled: boolean
  roleIds?: string[]
  visibilityMode?: 'derived' | 'explicit'
  derivedPermissionKeys?: string[]
  children?: MenuItem[]
  depth?: number
  parentLabelZh?: string
  syntheticKind?: 'workbench' | 'section'
  syntheticWorkbenchKey?: MenuWorkbenchSurface
}

export interface AccessRoleOption {
  id: string
  name: string
}

export type MenuVisibilityMode = 'derived' | 'explicit' | 'unmapped'

export interface MenuVisibilitySummary {
  derivedPermissionKeys: string[]
  explicitRoleIds: string[]
  mode: MenuVisibilityMode
}

export type MenuWorkbenchSurface = WorkbenchId | 'system' | 'unmapped'

export interface MenuWorkbenchSummary {
  key: MenuWorkbenchSurface
  label: string
  pathPlacement: MenuWorkbenchSurface
  pathPlacementLabel: string
  parentPlacement: MenuWorkbenchSurface | null
  parentPlacementLabel: string | null
  workspace: WorkspaceType | null
}

export const MENU_WORKBENCH_ORDER: MenuWorkbenchSurface[] = ['platform', 'virtualization', 'docker', 'delivery', 'ai', 'aiGateway', 'monitoring', 'settings', 'system', 'unmapped']

export const MENU_WORKBENCH_LABELS: Record<MenuWorkbenchSurface, string> = {
  platform: 'k8s工作台',
  virtualization: '虚拟化管理工作台',
  docker: 'Docker 工作台',
  ai: 'AI工作台',
  aiGateway: 'AI Gateway',
  monitoring: '监控工作台',
  settings: '设置中心',
  delivery: '应用交付工作台',
  system: '系统管理',
  unmapped: '未映射',
}

export const MENU_UNGROUPED_FILTER = '__ungrouped__'

export function resolveMenuWorkbenchKey(item: Pick<MenuItem, 'id' | 'path'>): MenuWorkbenchSurface {
  const workbenchId = getMenuWorkbenchId(item)
  if (workbenchId) {
    return workbenchId
  }
  const workspace = getMenuWorkspace(item)
  if (workspace === 'system') {
    return 'system'
  }
  return 'unmapped'
}

export function summarizeMenuWorkbench(
  item: Pick<MenuItem, 'id' | 'path' | 'parentId'>,
  menuLookup: Map<string, MenuItem>,
): MenuWorkbenchSummary {
  const workspace = getMenuWorkspace(item)
  const pathPlacement = resolveMenuWorkbenchKey(item)
  const parentItem = item.parentId ? menuLookup.get(item.parentId) : undefined
  const parentPlacement = parentItem ? resolveMenuWorkbenchKey(parentItem) : null
  const key = parentPlacement ?? pathPlacement

  return {
    key,
    label: MENU_WORKBENCH_LABELS[key],
    pathPlacement,
    pathPlacementLabel: MENU_WORKBENCH_LABELS[pathPlacement],
    parentPlacement,
    parentPlacementLabel: parentPlacement ? MENU_WORKBENCH_LABELS[parentPlacement] : null,
    workspace,
  }
}

export function flattenMenuItems(items: MenuItem[], depth = 0, parent?: MenuItem): MenuItem[] {
  return items.flatMap((item) => {
    const { children, ...rest } = item
    if (item.syntheticKind) {
      return flattenMenuItems(children ?? [], depth, parent)
    }
    const nextItem: MenuItem = {
      ...rest,
      depth,
      parentLabelZh: parent?.labelZh,
    }
    return [nextItem, ...flattenMenuItems(children ?? [], depth + 1, item)]
  })
}

export function collectMenuDescendantIds(item: MenuItem): string[] {
  return (item.children ?? []).flatMap((child) => [child.id, ...collectMenuDescendantIds(child)])
}

export function countDirectMenuChildren(item: Pick<MenuItem, 'children'>) {
  return item.children?.length ?? 0
}

export function compareMenuItems(left: MenuItem, right: MenuItem) {
  const leftSection = normalizeMenuSection(left.section)
  const rightSection = normalizeMenuSection(right.section)
  const sectionCompare = getMenuSectionOrder(leftSection) - getMenuSectionOrder(rightSection)
  if (sectionCompare !== 0) return sectionCompare
  if (left.sortOrder !== right.sortOrder) return left.sortOrder - right.sortOrder
  return left.path.localeCompare(right.path)
}

export function buildWorkbenchMenuTree(items: MenuItem[]) {
  const menuLookup = new Map(flattenMenuItems(items).map((item) => [item.id, item]))
  const groupedByWorkbench = new Map<MenuWorkbenchSurface, MenuItem[]>()

  for (const item of items) {
    const summary = summarizeMenuWorkbench(item, menuLookup)
    const current = groupedByWorkbench.get(summary.key) ?? []
    current.push(item)
    groupedByWorkbench.set(summary.key, current)
  }

  return MENU_WORKBENCH_ORDER
    .map((workbenchKey): MenuItem | null => {
      const workbenchItems = groupedByWorkbench.get(workbenchKey)
      if (!workbenchItems?.length) {
        return null
      }

      const directItems: MenuItem[] = []
      const sectionGroups = new Map<string, MenuItem[]>()

      for (const item of workbenchItems) {
        const section = normalizeMenuSection(item.section)
        if (!section) {
          directItems.push(item)
          continue
        }
        const current = sectionGroups.get(section) ?? []
        current.push(item)
        sectionGroups.set(section, current)
      }

      const sectionNodes = Array.from(sectionGroups.entries())
        .sort(([left], [right]) => {
          const sectionCompare = getMenuSectionOrder(left) - getMenuSectionOrder(right)
          if (sectionCompare !== 0) return sectionCompare
          return resolveMenuSectionLabel(left).localeCompare(resolveMenuSectionLabel(right))
        })
        .map(([section, sectionItems]): MenuItem => ({
          id: `__section__${workbenchKey}__${section}`,
          labelZh: resolveMenuSectionLabel(section),
          labelEn: resolveMenuSectionLabel(section, 'en_US'),
          path: '',
          iconKey: '',
          section,
          sortOrder: 0,
          enabled: true,
          syntheticKind: 'section',
          syntheticWorkbenchKey: workbenchKey,
          children: [...sectionItems].sort(compareMenuItems),
        }))

      return {
        id: `__workbench__${workbenchKey}`,
        labelZh: MENU_WORKBENCH_LABELS[workbenchKey],
        labelEn: MENU_WORKBENCH_LABELS[workbenchKey],
        path: '',
        iconKey: '',
        section: '',
        sortOrder: MENU_WORKBENCH_ORDER.indexOf(workbenchKey),
        enabled: true,
        syntheticKind: 'workbench',
        syntheticWorkbenchKey: workbenchKey,
        children: [...directItems].sort(compareMenuItems).concat(sectionNodes),
      } satisfies MenuItem
    })
    .filter((item): item is MenuItem => Boolean(item))
}

export function filterMenuTree(
  items: MenuItem[],
  options: {
    topLevelOnly: boolean
    section: string
    workbench: string
    enabled: 'all' | 'enabled' | 'disabled'
    visibility: 'all' | 'derived' | 'explicit' | 'unmapped'
  },
) {
  const menuLookup = new Map(flattenMenuItems(items).map((item) => [item.id, item]))
  const matches = (item: MenuItem) => {
    const summary = summarizeMenuVisibility(item)
    const normalizedSection = normalizeMenuSection(item.section)
    const matchesSection = !options.section ||
      (options.section === MENU_UNGROUPED_FILTER ? normalizedSection === '' : normalizedSection === options.section)
    const matchesWorkbench = !options.workbench || summarizeMenuWorkbench(item, menuLookup).key === options.workbench
    const matchesEnabled = options.enabled === 'all'
      ? true
      : options.enabled === 'enabled'
        ? item.enabled
        : !item.enabled
    const matchesVisibility = options.visibility === 'all' ? true : summary.mode === options.visibility
    return matchesSection && matchesWorkbench && matchesEnabled && matchesVisibility
  }

  const visit = (item: MenuItem, depth = 0): MenuItem | null => {
    const children = (item.children ?? [])
      .map((child) => visit(child, depth + 1))
      .filter((child): child is MenuItem => Boolean(child))

    const includeSelf = matches(item)
    const includeChildren = children.length > 0
    if (!includeSelf && !includeChildren) {
      return null
    }

    return {
      ...item,
      depth,
      children: includeChildren ? children : undefined,
    }
  }

  return items
    .map((item) => visit(item, 0))
    .filter((item): item is MenuItem => Boolean(item))
}

export function findMenuItemByID(items: MenuItem[], id: string): MenuItem | null {
  for (const item of items) {
    if (item.id === id) {
      return item
    }
    const child = findMenuItemByID(item.children ?? [], id)
    if (child) {
      return child
    }
  }
  return null
}

export function normalizeMenuSubmitValues(values: Record<string, unknown>) {
  const normalizedParentId = typeof values.parentId === 'string' ? values.parentId.trim() : values.parentId
  const roleIds = Array.isArray(values.roleIds)
    ? values.roleIds.map((item) => String(item).trim()).filter(Boolean)
    : []
  const normalizedSection = Array.isArray(values.section)
    ? String(values.section[0] || '').trim()
    : typeof values.section === 'string'
      ? values.section.trim()
      : ''
  const visibilityMode = values.visibilityMode === 'explicit' ? 'explicit' : 'derived'

  return {
    id: typeof values.id === 'string' ? values.id.trim() : values.id,
    labelZh: typeof values.labelZh === 'string' ? values.labelZh.trim() : values.labelZh,
    labelEn: typeof values.labelEn === 'string' ? values.labelEn.trim() : values.labelEn,
    path: typeof values.path === 'string' ? values.path.trim() : values.path,
    iconKey: typeof values.iconKey === 'string' ? values.iconKey.trim() : values.iconKey,
    section: normalizeMenuSection(normalizedSection),
    sortOrder: values.sortOrder,
    enabled: values.enabled,
    parentId: normalizedParentId ? normalizedParentId : null,
    roleIds: visibilityMode === 'explicit' ? roleIds : [],
  }
}

export function buildMenuFormValues(editing: MenuItem | null) {
  if (editing) {
    return {
      id: editing.id,
      labelZh: editing.labelZh,
      labelEn: editing.labelEn,
      parentId: editing.parentId || '',
      path: editing.path,
      iconKey: editing.iconKey,
      section: editing.section ? [normalizeMenuSection(editing.section)] : [],
      sortOrder: editing.sortOrder,
      enabled: editing.enabled,
      roleIds: editing.roleIds ?? [],
      visibilityMode: summarizeMenuVisibility(editing).mode === 'explicit' ? 'explicit' : 'derived',
    }
  }

  return {
    enabled: true,
    sortOrder: 0,
    section: [],
    parentId: '',
    roleIds: [],
    visibilityMode: 'derived',
  }
}

export function compactUniqueStrings(values: string[]) {
  return Array.from(new Set(values.map((item) => item.trim()).filter(Boolean))).sort((left, right) => left.localeCompare(right))
}

export function getMenuDerivedPermissionKeys(item: Pick<MenuItem, 'id' | 'path' | 'derivedPermissionKeys'>) {
  if (item.derivedPermissionKeys?.length) {
    return compactUniqueStrings(item.derivedPermissionKeys)
  }
  return compactUniqueStrings(
    routeMeta
      .filter((route) => {
        const routeMenuId = resolveRouteMenuId(route)
        return routeMenuId === item.id || route.path === item.path
      })
      .flatMap((route) => route.permissionKeysAny?.length ? route.permissionKeysAny : [resolveRoutePermission(route)])
      .filter((value): value is string => Boolean(value)),
  )
}

export function summarizeMenuVisibility(item: Pick<MenuItem, 'id' | 'path' | 'roleIds' | 'visibilityMode' | 'derivedPermissionKeys'>): MenuVisibilitySummary {
  const derivedPermissionKeys = getMenuDerivedPermissionKeys(item)
  const explicitRoleIds = compactUniqueStrings(item.roleIds ?? [])

  if (item.visibilityMode === 'explicit') {
    return { derivedPermissionKeys, explicitRoleIds, mode: 'explicit' }
  }
  if (item.visibilityMode === 'derived') {
    return { derivedPermissionKeys, explicitRoleIds, mode: derivedPermissionKeys.length > 0 ? 'derived' : 'unmapped' }
  }
  if (explicitRoleIds.length > 0) {
    return { derivedPermissionKeys, explicitRoleIds, mode: 'explicit' }
  }
  if (derivedPermissionKeys.length > 0) {
    return { derivedPermissionKeys, explicitRoleIds, mode: 'derived' }
  }
  return { derivedPermissionKeys, explicitRoleIds, mode: 'unmapped' }
}

export function getMenuVisibilityModeOptions(summary: MenuVisibilitySummary) {
  return [
    {
      value: 'derived',
      label: '自动派生',
      disabled: summary.derivedPermissionKeys.length === 0,
    },
    {
      value: 'explicit',
      label: '显式覆盖',
    },
  ]
}

export function buildMenuSectionFilterOptions(items: MenuItem[]) {
  return [
    { value: MENU_UNGROUPED_FILTER, label: '未分组' },
    ...buildMenuSectionOptions(items.map((item) => item.section)),
  ]
}

export interface AuditLog {
  id: string
  createdAt: string
  actorId: string
  actorName: string
  action: string
  resourceKind: string
  resourceName: string
  result: string
  summary: string
  requestPath?: string
  requestMethod?: string
  requestId?: string
  sourceIp?: string
  roles?: string[]
  teams?: string[]
  metadata?: Record<string, unknown>
}

export interface OperationLog {
  id: string
  createdAt: string
  actorId: string
  actorName: string
  operationType: string
  targetScope: Record<string, unknown>
  result: string
  summary: string
  requestPath?: string
  requestMethod?: string
  requestId?: string
  sourceIp?: string
  metadata?: Record<string, unknown>
}
