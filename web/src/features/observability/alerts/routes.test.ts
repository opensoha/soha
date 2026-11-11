import { describe, expect, it, vi } from 'vitest'
import { observabilityAlertRoutes } from './routes'

const listPage = vi.hoisted(() => () => null)
const detailPage = vi.hoisted(() => () => null)
vi.mock('./page', () => ({ AlertsPage: listPage }))
vi.mock('./detail-page', () => ({ AlertEventDetailPage: detailPage }))

describe('alert route manifest', () => {
  it('loads list and detail from separate leaf modules', async () => {
    expect(observabilityAlertRoutes.map((route) => route.meta.path)).toEqual([
      '/monitoring-workbench/alerts',
      '/monitoring-workbench/alerts/:eventId',
      '/observability/alerts/:eventId',
    ])
    await expect(observabilityAlertRoutes[0].load()).resolves.toEqual({ default: listPage })
    await expect(observabilityAlertRoutes[1].load()).resolves.toEqual({ default: detailPage })
    await expect(observabilityAlertRoutes[2].load()).resolves.toEqual({ default: detailPage })
  })
})
