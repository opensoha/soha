/** @vitest-environment jsdom */

import { ApiError } from '@/services/api-error'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import dayjs from 'dayjs'
import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { LogExplorer } from './log-explorer'

const apiMocks = vi.hoisted(() => ({
  queryLogs: vi.fn(),
  issueLogStreamTicket: vi.fn(),
  buildLogStreamURL: vi.fn(() => 'ws://soha.local/logs?stream_ticket=ticket-1'),
  logTargetKey: vi.fn((target: { kind: string }) => JSON.stringify(target)),
}))
const podMocks = vi.hoisted(() => ({ detail: vi.fn() }))
const downloadMocks = vi.hoisted(() => ({ downloadText: vi.fn() }))

vi.mock('./api', () => apiMocks)
vi.mock('@/features/platform', () => ({
  podQueries: { detail: podMocks.detail },
}))
vi.mock('@/utils/download', () => downloadMocks)
vi.mock('@/features/auth', () => ({
  hasPermission: () => false,
  usePermissionSnapshot: () => ({ data: { data: { permissionKeys: [] } } }),
}))

class WebSocketMock {
  static instances: WebSocketMock[] = []
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onopen: (() => void) | null = null
  readonly url: string

  constructor(url: string) {
    this.url = url
    WebSocketMock.instances.push(this)
  }

  close() {
    this.onclose?.()
  }
}

const roots: Root[] = []

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{`${location.pathname}${location.search}`}</output>
}

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('WebSocket', WebSocketMock)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
})

beforeEach(() => {
  vi.clearAllMocks()
  WebSocketMock.instances = []
  apiMocks.queryLogs.mockResolvedValue({
    entries: [
      {
        timestamp: '2026-07-31T02:00:00Z',
        message: 'server ready',
        source: { domain: 'kubernetes', podName: 'api-0', containerName: 'api' },
        sourceMode: 'runtime',
      },
    ],
    partial: false,
    truncated: false,
    scopeRestricted: false,
    coverage: { resolvedSources: 1, successfulSources: 1, failedSources: 0 },
  })
  apiMocks.issueLogStreamTicket.mockResolvedValue({
    ticket: 'ticket-1',
    expiresAt: '2026-07-31T02:01:00Z',
  })
  podMocks.detail.mockImplementation((scope, name) => ({
    queryKey: ['pods', 'detail', scope, name],
    queryFn: async () => ({ containers: [{ name: 'api' }, { name: 'sidecar' }] }),
    enabled: Boolean(name),
  }))
})

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function renderExplorer(element: React.ReactNode, initialEntry = '/') {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={client}>
          <MemoryRouter initialEntries={[initialEntry]}>
            {element}
            <LocationProbe />
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    if (client.isFetching() === 0 && WebSocketMock.instances.length > 0) break
  }
  return container
}

