/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { StatefulSetDetailPage } from './detail-page'

const testState = vi.hoisted(() => ({
  capability: vi.fn(),
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
          name: 'database',
          namespace: 'url-namespace',
          createdAt: '2026-01-01T00:00:00Z',
          selector: { app: 'database' },
          serviceName: 'database',
          pods: [
            {
              name: 'database-0',
              namespace: 'url-namespace',
              phase: 'Running',
              readyContainers: '1/1',
              restarts: 0,
              ageSeconds: 60,
              labels: { app: 'database' },
            },
          ],
          relatedResources: [
            {
              kind: 'Service',
              name: 'database',
              namespace: 'url-namespace',
              relation: 'selected-by-service',
            },
          ],
        },
      }
    }
    if (path.includes('/metrics?')) return { data: { rangeMinutes: 60, series: [] } }
    if (path.includes('/events?')) {
      return { data: [{ involvedKind: 'StatefulSet', involvedName: 'database' }] }
    }
    if (path.includes('/yaml?')) {
      return {
        data: {
          kind: 'StatefulSet',
          name: 'database',
          content:
            'apiVersion: apps/v1\nkind: StatefulSet\nmetadata:\n name: database\n namespace: url-namespace\nspec:\n template:\n  spec:\n   containers: [{name: db, image: db:v1}]\n',
        },
      }
    }
    return { data: [] }
  }),
)

vi.mock('@/features/auth', async (original) => ({
  ...(await original<typeof import('@/features/auth')>()),
  usePermissionSnapshot: () => ({
    data: {
      data: { permissionKeys: ['platform.workloads.stateful-sets.view', 'platform.pods.view'] },
    },
    isLoading: false,
  }),
}))
vi.mock('@/features/platform/shared/resource-gitops-status', () => ({
  ResourceGitOpsStatus: () => null,
}))
vi.mock('@/features/copilot', () => ({ useAIPageContext: vi.fn() }))
vi.mock('@/services/api-client', () => ({
  api: {
    delete: vi.fn(),
    get: apiGetMock,
    post: vi.fn(),
    put: vi.fn(async () => ({ data: { content: 'kind: StatefulSet' } })),
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
  useClusterCapabilityForCluster: (...args: unknown[]) => {
    testState.capability(...args)
    return { disabled: false, reason: '', status: 'available' }
  },
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
    await vi.dynamicImportSettled()
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function renderDetail(entry = '/workloads/statefulsets/database?namespace=url-namespace') {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <AntdApp>
          <MemoryRouter initialEntries={[entry]}>
            <Routes>
              <Route
                path="/workloads/statefulsets/:statefulSetName"
                element={<StatefulSetDetailPage />}
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

describe('statefulset detail boundaries', () => {
  it('checks mutation capability against the explicit detail cluster', async () => {
    await renderDetail(
      '/workloads/statefulsets/database?clusterId=cluster-b&namespace=url-namespace',
    )
    expect(testState.capability).toHaveBeenCalledWith('workload.mutations', 'zh_CN', 'cluster-b')
    expect(testState.scope.clusterId).toBe('cluster-a')
  })
  it('reads server configuration but keeps metrics, events and the YAML editor lazy', async () => {
    const container = await renderDetail()
    const requestedPaths = () => apiGetMock.mock.calls.map(([path]) => String(path))

    expect(requestedPaths()).toContain(
      '/clusters/cluster-a/workloads/statefulsets/database/detail?namespace=url-namespace',
    )
    // The resource URL scopes reads without replacing the list's global selection.
    expect(testState.scope.setNamespace).not.toHaveBeenCalled()
    expect(testState.scope.namespace).toBe('selected-ns')
    expect(requestedPaths().some((path) => path.includes('/metrics?'))).toBe(false)
    expect(requestedPaths().some((path) => path.includes('/events?'))).toBe(false)
    expect(requestedPaths().some((path) => path.includes('/yaml?namespace=url-namespace'))).toBe(
      true,
    )
    expect(container.querySelector('[data-testid="yaml-editor"]')).toBeNull()
    expect(container.querySelectorAll('[role="tab"]')).toHaveLength(9)
    expect(requestedPaths().some((path) => path.includes('/workloads/pods?'))).toBe(false)
    expect(container.textContent).toContain('database-0')
    expect(container.textContent).toContain('database')

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
