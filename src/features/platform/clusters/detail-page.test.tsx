/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { ClusterDetailPage } from './detail-page'
import { LogCollectionCard } from './log-collection-card'
import { ApiError } from '@/services/api-error'

const storeMocks = vi.hoisted(() => ({ setClusterId: vi.fn() }))
const authMocks = vi.hoisted(() => ({ permissions: new Set<string>() }))
const apiGetMock = vi.hoisted(() =>
  vi.fn(async (path: string) => {
    if (path.endsWith('/detail')) {
      return {
        data: {
          summary: {
            id: 'cluster-a',
            name: 'Primary',
            region: 'gke',
            environment: 'prod',
            labels: { owner: 'platform' },
            connectionMode: 'agent',
            version: 'v1.30',
            capabilities: ['metrics'],
            health: { status: 'healthy' },
          },
          diagnostics: {
            syncStrategy: 'watch',
            cacheStatus: 'ready',
            connectionState: 'connected',
          },
          connection: {
            mode: 'agent',
            credentialType: 'token',
            sourceType: 'agent',
            usesInformerCache: true,
          },
          monitoring: { prometheus: { baseUrl: 'http://prometheus', hasBearerToken: true } },
        },
      }
    }
    if (path.endsWith('/observability/logging')) {
      return {
        data: {
          clusterId: 'cluster-a',
          mode: 'runtime_only',
          status: 'disabled',
          namespace: '',
          releaseName: '',
          collector: { status: 'unknown', message: 'managed collection is not enabled' },
          backend: { status: 'unknown', message: 'managed collection is not enabled' },
          endToEnd: { status: 'unknown', message: 'managed collection is not enabled' },
          historyPreserved: true,
          updatedAt: '2026-07-31T00:00:00Z',
        },
      }
    }
    return {
      data: [
        {
          name: 'node-a',
          status: 'Ready',
          roles: ['worker'],
          podCount: 12,
          ageSeconds: 60,
        },
      ],
    }
  }),
)

vi.mock('@/features/auth', () => ({
  hasPermission: (_snapshot: unknown, key: string) => authMocks.permissions.has(key),
  usePermissionSnapshot: () => ({
    data: {
      data: {
        permissionKeys: [
          'platform.clusters.view',
          'platform.nodes.view',
          'platform.resource-monitoring.overview.view',
        ],
      },
    },
  }),
}))
vi.mock('@/services/api-client', () => ({ api: { get: apiGetMock } }))
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: (selector?: (state: typeof storeMocks) => unknown) =>
    selector ? selector(storeMocks) : { clusterId: 'cluster-a', namespace: null, ...storeMocks },
}))
vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))
vi.mock('@/components/management-list', () => ({
  ManagementState: ({ title, description }: { title?: ReactNode; description?: ReactNode }) => (
    <div>
      {title}
      {description}
    </div>
  ),
  ManagementToolbarSearch: ({
    value,
    onChange,
    placeholder,
  }: {
    value: string
    onChange: (value: string) => void
    placeholder?: string
  }) => (
    <input
      value={value}
      placeholder={placeholder}
      onChange={(event) => onChange(event.target.value)}
    />
  ),
}))
vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    dataSource = [],
    headerExtra,
    toolbar,
  }: {
    dataSource?: Array<{ name?: string }>
    headerExtra?: ReactNode
    toolbar?: ReactNode
  }) => (
    <div>
      {headerExtra}
      {toolbar}
      {dataSource.map((item) => item.name).join(',')}
    </div>
  ),
}))

const mountedRoots: Root[] = []

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({
      matches: false,
      media: '',
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})
beforeEach(() => {
  vi.clearAllMocks()
  authMocks.permissions = new Set([
    'platform.clusters.view',
    'platform.clusters.update',
    'platform.nodes.view',
    'platform.resource-monitoring.overview.view',
  ])
})
afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function renderDetail(entry = '/clusters/cluster-a') {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[entry]}>
          <Routes>
            <Route path="/clusters/:clusterId" element={<ClusterDetailPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await vi.waitFor(() => expect(container.textContent).toContain('Primary'))
  return container
}

