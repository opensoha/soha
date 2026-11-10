/** @vitest-environment jsdom */

import { ApiError } from '@/services/api-error'
import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { ObservabilityMetricsPage } from './metrics-page'
import { ObservabilityServicesPage } from './services-page'
import { ObservabilityTracesPage } from './traces-page'

const apiMocks = vi.hoisted(() => ({
  getService: vi.fn(),
  getServiceTopology: vi.fn(),
  listMetricCatalog: vi.fn(),
  listServices: vi.fn(),
  queryMetrics: vi.fn(),
  queryTraces: vi.fn(),
}))
const aiMocks = vi.hoisted(() => ({ useAIPageContext: vi.fn() }))

vi.mock('./api', () => apiMocks)
vi.mock('@/features/copilot', () => aiMocks)
vi.mock('@visactor/react-vchart', () => ({ LineChart: () => null }))
vi.mock('@/components/admin-table', () => ({
  AdminTable: ({ columns, dataSource, expandedRowRender }: any) => {
    const actionColumn = columns?.find((column: any) => column.key === 'actions')
    return (
      <div>
        {dataSource.map((record: any, index: number) => (
          <div key={record.id ?? record.spanId ?? index}>
            {actionColumn?.render?.(undefined, record, index)}
          </div>
        ))}
        {dataSource[0] && expandedRowRender ? expandedRowRender(dataSource[0]) : null}
      </div>
    )
  },
}))
vi.mock('@/components/platform-scope-toolbar', () => ({ PlatformScopeToolbar: () => null }))
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => ({ clusterId: null, namespace: null }),
}))

let container: HTMLDivElement
let root: Root

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{`${location.pathname}${location.search}`}</output>
}

beforeAll(() => {
  const getComputedStyle = window.getComputedStyle.bind(window)
  class ResizeObserverMock {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn(() => ({
      matches: false,
      media: '',
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  })
})

beforeEach(() => {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  apiMocks.listMetricCatalog.mockResolvedValue([
    { available: true, key: 'error_rate', label: '错误率' },
  ])
  apiMocks.listServices.mockResolvedValue({ items: [], meta: { state: 'empty' } })
  apiMocks.queryMetrics.mockResolvedValue({ meta: { state: 'empty' }, series: [] })
  apiMocks.queryTraces.mockResolvedValue({ meta: { state: 'empty' }, services: [], spans: [] })
})

afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  vi.clearAllMocks()
})

