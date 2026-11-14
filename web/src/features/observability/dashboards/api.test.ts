import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  deleteDashboard,
  getDashboard,
  importGrafanaDashboard,
  listDashboards,
  listMetricDataSources,
  queryDashboardPanel,
} from './api'

const get = vi.hoisted(() => vi.fn())
const post = vi.hoisted(() => vi.fn())
const remove = vi.hoisted(() => vi.fn())
vi.mock('@/services/api-client', () => ({ api: { delete: remove, get, post } }))

describe('observability dashboard api', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    get.mockResolvedValue({ data: [] })
    post.mockResolvedValue({ data: { series: [] } })
    remove.mockResolvedValue({ data: { status: 'deleted' } })
  })

  it('uses the dashboard import and persisted panel query endpoints', async () => {
    const importInput = { json: '{"title":"Imported"}', dataSourceId: 'prometheus-main' }
    const queryInput = {
      timeFrom: '2026-08-08T00:00:00Z',
      timeTo: '2026-08-08T01:00:00Z',
      stepSeconds: 60,
    }

    await listDashboards()
    await listMetricDataSources()
    await getDashboard('dashboard:1')
    await importGrafanaDashboard(importInput)
    await queryDashboardPanel('dashboard:1', 'panel/1', queryInput)
    await deleteDashboard('dashboard:1')

    expect(get).toHaveBeenNthCalledWith(1, '/observability/dashboards')
    expect(get).toHaveBeenNthCalledWith(2, '/observability/metrics/data-sources')
    expect(get).toHaveBeenNthCalledWith(3, '/observability/dashboards/dashboard%3A1')
    expect(post).toHaveBeenNthCalledWith(1, '/observability/dashboards/imports', importInput)
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/observability/dashboards/dashboard%3A1/panels/panel%2F1/query',
      queryInput,
    )
    expect(remove).toHaveBeenCalledWith('/observability/dashboards/dashboard%3A1')
  })
})
