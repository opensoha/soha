/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import type { ReactNode } from 'react'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { PodDetailPage } from './detail-page'

const testState = vi.hoisted(() => ({
  runtimeLoads: {
    logs: 0,
    metrics: 0,
    terminal: 0,
  },
  scope: {
    clusterId: 'cluster-a' as string | null,
    namespace: 'monitoring' as string | null,
    setClusterId: vi.fn(),
    setNamespace: vi.fn(),
  },
  openSession: vi.fn(),
}))

const apiGetMock = vi.hoisted(() =>
  vi.fn(async (path: string) => {
    if (path.includes('/detail?')) {
      return {
        data: {
          name: 'prometheus-0',
          namespace: 'monitoring',
          uid: 'uid-prometheus-0',
          phase: 'Running',
          createdAt: '2026-01-01T00:00:00Z',
          containers: [
            {
              name: 'prometheus',
              image: 'prometheus:v1',
              ready: true,
              restartCount: 0,
              state: 'running',
            },
            { name: 'migrate', role: 'init', state: 'terminated:Completed' },
          ],
          conditions: [],
          volumes: [],
          relatedResources: [],
          allowedActions: ['logs', 'exec'],
        },
      }
    }
    if (path.includes('/metrics?')) {
      return {
        data: {
          resourceKind: 'Pod',
          resourceName: 'prometheus-0',
          configured: true,
          rangeMinutes: 60,
          series: [],
        },
      }
    }
    if (path.includes('/events?')) return { data: [] }
    if (path.includes('/yaml?')) {
      return { data: { kind: 'Pod', name: 'prometheus-0', content: 'kind: Pod' } }
    }
    return { data: [] }
  }),
)

vi.mock('@/services/api-client', () => ({
  api: {
    get: apiGetMock,
    put: vi.fn(async () => ({ data: { content: 'kind: Pod' } })),
  },
}))

vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))

vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))

vi.mock('@/features/copilot', () => ({ useAIPageContext: vi.fn() }))

vi.mock('@/features/auth/permission-snapshot', () => ({
  usePermissionSnapshot: () => ({
    data: {
      data: {
        permissionKeys: ['platform.pods.view', 'platform.pods.logs', 'platform.pods.exec'],
        visibleMenuIds: [],
      },
    },
    isLoading: false,
  }),
  hasPermission: (snapshot: { permissionKeys: string[] } | undefined, permissionKey: string) =>
    snapshot?.permissionKeys.includes(permissionKey) ?? false,
  hasAllowedAction: (allowedActions: string[] | undefined, action: string) =>
    allowedActions?.includes(action) ?? false,
}))

vi.mock('@/features/platform/cluster-capabilities', () => ({
  useClusterCapabilityForCluster: () => ({
    disabled: false,
    isLoading: false,
    reason: '',
    status: 'available',
  }),
  useClusterCapability: () => ({
    disabled: false,
    isLoading: false,
    reason: '',
    status: 'available',
  }),
}))

vi.mock('@/features/platform/session-dock', () => ({
  useRealtimeSessionDock: () => ({ openSession: testState.openSession }),
}))

vi.mock('./pod-detail-events', () => ({
  PodDetailEvents: () => <div data-testid="events-panel">events-panel</div>,
}))

vi.mock('./pod-detail-metrics', () => {
  testState.runtimeLoads.metrics += 1
  return {
    PodDetailMetrics: () => <div data-testid="metrics-panel">metrics-panel</div>,
  }
})

vi.mock('@/components/pod-log-viewer', () => {
  testState.runtimeLoads.logs += 1
  return {
    PodLogViewer: ({
      onOpenLogCenter,
      toolbarExtra,
    }: {
      onOpenLogCenter?: () => void
      toolbarExtra?: ReactNode
    }) => (
      <div data-testid="logs-panel">
        logs-panel
        {toolbarExtra}
        <button onClick={onOpenLogCenter}>在日志中心打开</button>
      </div>
    ),
  }
})

vi.mock('@/components/pod-terminal', () => {
  testState.runtimeLoads.terminal += 1
  return {
    PodTerminal: ({ toolbarContent }: { toolbarContent?: ReactNode }) => (
      <div data-testid="terminal-panel">{toolbarContent}</div>
    ),
  }
})

vi.mock('@/components/k8s-yaml-editor', () => ({
  K8sYamlEditor: () => <div data-testid="yaml-editor">yaml-editor</div>,
}))

vi.mock('@/components/status-tag', () => ({
  MetadataTag: ({ label }: { label: React.ReactNode }) => <span>{label}</span>,
  BooleanTag: ({ value }: { value: boolean }) => <span>{String(value)}</span>,
  StatusTag: ({ value }: { value?: string }) => <span>{value}</span>,
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({ dataSource = [] }: { dataSource?: unknown[] }) => (
    <div data-testid="admin-table">{dataSource.length}</div>
  ),
}))

const mountedRoots: Root[] = []

function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location">{`${location.pathname}${location.search}`}</div>
}

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
  vi.clearAllMocks()
  testState.scope.clusterId = 'cluster-a'
  testState.scope.namespace = 'monitoring'
  testState.runtimeLoads.logs = 0
  testState.runtimeLoads.metrics = 0
  testState.runtimeLoads.terminal = 0
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

async function renderDetail(
  route = '/workloads/pods/prometheus-0?clusterId=url-cluster&namespace=url-namespace',
) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <AntdApp>
          <MemoryRouter initialEntries={[route]}>
            <Routes>
              <Route path="/workloads/pods/:podName" element={<PodDetailPage />} />
              <Route path="/monitoring-workbench/logs" element={<LocationProbe />} />
            </Routes>
          </MemoryRouter>
        </AntdApp>
      </QueryClientProvider>,
    )
  })
  await flushAsyncWork()
  await flushAsyncWork()
  return container
}

