import { describe, expect, it, vi } from 'vitest'
import { observabilityIntegrationRoutes } from './routes'

const routePage = vi.hoisted(() => () => null)

vi.mock('./page', () => ({ AlertIntegrationsPage: routePage }))

describe('alert integration route manifest', () => {
  it('preserves metadata and loads the integration leaf directly', async () => {
    const [route] = observabilityIntegrationRoutes

    expect(route.meta).toEqual({
      id: 'monitoring-workbench-integrations',
      path: '/monitoring-workbench/integrations',
      title: '告警集成',
      description: 'Alertmanager、Grafana Alerting 与通用 Webhook 接入',
      icon: 'IconConnection',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-integrations',
      permissionKey: 'observe.alert-integrations.view',
      scopeMode: 'passive',
    })
    expect(route.shell).toBe('app')
    await expect(route.load()).resolves.toEqual({ default: routePage })
  })
})
