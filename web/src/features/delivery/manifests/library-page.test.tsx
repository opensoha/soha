/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ManifestLibraryPage } from './library-page'

const testState = vi.hoisted(() => ({
  permissions: [] as string[],
  apiGet: vi.fn(async (path: string) => {
    if (path.startsWith('/delivery/manifest-packages')) {
      return {
        data: {
          items: [
            {
              id: 'manifest-1',
              name: 'Payments ingress',
              applicationId: 'payments',
              renderer: 'raw_yaml',
              status: 'draft',
              currentRevision: 0,
              files: [],
              bindings: [],
              createdAt: '2026-07-30T00:00:00Z',
              updatedAt: '2026-07-30T00:00:00Z',
            },
          ],
          total: 1,
          page: 1,
          pageSize: 20,
        },
      }
    }
    return { data: [] }
  }),
}))

vi.mock('@/features/auth', () => ({
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, permission: string) =>
    Boolean(snapshot?.permissionKeys?.includes(permission)),
  usePermissionSnapshot: () => ({ data: { data: { permissionKeys: testState.permissions } } }),
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: testState.apiGet,
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    empty,
  }: {
    columns: any[]
    dataSource: any[]
    empty?: ReactNode
  }) => (
    <div>
      {dataSource.length === 0 ? empty : null}
      {dataSource.map((item) => (
        <div key={item.id}>
          {columns.map((column, index) => (
            <div key={column.key ?? column.dataIndex ?? index}>
              {column.render?.(item[column.dataIndex], item, 0) ?? item[column.dataIndex]}
            </div>
          ))}
        </div>
      ))}
    </div>
  ),
}))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  testState.permissions = []
  testState.apiGet.mockClear()
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  window.matchMedia = vi.fn().mockImplementation(() => ({
    matches: false,
    media: '',
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
})

afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
})

async function renderPage(route: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[route]}>
        <QueryClientProvider client={queryClient}>
          <AntdApp>
            <ManifestLibraryPage />
          </AntdApp>
        </QueryClientProvider>
      </MemoryRouter>,
    )
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

describe('ManifestLibraryPage', () => {
  it('keeps the manifest index read-only even for editors', async () => {
    testState.permissions = [
      'delivery.application.update',
      'delivery.application.delete',
      'delivery.releases.trigger',
    ]
    await renderPage('/delivery/manifests')

    expect(container.textContent).not.toContain('新建清单包')
    expect(container.querySelector('[aria-label="编辑清单"]')).toBeNull()
    expect(container.querySelector('[aria-label="发布版本"]')).toBeNull()
    expect(container.querySelector('[aria-label="删除清单"]')).toBeNull()
    expect(container.querySelector('.soha-manifest-name')?.tagName).toBe('DIV')
  })
})
