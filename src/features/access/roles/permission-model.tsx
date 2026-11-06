import type { PermissionDefinition } from '@opensoha/contracts/gen/ts/sohaapi'
import type { DataNode } from 'antd/es/tree'
import type { LocaleCode } from '@/i18n'
import { resolveRoutePermission, routeMeta } from '@/routes/meta'
import { toStringArray } from '../shared/utils'

const ROLE_PERMISSION_WORKBENCH_LABELS: Record<string, string> = {
  platform: 'k8s工作台',
  delivery: '持续交付',
  monitoring: '可观测与值班',
  home: '应用门户',
  ai: 'AI 工作台',
  compute: '计算资源工作台',
  security: '身份与安全',
  settings: '设置中心',
  unknown: '其他权限',
}

const PERMISSION_DOMAIN_WORKBENCH: Record<string, string> = {
  ai: 'ai',
  access: 'settings',
  delivery: 'delivery',
  docker: 'compute',
  identity: 'security',
  observe: 'monitoring',
  overview: 'platform',
  platform: 'platform',
  plugin: 'settings',
  secret: 'settings',
  settings: 'settings',
  system: 'settings',
  virtualization: 'compute',
  workbench: 'unknown',
  workspace: 'unknown',
}

const ROUTE_PERMISSION_OWNERS: Record<string, string> = {
  'ai.data-sources.create': 'ai-workbench-data-sources',
  'ai.data-sources.update': 'ai-workbench-data-sources',
  'ai.data-sources.validate': 'ai-workbench-data-sources',
  'ai.data-sources.view': 'ai-workbench-data-sources',
  'ai.gateway.approvals.manage': 'ai-gateway-governance',
  'ai.gateway.clients.manage': 'ai-gateway-clients',
  'ai.gateway.grants.manage': 'ai-gateway-governance',
  'ai.gateway.invoke': 'ai-gateway-tokens',
  'ai.gateway.policies.manage': 'ai-gateway-governance',
  'ai.gateway.skills.create': 'ai-workbench-skills',
  'ai.gateway.skills.delete': 'ai-workbench-skills',
  'ai.gateway.skills.manage': 'ai-workbench-skills',
  'ai.gateway.skills.update': 'ai-workbench-skills',
  'ai.gateway.skills.view': 'ai-workbench-skills',
  'ai.gateway.tokens.manage': 'ai-gateway-tokens',
  'ai.gateway.view': 'ai-gateway-manifest',
  'observe.ai.chat': 'ai-workbench-chat',
  'observe.ai.inspection.cancel': 'ai-workbench-inspection',
  'observe.ai.inspection.create': 'ai-workbench-inspection',
  'observe.ai.inspection.delete': 'ai-workbench-inspection',
  'observe.ai.inspection.manage': 'ai-workbench-inspection',
  'observe.ai.inspection.run': 'ai-workbench-inspection',
  'observe.ai.inspection.update': 'ai-workbench-inspection',
  'observe.ai.inspection.validate': 'ai-workbench-inspection',
  'observe.ai.root-cause.run': 'ai-workbench-chat',
  'settings.ai.manage': 'ai-workbench-model-settings',
  'settings.ai.update': 'ai-workbench-model-settings',
  'settings.ai.view': 'ai-workbench-model-settings',
  'platform.configuration.secret-data.view': 'configuration-secrets',
  'platform.access-control.access-reviews.execute': 'platform-access-control-serviceaccounts',
  'platform.helm.values.view': 'helm-releases',
  'platform.helm.releases.rollback': 'helm-releases',
  'platform.observability.logging.disable': 'clusters',
  'platform.observability.logging.enable': 'clusters',
  'platform.rbac.bind': 'platform-access-control',
  'platform.rbac.escalate': 'platform-access-control',
  'platform.deployment.create': 'workloads-deployments',
  'platform.deployment.delete': 'workloads-deployments',
  'platform.deployment.restart': 'workloads-deployments',
  'platform.deployment.rollback': 'workloads-deployments',
  'platform.deployment.scale': 'workloads-deployments',
  'platform.deployment.update': 'workloads-deployments',
  'platform.deployment.view': 'workloads-deployments',
  'platform.workloads.stateful-sets.create': 'workloads-statefulsets',
  'platform.workloads.stateful-sets.delete': 'workloads-statefulsets',
  'platform.workloads.stateful-sets.restart': 'workloads-statefulsets',
  'platform.workloads.stateful-sets.scale': 'workloads-statefulsets',
  'platform.workloads.stateful-sets.update': 'workloads-statefulsets',
  'platform.workloads.stateful-sets.view': 'workloads-statefulsets',
  'platform.workloads.daemon-sets.create': 'workloads-daemonsets',
  'platform.workloads.daemon-sets.delete': 'workloads-daemonsets',
  'platform.workloads.daemon-sets.restart': 'workloads-daemonsets',
  'platform.workloads.daemon-sets.update': 'workloads-daemonsets',
  'platform.workloads.daemon-sets.view': 'workloads-daemonsets',
  'platform.workloads.jobs.create': 'workloads-jobs',
  'platform.workloads.jobs.delete': 'workloads-jobs',
  'platform.workloads.jobs.update': 'workloads-jobs',
  'platform.workloads.jobs.view': 'workloads-jobs',
  'platform.workloads.cron-jobs.create': 'workloads-cronjobs',
  'platform.workloads.cron-jobs.delete': 'workloads-cronjobs',
  'platform.workloads.cron-jobs.suspend': 'workloads-cronjobs',
  'platform.workloads.cron-jobs.update': 'workloads-cronjobs',
  'platform.workloads.cron-jobs.view': 'workloads-cronjobs',
  'platform.pods.delete': 'workloads-pods',
  'platform.pods.exec': 'workloads-pods',
  'platform.pods.logs': 'workloads-pods',
  'platform.pods.update': 'workloads-pods',
  'platform.pods.view': 'workloads-pods',
  'plugin.configure': 'plugins-marketplace',
  'plugin.install': 'plugins-marketplace',
  'plugin.lifecycle': 'plugins-marketplace',
  'plugin.remove': 'plugins-marketplace',
  'plugin.upgrade': 'plugins-marketplace',
  'plugin.view': 'plugins-marketplace',
  'docker.operations.cancel': 'compute-workbench-tasks-operations',
  'docker.operations.claim': 'compute-workbench-tasks-operations',
  'docker.operations.retry': 'compute-workbench-tasks-operations',
  'docker.overview.view': 'compute-workbench-overview',
  'docker.dlp-appeals.review': 'docker-workbench-projects',
  'docker.dlp-events.view': 'docker-workbench-projects',
  'docker.dlp-policy.publish': 'docker-workbench-projects',
  'docker.dlp-policy.update': 'docker-workbench-projects',
  'docker.dlp-policy.view': 'docker-workbench-projects',
  'docker.ports.create': 'docker-workbench-projects',
  'docker.ports.delete': 'docker-workbench-projects',
  'docker.ports.update': 'docker-workbench-projects',
  'docker.ports.view': 'docker-workbench-projects',
  'docker.services.logs': 'docker-workbench-projects',
  'docker.services.restart': 'docker-workbench-projects',
  'docker.services.start': 'docker-workbench-projects',
  'docker.services.stop': 'docker-workbench-projects',
  'docker.services.terminal': 'docker-workbench-projects',
  'docker.services.view': 'docker-workbench-projects',
  'virtualization.operations.cancel': 'compute-workbench-tasks-operations',
  'virtualization.operations.retry': 'compute-workbench-tasks-operations',
  'virtualization.vm_access.update': 'virtualization-workbench-vms',
  'virtualization.overview.view': 'compute-workbench-overview',
  'virtualization.sync.sync': 'compute-workbench-tasks-operations',
  'secret.create': 'settings-secrets',
  'secret.revoke': 'settings-secrets',
  'secret.rotate': 'settings-secrets',
  'secret.update': 'settings-secrets',
  'secret.use': 'settings-secrets',
  'secret.view': 'settings-secrets',
}