describe('LogExplorer', () => {
  it('clears snapshot entries and stops automatic reconnect after a refused stream ticket', async () => {
    apiMocks.issueLogStreamTicket.mockRejectedValueOnce(
      new ApiError(403, 'Ticket refused', { method: 'POST' }),
    )
    const container = await renderExplorer(
      <LogExplorer autoStart clusterId="cluster-a" embedded namespace="apps" />,
    )
    expect(container.textContent).toContain('实时日志连接无访问权限')
    expect(container.textContent).not.toContain('server ready')
    expect(WebSocketMock.instances).toHaveLength(0)
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 1100))
    })
    expect(apiMocks.issueLogStreamTicket).toHaveBeenCalledOnce()
    const reconnect = container.querySelector<HTMLButtonElement>('[aria-label="重新连接实时日志"]')
    expect(reconnect).not.toBeNull()
    await act(async () => {
      reconnect!.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(WebSocketMock.instances).toHaveLength(1)
    expect(container.textContent).not.toContain('server ready')
    await act(async () => {
      WebSocketMock.instances[0].onmessage?.({
        data: JSON.stringify({
          type: 'entry',
          entry: {
            timestamp: '2026-10-16T00:00:00Z',
            message: 'new authorized event',
            source: { domain: 'kubernetes', podName: 'api-0' },
            sourceMode: 'runtime',
          },
        }),
      })
    })
    expect(container.textContent).toContain('new authorized event')
  })

  it('loads an authorized snapshot and opens a query-bound live stream', async () => {
    const container = await renderExplorer(
      <LogExplorer
        autoStart
        clusterId="cluster-a"
        embedded
        namespace="apps"
        preset={{ workloadKind: 'Deployment', workloadName: 'api' }}
      />,
    )

    expect(container.textContent).toContain('server ready')
    expect(apiMocks.queryLogs).toHaveBeenCalledOnce()
    expect(apiMocks.issueLogStreamTicket).toHaveBeenCalledOnce()
    expect(WebSocketMock.instances[0]?.url).toContain('stream_ticket=ticket-1')
    expect(container.textContent).toContain('在日志中心打开')
    expect(container.textContent).not.toContain('Live')
    expect(container.textContent).not.toContain('History')
    expect(container.querySelector('.soha-log-results-explorer')).toBeNull()
    expect(container.querySelector('.soha-log-explorer.is-embedded')).not.toBeNull()
    expect(container.querySelector('.soha-log-results-card .ant-card-head')).toBeNull()
    const connectionStatus = container.querySelector('[role="status"][aria-live="polite"]')
    expect(connectionStatus).not.toBeNull()

    await act(async () => {
      WebSocketMock.instances[0]?.onopen?.()
      WebSocketMock.instances[0]?.onmessage?.({
        data: JSON.stringify({
          type: 'entry',
          entry: {
            timestamp: '2026-07-31T02:00:01Z',
            message: 'request handled',
            source: { domain: 'kubernetes', podName: 'api-1', containerName: 'api' },
            sourceMode: 'runtime',
          },
        }),
      })
      await Promise.resolve()
    })
    expect(container.textContent).toContain('request handled')

    const stop = container.querySelector<HTMLButtonElement>('[aria-label="停止实时日志"]')
    expect(stop).not.toBeNull()
    await act(async () => stop?.click())
    expect(container.textContent).toContain('实时会话已手动停止')
    expect(connectionStatus?.textContent).toContain('实时会话已手动停止')
  })

  it('queries durable history without opening a live stream', async () => {
    const longAttributeValue = 'cluster-segment-'.repeat(20)
    apiMocks.queryLogs
      .mockResolvedValueOnce({
        entries: [
          {
            timestamp: '2026-07-31T01:00:00Z',
            message: 'historical entry',
            severity: 'ERROR',
            stream: 'stderr',
            traceId: 'trace-1',
            attributes: { 'service.name': 'orders', 'k8s.long': longAttributeValue },
            source: {
              domain: 'kubernetes',
              namespace: 'apps',
              podName: 'api-0',
              containerName: 'api',
            },
            sourceMode: 'durable',
          },
        ],
        nextCursor: 'next-page',
        partial: false,
        truncated: true,
        scopeRestricted: false,
      })
      .mockResolvedValueOnce({
        entries: [
          {
            timestamp: '2026-07-31T00:59:59Z',
            message: 'older historical entry',
            source: {
              domain: 'kubernetes',
              namespace: 'apps',
              podName: 'api-0',
              containerName: 'api',
            },
            sourceMode: 'durable',
          },
        ],
        nextCursor: 'last-page',
        partial: false,
        truncated: false,
        scopeRestricted: false,
      })
      .mockResolvedValueOnce({
        entries: [],
        nextCursor: '',
        partial: false,
        truncated: false,
        scopeRestricted: false,
      })
    const container = await renderExplorer(
      <LogExplorer
        clusterId="cluster-a"
        namespace="apps"
        preset={{
          dataSourceId: 'loki-main',
          podNames: ['api-0'],
          containers: ['api'],
          text: 'timeout',
        }}
      />,
    )
    expect(container.textContent).not.toContain('Live')
    expect(container.textContent).not.toContain('History')
    expect(container.querySelector('.soha-log-query-card .ant-card-head')).toBeNull()
    expect(container.querySelector('.soha-log-results-card .ant-card-head')).toBeNull()
    expect(container.textContent).toContain('筛选查询')
    expect(container.textContent).toContain('高级语句')
    expect(container.querySelector('[aria-label="SohaQL 高级查询语句"]')).toBeNull()
    expect(container.querySelector('.soha-log-time-trigger')?.textContent).toContain('最近 15 分钟')
    expect(container.querySelector('.soha-log-query-range-picker')).toBeNull()
    expect(container.querySelector('.soha-log-query-mode-row [aria-label="行数"]')).not.toBeNull()
    expect(container.querySelector('.soha-log-query-limit-select')?.textContent).toContain('200 行')
    expect(container.textContent).not.toContain('每批')
    expect(container.querySelector('.soha-log-query-actions [aria-label="时间范围"]')).toBeNull()
    expect(container.textContent).not.toContain('工作负载类型')
    expect(container.textContent).not.toContain('工作负载名称')
    expect(container.querySelector('[aria-label="日志全文搜索"]')).not.toBeNull()
    expect(container.querySelector('input[placeholder="搜索所选 Pod 的日志内容"]')).not.toBeNull()
    expect(container.querySelector('[aria-label="容器"]')).not.toBeNull()
    expect(container.textContent).not.toContain('指定容器')
    expect(podMocks.detail).toHaveBeenCalledWith(
      { clusterId: 'cluster-a', namespace: 'apps' },
      'api-0',
    )

    const submit = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent?.includes('查询日志'),
    )
    await act(async () => {
      submit?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    for (
      let attempt = 0;
      attempt < 20 && !container.textContent?.includes('historical entry');
      attempt += 1
    ) {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
    }

    expect(container.textContent).toContain('historical entry')
    expect(container.querySelector('.soha-log-results-fields')?.textContent).toContain('显示字段')
    expect(container.querySelector('.soha-log-results-fields')?.textContent).toContain(
      'service.name',
    )
    expect(container.querySelector('.soha-log-result-row > summary')).not.toBeNull()
    expect(container.querySelector('.soha-log-result-detail')?.textContent).toContain('trace-1')
    expect(container.querySelector('.soha-log-result-detail')?.textContent).toContain(
      longAttributeValue,
    )
    expect(container.querySelector('.soha-log-result-detail dl')).not.toBeNull()
    expect(container.querySelector('.soha-log-result-header .is-source')).not.toBeNull()
    await act(async () => {
      container.querySelector<HTMLInputElement>('input[value="source"]')?.click()
    })
    expect(container.querySelector('.soha-log-result-header .is-source')).toBeNull()
    expect(apiMocks.queryLogs).toHaveBeenCalledOnce()
    expect(apiMocks.queryLogs.mock.calls[0]?.[1]).toMatchObject({
      dataSourceId: 'loki-main',
      sourceMode: 'durable',
      selector: { namespace: 'apps', podNames: ['api-0'], containers: ['api'] },
      direction: 'backward',
      text: 'timeout',
    })
    expect(apiMocks.queryLogs.mock.calls[0]?.[1]).toEqual(
      expect.objectContaining({ from: expect.any(String), to: expect.any(String) }),
    )
    const firstQuery = apiMocks.queryLogs.mock.calls[0]?.[1] as { from: string; to: string }
    expect(Date.parse(firstQuery.to) - Date.parse(firstQuery.from)).toBe(900_000)
    expect(apiMocks.issueLogStreamTicket).not.toHaveBeenCalled()
    expect(container.textContent).not.toContain('下一页')

    const resultTable = container.querySelector<HTMLElement>('.soha-log-results-table')
    Object.defineProperties(resultTable, {
      clientHeight: { configurable: true, value: 400 },
      scrollHeight: { configurable: true, value: 1000 },
      scrollTop: { configurable: true, value: 600 },
    })
    await act(async () => {
      resultTable?.dispatchEvent(new Event('scroll', { bubbles: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    for (
      let attempt = 0;
      attempt < 20 && !container.textContent?.includes('older historical entry');
      attempt += 1
    ) {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
    }

    expect(apiMocks.queryLogs).toHaveBeenCalledTimes(2)
    expect(apiMocks.queryLogs.mock.calls[1]?.[1]).toMatchObject({ cursor: 'next-page' })
    expect(container.textContent).toContain('older historical entry')

    await act(async () => {
      resultTable?.dispatchEvent(new Event('scroll', { bubbles: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    for (let attempt = 0; attempt < 20 && apiMocks.queryLogs.mock.calls.length < 3; attempt += 1) {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
    }

    expect(apiMocks.queryLogs.mock.calls[2]?.[1]).toMatchObject({ cursor: 'last-page' })
    for (
      let attempt = 0;
      attempt < 20 && !container.textContent?.includes('已加载全部日志');
      attempt += 1
    ) {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
    }
    const resultRows = container.querySelectorAll('.soha-log-result-row > summary')
    expect(resultRows[0]?.textContent).toContain('historical entry')
    expect(resultRows[1]?.textContent).toContain('older historical entry')
    expect(container.textContent).toContain('已加载全部日志')

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="导出当前快照"]')?.click()
    })
    expect(apiMocks.queryLogs).toHaveBeenCalledTimes(3)
    expect(downloadMocks.downloadText).toHaveBeenCalledOnce()
    const exported = String(downloadMocks.downloadText.mock.calls[0]?.[1])
    expect(exported).toContain('historical entry')
    expect(exported).toContain('older historical entry')
  })

  it('shows an absolute picker only for an explicit absolute range', async () => {
    const container = await renderExplorer(
      <LogExplorer
        clusterId="cluster-a"
        namespace="apps"
        preset={{ from: '2026-07-31T00:00:00Z', to: '2026-07-31T01:00:00Z' }}
      />,
    )

    const trigger = container.querySelector<HTMLButtonElement>('.soha-log-time-trigger')
    expect(trigger?.textContent).toContain(
      `${dayjs('2026-07-31T00:00:00Z').format('YYYY-MM-DD HH:mm')} - ${dayjs('2026-07-31T01:00:00Z').format('YYYY-MM-DD HH:mm')}`,
    )
    expect(document.querySelector('.soha-log-time-popover')).toBeNull()

    await act(async () => {
      trigger?.click()
      await Promise.resolve()
    })

    expect(document.querySelector('.soha-log-time-popover')?.textContent).toContain('相对时间')
    expect(document.querySelector('.soha-log-time-popover')?.textContent).toContain('绝对时间')
    expect(document.querySelector('.soha-log-query-range-picker')).toBeNull()

    const absoluteToggle = Array.from(
      document.querySelectorAll<HTMLButtonElement>('.soha-log-time-absolute-section button'),
    ).find((button) => button.textContent?.includes('绝对时间'))
    await act(async () => absoluteToggle?.click())
    expect(document.querySelectorAll('.soha-log-query-range-picker input')).toHaveLength(2)

    const relativeOption = Array.from(
      document.querySelectorAll<HTMLButtonElement>('.soha-log-time-relative-list button'),
    ).find((button) => button.textContent?.includes('最近 15 分钟'))
    await act(async () => relativeOption?.click())
    expect(trigger?.textContent).toContain('最近 15 分钟')
  })

  it('queries the current namespace when optional Pod and container filters are empty', async () => {
    apiMocks.queryLogs.mockResolvedValueOnce({
      entries: [],
      nextCursor: '',
      partial: false,
      truncated: false,
      scopeRestricted: false,
    })
    const container = await renderExplorer(
      <LogExplorer clusterId="cluster-a" namespace="apps" preset={{ text: 'request-42' }} />,
    )

    expect(container.textContent).toContain('Pod（可选）')
    expect(container.textContent).toContain('容器（可选）')
    expect(container.textContent).toContain('全部 Pod')
    expect(
      container.querySelector('input[placeholder="搜索当前命名空间全部 Pod 的日志内容"]'),
    ).not.toBeNull()

    const submit = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent?.includes('查询日志'),
    )
    await act(async () => {
      submit?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(apiMocks.queryLogs).toHaveBeenCalledOnce()
    expect(apiMocks.queryLogs.mock.calls[0]?.[1]).toMatchObject({
      sourceMode: 'durable',
      selector: { namespace: 'apps' },
      text: 'request-42',
    })
    expect(apiMocks.queryLogs.mock.calls[0]?.[1].selector.podNames).toBeUndefined()
    expect(apiMocks.queryLogs.mock.calls[0]?.[1].selector.containers).toBeUndefined()
  })

  it('preserves unrelated query context when syncing log filters', async () => {
    const container = await renderExplorer(
      <LogExplorer
        clusterId="cluster-a"
        namespace="apps"
        preset={{ text: 'request-42' }}
        syncURL
      />,
      '/monitoring-workbench/logs?signal=logs&compare=metrics&dashboardId=dash-1&panelId=panel-1&stepSeconds=30&var-region=cn-north&dataSourceId=prometheus&metricKey=http.requests&text=stale',
    )
    const form = container.querySelector<HTMLFormElement>('form')

    await act(async () => {
      form?.requestSubmit()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(apiMocks.queryLogs).toHaveBeenCalledOnce()
    const location = new URL(
      container.querySelector('[data-testid="location"]')?.textContent ?? '',
      'https://soha.local',
    )
    expect(location.pathname).toBe('/monitoring-workbench/logs')
    expect(Object.fromEntries(location.searchParams)).toMatchObject({
      signal: 'logs',
      compare: 'metrics',
      dashboardId: 'dash-1',
      panelId: 'panel-1',
      stepSeconds: '30',
      'var-region': 'cn-north',
      dataSourceId: 'prometheus',
      metricKey: 'http.requests',
      cluster: 'cluster-a',
      namespace: 'apps',
      text: 'request-42',
    })
  })

  it('opens a correlated Trace without dropping the log scope or absolute window', async () => {
    apiMocks.queryLogs.mockResolvedValueOnce({
      entries: [
        {
          timestamp: '2026-08-30T00:05:00Z',
          message: 'checkout failed',
          source: { domain: 'kubernetes', podName: 'api-0', containerName: 'api' },
          sourceMode: 'durable',
          traceId: 'trace-1',
          spanId: 'span-1',
        },
      ],
      partial: false,
      truncated: false,
      scopeRestricted: false,
      coverage: { resolvedSources: 1, successfulSources: 1, failedSources: 0 },
    })
    const from = '2026-08-30T00:00:00Z'
    const to = '2026-08-30T00:15:00Z'
    const container = await renderExplorer(
      <LogExplorer
        clusterId="cluster-a"
        namespace="apps"
        preset={{
          applicationId: 'shop',
          environmentId: 'prod',
          from,
          service: 'checkout',
          to,
          workloadKind: 'Deployment',
          workloadName: 'api',
        }}
        syncURL
      />,
      `/monitoring-workbench/logs?cluster=cluster-a&namespace=apps&application=shop&environment=prod&service=checkout&workloadKind=Deployment&workload=api&from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
    )

    await act(async () => {
      container.querySelector<HTMLFormElement>('form')?.requestSubmit()
    })
    for (
      let attempt = 0;
      attempt < 20 && !container.textContent?.includes('checkout failed');
      attempt += 1
    ) {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
    }
    const relatedTrace = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('查看关联链路'),
    )
    expect(relatedTrace).not.toBeUndefined()
    await act(async () => {
      relatedTrace?.click()
    })

    const location = new URL(
      container.querySelector('[data-testid="location"]')?.textContent ?? '',
      'https://soha.local',
    )
    expect(location.pathname).toBe('/monitoring-workbench/traces')
    expect(Object.fromEntries(location.searchParams)).toMatchObject({
      cluster: 'cluster-a',
      namespace: 'apps',
      application: 'shop',
      environment: 'prod',
      service: 'checkout',
      workload: 'api',
      traceId: 'trace-1',
      spanId: 'span-1',
      from: '2026-08-30T00:00:00.000Z',
      to: '2026-08-30T00:15:00.000Z',
    })
  })

  it('queries durable history across all namespaces when no namespace is selected', async () => {
    apiMocks.queryLogs.mockResolvedValueOnce({
      entries: [],
      nextCursor: '',
      partial: false,
      truncated: false,
      scopeRestricted: false,
    })
    const container = await renderExplorer(<LogExplorer clusterId="cluster-a" namespace="" />)
    const submit = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent?.includes('查询日志'),
    )

    expect(submit?.disabled).toBe(false)
    await act(async () => {
      submit?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(apiMocks.queryLogs).toHaveBeenCalledWith(
      { kind: 'cluster', clusterId: 'cluster-a', namespace: '' },
      expect.objectContaining({
        sourceMode: 'durable',
        selector: expect.not.objectContaining({ namespace: expect.anything() }),
      }),
      expect.any(AbortSignal),
    )
  })

  it('keeps the advanced SohaQL editor available from the visual filter builder', async () => {
    const container = await renderExplorer(
      <LogExplorer clusterId="cluster-a" namespace="apps" preset={{ podNames: ['api-0'] }} />,
    )
    const advanced = Array.from(
      container.querySelectorAll<HTMLElement>('.ant-segmented-item'),
    ).find((item) => item.textContent?.includes('高级语句'))

    expect(container.textContent).not.toContain('工作负载类型')
    expect(container.textContent).not.toContain('工作负载名称')
    expect(advanced).toBeDefined()
    await act(async () => advanced?.click())

    expect(
      container.querySelector<HTMLTextAreaElement>('[aria-label="SohaQL 高级查询语句"]')?.value,
    ).toBe('{pod="api-0"}')
    expect(container.textContent).toContain('SohaQL')

    const builder = Array.from(container.querySelectorAll<HTMLElement>('.ant-segmented-item')).find(
      (item) => item.textContent?.includes('筛选查询'),
    )

    expect(builder).toBeDefined()
    await act(async () => builder?.click())

    expect(container.textContent).not.toContain('工作负载类型')
    expect(container.textContent).not.toContain('工作负载名称')
    expect(container.textContent).toContain('api-0')
    expect(container.querySelector('[aria-label="SohaQL 高级查询语句"]')).toBeNull()
  })

  it('keeps multiple builder filters intact when advanced SohaQL cannot represent them', async () => {
    const container = await renderExplorer(
      <LogExplorer
        clusterId="cluster-a"
        namespace="apps"
        preset={{ podNames: ['api-0', 'api-1'] }}
      />,
    )
    const advanced = Array.from(
      container.querySelectorAll<HTMLElement>('.ant-segmented-item'),
    ).find((item) => item.textContent?.includes('高级语句'))

    await act(async () => advanced?.click())

    expect(container.querySelector('[aria-label="SohaQL 高级查询语句"]')).toBeNull()
    expect(container.textContent).toContain('api-0')
    expect(container.textContent).toContain('api-1')
  })

  it('updates the query editor when an in-place deep link changes', async () => {
    function Harness() {
      const [podName, setPodName] = useState('api-0')
      return (
        <>
          <button onClick={() => setPodName('api-1')}>切换深链</button>
          <LogExplorer clusterId="cluster-a" namespace="apps" preset={{ podNames: [podName] }} />
        </>
      )
    }

    const container = await renderExplorer(<Harness />)
    const advanced = Array.from(
      container.querySelectorAll<HTMLElement>('.ant-segmented-item'),
    ).find((item) => item.textContent?.includes('高级语句'))
    await act(async () => advanced?.click())
    const query = () =>
      container.querySelector<HTMLTextAreaElement>('[aria-label="SohaQL 高级查询语句"]')

    expect(query()?.value).toBe('{pod="api-0"}')
    await act(async () => {
      Array.from(container.querySelectorAll('button'))
        .find((button) => button.textContent === '切换深链')
        ?.click()
    })
    expect(query()?.value).toBe('{pod="api-1"}')
  })

  it('opens the log center with the current embedded filters', async () => {
    const container = await renderExplorer(
      <LogExplorer
        clusterId="cluster-a"
        embedded
        namespace="apps"
        preset={{ podNames: ['api-0'] }}
      />,
    )
    const textFilter = container.querySelector<HTMLInputElement>(
      'input[placeholder="服务端文本匹配"]',
    )

    await act(async () => {
      if (textFilter) {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(
          textFilter,
          'timeout',
        )
      }
      textFilter?.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => {
      Array.from(container.querySelectorAll('button'))
        .find((button) => button.textContent?.includes('在日志中心打开'))
        ?.click()
    })

    expect(container.querySelector('[data-testid="location"]')?.textContent).toContain(
      '/monitoring-workbench/logs?cluster=cluster-a&namespace=apps&text=timeout&range=900&tail=200&allContainers=true&pod=api-0',
    )
  })

  it('uses the shared explorer for a Docker project service', async () => {
    const container = await renderExplorer(
      <LogExplorer
        autoStart
        embedded
        scopeControl={<span>服务 api</span>}
        target={{ kind: 'docker', projectId: 'project-1', serviceName: 'api' }}
      />,
    )

    expect(apiMocks.queryLogs).toHaveBeenCalledWith(
      { kind: 'docker', projectId: 'project-1', serviceName: 'api' },
      expect.objectContaining({ selector: { dockerService: 'api' }, sourceMode: 'runtime' }),
      expect.any(AbortSignal),
    )
    expect(apiMocks.issueLogStreamTicket).toHaveBeenCalled()
    expect(container.textContent).not.toContain('工作负载类型')
    expect(container.textContent).not.toContain('History')
    expect(container.querySelector('.soha-log-query-card')).toBeNull()
    expect(container.textContent).not.toContain('文本筛选')
    expect(container.textContent).not.toContain('时间范围')
    expect(container.textContent).not.toContain('每个来源读取行数')
    expect(container.textContent).toContain('在日志中心打开')
    expect(container.textContent).not.toContain('查询并连接')
    expect(container.querySelector('.soha-log-results-toolbar')).not.toBeNull()
    expect(container.querySelector('.soha-log-results-toolbar')?.textContent).toContain('服务 api')
    expect(container.textContent).toContain('自动滚动')

    await act(async () => {
      Array.from(container.querySelectorAll('button'))
        .find((button) => button.textContent?.includes('在日志中心打开'))
        ?.click()
    })
    expect(container.querySelector('[data-testid="location"]')?.textContent).toContain(
      '/monitoring-workbench/logs?source=docker&dockerProject=project-1&dockerService=api',
    )
  })

  it('queries delivery history without forcing the default namespace', async () => {
    const target = {
      kind: 'delivery' as const,
      applicationId: 'app-1',
      environmentId: 'binding-1',
      namespace: '',
    }
    const container = await renderExplorer(<LogExplorer target={target} />)
    const submit = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent?.includes('查询日志'),
    )

    await act(async () => {
      submit?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(apiMocks.queryLogs).toHaveBeenCalledWith(
      target,
      expect.objectContaining({
        sourceMode: 'durable',
        selector: expect.not.objectContaining({ namespace: expect.anything() }),
      }),
      expect.any(AbortSignal),
    )
    expect(apiMocks.issueLogStreamTicket).not.toHaveBeenCalled()
  })
})
