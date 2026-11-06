/** @vitest-environment jsdom */

import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import type { ManifestDeployment, ManifestPackage } from '@/features/delivery'
import { PlatformManifestsPage } from './page'

const testState = vi.hoisted(() => ({
  apiGet: vi.fn<(path: string) => Promise<{ data: unknown }>>(),
  scope: { clusterId: null as string | null, namespace: null as string | null },
  permissionKeys: ['delivery.applications.view'],
}))

vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))

vi.mock('@/features/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth')>()),
  usePermissionSnapshot: () => ({
    data: { data: { permissionKeys: testState.permissionKeys } },
    error: null,
  }),
}))

vi.mock('@/services/api-client', () => ({
  api: { get: testState.apiGet, post: vi.fn(), put: vi.fn(), delete: vi.fn() },
}))

// Keep the real AdminTable: package pagination must not slice its expanded binding rows.
let container: HTMLDivElement
let root: ReturnType<typeof createRoot>
let queryClient: QueryClient

function manifest(index = 1, bindingCount = 1): ManifestPackage {
  return {
    id: `manifest-${index}`,
    name: index === 1 ? 'API Ingress' : `Manifest ${index}`,
    applicationId: 'app-1',
    serviceId: 'service-1',
    renderer: 'raw_yaml',
    status: 'published',
    currentRevision: 3,
    files: [],
    bindings: Array.from({ length: bindingCount }, (_, binding) => ({
      id: `binding-${index}-${binding + 1}`,
      applicationEnvironmentId: `environment-${index}-${binding + 1}`,
      environmentKey: `env-${index}-${binding + 1}`,
      clusterId: 'cluster-1',
      namespace: 'demo',
      overlay: {},
      status: 'drifted',
    })),
    createdAt: '2026-08-27T09:00:00Z',
    updatedAt: '2026-08-27T10:00:00Z',
  }
}

const deployment: ManifestDeployment = {
  id: 'deployment-1',
  packageId: 'manifest-1',
  bindingId: 'binding-1-1',
  generation: 3,
  spec: {
    desiredRevision: 3,
    desiredDigest: 'desired',
    reconcilePolicy: 'continuous',
    driftPolicy: 'report',
    deletionPolicy: 'orphan',
  },
  status: {
    observedGeneration: 3,
    appliedRevision: 2,
    phase: 'drifted',
    conditions: [],
    inventory: [],
    drift: {
      drifted: true,
      observedAt: '2026-08-27T10:00:00Z',
      resources: [
        {
          apiVersion: 'apps/v1',
          kind: 'Deployment',
          namespace: 'demo',
          name: 'api',
          fields: [{ path: 'spec.replicas', desiredValue: 3, observedValue: 2 }],
        },
      ],
    },
  },
  createdAt: '2026-08-27T09:00:00Z',
  updatedAt: '2026-08-27T10:00:00Z',
}

function respondWith(items: ManifestPackage[], deployments: ManifestDeployment[] = []) {
  testState.apiGet.mockImplementation(async (path) => {
    if (path === '/applications') return { data: [] }
    if (path.startsWith('/delivery/manifest-deployments')) {
      return { data: { items: deployments, total: deployments.length, page: 1, pageSize: 100 } }
    }
    if (path.startsWith('/delivery/manifest-packages')) {
      return { data: { items, total: items.length, page: 1, pageSize: 15 } }
    }
    throw new Error(`Unexpected GET ${path}`)
  })
}

async function flush() {
  await new Promise((resolve) => setTimeout(resolve, 0))
  await new Promise((resolve) => setTimeout(resolve, 0))
}

async function renderPage() {
  await act(async () => {
    root.render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <PlatformManifestsPage />
        </QueryClientProvider>
      </MemoryRouter>,
    )
    await flush()
  })
}