const PERMISSION_WORKBENCH_OWNERS: Record<string, string> = {
  'platform.resource-creation.use': 'platform',
  'workbench.ai.view': 'ai',
  'workbench.compute.view': 'compute',
  'workbench.delivery.view': 'delivery',
  'workbench.home.view': 'home',
  'workbench.monitoring.view': 'monitoring',
  'workbench.platform.view': 'platform',
  'workbench.security.view': 'security',
  'workbench.settings.view': 'settings',
}

type RolePermissionTreeNode = DataNode & { children?: RolePermissionTreeNode[] }
export type Translate = (key: string, fallback?: string) => string

export function localizePermissionDefinitions(
  definitions: PermissionDefinition[],
  localeCode: LocaleCode,
  translate: Translate,
) {
  return definitions.map((permission) => ({
    ...permission,
    displayName: translate(
      `permission.key.${permission.key}`,
      localeCode === 'zh_CN' ? permission.displayName : permission.key,
    ),
  }))
}

export function normalizePermissionKeys(value: unknown) {
  return toStringArray(value).sort((left, right) => left.localeCompare(right))
}

export const normalizeRolePermissionKeys = normalizePermissionKeys

function permissionTreeKey(permissionKey: string) {
  return `permission:${permissionKey}`
}

function routePermissionKeys(route: (typeof routeMeta)[number]) {
  const keys = route.permissionKeysAny?.length
    ? route.permissionKeysAny
    : [resolveRoutePermission(route)].filter(Boolean)
  return normalizePermissionKeys(keys)
}