async function clickTab(container: HTMLElement, label: string) {
  const tab = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find((item) =>
    item.textContent?.includes(label),
  )
  expect(tab).toBeDefined()
  await act(async () => tab?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
  await flushAsyncWork()
}

describe('pod detail page lazy boundaries', () => {
  it('opens the YAML editor directly from the tab query', async () => {
    const container = await renderDetail(
      '/workloads/pods/prometheus-0?clusterId=url-cluster&namespace=url-namespace&tab=yaml',
    )

    expect(apiGetMock.mock.calls.some(([path]) => String(path).includes('/yaml?'))).toBe(true)
    expect(container.querySelector('[data-testid="yaml-editor"]')).not.toBeNull()
  })

  it('loads tab data and metrics on demand and leaves live runtimes to the dock', async () => {
    const container = await renderDetail()
    const requestedPaths = () => apiGetMock.mock.calls.map(([path]) => String(path))

    expect(requestedPaths().filter((path) => path.includes('/detail?'))).toEqual([
      '/clusters/url-cluster/workloads/pods/prometheus-0/detail?namespace=url-namespace',
    ])
    expect(testState.scope.setClusterId).toHaveBeenCalledWith('url-cluster')
    // The resource URL scopes reads without replacing the list's global selection.
    expect(testState.scope.setNamespace).not.toHaveBeenCalled()
    expect(testState.scope.namespace).toBe('monitoring')
    expect(requestedPaths().filter((path) => path.includes('/metrics?'))).toHaveLength(1)
    expect(requestedPaths().some((path) => path.includes('/events?'))).toBe(false)
    expect(requestedPaths().some((path) => path.includes('/yaml?'))).toBe(false)
    expect(testState.runtimeLoads).toEqual({ logs: 0, metrics: 0, terminal: 0 })

    await clickTab(container, '指标')
    expect(requestedPaths().some((path) => path.includes('/metrics?namespace=url-namespace'))).toBe(
      true,
    )
    expect(container.querySelector('[data-testid="metrics-panel"]')).not.toBeNull()
    expect(testState.runtimeLoads).toEqual({ logs: 0, metrics: 1, terminal: 0 })

    await clickTab(container, '日志')
    expect(container.querySelector('[data-testid="logs-panel"]')).toBeNull()
    expect(container.textContent).toContain('打开日志会话')
    expect(container.textContent).toContain('在日志中心打开')
    expect(testState.runtimeLoads.logs).toBe(0)

    await clickTab(container, '终端')
    expect(container.querySelector('.soha-terminal-controls')).not.toBeNull()
    expect(container.textContent).toContain('容器:')
    expect(container.textContent).toContain('Shell:')
    expect(container.textContent).toContain('打开终端会话')
    expect(container.querySelector('[data-testid="terminal-panel"]')).toBeNull()
    expect(testState.runtimeLoads.terminal).toBe(0)
    expect(testState.openSession).not.toHaveBeenCalled()

    await clickTab(container, '事件')
    expect(requestedPaths().some((path) => path.includes('/events?namespace=url-namespace'))).toBe(
      true,
    )
    expect(container.querySelector('[data-testid="events-panel"]')).not.toBeNull()

    await clickTab(container, 'YAML')
    expect(requestedPaths().some((path) => path.includes('/yaml?namespace=url-namespace'))).toBe(
      true,
    )
    expect(container.querySelector('[data-testid="yaml-editor"]')).not.toBeNull()
  })

  it('opens the log center with the current pod scope', async () => {
    const container = await renderDetail()

    await clickTab(container, '日志')
    const openLogCenter = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('在日志中心打开'),
    )
    expect(openLogCenter).toBeDefined()

    await act(async () => openLogCenter?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()

    expect(container.querySelector('[data-testid="location"]')?.textContent).toBe(
      '/monitoring-workbench/logs?cluster=url-cluster&namespace=url-namespace&pod=prometheus-0&container=prometheus',
    )
  })

  it('opens scoped log and terminal sessions in the persistent dock', async () => {
    const container = await renderDetail()

    await clickTab(container, '日志')
    const openLogSession = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('打开日志会话'),
    )
    await act(async () => openLogSession?.dispatchEvent(new MouseEvent('click', { bubbles: true })))

    expect(testState.openSession).toHaveBeenLastCalledWith({
      clusterId: 'url-cluster',
      container: 'prometheus',
      kind: 'logs',
      namespace: 'url-namespace',
      podName: 'prometheus-0',
      podUid: 'uid-prometheus-0',
      streamingDisabledReason: undefined,
    })

    await clickTab(container, '终端')
    const openTerminalSession = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('打开终端会话'),
    )
    await act(async () =>
      openTerminalSession?.dispatchEvent(new MouseEvent('click', { bubbles: true })),
    )

    expect(testState.openSession).toHaveBeenLastCalledWith({
      clusterId: 'url-cluster',
      container: 'prometheus',
      kind: 'terminal',
      namespace: 'url-namespace',
      podName: 'prometheus-0',
      podUid: 'uid-prometheus-0',
      shell: '/bin/sh',
    })
    expect(testState.runtimeLoads).toMatchObject({ logs: 0, terminal: 0 })
  })

  it('blocks terminal launch for a terminated container selected by URL', async () => {
    const container = await renderDetail(
      '/workloads/pods/prometheus-0?clusterId=url-cluster&namespace=url-namespace&tab=terminal&container=migrate',
    )
    const launch = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === '打开终端会话',
    )
    expect(launch?.disabled).toBe(true)
    await act(async () => launch?.click())
    expect(testState.openSession).not.toHaveBeenCalled()
  })
})
