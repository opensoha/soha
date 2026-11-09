/** @vitest-environment jsdom */

import { act } from 'react'
import { ApiError } from '@/services/api-error'
import type { NetworkProxyCloseInput } from '@opensoha/contracts/gen/ts/sohaapi'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import ProxyConnectionsPage from './connections-page'
import ProxyOverviewPage from './overview-page'

const state = vi.hoisted(() => ({
  permissions: [] as string[],
  instance: {
    id: 'proxy-mihomo',
    name: 'Mihomo',
    engine: 'mihomo',
    processEpoch: 'test-epoch',
    status: 'online',
    capabilities: ['traffic', 'connections', 'close_connection'],
    desiredRevision: 1,
    observedRevision: 1,
    createdAt: '2026-09-27T00:00:00Z',
    updatedAt: '2026-09-27T00:00:00Z',
  },
}))
const apiMocks = vi.hoisted(() => ({
  getConnections: vi.fn(),
  getTraffic: vi.fn(),
  closeConnection: vi.fn(),
  getCloseCommand: vi.fn(),
}))

vi.mock('@/features/auth', () => ({
  hasPermission: (_snapshot: unknown, key: string) => state.permissions.includes(key),
}))
vi.mock('./context', () => ({
  useProxyContext: () => ({
    allowed: state.permissions.includes('network_access.proxy_instances.view'),
    permissions: { data: { data: {} }, isLoading: false },
    instances: { data: [state.instance], isLoading: false, isError: false },
    selected: state.instance,
    select: vi.fn(),
  }),
  ProxyInstanceHeader: () => <div>Mihomo</div>,
}))
vi.mock('./api', () => apiMocks)
vi.mock('@visactor/react-vchart', () => ({ LineChart: () => <div>chart</div> }))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

async function renderPage(page: 'connections' | 'overview') {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={client}>
          <MemoryRouter>
            {page === 'connections' ? <ProxyConnectionsPage /> : <ProxyOverviewPage />}
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await act(async () => new Promise((resolve) => window.setTimeout(resolve, 0)))
}

