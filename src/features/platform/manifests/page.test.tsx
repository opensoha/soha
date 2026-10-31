/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { PlatformManifestsPage } from './page'

const testState = vi.hoisted(() => ({
  apiGet: vi.fn(async (_path?: string) => ({
    data: { items: [] as unknown[], total: 0, page: 1, pageSize: 15 },
  })),
  scope: { clusterId: null as string | null, namespace: null as string | null },
}))

vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))

vi.mock('@/services/api-client', () => ({
  api: { get: testState.apiGet, post: vi.fn(), put: vi.fn(), delete: vi.fn() },
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    empty,
    title,
  }: {
    columns: Array<{
      key?: string
      title?: ReactNode
      dataIndex?: string | string[]
      render?: (value: unknown, row: Record<string, unknown>) => ReactNode
    }>
    dataSource: Array<Record<string, unknown>>
    empty?: ReactNode
    title?: ReactNode
  }) => (
    <div>
      <div data-testid="manifest-total">{title}</div>
      {columns
        .filter((column) => column.key !== 'actions')
        .map((column, index) => (
          <span key={column.key ?? String(column.dataIndex ?? index)}>{column.title}</span>
        ))}
      {dataSource.map((row, rowIndex) => (
        <div key={rowIndex}>
          {columns
            .filter((column) => column.key !== 'actions')
            .map((column, columnIndex) => {
              const path = Array.isArray(column.dataIndex)
                ? column.dataIndex
                : column.dataIndex
                  ? [column.dataIndex]
                  : []
              const value = path.reduce<unknown>(
                (current, key) =>
                  current && typeof current === 'object'
                    ? (current as Record<string, unknown>)[key]
                    : undefined,
                row,
              )
              return (
                <span key={column.key ?? String(column.dataIndex ?? columnIndex)}>
                  {column.render ? column.render(value, row) : String(value ?? '')}
                </span>
              )
            })}
        </div>
      ))}
      {dataSource.length === 0 ? empty : null}
    </div>
  ),
}))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  testState.apiGet.mockClear()
  testState.scope = { clusterId: null, namespace: null }
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})

afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
})

it('does not query manifests until a cluster is selected', async () => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () => {
    root.render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <PlatformManifestsPage />
        </QueryClientProvider>
      </MemoryRouter>,
    )
    await Promise.resolve()
  })

  expect(testState.apiGet).not.toHaveBeenCalled()
  expect(container.textContent).toContain('请先选择集群')
})

it('shows the application source, desired and observed manifest state', async () => {
  testState.scope = { clusterId: 'cluster-1', namespace: 'demo' }
  testState.apiGet.mockImplementation(async (path = '') => {
    if (path.startsWith('/delivery/manifest-deployments')) {
      return {
        data: {
          items: [
            {
              id: 'deployment-1',
              packageId: 'manifest-1',
              bindingId: 'binding-1',
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
            },
          ],
          total: 1,
          page: 1,
          pageSize: 100,
        },
      }
    }
    return {
      data: {
        items: [
          {
            id: 'manifest-1',
            name: 'API Ingress',
            applicationId: 'app-1',
            serviceId: 'service-1',
            renderer: 'raw_yaml',
            status: 'published',
            currentRevision: 3,
            files: [],
            bindings: [
              {
                id: 'binding-1',
                applicationEnvironmentId: 'environment-1',
                environmentKey: 'dev',
                clusterId: 'cluster-1',
                namespace: 'demo',
                overlay: {},
                status: 'drifted',
              },
            ],
            createdAt: '2026-08-27T09:00:00Z',
            updatedAt: '2026-08-27T10:00:00Z',
          },
        ],
        total: 1,
        page: 1,
        pageSize: 15,
      },
    }
  })
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  await act(async () => {
    root.render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <PlatformManifestsPage />
        </QueryClientProvider>
      </MemoryRouter>,
    )
  })
  await act(async () => new Promise((resolve) => setTimeout(resolve, 0)))
  await act(async () => new Promise((resolve) => setTimeout(resolve, 0)))

  expect(testState.apiGet).toHaveBeenCalledWith(
    '/delivery/manifest-deployments?packageId=manifest-1&page=1&pageSize=100',
  )
  expect(container.textContent).toContain('服务 service-1')
  expect(container.textContent).toContain('期望 v3')
  expect(container.textContent).toContain('实际 v2')
  expect(container.textContent).toContain('1 个资源')

  testState.apiGet.mockImplementation(async (path = '') => {
    if (path.startsWith('/delivery/manifest-deployments'))
      throw new ApiError(403, 'Deployment evidence denied', { method: 'GET' })
    throw new Error('Unexpected unrelated refetch')
  })
  await act(async () => {
    await queryClient.invalidateQueries({
      predicate: (query) => query.queryKey.includes('deployments'),
    })
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  expect(container.textContent).not.toContain('实际 v2')
  expect(container.textContent).toContain('Deployment evidence denied')
})

it('does not turn a failed manifest read into a successful zero total', async () => {
  testState.scope = { clusterId: 'cluster-1', namespace: 'demo' }
  testState.apiGet.mockRejectedValueOnce(
    new ApiError(503, 'Manifest service unavailable', { method: 'GET' }),
  )
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () =>
    root.render(
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <PlatformManifestsPage />
        </QueryClientProvider>
      </MemoryRouter>,
    ),
  )
  await vi.waitFor(() =>
    expect(container.querySelector('[data-testid="manifest-total"]')?.textContent).toBe(
      '清单未加载',
    ),
  )
  client.clear()
})