describe('cluster detail page', () => {
  it('keeps readable log collection state on a transient error and hides it after denial', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    mountedRoots.push(root)
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    await act(async () =>
      root.render(
        <QueryClientProvider client={client}>
          <LogCollectionCard
            scope={{ clusterId: 'cluster-a', namespace: null }}
            localeCode="zh_CN"
          />
        </QueryClientProvider>,
      ),
    )
    await vi.waitFor(() => expect(container.textContent).toContain('runtime_only'))
    const query = client
      .getQueryCache()
      .findAll()
      .find((item) => item.queryKey.includes('log-collection'))!
    expect(query).toBeDefined()
    await act(async () =>
      query.setState({
        status: 'error',
        error: new ApiError(503, 'unavailable'),
      }),
    )
    await vi.waitFor(() => {
      expect(container.textContent).toContain('runtime_only')
      expect(container.querySelectorAll('.soha-read-feedback')).toHaveLength(1)
      expect(container.textContent).not.toContain('日志采集状态加载失败')
    })
    await act(async () => query.setState({ status: 'error', error: new ApiError(403, 'denied') }))
    await vi.waitFor(() => {
      expect(container.textContent).not.toContain('runtime_only')
      expect(container.textContent).toContain('日志采集状态无访问权限')
    })
    client.clear()
  })

  it('loads unwrapped detail and node snapshot data for the route cluster', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    mountedRoots.push(root)
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    await act(async () => {
      root.render(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={['/clusters/cluster-a']}>
            <Routes>
              <Route path="/clusters/:clusterId" element={<ClusterDetailPage />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )
    })
    await flushAsyncWork()
    await flushAsyncWork()

    expect(apiGetMock).toHaveBeenCalledWith('/clusters/cluster-a/detail')
    expect(apiGetMock).toHaveBeenCalledWith('/clusters/cluster-a/infrastructure/nodes')
    expect(apiGetMock).not.toHaveBeenCalledWith('/clusters/cluster-a/observability/logging')
    expect(
      Array.from(container.querySelectorAll('[role="tab"]')).map((el) => el.textContent),
    ).toEqual(['概览', '节点', '监控接入', '连接与 Agent', '日志采集'])
    const selectTab = async (name: string) => {
      await act(async () =>
        Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]'))
          .find((el) => el.textContent === name)
          ?.click(),
      )
      await flushAsyncWork()
    }
    await selectTab('连接与 Agent')
    expect(container.textContent).not.toContain('集群详情')
    expect(container.textContent).toContain('Token')
    expect(container.textContent).toContain('持续监听')
    expect(container.textContent).toContain('就绪')
    expect(container.textContent).toContain('已连接')
    expect(container.textContent).toContain('日志采集')
    await selectTab('日志采集')
    expect(apiGetMock).toHaveBeenCalledWith('/clusters/cluster-a/observability/logging')
    expect(container.textContent).toContain('runtime_only')
    await selectTab('概览')
    expect(container.textContent).toContain('node-a')
  })

  it('refreshes only the route cluster and currently readable detail scope', async () => {
    const container = await renderDetail('/clusters/cluster-a?tab=connection')
    expect(apiGetMock).toHaveBeenCalledWith('/clusters/cluster-a/detail')
    expect(apiGetMock).not.toHaveBeenCalledWith('/clusters/cluster-a/infrastructure/nodes')
    apiGetMock.mockClear()
    await act(async () =>
      container.querySelector<HTMLButtonElement>('[aria-label="刷新 Cluster"]')?.click(),
    )
    await vi.waitFor(() => expect(apiGetMock).toHaveBeenCalledTimes(1))
    expect(apiGetMock).toHaveBeenCalledWith('/clusters/cluster-a/detail')

    await act(async () =>
      Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]'))
        .find((el) => el.textContent === '节点')
        ?.click(),
    )
    await vi.waitFor(() => expect(container.textContent).toContain('node-a'))
    apiGetMock.mockClear()
    await act(async () =>
      Array.from(container.querySelectorAll<HTMLButtonElement>('button'))
        .find((el) => el.textContent === '刷新节点')
        ?.click(),
    )
    await vi.waitFor(() => expect(apiGetMock).toHaveBeenCalledTimes(1))
    expect(apiGetMock).toHaveBeenCalledWith('/clusters/cluster-a/infrastructure/nodes')
  })

  it('keeps node reads, editing and monitoring checks behind their exact permissions', async () => {
    authMocks.permissions = new Set(['platform.clusters.view'])
    const container = await renderDetail()
    expect(apiGetMock).not.toHaveBeenCalledWith('/clusters/cluster-a/infrastructure/nodes')
    expect(container.textContent).toContain('需要节点查看权限')
    expect(container.textContent).not.toContain('编辑集群')
    expect(container.textContent).not.toContain('进入工作台')

    await act(async () =>
      Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]'))
        .find((el) => el.textContent === '监控接入')
        ?.click(),
    )
    await flushAsyncWork()
    expect(container.textContent).toContain('http://prometheus')
    expect(container.textContent).not.toContain('编辑监控配置')
    const checkButton = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(
      (el) => el.textContent === '校验接入',
    )
    expect(checkButton).toBeDefined()
    expect(checkButton?.disabled).toBe(true)
    expect(apiGetMock.mock.calls.some(([path]) => path.includes('/resource-monitoring'))).toBe(
      false,
    )
  })
})
