import { describe, expect, it, vi } from 'vitest'
import { observabilityDashboardRoutes } from './routes'

const listPage = vi.hoisted(() => () => null)
const detailPage = vi.hoisted(() => () => null)
vi.mock('./list-page', () => ({ ObservabilityDashboardsPage: listPage }))
vi.mock('./detail-page', () => ({ ObservabilityDashboardDetailPage: detailPage }))

describe('observability dashboard routes', () => {
  it('keeps list and detail routes leaf-loaded', async () => {
    expect(observabilityDashboardRoutes.map((route) => route.meta.path)).toEqual([
      '/monitoring-workbench/dashboards',
      '/monitoring-workbench/dashboards/:dashboardId',
    ])
    expect(observabilityDashboardRoutes[0].meta.permissionKey).toBe('observe.monitoring.view')
    expect(observabilityDashboardRoutes[1].meta.navVisible).toBe(false)
    await expect(observabilityDashboardRoutes[0].load()).resolves.toEqual({
      default: expect.any(Function),
    })
    await expect(observabilityDashboardRoutes[1].load()).resolves.toEqual({
      default: expect.any(Function),
    })
  })
})
