import { describe, expect, it, vi } from 'vitest'
import { observabilityNotificationRoutes } from './routes'

const routePage = vi.hoisted(() => () => null)

vi.mock('./page', () => ({ NotificationsPage: routePage }))

describe('notification route manifest', () => {
  it('preserves canonical metadata and loads the notification leaf', async () => {
    const [route] = observabilityNotificationRoutes
    expect(route.meta).toMatchObject({
      id: 'monitoring-workbench-notifications',
      path: '/monitoring-workbench/notifications',
      title: '通知策略',
      permissionKey: 'observe.notifications.view',
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-notifications',
    })
    expect('aliases' in route).toBe(false)
    await expect(route.load()).resolves.toEqual({ default: routePage })
  })
})
