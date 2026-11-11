/** @vitest-environment jsdom */

import { describe, expect, it } from 'vitest'
import { isKnownMenuIcon } from '../menu-icons'
import {
  buildWorkbenchMenuTree,
  filterMenuTree,
  flattenMenuItems,
  getMenuDerivedPermissionKeys,
  normalizeMenuSubmitValues,
  summarizeMenuVisibility,
} from '../system-model'

describe('menu visibility helpers', () => {
  it('derives permission-based visibility for route-backed menus', () => {
    const summary = summarizeMenuVisibility({
      id: 'menus',
      path: '/system/menus',
    })

    expect(summary.mode).toBe('derived')
    expect(summary.derivedPermissionKeys).toEqual(['system.menus.view'])
    expect(summary.explicitRoleIds).toEqual([])
  })

  it('ignores stale role bindings for route-backed menus', () => {
    const summary = summarizeMenuVisibility({
      id: 'menus',
      path: '/system/menus',
      roleIds: ['ops-admin', ' ops-admin ', 'system-admin'],
    })

    expect(summary.mode).toBe('derived')
    expect(summary.derivedPermissionKeys).toEqual(['system.menus.view'])
    expect(summary.explicitRoleIds).toEqual(['ops-admin', 'system-admin'])
  })

  it('drops explicit roles when a route permission owns the menu', () => {
    expect(
      normalizeMenuSubmitValues({
        id: 'menus',
        path: '/system/menus',
        labelZh: '菜单管理',
        labelEn: 'Menu Management',
        iconKey: 'menu-square',
        section: ['users'],
        sortOrder: 1,
        enabled: true,
        visibilityMode: 'explicit',
        roleIds: ['admin'],
      }).roleIds,
    ).toEqual([])
  })

  it('marks unmapped menus as requiring explicit configuration', () => {
    const summary = summarizeMenuVisibility({
      id: 'custom-unmapped',
      path: '/custom/unmapped',
    })

    expect(summary.mode).toBe('unmapped')
    expect(summary.derivedPermissionKeys).toEqual([])
    expect(summary.explicitRoleIds).toEqual([])
  })

  it('uses backend-provided derived permission keys when present', () => {
    expect(
      getMenuDerivedPermissionKeys({
        id: 'custom-unmapped',
        path: '/custom/unmapped',
        derivedPermissionKeys: ['custom.view', ' custom.view ', 'custom.manage'],
      }),
    ).toEqual(['custom.manage', 'custom.view'])
  })

  it('derives visibility for any-child workbench containers', () => {
    const summary = summarizeMenuVisibility({
      id: 'virtualization-workbench',
      path: '/compute/virtualization',
    })

    expect(summary.mode).toBe('derived')
    expect(summary.derivedPermissionKeys).toEqual(
      expect.arrayContaining([
        'virtualization.vms.view',
        'virtualization.clusters.view',
        'virtualization.images.view',
        'virtualization.storage.view',
        'virtualization.flavors.view',
      ]),
    )
  })

  it('recognizes every icon key currently persisted by the menu seeds', () => {
    const iconKeys = [
      'activity',
      'bell',
      'blocks',
      'book',
      'bot',
      'boxes',
      'clipboard-list',
      'cluster',
      'code',
      'cog',
      'desktop',
      'docker',
      'file-clock',
      'flavor',
      'gauge',
      'globe',
      'history',
      'home',
      'image',
      'inspect',
      'key',
      'link',
      'megaphone',
      'menu-square',
      'network',
      'palette',
      'panels-top-left',
      'puzzle',
      'radio-tower',
      'server',
      'settings',
      'shield',
      'siren',
      'storage',
      'sync',
      'user',
      'users',
      'waves',
      'wrench',
    ]

    expect(iconKeys.filter((iconKey) => !isKnownMenuIcon(iconKey))).toEqual([])
  })

  it('limits the default tree to top-level rows', () => {
    const filtered = filterMenuTree(
      [
        {
          id: 'parent',
          labelZh: 'Parent',
          labelEn: 'Parent',
          path: '/parent',
          iconKey: 'menu-square',
          section: 'admin',
          sortOrder: 1,
          enabled: true,
          children: [
            {
              id: 'child-leaf',
              parentId: 'parent',
              labelZh: 'Child',
              labelEn: 'Child',
              path: '/parent/child',
              iconKey: 'menu-square',
              section: 'admin',
              sortOrder: 1,
              enabled: true,
            },
          ],
        },
        {
          id: 'standalone-leaf',
          labelZh: 'Leaf',
          labelEn: 'Leaf',
          path: '/leaf',
          iconKey: 'menu-square',
          section: 'admin',
          sortOrder: 2,
          enabled: true,
        },
      ],
      {
        topLevelOnly: true,
        section: '',
        workbench: '',
        enabled: 'all',
        visibility: 'all',
      },
    )

    expect(filtered[0].children).toBeUndefined()
    expect(filtered[1].children).toBeUndefined()
  })

  it('can filter menus with no section as ungrouped', () => {
    const filtered = filterMenuTree(
      [
        {
          id: 'ungrouped',
          labelZh: 'Ungrouped',
          labelEn: 'Ungrouped',
          path: '/',
          iconKey: 'gauge',
          section: '',
          sortOrder: 1,
          enabled: true,
        },
        {
          id: 'grouped',
          labelZh: 'Grouped',
          labelEn: 'Grouped',
          path: '/grouped',
          iconKey: 'menu-square',
          section: 'admin',
          sortOrder: 2,
          enabled: true,
        },
      ],
      {
        topLevelOnly: true,
        section: '__ungrouped__',
        workbench: '',
        enabled: 'all',
        visibility: 'all',
      },
    )

    expect(filtered.map((item) => item.id)).toEqual(['ungrouped'])
  })

  it('unwraps canonical workbench roots without regrouping the compute hierarchy', () => {
    const roots = [
      ['home-workbench', '/portal'],
      ['compute-workbench', '/compute'],
      ['ai-workbench', '/ai-workbench'],
      ['monitoring-workbench', '/monitoring-workbench'],
      ['identity', '/internal-workbench'],
      ['settings', '/settings'],
      ['system', '/system'],
    ].map(([id, path], index) => ({
      id,
      labelZh: id,
      labelEn: id,
      path,
      iconKey: 'menu-square',
      section: 'ops',
      sortOrder: index,
      enabled: true,
      children: [
        {
          id: `${id}-child`,
          parentId: id,
          labelZh: `${id} child`,
          labelEn: `${id} child`,
          path: `${path}/child`,
          iconKey: 'menu-square',
          section: 'ops',
          sortOrder: 1,
          enabled: true,
        },
      ],
    }))

    const grouped = buildWorkbenchMenuTree(roots)
    const visibleMenuIds = flattenMenuItems(grouped).map((item) => item.id)

    expect(visibleMenuIds).not.toEqual(expect.arrayContaining(roots.map((item) => item.id)))
    expect(visibleMenuIds).toEqual(expect.arrayContaining(roots.map((item) => `${item.id}-child`)))
    expect(grouped.find((item) => item.syntheticWorkbenchKey === 'compute')?.children?.[0].id).toBe(
      'compute-workbench-child',
    )
  })
})