async function renderPage(page: React.ReactNode, route: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={[route]}>
            {page}
            <LocationProbe />
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

describe('observability signal evidence links', () => {
  it.each([
    {
      entityKind: 'monitoring.signal.services',
      page: <ObservabilityServicesPage />,
      route:
        '/monitoring-workbench/services?dataSourceId=oap-main&cluster=cluster-a&namespace=apps&service=checkout&from=2026-08-30T00%3A00%3A00Z&to=2026-08-30T00%3A15%3A00Z',
      visibleFilters: { dataSourceId: 'oap-main', service: 'checkout' },
    },
    {
      entityKind: 'monitoring.signal.metrics',
      page: <ObservabilityMetricsPage />,
      route:
        '/monitoring-workbench/metrics?dataSourceId=prom-main&cluster=cluster-a&namespace=apps&service=checkout&workload=api&metricKey=error_rate&from=2026-08-30T00%3A00%3A00Z&to=2026-08-30T00%3A15%3A00Z',
      visibleFilters: { dataSourceId: 'prom-main', metricKey: 'error_rate' },
    },
    {
      entityKind: 'monitoring.signal.traces',
      page: <ObservabilityTracesPage />,
      route:
        '/monitoring-workbench/traces?dataSourceId=jaeger-main&cluster=cluster-a&namespace=apps&service=checkout&workload=api&traceId=trace-1&from=2026-08-30T00%3A00%3A00Z&to=2026-08-30T00%3A15%3A00Z',
      visibleFilters: { dataSourceId: 'jaeger-main', traceId: 'trace-1' },
    },
  ])('registers $entityKind with exact investigation context', async (testCase) => {
    await renderPage(testCase.page, testCase.route)

    const context = aiMocks.useAIPageContext.mock.calls.slice(-1)[0]?.[0]
    expect(context).toEqual(
      expect.objectContaining({
        clusterId: 'cluster-a',
        entityKind: testCase.entityKind,
        namespace: 'apps',
        service: 'checkout',
        sourceWorkbench: 'monitoring',
        visibleFilters: expect.objectContaining({
          ...testCase.visibleFilters,
          timeFrom: '2026-08-30T00:00:00Z',
          timeTo: '2026-08-30T00:15:00Z',
        }),
      }),
    )
    expect(context).not.toHaveProperty('dataSourceId')
    expect(context).not.toHaveProperty('sourceRoute')
  })

  it('explains the query Provider requirement before submitting a trace query', async () => {
    await renderPage(<ObservabilityTracesPage />, '/monitoring-workbench/traces')

    expect(container.textContent).toContain('OpenTelemetry 负责采集与导出')
    expect(container.textContent).toContain('链路查询需要可用的查询 Provider')
    expect(apiMocks.queryTraces).not.toHaveBeenCalled()
  })

  it('surfaces unavailable trace queries from a legacy evidence link', async () => {
    apiMocks.queryTraces.mockRejectedValue(new ApiError(404, '未找到对应资源', { code: 'not_found' }))
    await renderPage(
      <ObservabilityTracesPage />,
      '/monitoring-workbench/traces?dataSourceId=jaeger-main&traceId=trace-1&from=2026-08-30T00%3A00%3A00Z&to=2026-08-30T00%3A15%3A00Z',
    )

    expect(apiMocks.queryTraces).toHaveBeenCalledWith(
      expect.objectContaining({ dataSourceId: 'jaeger-main', traceId: 'trace-1' }),
    )
    expect(container.textContent).toContain('未找到对应资源')
    expect(container.textContent).toContain('请确认链路查询 Provider')
    expect(container.textContent).not.toContain('当前范围暂无链路')
  })

  it('explains the Provider requirement when the service list cannot resolve a source', async () => {
    apiMocks.listServices.mockRejectedValue(new ApiError(404, '未找到对应资源', { code: 'not_found' }))
    await renderPage(<ObservabilityServicesPage />, '/monitoring-workbench/services')

    expect(container.textContent).toContain('请确认服务查询 Provider')
  })

  it('runs a metric evidence link against its exact datasource and absolute window', async () => {
    await renderPage(
      <ObservabilityMetricsPage />,
      '/monitoring-workbench/metrics?dataSourceId=prom-main&cluster=cluster-a&namespace=apps&service=checkout&workload=api&metricKey=error_rate&from=2026-08-30T00%3A00%3A00Z&to=2026-08-30T00%3A15%3A00Z',
    )

    expect(apiMocks.queryMetrics.mock.calls[0]?.[0]).toEqual({
      dataSourceId: 'prom-main',
      metricKey: 'error_rate',
      scope: {
        clusterId: 'cluster-a',
        namespace: 'apps',
        service: 'checkout',
        workload: 'api',
      },
      stepSeconds: 60,
      timeFrom: '2026-08-30T00:00:00.000Z',
      timeTo: '2026-08-30T00:15:00.000Z',
    })
  })

  it('runs a trace evidence link against its exact datasource and absolute window', async () => {
    await renderPage(
      <ObservabilityTracesPage />,
      '/monitoring-workbench/traces?dataSourceId=jaeger-main&cluster=cluster-a&namespace=apps&service=checkout&workload=api&traceId=trace-1&from=2026-08-30T00%3A00%3A00Z&to=2026-08-30T00%3A15%3A00Z',
    )

    expect(apiMocks.queryTraces.mock.calls[0]?.[0]).toEqual({
      dataSourceId: 'jaeger-main',
      limit: 100,
      minDurationMs: 0,
      scope: {
        clusterId: 'cluster-a',
        namespace: 'apps',
        service: 'checkout',
        workload: 'api',
      },
      timeFrom: '2026-08-30T00:00:00.000Z',
      timeTo: '2026-08-30T00:15:00.000Z',
      traceId: 'trace-1',
    })
  })

  it('opens correlated logs without dropping the trace scope or absolute window', async () => {
    apiMocks.queryTraces.mockResolvedValueOnce({
      backendType: 'jaeger',
      dataSourceId: 'jaeger-main',
      meta: { state: 'success' },
      services: ['checkout'],
      spans: [
        {
          durationMs: 120,
          error: true,
          operation: 'GET /checkout',
          service: 'checkout',
          spanId: 'span-1',
          startTime: '2026-08-30T00:00:00Z',
          tags: {},
          traceId: 'trace-1',
        },
      ],
    })

    await renderPage(
      <ObservabilityTracesPage />,
      '/monitoring-workbench/traces?dataSourceId=jaeger-main&cluster=cluster-a&namespace=apps&application=shop&environment=prod&service=checkout&workload=api&traceId=trace-1&from=2026-08-30T00%3A00%3A00Z&to=2026-08-30T00%3A15%3A00Z',
    )

    const relatedLogs = container.querySelector<HTMLButtonElement>(
      'button[aria-label="查看关联日志"]',
    )
    expect(relatedLogs).not.toBeNull()
    await act(async () => relatedLogs?.click())

    const location = new URL(
      container.querySelector('[data-testid="location"]')?.textContent ?? '',
      'https://soha.local',
    )
    expect(location.pathname).toBe('/monitoring-workbench/logs')
    expect(Object.fromEntries(location.searchParams)).toMatchObject({
      cluster: 'cluster-a',
      namespace: 'apps',
      application: 'shop',
      environment: 'prod',
      service: 'checkout',
      workload: 'api',
      traceId: 'trace-1',
      spanId: 'span-1',
      from: '2026-08-30T00:00:00Z',
      to: '2026-08-30T00:15:00Z',
    })
    expect(location.searchParams.has('dataSourceId')).toBe(false)
  })

  it('provides a text summary and native table equivalent for metric charts', async () => {
    apiMocks.queryMetrics.mockResolvedValueOnce({
      dataSourceId: 'prom-main',
      backendType: 'prometheus',
      meta: { state: 'success' },
      series: [
        {
          key: 'error_rate',
          label: '错误率',
          latest: 0.02,
          points: [
            { timestamp: '2026-08-30T00:00:00Z', value: 0.01 },
            { timestamp: '2026-08-30T00:15:00Z', value: 0.02 },
          ],
          unit: '%',
        },
      ],
    })

    await renderPage(
      <ObservabilityMetricsPage />,
      '/monitoring-workbench/metrics?metricKey=error_rate&from=2026-08-30T00%3A00%3A00Z&to=2026-08-30T00%3A15%3A00Z',
    )

    expect(container.textContent).toContain('区间共 2 个数据点')
    expect(container.querySelector('table[aria-label="错误率数据表"]')).toBeNull()
    await act(async () => {
      const details = container.querySelector('details')
      if (details) {
        details.open = true
        details.dispatchEvent(new Event('toggle'))
      }
    })
    expect(container.querySelector('table[aria-label="错误率数据表"]')).not.toBeNull()
  })

  it('exposes trace waterfall rows to keyboard and assistive technology', async () => {
    apiMocks.queryTraces.mockResolvedValueOnce({
      backendType: 'jaeger',
      dataSourceId: 'jaeger-main',
      meta: { state: 'success' },
      services: ['checkout'],
      spans: [
        {
          durationMs: 120,
          error: false,
          operation: 'GET /checkout',
          service: 'checkout',
          spanId: 'span-1',
          startTime: '2026-08-30T00:00:00Z',
          tags: {},
          traceId: 'trace-1',
        },
      ],
      summary: 'one trace',
    })

    await renderPage(
      <ObservabilityTracesPage />,
      '/monitoring-workbench/traces?traceId=trace-1&from=2026-08-30T00%3A00%3A00Z&to=2026-08-30T00%3A15%3A00Z',
    )

    expect(
      container.querySelector(
        '[role="listitem"][tabindex="0"][aria-label*="checkout GET /checkout 120.0 ms"]',
      ),
    ).not.toBeNull()
  })

  it('renders complete service dependency evidence as a native table', async () => {
    const service = {
      displayName: 'Checkout',
      endpoints: [],
      id: 'checkout',
      instances: [],
      name: 'checkout',
      status: 'healthy',
    }
    apiMocks.listServices.mockResolvedValueOnce({ items: [service], meta: { state: 'success' } })
    apiMocks.getService.mockResolvedValueOnce({ data: service, meta: { state: 'success' } })
    apiMocks.getServiceTopology.mockResolvedValueOnce({
      data: {
        beta: true,
        edges: [
          {
            errorRate: 0.025,
            latencyP95Ms: 87.3,
            requestRate: 12.5,
            sourceServiceId: 'checkout',
            status: 'healthy',
            targetServiceId: 'payments',
          },
        ],
        meta: { state: 'success' },
        nodes: [],
      },
    })

    await renderPage(<ObservabilityServicesPage />, '/monitoring-workbench/services')
    const detailButton = container.querySelector<HTMLButtonElement>(
      'button[aria-label="查看服务详情"]',
    )
    expect(detailButton).not.toBeNull()
    await act(async () => detailButton?.click())
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    const table = document.querySelector('table[aria-label="服务依赖数据表"]')
    expect(table).not.toBeNull()
    expect(table?.textContent).toContain('checkoutpayments健康12.50/s2.50%87.3 ms')
  })
})