async function openDetails() {
  await vi.waitFor(() =>
    expect(container.querySelector('.soha-platform-manifest-link')).not.toBeNull(),
  )
  await act(async () => {
    container.querySelector<HTMLButtonElement>('.soha-platform-manifest-link')!.click()
    await flush()
  })
  expect(document.querySelector('.ant-drawer-open [role="dialog"]')).not.toBeNull()
}

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'matchMedia',
    vi.fn((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  )
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  testState.apiGet.mockReset()
  testState.permissionKeys = ['delivery.applications.view']
  testState.scope = { clusterId: null, namespace: null }
  respondWith([])
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})

afterEach(async () => {
  await act(async () => root.unmount())
  queryClient.clear()
  container.remove()
  vi.unstubAllGlobals()
})

it('does not query manifests until a cluster is selected', async () => {
  await renderPage()
  expect(testState.apiGet.mock.calls.some(([path]) => path.startsWith('/delivery/manifest'))).toBe(
    false,
  )
  expect(container.textContent).toContain('请先选择集群')
})

it('shows the application source, desired and observed manifest state and hides denied evidence', async () => {
  testState.scope = { clusterId: 'cluster-1', namespace: 'demo' }
  respondWith([manifest()], [deployment])
  await renderPage()
  await vi.waitFor(() => expect(container.textContent).toContain('实际 v2'))

  expect(testState.apiGet).toHaveBeenCalledWith(
    '/delivery/manifest-deployments?packageId=manifest-1&page=1&pageSize=100',
  )
  expect(container.textContent).toContain('服务 service-1')
  expect(container.textContent).toContain('期望 v3')
  expect(container.textContent).toContain('1 个资源')

  testState.apiGet.mockImplementation(async (path) => {
    if (path.startsWith('/delivery/manifest-deployments'))
      throw new ApiError(403, 'Deployment evidence denied', { method: 'GET' })
    throw new Error('Unexpected unrelated refetch')
  })
  await act(async () => {
    await queryClient.invalidateQueries({
      predicate: (query) => query.queryKey.includes('deployments'),
    })
    await flush()
  })
  expect(container.textContent).not.toContain('实际 v2')
  expect(container.textContent).toContain('Deployment evidence denied')
})

it.each([403, 503])(
  'presents deployment %i feedback inside the open drawer with one visible owner',
  async (status) => {
    testState.scope = { clusterId: 'cluster-1', namespace: 'demo' }
    respondWith([manifest()], [deployment])
    await renderPage()
    await vi.waitFor(() => expect(container.textContent).toContain('实际 v2'))
    await openDetails()

    testState.apiGet.mockImplementation(async (path) => {
      if (path.startsWith('/delivery/manifest-deployments'))
        throw new ApiError(status, 'Deployment evidence unavailable', { method: 'GET' })
      throw new Error('Unexpected unrelated refetch')
    })
    await act(async () => {
      await queryClient.invalidateQueries({
        predicate: (query) => query.queryKey.includes('deployments'),
      })
      await flush()
    })

    const drawer = document.querySelector('.ant-drawer-open [role="dialog"]')!
    const feedback = document.querySelectorAll('.soha-read-feedback')
    expect(feedback).toHaveLength(1)
    expect(feedback[0].closest('[role="dialog"]')).toBe(drawer)
    expect(drawer.textContent).toContain('Deployment evidence unavailable')
    expect(drawer.textContent).toContain(
      status === 403 ? '部署记录无访问权限' : '正在展示上次读取结果',
    )
    expect(drawer.textContent).not.toContain('实际 v2')
    expect(drawer.querySelector('button[aria-label="重试读取部署记录"]') !== null).toBe(
      status === 503,
    )
    expect(container.querySelector('.soha-read-feedback')).toBeNull()
  },
)

it('does not turn a failed manifest read into a successful zero total', async () => {
  testState.scope = { clusterId: 'cluster-1', namespace: 'demo' }
  testState.apiGet.mockImplementation(async (path) => {
    if (path === '/applications') return { data: [] }
    throw new ApiError(503, 'Manifest service unavailable', { method: 'GET' })
  })
  await renderPage()
  await vi.waitFor(() =>
    expect(container.querySelector('[role="status"]')?.textContent).toBe('清单未加载'),
  )
  expect(container.textContent).toContain('Manifest service unavailable')
})

