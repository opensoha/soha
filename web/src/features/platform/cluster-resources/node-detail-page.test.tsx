/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { NodeDetailPage } from './node-detail-page'

const testState = vi.hoisted(() => ({
  capability: {
    disabled: false,
    isLoading: false,
    reason: '',
    status: 'available' as 'available' | 'partial',
  },
  runtimeLoads: { yaml: 0 },
  permissions: ['platform.nodes.view', 'platform.pods.view'],
  scope: {
    clusterId: 'stored-cluster' as string | null,
    namespace: null as string | null,
    setClusterId: vi.fn(),
  },
}))

const apiGetMock = vi.hoisted(() =>
  vi.fn(async (path: string) => {
    if (path.endsWith('/detail')) {
      return {
        data: {
          name: 'node-a',
          status: 'Ready',
          roles: ['worker'],
          version: 'v1.32.0',
          internalIp: '10.0.0.1',
          podCount: 2,
          ageSeconds: 60,
          labels: { 'node-role.kubernetes.io/worker': 'true' },
          annotations: { 'example.com/hardware': 'bare-metal' },
          taints: [{ key: 'dedicated', value: 'gpu', effect: 'NoSchedule' }],
          conditions: [],
          pods: [
            {
              name: 'api-0',
              namespace: 'apps',
              phase: 'Running',
              readyContainers: '1/1',
              restarts: 0,
              ageSeconds: 30,
            },
            {
              name: 'monitor-0',
              namespace: 'monitoring',
              phase: 'Running',
              readyContainers: '1/1',
              restarts: 0,
              ageSeconds: 30,
            },
          ],
        },
      }
    }
    if (path.endsWith('/yaml')) {
      return { data: { kind: 'Node', name: 'node-a', content: 'kind: Node' } }
    }
    return { data: [] }
  }),
)

vi.mock('@/features/auth', () => ({
  hasPermission: (_snapshot: unknown, key: string) => testState.permissions.includes(key),
  hasAllowedAction: (a: string[] | undefined, k: string) => a?.includes(k) ?? false,
  usePermissionSnapshot: () => ({
    data: { data: { permissionKeys: testState.permissions } },
    isPending: false,
  }),
}))
vi.mock('@/services/api-client', () => ({
  api: { get: apiGetMock, put: vi.fn(async () => ({ data: { content: 'kind: Node' } })) },
}))
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))
vi.mock('@/features/platform/cluster-capabilities', () => ({
  useClusterCapabilityForCluster: () => testState.capability,
  useClusterCapability: () => testState.capability,
}))
vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))
vi.mock('@/components/k8s-yaml-editor', () => {
  testState.runtimeLoads.yaml += 1
  return { K8sYamlEditor: () => <div data-testid="yaml-editor">yaml-editor</div> }
})
vi.mock('@/features/platform/node-resource-utils', () => ({
  NodeResourcePanel: () => <div>node-resource-panel</div>,
  formatCpu: (v?: string) => v || '—',
  formatBytesAsG: (v?: string) => v || '—',
  parseStringMap: () => ({}),
  parseTaints: () => [],
  stringifyMap: () => '{}',
  stringifyTaints: () => '[]',
}))
vi.mock('@/components/status-tag', () => ({
  MetadataTag: ({ label }: { label: import('react').ReactNode }) => <span>{label}</span>,
  StatusTag: ({ value }: { value?: string }) => <span>{value}</span>,
}))
vi.mock('@/components/admin-table', () => ({
  AdminTable: ({ dataSource = [] }: { dataSource?: unknown[] }) => (
    <div data-testid="admin-table">{dataSource.length}</div>
  ),
}))

const mountedRoots: Root[] = []

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  class ResizeObserverMock {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
})

beforeEach(() => {
  vi.clearAllMocks()
  testState.runtimeLoads.yaml = 0
  testState.permissions = ['platform.nodes.view', 'platform.pods.view']
  testState.scope.clusterId = 'stored-cluster'
  testState.capability.disabled = false
  testState.capability.isLoading = false
  testState.capability.reason = ''
  testState.capability.status = 'available'
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

async function renderDetail() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={['/cluster-resources/nodes/node-a?clusterId=url-cluster']}>
            <Routes>
              <Route path="/cluster-resources/nodes/:nodeName" element={<NodeDetailPage />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await flushAsyncWork()
  await flushAsyncWork()
  return container
}

describe('node detail lazy YAML boundary', () => {
  it('uses the shared overview without duplicating metadata and summarizes scheduled namespaces', async () => {
    const container = await renderDetail()
    const headers = Array.from(container.querySelectorAll<HTMLElement>('.ant-collapse-header'))
    expect(headers).toHaveLength(2)
    for (const header of headers) await act(async () => header.click())
    const content = container.textContent ?? ''

    expect(content).toContain('node-role.kubernetes.io/worker')
    expect(content).toContain('example.com/hardware')
    expect(content).toContain('dedicated=gpu')
    expect(content).toContain('命名空间数')
    expect(content).not.toContain('基础信息')
  })

  it('prefers the URL cluster and loads YAML code and data only after tab activation', async () => {
    const container = await renderDetail()
    const requestedPaths = () => apiGetMock.mock.calls.map(([path]) => String(path))

    expect(testState.scope.setClusterId).toHaveBeenCalledWith('url-cluster')
    expect(requestedPaths()).toContain('/clusters/url-cluster/infrastructure/nodes/node-a/detail')
    expect(requestedPaths().some((path) => path.endsWith('/yaml'))).toBe(false)
    expect(testState.runtimeLoads.yaml).toBe(0)

    const yamlTab = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find(
      (item) => item.textContent?.includes('YAML'),
    )
    expect(yamlTab).toBeDefined()
    await act(async () => yamlTab?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()
    await flushAsyncWork()

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50))
    })
    await flushAsyncWork()

    expect(requestedPaths()).toContain('/clusters/url-cluster/infrastructure/nodes/node-a/yaml')
    expect(testState.runtimeLoads.yaml).toBe(1)
    expect(container.querySelector('[data-testid="yaml-editor"]')).not.toBeNull()
  })

  it.each([
    ['platform.workloads.overview.view', true],
    ['platform.events.view', false],
  ])('gates Node events using %s', async (permission, shouldRead) => {
    testState.permissions.push(permission)
    const container = await renderDetail()
    const eventsTab = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find(
      (item) => item.textContent?.includes('事件'),
    )
    expect(eventsTab).toBeDefined()
    await act(async () => eventsTab?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()
    expect(
      apiGetMock.mock.calls.some(([path]) => path === '/clusters/url-cluster/events?limit=100'),
    ).toBe(shouldRead)
  })

  it('requests node YAML when Agent support is partial but not disabled', async () => {
    testState.capability.disabled = false
    testState.capability.reason =
      'Built-in generic and custom-resource YAML is supported; node YAML still needs parity cleanup.'
    testState.capability.status = 'partial'
    const container = await renderDetail()
    const requestedPaths = () => apiGetMock.mock.calls.map(([path]) => String(path))

    const yamlTab = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find(
      (item) => item.textContent?.includes('YAML'),
    )
    expect(yamlTab).toBeDefined()
    await act(async () => yamlTab?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()

    expect(container.textContent).not.toContain('节点 YAML 暂不可用')
    expect(requestedPaths().some((path) => path.endsWith('/yaml'))).toBe(true)
    expect(container.querySelector('[data-testid="yaml-editor"]')).not.toBeNull()
  })
})