describe('proxy runtime pages', () => {
  beforeAll(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
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
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    })
    const getComputedStyle = window.getComputedStyle.bind(window)
    vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
  })

  beforeEach(() => {
    state.permissions = []
    state.instance.engine = 'mihomo'
    state.instance.processEpoch = 'test-epoch'
    state.instance.capabilities = ['traffic', 'connections', 'close_connection']
    apiMocks.getConnections.mockResolvedValue({
      instanceId: state.instance.id,
      state: 'available',
      processEpoch: 'test-epoch',
      freshness: 'fresh',
      connections: [],
    })
    apiMocks.getTraffic.mockResolvedValue({
      instanceId: state.instance.id,
      supported: true,
      samples: [],
    })
    vi.clearAllMocks()
  })

  afterEach(async () => {
    await act(async () => root?.unmount())
    container?.remove()
  })

  it('does not query traffic or connections without view permission', async () => {
    await renderPage('overview')
    expect(apiMocks.getTraffic).not.toHaveBeenCalled()
    expect(container.textContent).toContain('权限')
    await act(async () => root.unmount())
    container.remove()
    await renderPage('connections')
    expect(apiMocks.getConnections).not.toHaveBeenCalled()
  })

  it('shows V2Ray connection limits without a connection table', async () => {
    state.permissions = [
      'network_access.proxy_instances.view',
      'network_access.proxy_connections.view',
    ]
    state.instance.engine = 'v2ray'
    state.instance.capabilities = ['traffic']
    apiMocks.getConnections.mockResolvedValue({
      instanceId: state.instance.id,
      state: 'unsupported',
      connections: [],
    })
    await renderPage('connections')
    await act(async () =>
      vi.waitFor(() => expect(container.textContent).toContain('不提供逐连接详情')),
    )
    expect(container.querySelector('table')).toBeNull()
  })

  it('shows a fetch error instead of presenting missing traffic as zero', async () => {
    state.permissions = ['network_access.proxy_instances.view']
    apiMocks.getTraffic.mockRejectedValue(new Error('offline'))
    await renderPage('overview')
    await act(async () =>
      vi.waitFor(() => expect(container.textContent).toContain('代理流量暂不可用')),
    )
  })

  it('reports truncation and blocks closes from a different process epoch', async () => {
    state.permissions = [
      'network_access.proxy_instances.view',
      'network_access.proxy_connections.view',
      'network_access.proxy_connections.close',
    ]
    apiMocks.getConnections.mockResolvedValue({
      instanceId: state.instance.id,
      state: 'available',
      processEpoch: 'previous-epoch',
      freshness: 'fresh',
      observedAt: '2026-09-27T00:00:00Z',
      totalActive: 500,
      returnedCount: 1,
      truncated: true,
      connections: [
        {
          id: 'old',
          destination: 'old.example:443',
          network: 'tcp',
          uploadBytes: 1,
          downloadBytes: 1,
        },
      ],
    })
    await renderPage('connections')
    await act(async () =>
      vi.waitFor(() => expect(container.textContent).toContain('已返回 1 / 活动总数 500')),
    )
    expect(container.textContent).toContain('已截断')
    expect(container.querySelector('.ant-table-row button.ant-btn-dangerous')).toBeNull()
    expect(apiMocks.closeConnection).not.toHaveBeenCalled()
  })
  it.each(['accepted', 'rejected', 'unknown'])(
    'confirms close and recovers the exact request: %s',
    async (scenario) => {
      state.permissions = [
        'network_access.proxy_instances.view',
        'network_access.proxy_connections.view',
        'network_access.proxy_connections.close',
      ]
      apiMocks.getConnections.mockResolvedValue({
        instanceId: state.instance.id,
        state: 'available',
        processEpoch: 'test-epoch',
        freshness: 'fresh',
        observedAt: '2026-09-27T00:00:00Z',
        connections: [
          {
            id: 'connection-1',
            destination: 'example.com:443',
            network: 'tcp',
            uploadBytes: 1,
            downloadBytes: 2,
          },
        ],
      })
      const receiptFor = (
        connectionId: string,
        input: NetworkProxyCloseInput,
        status: 'pending' | 'closed',
      ) => ({
        commandId: input.requestId,
        connectionId,
        processEpoch: input.processEpoch,
        expiresAt: input.expiresAt,
        status,
      })
      apiMocks.closeConnection.mockImplementation(
        async (_id: string, connectionId: string, input: NetworkProxyCloseInput) => {
          if (scenario === 'rejected') throw new ApiError(409, 'epoch conflict', { method: 'POST' })
          if (scenario === 'unknown')
            throw new ApiError(0, 'lost close response', { method: 'POST' })
          return receiptFor(connectionId, input, 'pending')
        },
      )
      apiMocks.getCloseCommand.mockImplementation(async () => {
        const [, connectionId, input] = apiMocks.closeConnection.mock.calls[0]
        const receipt = receiptFor(
          connectionId,
          input,
          scenario === 'accepted' ? 'pending' : 'closed',
        )
        return scenario === 'unknown' ? { ...receipt, processEpoch: 'mismatched-epoch' } : receipt
      })
      await renderPage('connections')
      await act(async () =>
        vi.waitFor(() => expect(container.textContent).toContain('example.com:443')),
      )
      const disconnect = container.querySelector<HTMLButtonElement>(
        '.ant-table-row button.ant-btn-dangerous',
      )
      expect(disconnect).toBeDefined()
      await act(async () => disconnect?.click())
      expect(apiMocks.closeConnection).not.toHaveBeenCalled()
      const confirm = document.querySelector<HTMLButtonElement>('.ant-popconfirm .ant-btn-primary')
      expect(confirm).toBeDefined()
      await act(async () => confirm?.click())
      expect(apiMocks.closeConnection).toHaveBeenCalledWith(
        'proxy-mihomo',
        'connection-1',
        expect.objectContaining({
          processEpoch: 'test-epoch',
          requestId: expect.any(String),
          issuedAt: expect.any(String),
          expiresAt: expect.any(String),
        }),
      )
      if (scenario === 'rejected') {
        await act(async () =>
          vi.waitFor(() => expect(container.textContent).toContain('epoch conflict')),
        )
        expect(disconnect?.disabled).toBe(false)
        expect(apiMocks.getCloseCommand).not.toHaveBeenCalled()
      }
      if (scenario === 'unknown') {
        const [, connectionId, input] = apiMocks.closeConnection.mock.calls[0]
        await act(async () =>
          vi.waitFor(() =>
            expect(apiMocks.getCloseCommand).toHaveBeenCalledWith('proxy-mihomo', input.requestId),
          ),
        )
        expect(disconnect?.disabled).toBe(true)
        expect(container.textContent).toContain('关闭命令 · 未知')
        state.instance.processEpoch = 'next-epoch'
        apiMocks.getCloseCommand.mockResolvedValue(receiptFor(connectionId, input, 'closed'))
        const alert = [...container.querySelectorAll('.ant-alert')].find((el) =>
          el.textContent?.includes('关闭命令 · 未知'),
        )
        const refresh = alert?.querySelector('button')
        expect(refresh).toBeDefined()
        await act(async () => refresh?.click())
        await vi.waitFor(async () => {
          await act(async () => new Promise((resolve) => setTimeout(resolve, 0)))
          expect(container.textContent).toContain('关闭命令 · closed')
          expect(container.textContent).not.toContain('当前页已暂停再次提交')
        })
        expect(apiMocks.getCloseCommand).toHaveBeenLastCalledWith('proxy-mihomo', input.requestId)
        expect(apiMocks.closeConnection).toHaveBeenCalledTimes(1)
      }
    },
  )
})