it('keeps unobserved inventory and drift distinct from confirmed zero observations', async () => {
  testState.scope = { clusterId: 'cluster-1', namespace: 'demo' }
  respondWith([manifest()])
  await renderPage()
  await vi.waitFor(() => expect(container.textContent).toContain('未观测'))
  await openDetails()
  const drawer = document.querySelector('.ant-drawer-open [role="dialog"]')!
  expect(drawer.textContent).toContain('未知')
  expect(drawer.textContent).toContain('暂无资源清单记录')
  expect(drawer.textContent).toContain('尚无差异检测记录')
  expect(drawer.textContent).not.toContain('未发现差异')
  expect(drawer.textContent).not.toContain('实际资源 (0)')
})

it.each(['search', 'reset'] as const)(
  'closes selected details after %s even when the package still matches',
  async (action) => {
    testState.scope = { clusterId: 'cluster-1', namespace: 'demo' }
    respondWith([manifest()])
    await renderPage()
    await openDetails()
    await act(async () => {
      if (action === 'search') {
        const input = container.querySelector<HTMLInputElement>(
          'input[placeholder="搜索清单名称或描述"]',
        )!
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
          input,
          'API',
        )
        input.dispatchEvent(new Event('input', { bubbles: true }))
      }
    })
    await act(async () => {
      if (action === 'search')
        container
          .querySelector('form')!
          .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      else
        Array.from(container.querySelectorAll('button'))
          .find((button) => button.textContent === '重置')!
          .click()
      await flush()
    })
    if (action === 'search') {
      expect(
        testState.apiGet.mock.calls.some(
          ([path]) => new URL(path, 'http://localhost').searchParams.get('search') === 'API',
        ),
      ).toBe(true)
    }
    await vi.waitFor(() =>
      expect(document.querySelector('.ant-drawer-open [role="dialog"]')).toBeNull(),
    )
  },
)

it.each([1, 2])(
  'renders every binding from server package page %i without applying a second row slice',
  async (page) => {
    testState.scope = { clusterId: 'cluster-1', namespace: 'demo' }
    testState.apiGet.mockImplementation(async (path) => {
      if (path === '/applications') return { data: [] }
      if (path.startsWith('/delivery/manifest-deployments')) {
        return { data: { items: [], total: 0, page: 1, pageSize: 100 } }
      }
      if (path.startsWith('/delivery/manifest-packages')) {
        const current = Number(new URL(path, 'http://localhost').searchParams.get('page'))
        return {
          data: {
            items: Array.from({ length: 15 }, (_, index) =>
              manifest((current - 1) * 15 + index + 1, 2),
            ),
            total: 45,
            page: current,
            pageSize: 15,
          },
        }
      }
      throw new Error(`Unexpected GET ${path}`)
    })
    await renderPage()
    await vi.waitFor(() =>
      expect(container.querySelectorAll('tbody tr[data-row-key]').length).toBeGreaterThan(0),
    )
    if (page === 2) {
      await act(async () => {
        container.querySelector<HTMLElement>('.ant-pagination-item-2')!.click()
        await flush()
      })
    }
    await vi.waitFor(() =>
      expect(container.textContent).toContain(page === 1 ? 'env-1-1' : 'env-30-2'),
    )
    expect(
      testState.apiGet.mock.calls.some(([path]) => {
        const url = new URL(path, 'http://localhost')
        return (
          url.pathname === '/delivery/manifest-packages' &&
          url.searchParams.get('page') === String(page)
        )
      }),
    ).toBe(true)
    expect(container.textContent).toContain('45 份清单')
    expect(container.textContent).toContain('当前页 15 份清单 / 30 条绑定')
    expect(container.querySelectorAll('tbody tr[data-row-key]')).toHaveLength(30)
    for (let index = (page - 1) * 15 + 1; index <= page * 15; index++) {
      for (let binding = 1; binding <= 2; binding++) {
        expect(container.textContent).toContain(`env-${index}-${binding}`)
      }
    }
  },
)
