import type { PermissionCatalog, PermissionDefinition } from '@opensoha/contracts/gen/ts/sohaapi'
import permissionCatalogArtifact from '@opensoha/contracts/auth/permission-catalog.json'
import type { DataNode } from 'antd/es/tree'
import { describe, expect, it } from 'vitest'
import { enUS } from '@/i18n/locales/en_US'
import { zhCN } from '@/i18n/locales/zh_CN'
import {
  buildRolePermissionTreeData,
  localizePermissionDefinitions,
  normalizeRolePermissionKeys,
} from './permission-model'

const resourceCreationPermission = {
  key: 'platform.resource-creation.use',
  domain: 'platform',
  resource: 'resource-creation',
  action: 'use',
  displayName: '使用 Kubernetes YAML 创建器',
  riskLevel: 'mutate',
  scopeKinds: ['workspace', 'cluster', 'namespace', 'resource'],
  approvalPolicy: 'never',
  status: 'active',
  assignable: true,
} satisfies PermissionDefinition

const workbenchPermissions = [
  ['ai', '访问 AI 工作台'],
  ['compute', '访问计算资源工作台'],
  ['delivery', '访问持续交付工作台'],
  ['home', '访问应用门户'],
  ['monitoring', '访问可观测与值班工作台'],
  ['platform', '访问 k8s工作台'],
  ['security', '访问身份与安全工作台'],
  ['settings', '访问设置中心'],
].map(
  ([workbench, displayName]) =>
    ({
      key: `workbench.${workbench}.view`,
      domain: 'workbench',
      resource: workbench,
      action: 'view',
      displayName,
      riskLevel: 'read',
      scopeKinds: ['tenant', 'workspace'],
      approvalPolicy: 'never',
      status: 'active',
      assignable: true,
    }) satisfies PermissionDefinition,
)

const definitions = [
  ...(permissionCatalogArtifact as PermissionCatalog).permissions.filter(
    (permission) =>
      permission.key !== resourceCreationPermission.key && !permission.key.startsWith('workbench.'),
  ),
  resourceCreationPermission,
  ...workbenchPermissions,
]

function nodePath(nodes: DataNode[], targetKey: string, parents: string[] = []): string[] {
  for (const node of nodes) {
    const path = [...parents, String(node.key)]
    if (String(node.key) === targetKey) return path
    const childPath = nodePath((node.children ?? []) as DataNode[], targetKey, path)
    if (childPath.length) return childPath
  }
  return []
}

function treeKeys(nodes: DataNode[]): string[] {
  return nodes.flatMap((node) => [
    String(node.key),
    ...treeKeys((node.children ?? []) as DataNode[]),
  ])
}

function findNode(nodes: DataNode[], targetKey: string): DataNode | undefined {
  for (const node of nodes) {
    if (String(node.key) === targetKey) return node
    const child = findNode((node.children ?? []) as DataNode[], targetKey)
    if (child) return child
  }
}

