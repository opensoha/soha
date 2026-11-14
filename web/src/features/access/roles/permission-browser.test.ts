import type { PermissionCatalog, PermissionDefinition } from '@opensoha/contracts/gen/ts/sohaapi'
import permissionCatalogArtifact from '@opensoha/contracts/auth/permission-catalog.json'
import { describe, expect, it } from 'vitest'
import { buildRolePermissionTreeData } from './permission-model'
import {
  type PermissionBrowserNode,
  buildRolePermissionBrowserData,
  permissionValuesForNode,
  toggleRolePermissionValues,
} from './permission-browser'

const workbenchPermissions = [
  'ai',
  'compute',
  'delivery',
  'home',
  'monitoring',
  'platform',
  'security',
  'settings',
].map(
  (workbench) =>
    ({
      key: `workbench.${workbench}.view`,
      domain: 'workbench',
      resource: workbench,
      action: 'view',
      displayName: `访问 ${workbench} 工作台`,
      riskLevel: 'read',
      scopeKinds: ['tenant', 'workspace'],
      approvalPolicy: 'never',
      status: 'active',
      assignable: true,
    }) satisfies PermissionDefinition,
)
const definitions = [
  ...(permissionCatalogArtifact as PermissionCatalog).permissions.filter(
    (permission) => !permission.key.startsWith('workbench.'),
  ),
  ...workbenchPermissions,
]

function flattenNodes(nodes: PermissionBrowserNode[]): PermissionBrowserNode[] {
  return nodes.flatMap((node) => [node, ...flattenNodes(node.children)])
}

describe('role permission browser model', () => {
  it('projects every assignable catalog permission into one page-owned browser', () => {
    const data = buildRolePermissionBrowserData(buildRolePermissionTreeData(definitions))
    const permissionKeys = data.workbenches.flatMap(permissionValuesForNode)
    const assignableKeys = definitions
      .filter((definition) => definition.assignable && definition.status === 'active')
      .map((definition) => definition.key)

    expect(new Set(permissionKeys)).toEqual(new Set(assignableKeys))
    expect(new Set(permissionKeys).size).toBe(permissionKeys.length)
  })

  it('does not render menu branches without permissions', () => {
    const data = buildRolePermissionBrowserData(buildRolePermissionTreeData(definitions))
    expect(
      flattenNodes(data.workbenches).every((node) => permissionValuesForNode(node).length > 0),
    ).toBe(true)
  })

  it('gives every workbench one independent entry and separates resource creation', () => {
    const data = buildRolePermissionBrowserData(buildRolePermissionTreeData(definitions))
    const entries = {
      ai: 'workbench.ai.view',
      compute: 'workbench.compute.view',
      delivery: 'workbench.delivery.view',
      home: 'workbench.home.view',
      monitoring: 'workbench.monitoring.view',
      platform: 'workbench.platform.view',
      security: 'workbench.security.view',
      settings: 'workbench.settings.view',
    }
    for (const [workbenchID, permissionKey] of Object.entries(entries)) {
      const workbench = data.workbenches.find((node) => node.key === `workbench:${workbenchID}`)
      const entry = workbench?.children.find(
        (node) => node.key === `entry:workbench:${workbenchID}`,
      )
      expect(entry?.title, workbenchID).toBe('工作台入口')
      expect(entry && permissionValuesForNode(entry)).toEqual([permissionKey])
    }

    const platform = data.workbenches.find((node) => node.key === 'workbench:platform')
    const resourceCreation = platform?.children.find(
      (node) => node.key === 'capability:platform-resource-creation',
    )

    expect(resourceCreation?.title).toBe('资源创建')
    expect(resourceCreation && permissionValuesForNode(resourceCreation)).toEqual([
      'platform.resource-creation.use',
    ])
  })

  it('batch toggles a branch without dropping unknown stored permissions', () => {
    expect(
      toggleRolePermissionValues(
        ['future.permission.view', 'platform.configuration.view'],
        ['platform.configuration.view', 'platform.configuration.secret-data.view'],
        false,
      ),
    ).toEqual(['future.permission.view'])

    expect(
      toggleRolePermissionValues(
        ['future.permission.view'],
        ['platform.configuration.view', 'platform.configuration.secret-data.view'],
        true,
      ),
    ).toEqual([
      'future.permission.view',
      'platform.configuration.secret-data.view',
      'platform.configuration.view',
    ])
  })
})
