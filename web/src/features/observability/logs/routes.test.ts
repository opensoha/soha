import { describe, expect, it, vi } from 'vitest'
import { observabilityLogRoutes } from './routes'

const routePage = vi.hoisted(() => () => null)
const dataSourcesPage = vi.hoisted(() => () => null)
vi.mock('./page', () => ({ LogsPage: routePage }))
vi.mock('./data-sources-page', () => ({ LogDataSourcesPage: dataSourcesPage }))

describe('observability log route manifest', () => {
  it('keeps observability scope inside the log workbench', async () => {
    const [route, dataSourcesRoute] = observabilityLogRoutes
    expect(route.meta).toMatchObject({
      id: 'monitoring-workbench-logs',
      path: '/monitoring-workbench/logs',
      menuId: 'monitoring-workbench-logs',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
      navVisible: true,
      tabbar: true,
    })
    await expect(route.load()).resolves.toEqual({ default: routePage })
    expect(dataSourcesRoute.meta).toMatchObject({
      id: 'monitoring-workbench-log-data-sources',
      path: '/monitoring-workbench/log-data-sources',
      menuId: 'monitoring-workbench-log-data-sources',
      permissionKey: 'observe.log-data-sources.view',
      scopeMode: 'passive',
      navVisible: true,
    })
    await expect(dataSourcesRoute.load()).resolves.toEqual({ default: dataSourcesPage })
  })
})