function permissionStem(permissionKey: string) {
  return permissionKey.slice(0, Math.max(0, permissionKey.lastIndexOf('.')))
}

function routeDepth(
  route: (typeof routeMeta)[number],
  routeByID: Map<string, (typeof routeMeta)[number]>,
) {
  let depth = 0
  let current = route
  const visited = new Set<string>()
  while (current.parentId && !visited.has(current.parentId)) {
    visited.add(current.parentId)
    const parent = routeByID.get(current.parentId)
    if (!parent) break
    depth += 1
    current = parent
  }
  return depth
}

function normalizeWorkbenchID(route: (typeof routeMeta)[number]) {
  const workbench = route.workbenchId || route.group || 'unknown'
  if (workbench === 'identity') return 'security'
  if (workbench === 'observe') return 'monitoring'
  if (workbench === 'ai-gateway') return 'ai'
  if (workbench === 'access' || workbench === 'system') return 'settings'
  return workbench
}

export function buildRolePermissionTreeData(
  definitions: PermissionDefinition[],
  storedPermissionKeys: string[] = [],
  translate?: Translate,
): DataNode[] {
  const routeItems = routeMeta.filter(
    (route) => route.requiresAuth && route.navVisible && route.menuId,
  )
  const routeByID = new Map(routeItems.map((route) => [route.id, route]))
  const nodeByRouteID = new Map<string, RolePermissionTreeNode>()
  const routeOrder = new Map(routeMeta.map((route, index) => [route.id, index]))

  routeItems.forEach((route) => {
    nodeByRouteID.set(route.id, {
      key: `route:${route.id}`,
      title: translate?.(`route.${route.id}.title`, route.title) ?? route.title,
      children: [],
    })
  })

  const rootsByWorkbench = new Map<string, RolePermissionTreeNode[]>()
  const permissionsByWorkbench = new Map<string, RolePermissionTreeNode[]>()
  routeItems.forEach((route) => {
    const node = nodeByRouteID.get(route.id)
    if (!node) return
    const parent = route.parentId ? nodeByRouteID.get(route.parentId) : null
    if (parent) {
      parent.children = [...(parent.children ?? []), node]
      return
    }
    const workbench = normalizeWorkbenchID(route)
    rootsByWorkbench.set(workbench, [...(rootsByWorkbench.get(workbench) ?? []), node])
  })

  function permissionOwner(permissionKey: string) {
    const explicitOwner = ROUTE_PERMISSION_OWNERS[permissionKey]
    if (explicitOwner && nodeByRouteID.has(explicitOwner)) return explicitOwner

    const targetStem = permissionStem(permissionKey)
    return routeItems
      .flatMap((route) =>
        routePermissionKeys(route)
          .filter((routePermissionKey) => {
            const stem = permissionStem(routePermissionKey)
            return (
              routePermissionKey === permissionKey ||
              targetStem === stem ||
              targetStem.startsWith(`${stem}.`)
            )
          })
          .map((routePermissionKey) => ({
            id: route.id,
            exactRank: routePermissionKey === permissionKey ? 0 : 1,
            aggregateRank: route.permissionKeysAny?.length ? 1 : 0,
            stemLength: permissionStem(routePermissionKey).length,
            depth: routeDepth(route, routeByID),
            order: routeOrder.get(route.id) ?? 0,
          })),
      )
      .sort(
        (left, right) =>
          left.exactRank - right.exactRank ||
          left.aggregateRank - right.aggregateRank ||
          right.stemLength - left.stemLength ||
          right.depth - left.depth ||
          left.order - right.order,
      )[0]?.id
  }

  const assignableDefinitions = definitions.filter(
    (definition) => definition.assignable && definition.status === 'active',
  )
  assignableDefinitions.forEach((definition) => {
    const node: RolePermissionTreeNode = {
      key: permissionTreeKey(definition.key),
      title:
        definition.riskLevel === 'high'
          ? `${definition.displayName} · ${translate?.('access.roles.highRisk', '高风险') ?? '高风险'}`
          : definition.displayName,
    }
    const workbenchOwner = PERMISSION_WORKBENCH_OWNERS[definition.key]
    if (workbenchOwner) {
      permissionsByWorkbench.set(workbenchOwner, [
        ...(permissionsByWorkbench.get(workbenchOwner) ?? []),
        node,
      ])
      return
    }
    const owner = permissionOwner(definition.key)
    const ownerNode = owner ? nodeByRouteID.get(owner) : null
    if (ownerNode) {
      ownerNode.children = [...(ownerNode.children ?? []), node]
      return
    }
    const workbench = PERMISSION_DOMAIN_WORKBENCH[definition.domain] || 'unknown'
    const roots = rootsByWorkbench.get(workbench) ?? []
    const domainKey = `permissions:${definition.domain}`
    const domainNode = roots.find((root) => root.key === domainKey)
    if (domainNode) {
      domainNode.children = [...(domainNode.children ?? []), node]
    } else {
      roots.push({
        key: domainKey,
        title:
          translate?.(`permission.domain.${definition.domain}`, definition.domain) ??
          definition.domain,
        children: [node],
      })
    }
    rootsByWorkbench.set(workbench, roots)
  })

  const pruneEmptyNodes = (nodes: RolePermissionTreeNode[]): RolePermissionTreeNode[] =>
    nodes
      .map((node) => {
        const children = pruneEmptyNodes(node.children ?? [])
        return children.length ? { ...node, children } : node
      })
      .filter(
        (node) => String(node.key).startsWith('permission:') || (node.children?.length ?? 0) > 0,
      )

  const workbenchTree = Array.from(rootsByWorkbench.entries())
    .map(([workbench, children]) => {
      const fallbackTitle = ROLE_PERMISSION_WORKBENCH_LABELS[workbench] || workbench
      const title =
        translate?.(`access.roles.workbench.${workbench}`, fallbackTitle) ?? fallbackTitle
      const prunedChildren = pruneEmptyNodes(
        children.sort((left, right) => {
          const leftID = String(left.key).replace('route:', '')
          const rightID = String(right.key).replace('route:', '')
          return (routeOrder.get(leftID) ?? 0) - (routeOrder.get(rightID) ?? 0)
        }),
      )
      return {
        key: `workbench:${workbench}`,
        title,
        children: [
          ...(permissionsByWorkbench.get(workbench) ?? []),
          ...prunedChildren.flatMap((node) =>
            String(node.title).replace(/\s/g, '').toLowerCase() ===
              title.replace(/\s/g, '').toLowerCase() && node.children?.length
              ? node.children
              : [node],
          ),
        ],
      }
    })
    .filter((node) => node.children.length > 0)

  const assignableKeys = new Set(assignableDefinitions.map((definition) => definition.key))
  const definitionByKey = new Map(definitions.map((definition) => [definition.key, definition]))
  const compatibilityKeys = normalizePermissionKeys(storedPermissionKeys).filter(
    (permissionKey) => !assignableKeys.has(permissionKey),
  )
  if (compatibilityKeys.length) {
    workbenchTree.push({
      key: 'workbench:compatibility',
      title:
        translate?.('access.roles.compatibilityReadonly', '兼容权限（只读）') ?? '兼容权限（只读）',
      children: compatibilityKeys.map((permissionKey) => {
        const definition = definitionByKey.get(permissionKey)
        return {
          key: permissionTreeKey(permissionKey),
          title: definition
            ? `${definition.displayName} (${permissionKey})`
            : `${translate?.('access.roles.unknownPermission', '未知权限') ?? '未知权限'} (${permissionKey})`,
          disabled: true,
        }
      }),
    })
  }

  return workbenchTree
}
