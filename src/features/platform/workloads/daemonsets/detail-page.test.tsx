/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { DaemonSetDetailPage } from './detail-page'

const testState = vi.hoisted(() => ({
  scope: {
    clusterId: 'cluster-a',
    namespace: 'selected-ns',
    setClusterId: vi.fn(),
    setNamespace: vi.fn(),
  },
}))
const apiGetMock = vi.hoisted(() =>
  vi.fn(async (path: string) => {
    if (path.includes('/detail?')) {
      return {
        data: {
          name: 'node-agent',
          namespace: 'selected-ns',
          createdAt: '2026-01-01T00:00:00Z',
          selector: { app: 'node-agent' },
          pods: [
            {
              name: 'node-agent-1',
              namespace: 'selected-ns',
              phase: 'Running',
              readyContainers: '1/1',
              restarts: 0,
              ageSeconds: 60,
              labels: { app: 'node-agent' },
            },
          ],
          relatedResources: [
            {
              kind: 'ConfigMap',
              name: 'node-agent-config',
              namespace: 'selected-ns',
              relation: 'config',
            },
          ],
        },
      }
    }
    if (path.includes('/metrics?')) return { data: { rangeMinutes: 60, series: [] } }
    if (path.includes('/events?')) {
      return { data: [{ involvedKind: 'DaemonSet', involvedName: 'node-agent' }] }
    }
    if (path.includes('/yaml?')) {
      return { data: { kind: 'DaemonSet', name: 'node-agent', content: 'kind: DaemonSet' } }
    }
    return { data: [] }
  }),
)

vi.mock('@/services/api-client', () => ({
  api: {
    delete: vi.fn(),
    get: apiGetMock,
    post: vi.fn(),
    put: vi.fn(async () => ({ data: { content: 'kind: DaemonSet' } })),
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
vi.mock('@/features/platform/cluster-capabilities', () => ({
  useClusterCapability: () => ({ disabled: false, reason: '', status: 'available' }),
}))
vi.mock('../shared/detail-events', () => ({
  WorkloadDetailEvents: () => <div data-testid="events-panel">events-panel</div>,
}))
vi.mock('../shared/detail-metrics', () => ({
  WorkloadDetailMetrics: () => <div data-testid="metrics-panel">metrics-panel</div>,
}))
vi.mock('@/components/k8s-yaml-editor', () => ({
  K8sYamlEditor: () => <div data-testid="yaml-editor">yaml-editor</div>,
}))
vi.mock('@/components/status-tag', () => ({
  StatusTag: ({ value }: { value?: string }) => <span>{value}</span>,
  BooleanTag: ({ value }: { value: boolean }) => <span>{String(value)}</span>,
  MetadataTag: ({ label }: { label: string }) => <span>{label}</span>,
}))

const mountedRoots: Root[] = []

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
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('ResizeObserver', ResizeObserverMock)
})

beforeEach(() => vi.clearAllMocks())
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

async function renderDetail() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <AntdApp>
          <MemoryRouter
            initialEntries={['/workloads/daemonsets/node-agent?namespace=url-namespace']}
          >
            <Routes>
              <Route
                path="/workloads/daemonsets/:daemonSetName"
                element={<DaemonSetDetailPage />}
              />
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

function clickTab(container: HTMLElement, label: string) {
  const tab = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find((item) =>
    item.textContent?.includes(label),
  )
  expect(tab).toBeDefined()
  tab?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
}

describe('daemonset detail boundaries', () => {
  it('loads metrics, events, and YAML only after their tabs are activated', async () => {
    const container = await renderDetail()
    const requestedPaths = () => apiGetMock.mock.calls.map(([path]) => String(path))

    expect(requestedPaths()).toContain(
      '/clusters/cluster-a/workloads/daemonsets/node-agent/detail?namespace=url-namespace',
    )
    expect(testState.scope.setNamespace).toHaveBeenCalledWith('url-namespace')
    expect(requestedPaths().some((path) => path.includes('/metrics?'))).toBe(false)
    expect(requestedPaths().some((path) => path.includes('/events?'))).toBe(false)
    expect(requestedPaths().some((path) => path.includes('/yaml?'))).toBe(false)
    expect(requestedPaths().some((path) => path.includes('/workloads/pods?'))).toBe(false)
    expect(container.textContent).toContain('node-agent-1')
    await act(async () => clickTab(container, '关联资源'))
    await flushAsyncWork()
    expect(container.textContent).toContain('node-agent-config')

    await act(async () => clickTab(container, '指标'))
    await flushAsyncWork()
    expect(requestedPaths().some((path) => path.includes('/metrics?namespace=url-namespace'))).toBe(
      true,
    )
    expect(container.querySelector('[data-testid="metrics-panel"]')).not.toBeNull()

    await act(async () => clickTab(container, '事件'))
    await flushAsyncWork()
    expect(requestedPaths().some((path) => path.includes('/events?namespace=url-namespace'))).toBe(
      true,
    )
    expect(container.querySelector('[data-testid="events-panel"]')).not.toBeNull()

    await act(async () => clickTab(container, 'YAML'))
    await flushAsyncWork()
    expect(requestedPaths().some((path) => path.includes('/yaml?namespace=url-namespace'))).toBe(
      true,
    )
    expect(container.querySelector('[data-testid="yaml-editor"]')).not.toBeNull()
  })
})
