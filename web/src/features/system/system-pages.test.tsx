/** @vitest-environment jsdom */

import { describe, expect, it } from 'vitest'
import { filterMenuTree, getMenuDerivedPermissionKeys, summarizeMenuVisibility } from './system-pages'

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

  it('prefers explicit override mode when role bindings are present', () => {
    const summary = summarizeMenuVisibility({
      id: 'menus',
      path: '/system/menus',
      roleIds: ['ops-admin', ' ops-admin ', 'system-admin'],
    })

    expect(summary.mode).toBe('explicit')
    expect(summary.derivedPermissionKeys).toEqual(['system.menus.view'])
    expect(summary.explicitRoleIds).toEqual(['ops-admin', 'system-admin'])
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

  it('removes empty children arrays from leaf nodes used by the tree table', () => {
    const filtered = filterMenuTree([
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
    ], {
      topLevelOnly: true,
      section: '',
      workbench: '',
      enabled: 'all',
      visibility: 'all',
    })

    expect(filtered[0].children?.[0].children).toBeUndefined()
    expect(filtered[1].children).toBeUndefined()
  })

  it('can filter menus with no section as ungrouped', () => {
    const filtered = filterMenuTree([
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
    ], {
      topLevelOnly: true,
      section: '__ungrouped__',
      workbench: '',
      enabled: 'all',
      visibility: 'all',
    })

    expect(filtered.map((item) => item.id)).toEqual(['ungrouped'])
  })
})
