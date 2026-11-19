/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ManifestLibraryPage } from './library-page'

const testState = vi.hoisted(() => ({
  permissions: [] as string[],
  paginated: false,
  apiGet: vi.fn(async (path: string) => {
    if (path.startsWith('/delivery/manifest-packages')) {
      const page = Number(new URLSearchParams(path.split('?')[1]).get('page') || 1)
      const item = {
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
      }
      return {
        data: {
          items: testState.paginated
            ? Array.from({ length: 20 }, (_, index) => ({
                ...item,
                id: `manifest-${page}-${index}`,
              }))
            : [item],
          total: testState.paginated ? 41 : 1,
          page,
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

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  testState.permissions = []
  testState.paginated = false
  testState.apiGet.mockClear()
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  const getComputedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
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
  vi.restoreAllMocks()
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
  it('uses the server total and retains filters when changing pages', async () => {
    testState.paginated = true
    await renderPage('/delivery/manifests?search=payments&applicationId=payments')

    expect(container.textContent).toContain('当前 1-20 / 41 条')
    const next = container.querySelector<HTMLElement>('.ant-pagination-next button')
    expect(next).not.toBeNull()
    await act(async () => {
      next!.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(testState.apiGet).toHaveBeenCalledWith(
      '/delivery/manifest-packages?applicationId=payments&search=payments&page=2&pageSize=20',
    )
    expect(container.textContent).toContain('当前 21-40 / 41 条')
  })
})
