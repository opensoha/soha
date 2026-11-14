import { describe, expect, it } from 'vitest'
import { validateRouteDefinitions } from '@/routes/definitions'
import { canAccessRoute } from '@/routes/meta'
import type { PermissionSnapshot } from '@/types'
import { companionRoutes } from './routes'

describe('Companion routes', () => {
  it('keeps the settings page under AI Workbench and chat permission', () => {
    const route = companionRoutes.find((item) => item.meta.id === 'ai-workbench-companion')
    expect(route?.meta).toMatchObject({
      path: '/ai-workbench/companion',
      parentId: 'ai-workbench',
      menuId: 'ai-workbench-companion',
      permissionKey: 'observe.ai.chat',
      navVisible: true,
    })
    expect(validateRouteDefinitions(companionRoutes)).toEqual([])

    const snapshot = {
      permissionKeys: ['workbench.ai.view', 'observe.ai.chat'],
      visibleMenuIds: ['ai-workbench-companion'],
      visibleMenus: [],
    } as PermissionSnapshot
    expect(canAccessRoute(route!.meta, snapshot)).toBe(true)
    expect(
      canAccessRoute(route!.meta, { ...snapshot, permissionKeys: ['workbench.ai.view'] }),
    ).toBe(false)
  })
})
