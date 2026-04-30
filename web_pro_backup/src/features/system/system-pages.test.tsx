/** @vitest-environment jsdom */

import { describe, expect, it } from 'vitest'
import { getMenuDerivedPermissionKeys, summarizeMenuVisibility } from './system-pages'

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
})
