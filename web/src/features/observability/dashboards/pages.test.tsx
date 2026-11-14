/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { DashboardMetricSeries, ObservabilityDashboardDetailPage } from './detail-page'
import { ObservabilityDashboardsPage } from './list-page'

const apiMocks = vi.hoisted(() => ({
  deleteDashboard: vi.fn(),
  getDashboard: vi.fn(),
  listDashboards: vi.fn(),
  listMetricDataSources: vi.fn(),
  queryDashboardPanel: vi.fn(),
}))
const aiMocks = vi.hoisted(() => ({ useAIPageContext: vi.fn() }))

vi.mock('./api', () => apiMocks)
vi.mock('@/features/copilot', () => aiMocks)
vi.mock('@visactor/react-vchart', () => ({ LineChart: () => null }))
vi.mock('@/components/management-data-page', () => ({
  ManagementDataPage: ({
    header,
    tableNode,
  }: {
    header?: { actions?: React.ReactNode }
    tableNode?: React.ReactNode
  }) => (
    <div>
      {header?.actions}
      {tableNode}
    </div>
  ),
}))
vi.mock('./import-modal', () => ({ ImportDashboardModal: () => null }))
vi.mock('@/features/auth', () => ({
  hasPermission: () => false,
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))

let container: HTMLDivElement
let root: Root

beforeAll(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))

beforeEach(() => {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  apiMocks.listDashboards.mockResolvedValue([])
  apiMocks.listMetricDataSources.mockResolvedValue([])
})

afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  vi.clearAllMocks()
})

async function renderPage(page: React.ReactNode, route: string, path: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={[route]}>
            <Routes>
              <Route path={path} element={page} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

describe('dashboard AI context', () => {
  it('registers list evidence without inventing resource scope', async () => {
    apiMocks.listDashboards.mockResolvedValueOnce([
      {
        dataSourceId: 'prom-main',
        id: 'dash-1',
        name: 'Checkout RED',
        panels: [],
        source: 'grafana',
        tags: [],
        updatedAt: '2026-08-30T00:00:00Z',
      },
    ])
    apiMocks.listMetricDataSources.mockResolvedValueOnce([{ id: 'prom-main', name: 'Prometheus' }])

    await renderPage(
      <ObservabilityDashboardsPage />,
      '/monitoring-workbench/dashboards',
      '/monitoring-workbench/dashboards',
    )

    expect(aiMocks.useAIPageContext).toHaveBeenLastCalledWith(
      expect.objectContaining({
        entityKind: 'monitoring.dashboard.list',
        pinnedData: { dashboardCount: 1, dataSourceCount: 1 },
        sourceWorkbench: 'monitoring',
      }),
    )
  })

  it('registers dashboard playback, variables and bound datasource as entity evidence', async () => {
    apiMocks.queryDashboardPanel.mockResolvedValueOnce({ series: [] })
    apiMocks.getDashboard.mockResolvedValueOnce({
      dataSourceId: 'prom-main',
      id: 'dash-1',
      importWarnings: [],
      name: 'Checkout RED',
      panels: [
        {
          id: 'cpu',
          layout: { h: 8, w: 12, x: 0, y: 0 },
          queryable: true,
          targets: [{ expression: 'up', refId: 'A' }],
          title: 'CPU',
          type: 'timeseries',
        },
      ],
      source: 'grafana',
      tags: [],
      updatedAt: '2026-08-30T00:00:00Z',
      variables: [{ defaultValue: 'cn', label: 'Region', name: 'region', options: ['cn', 'us'] }],
    })
    apiMocks.listMetricDataSources.mockResolvedValueOnce([{ id: 'prom-main', name: 'Prometheus' }])

    await renderPage(
      <ObservabilityDashboardDetailPage />,
      '/monitoring-workbench/dashboards/dash-1?from=2026-08-30T00%3A00%3A00Z&to=2026-08-30T00%3A15%3A00Z&range=15&var-region=cn',
      '/monitoring-workbench/dashboards/:dashboardId',
    )

    const context = aiMocks.useAIPageContext.mock.calls.slice(-1)[0]?.[0]
    expect(context).toEqual(
      expect.objectContaining({
        entityKind: 'monitoring.dashboard',
        entityName: 'Checkout RED',
        pinnedData: { dashboardId: 'dash-1', dataSourceId: 'prom-main', panelCount: 1 },
        sourceWorkbench: 'monitoring',
        timeRangeMinutes: 15,
        visibleFilters: {
          region: 'cn',
          timeFrom: '2026-08-30T00:00:00.000Z',
          timeTo: '2026-08-30T00:15:00.000Z',
        },
      }),
    )
    expect(context).not.toHaveProperty('dataSourceId')
    expect(context).not.toHaveProperty('sourceRoute')
    expect(container.querySelector('[aria-label="在 Explore 打开 CPU"]')).toBeNull()

    const playbackButton = container.querySelector<HTMLButtonElement>('[aria-label="播放时间窗"]')
    expect(playbackButton?.getAttribute('aria-pressed')).toBe('false')
    expect(container.querySelector('[role="status"][aria-live="polite"]')?.textContent).toContain(
      '自动播放：已暂停',
    )

    await act(async () => playbackButton?.click())

    const pauseButton = container.querySelector<HTMLButtonElement>('[aria-label="暂停播放"]')
    expect(pauseButton?.getAttribute('aria-pressed')).toBe('true')
    expect(container.querySelector('[role="status"][aria-live="polite"]')?.textContent).toContain(
      '自动播放：进行中',
    )
  })

  it('provides a data-table equivalent for dashboard time-series charts', async () => {
    await renderPage(
      <DashboardMetricSeries
        panelType="timeseries"
        series={[
          {
            key: 'error-rate',
            label: '错误率',
            latest: 0.02,
            points: [{ timestamp: '2026-08-30T00:15:00Z', value: 0.02 }],
            unit: '%',
          },
        ]}
      />,
      '/',
      '/',
    )

    expect(container.querySelector('[role="region"][aria-label="仪表盘指标数据表"]')).toBeNull()
    await act(async () => {
      const details = container.querySelector('details')
      if (details) {
        details.open = true
        details.dispatchEvent(new Event('toggle'))
      }
    })

    const dataRegion = container.querySelector('[role="region"][aria-label="仪表盘指标数据表"]')
    expect(dataRegion).not.toBeNull()
    expect(dataRegion?.querySelector('table')).not.toBeNull()
    expect(dataRegion?.textContent).toContain('错误率')
    expect(dataRegion?.textContent).toContain('2026')
    expect(container.querySelector('.soha-dashboard-chart')?.getAttribute('aria-hidden')).toBe(
      'true',
    )
  })
})
