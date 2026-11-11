import { describe, expect, it, vi } from 'vitest'
import { observabilityEventRoutes } from './routes'

const routePage = vi.hoisted(() => () => null)

vi.mock('./page', () => ({ EventsPage: routePage }))

describe('observability event route manifest', () => {
  it('preserves metadata and loads the event leaf directly', async () => {
    expect(observabilityEventRoutes).toHaveLength(1)
    const [route] = observabilityEventRoutes

    expect(route.meta).toEqual({
      id: 'monitoring-workbench-events',
      path: '/monitoring-workbench/events',
      title: '事件流',
      description: '事件时间线与上下文',
      icon: 'IconBell',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-events',
      permissionKey: 'observe.events.view',
      scopeMode: 'passive',
    })
    expect(route.shell).toBe('app')
    await expect(route.load()).resolves.toEqual({ default: routePage })
  })
})
