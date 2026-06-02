/** @vitest-environment jsdom */

import { act } from 'react'
import type { ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { PodDetailPage, WorkloadsPodsPage } from './workloads-pages'

const testState = vi.hoisted(() => ({
  responses: {} as Record<string, unknown>,
  scope: {
    clusterId: 'cluster-a' as string | null,
    namespace: 'monitoring' as string | null,
    setClusterId: vi.fn(),
    setNamespace: vi.fn(),
  },
}))

const apiGetMock = vi.hoisted(() => vi.fn((path: string) => Promise.resolve({ data: testState.responses[path] ?? [] })))

vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))

vi.mock('@/i18n', () => ({
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: apiGetMock,
    delete: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
  },
}))

vi.mock('@/components/platform-scope-toolbar', () => ({
  PlatformScopeToolbar: () => <div data-testid="scope-toolbar">scope-toolbar</div>,
}))

vi.mock('@/components/resource-events-timeline', () => ({
  ResourceEventsTimeline: () => <div data-testid="resource-events-timeline">resource-events-timeline</div>,
}))

vi.mock('@/components/status-tag', () => ({
  BooleanTag: ({ value }: { value: boolean }) => <span>{String(value)}</span>,
  StatusTag: ({ value }: { value: string }) => <span>{value}</span>,
}))

vi.mock('@/components/resource-metrics-panel', () => ({
  ResourceMetricsPanel: () => <div data-testid="resource-metrics-panel">resource-metrics-panel</div>,
}))

vi.mock('@/features/platform/node-resource-utils', () => ({
  ResourceProgressCell: () => <div data-testid="resource-progress-cell">resource-progress-cell</div>,
  formatBytesAsG: () => '-',
  formatCpu: () => '-',
}))

vi.mock('@/components/stat-grid', () => ({
  StatGrid: () => <div data-testid="stat-grid">stat-grid</div>,
}))

vi.mock('@/components/resource-actions', () => ({
  TABLE_ACTIONS_COLUMN_CLASS_NAME: 'soha-actions',
  useResourceActions: () => ({
    column: { title: '', dataIndex: '__actions__', render: () => null },
    modalNode: null,
  }),
}))

vi.mock('@/features/auth/permission-snapshot', () => ({
  hasAllowedAction: () => true,
}))

vi.mock('@/utils/download', () => ({
  downloadJSON: vi.fn(),
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    headerExtra,
    title,
    toolbar,
    toolbarExtra,
    dataSource,
    loading,
  }: {
    headerExtra?: ReactNode
    title?: ReactNode
    toolbar?: ReactNode
    toolbarExtra?: ReactNode
    dataSource?: unknown[]
    loading?: boolean
  }) => (
    <div data-testid="admin-table">
      {title ? <div data-testid="table-title">{title}</div> : null}
      {headerExtra ? <div data-testid="table-header-extra">{headerExtra}</div> : null}
      {toolbar ? <div data-testid="table-toolbar">{toolbar}</div> : null}
      {toolbarExtra ? <div data-testid="table-toolbar-extra">{toolbarExtra}</div> : null}
      <div data-testid="table-loading">{String(Boolean(loading))}</div>
      <div data-testid="table-rows">{dataSource?.length ?? 0}</div>
    </div>
  ),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function setResponses(responses: Record<string, unknown>) {
  testState.responses = responses
}

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

async function renderWithProviders(node: ReactNode, route = '/workloads/pods') {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter
            initialEntries={[route]}
            future={{
              v7_startTransition: true,
              v7_relativeSplatPath: true,
            }}
          >
            {node}
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })

  await flushAsyncWork()

  return container
}

describe('workloads pods page refresh controls', () => {
  beforeAll(() => {
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
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

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  })

  beforeEach(() => {
    vi.useFakeTimers()
    testState.scope.clusterId = 'cluster-a'
    testState.scope.namespace = 'monitoring'
    setResponses({
      '/clusters/cluster-a/workloads/pods?namespace=monitoring': [
        {
          name: 'prometheus-0',
          namespace: 'monitoring',
          phase: 'Running',
          readyContainers: '1/1',
          restarts: 0,
          podIp: '10.0.0.10',
          nodeName: 'node-a',
          cpu: '10m',
          memory: '64Mi',
          ageSeconds: 60,
        },
      ],
    })
  })

  afterEach(async () => {
    await act(async () => {
      for (const root of roots) {
        root.unmount()
      }
    })
    roots = []
    for (const container of containers) {
      container.remove()
    }
    containers = []
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  it('manually refetches pod data from the refresh button', async () => {
    const container = await renderWithProviders(<WorkloadsPodsPage />)
    expect(apiGetMock).toHaveBeenCalledTimes(1)

    const refreshButton = container.querySelector('button[aria-label="Refresh"]')
    if (!refreshButton) {
      throw new Error('refresh button not found')
    }

    await act(async () => {
      refreshButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushAsyncWork()

    expect(apiGetMock).toHaveBeenCalledTimes(2)
  })

  it('polls pod data automatically when auto refresh is enabled', async () => {
    await renderWithProviders(<WorkloadsPodsPage />)
    expect(apiGetMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      vi.advanceTimersByTime(15000)
    })
    await flushAsyncWork()

    expect(apiGetMock).toHaveBeenCalledTimes(2)
  })

  it('renders container, volume, and related resource tabs on pod detail page', async () => {
    setResponses({
      '/clusters/cluster-a/workloads/pods/demo-pod/detail?namespace=monitoring': {
        name: 'demo-pod',
        namespace: 'monitoring',
        phase: 'Running',
        createdAt: '2026-05-07T12:00:00Z',
        containers: [
          {
            name: 'app',
            image: 'demo:1.0',
            ready: true,
            restartCount: 0,
            state: 'running',
          },
        ],
        volumes: [
          {
            name: 'config-volume',
            type: 'ConfigMap',
            sourceName: 'demo-config',
            readOnly: false,
            details: ['ConfigMap: demo-config'],
            volumeMounts: [{ name: 'app', mountPath: '/etc/config', readOnly: false }],
            referencedConfigMaps: ['demo-config'],
          },
        ],
        relatedResources: [
          {
            kind: 'Service',
            name: 'demo-svc',
            namespace: 'monitoring',
            relations: ['selected-by-service'],
            details: ['Type: ClusterIP'],
          },
        ],
      },
    })

    const container = await renderWithProviders(
      <Routes>
        <Route path="/workloads/pods/:podName" element={<PodDetailPage />} />
      </Routes>,
      '/workloads/pods/demo-pod?namespace=monitoring',
    )

    await act(async () => {
      vi.runAllTimers()
      await Promise.resolve()
    })

    expect(container.textContent).toContain('容器')
    expect(container.textContent).toContain('卷')
    expect(container.textContent).toContain('相关资源')
  })
})