describe('role permission tree model', () => {
  it('uses readable localized Compute permission names', () => {
    const computePermission = definitions.find(
      (definition) => definition.key === 'virtualization.vms.view',
    )
    if (!computePermission) throw new Error('missing Compute permission fixture')
    const translate =
      (dictionary: Record<string, string>) =>
      (key: string, fallback = key) =>
        dictionary[key] || fallback

    expect(
      localizePermissionDefinitions([computePermission], 'en_US', translate(enUS))[0].displayName,
    ).toBe('View virtual machines')
    expect(
      localizePermissionDefinitions([computePermission], 'zh_CN', translate(zhCN))[0].displayName,
    ).toBe('查看虚拟机')
  })

  it('normalizes role permissions without inferring workbench entry access', () => {
    expect(normalizeRolePermissionKeys(['platform.pods.view'])).toEqual(['platform.pods.view'])
    expect(normalizeRolePermissionKeys(['docker.projects.view'])).toEqual(['docker.projects.view'])
    expect(normalizeRolePermissionKeys(['workspace.resource.view', 'platform.pods.view'])).toEqual([
      'platform.pods.view',
      'workspace.resource.view',
    ])
    expect(normalizeRolePermissionKeys(['identity.portal.view'])).toEqual(['identity.portal.view'])
    expect(normalizeRolePermissionKeys(['workspace.application.view'])).toEqual([
      'workspace.application.view',
    ])
  })

  it('places sensitive reads under their owning menus', () => {
    const tree = buildRolePermissionTreeData(definitions)
    expect(nodePath(tree, 'permission:platform.configuration.secret-data.view')).toContain(
      'route:configuration-secrets',
    )
    expect(nodePath(tree, 'permission:platform.helm.values.view')).toContain('route:helm-releases')
  })

  it.each([
    ['observe.ai.chat', 'ai-workbench-chat'],
    ['observe.ai.root-cause.run', 'ai-workbench-chat'],
    ['observe.ai.inspection.cancel', 'ai-workbench-inspection'],
    ['observe.ai.inspection.create', 'ai-workbench-inspection'],
    ['observe.ai.inspection.delete', 'ai-workbench-inspection'],
    ['observe.ai.inspection.run', 'ai-workbench-inspection'],
    ['observe.ai.inspection.update', 'ai-workbench-inspection'],
    ['observe.ai.inspection.validate', 'ai-workbench-inspection'],
  ])('places AI permission %s under its owning page', (permissionKey, routeID) => {
    expect(
      nodePath(buildRolePermissionTreeData(definitions), `permission:${permissionKey}`),
    ).toContain(`route:${routeID}`)
  })

  it('places exact workload operations under their owning pages', () => {
    const tree = buildRolePermissionTreeData(definitions)
    const owners = {
      'workloads-deployments': [
        'platform.deployment.create',
        'platform.deployment.delete',
        'platform.deployment.restart',
        'platform.deployment.rollback',
        'platform.deployment.scale',
        'platform.deployment.update',
        'platform.deployment.view',
      ],
      'workloads-statefulsets': [
        'platform.workloads.stateful-sets.create',
        'platform.workloads.stateful-sets.delete',
        'platform.workloads.stateful-sets.restart',
        'platform.workloads.stateful-sets.scale',
        'platform.workloads.stateful-sets.update',
        'platform.workloads.stateful-sets.view',
      ],
      'workloads-daemonsets': [
        'platform.workloads.daemon-sets.create',
        'platform.workloads.daemon-sets.delete',
        'platform.workloads.daemon-sets.restart',
        'platform.workloads.daemon-sets.update',
        'platform.workloads.daemon-sets.view',
      ],
      'workloads-jobs': [
        'platform.workloads.jobs.create',
        'platform.workloads.jobs.delete',
        'platform.workloads.jobs.update',
        'platform.workloads.jobs.view',
      ],
      'workloads-cronjobs': [
        'platform.workloads.cron-jobs.create',
        'platform.workloads.cron-jobs.delete',
        'platform.workloads.cron-jobs.suspend',
        'platform.workloads.cron-jobs.update',
        'platform.workloads.cron-jobs.view',
      ],
    }
    for (const [routeID, permissionKeys] of Object.entries(owners)) {
      for (const permissionKey of permissionKeys) {
        expect(nodePath(tree, `permission:${permissionKey}`)).toContain(`route:${routeID}`)
      }
    }
    expect(nodePath(tree, 'permission:platform.pods.exec')).toContain('route:workloads-pods')
    expect(nodePath(tree, 'permission:platform.pods.update')).toContain('route:workloads-pods')
  })

  it.each([
    ['platform.workloads.overview.view', 'workloads-overview'],
    ['platform.network.topology.view', 'network-topology'],
    ['platform.observability.logging.enable', 'clusters'],
    ['platform.observability.logging.disable', 'clusters'],
    ['platform.rbac.bind', 'platform-access-control'],
    ['platform.rbac.escalate', 'platform-access-control'],
  ])('places %s under its owning page', (permissionKey, routeID) => {
    expect(
      nodePath(buildRolePermissionTreeData(definitions), `permission:${permissionKey}`),
    ).toContain(`route:${routeID}`)
  })

  it.each([
    ['platform.resource-creation.use', 'workbench:platform'],
    ['workbench.home.view', 'workbench:home'],
    ['workbench.platform.view', 'workbench:platform'],
    ['workbench.compute.view', 'workbench:compute'],
    ['workbench.delivery.view', 'workbench:delivery'],
    ['workbench.ai.view', 'workbench:ai'],
    ['workbench.monitoring.view', 'workbench:monitoring'],
    ['workbench.settings.view', 'workbench:settings'],
    ['workbench.security.view', 'workbench:security'],
  ])('places %s on its workbench entry', (permissionKey, workbenchID) => {
    const path = nodePath(buildRolePermissionTreeData(definitions), `permission:${permissionKey}`)
    expect(path).toContain(workbenchID)
    expect(path.some((key) => key.startsWith('permissions:'))).toBe(false)
  })

  it('does not leave Kubernetes permissions in an unowned domain bucket', () => {
    const platform = findNode(buildRolePermissionTreeData(definitions), 'workbench:platform')
    expect(treeKeys((platform?.children ?? []) as DataNode[])).not.toContain('permissions:platform')
    expect(treeKeys((platform?.children ?? []) as DataNode[])).not.toContain(
      'permissions:workspace',
    )
  })

  it('keeps legacy workspace scopes outside workbench entries', () => {
    const tree = buildRolePermissionTreeData(definitions)
    for (const permissionKey of ['workspace.resource.view', 'workspace.application.view']) {
      const path = nodePath(tree, `permission:${permissionKey}`)
      expect(path).toContain('workbench:unknown')
      expect(path.some((key) => key.startsWith('entry:'))).toBe(false)
    }
  })

  it.each([
    ['virtualization.vms.power', 'virtualization-workbench-vms'],
    ['virtualization.vms.connect', 'virtualization-workbench-vms'],
    ['virtualization.vm_access.update', 'virtualization-workbench-vms'],
    ['virtualization.operations.retry', 'compute-workbench-tasks-operations'],
    ['virtualization.sync.sync', 'compute-workbench-tasks-operations'],
    ['docker.services.logs', 'docker-workbench-projects'],
    ['docker.dlp-appeals.review', 'docker-workbench-projects'],
    ['docker.dlp-events.view', 'docker-workbench-projects'],
    ['docker.dlp-policy.publish', 'docker-workbench-projects'],
    ['docker.dlp-policy.update', 'docker-workbench-projects'],
    ['docker.dlp-policy.view', 'docker-workbench-projects'],
    ['docker.ports.update', 'docker-workbench-projects'],
    ['docker.operations.retry', 'compute-workbench-tasks-operations'],
  ])('places compute permission %s under its owning page', (permissionKey, routeID) => {
    expect(
      nodePath(buildRolePermissionTreeData(definitions), `permission:${permissionKey}`),
    ).toContain(`route:${routeID}`)
  })

  it('does not leave compute permissions in unowned domain buckets', () => {
    const compute = findNode(buildRolePermissionTreeData(definitions), 'workbench:compute')
    const keys = treeKeys((compute?.children ?? []) as DataNode[])
    expect(keys).not.toContain('permissions:docker')
    expect(keys).not.toContain('permissions:virtualization')
  })

  it.each([
    ['platform.workloads.replica-sets.view', 'workloads-replicasets'],
    ['platform.workloads.replication-controllers.view', 'workloads-replicationcontrollers'],
    ['platform.workloads.stateful-sets.view', 'workloads-statefulsets'],
    ['platform.workloads.daemon-sets.view', 'workloads-daemonsets'],
    ['platform.workloads.jobs.view', 'workloads-jobs'],
    ['platform.workloads.cron-jobs.view', 'workloads-cronjobs'],
    ['platform.configuration.config-maps.view', 'configuration-configmaps'],
    ['platform.configuration.secrets.view', 'configuration-secrets'],
    ['platform.network.services.view', 'network-services'],
    ['platform.network.ingresses.view', 'network-ingresses'],
    ['platform.storage.persistent-volume-claims.view', 'storage-pvc'],
    ['platform.storage.persistent-volumes.view', 'storage-pv'],
    ['platform.access-control.service-accounts.view', 'platform-access-control-serviceaccounts'],
    ['platform.access-control.roles.view', 'platform-access-control-roles'],
  ])('places %s under its exact resource page', (permissionKey, routeID) => {
    expect(
      nodePath(buildRolePermissionTreeData(definitions), `permission:${permissionKey}`),
    ).toContain(`route:${routeID}`)
  })

  it('contains every active assignable catalog permission exactly once', () => {
    const keys = treeKeys(buildRolePermissionTreeData(definitions))
    const permissionKeys = keys.filter((key) => key.startsWith('permission:'))
    const assignableKeys = definitions
      .filter((definition) => definition.assignable && definition.status === 'active')
      .map((definition) => `permission:${definition.key}`)

    expect(new Set(permissionKeys)).toEqual(new Set(assignableKeys))
    expect(new Set(permissionKeys).size).toBe(permissionKeys.length)
  })

  it('uses localized route titles in the permission browser', () => {
    const tree = buildRolePermissionTreeData(definitions, [], (key, fallback) =>
      key === 'route.ai-workbench-agent-providers.title' ? 'Agent 提供方' : (fallback ?? key),
    )

    expect(findNode(tree, 'route:ai-workbench-agent-providers')?.title).toBe('Agent 提供方')
  })

  it('places Skills permissions under the independent Skills page', () => {
    for (const permissionKey of [
      'ai.gateway.skills.view',
      'ai.gateway.skills.create',
      'ai.gateway.skills.update',
      'ai.gateway.skills.delete',
    ]) {
      expect(
        nodePath(buildRolePermissionTreeData(definitions), `permission:${permissionKey}`),
      ).toContain('route:ai-workbench-skills')
    }
  })

  it('places Data Sources permissions under the independent Data Sources page', () => {
    for (const permissionKey of [
      'ai.data-sources.view',
      'ai.data-sources.create',
      'ai.data-sources.update',
      'ai.data-sources.validate',
    ]) {
      expect(
        nodePath(buildRolePermissionTreeData(definitions), `permission:${permissionKey}`),
      ).toContain('route:ai-workbench-data-sources')
    }
  })

  it('preserves legacy and unknown stored permissions in a disabled compatibility area', () => {
    const tree = buildRolePermissionTreeData(definitions, [
      'ai.gateway.manage',
      'future.permission.view',
    ])
    const compatibility = tree.find((node) => node.key === 'workbench:compatibility')

    expect(compatibility?.children).toHaveLength(2)
    expect(compatibility?.children?.every((node) => node.disabled)).toBe(true)
  })
})
