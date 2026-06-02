/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PermissionSnapshot } from '@/types'
import { AccessCenterPage, AccessRolesPage } from './access-pages'
import { AccessScopeGrantsPage } from './scope-grants-page'

const testState = vi.hoisted(() => ({
  snapshot: {
    permissionKeys: [],
    visibleMenuIds: [],
    visibleMenus: [],
  } as PermissionSnapshot,
  responses: {} as Record<string, unknown>,
}))

vi.mock('@/features/auth/permission-snapshot', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth/permission-snapshot')>('@/features/auth/permission-snapshot')
  return {
    ...actual,
    usePermissionSnapshot: () => ({
      data: { data: testState.snapshot },
      isLoading: false,
    }),
  }
})

vi.mock('@/services/api-client', () => ({
  api: {
    get: vi.fn((path: string) => Promise.resolve({ data: testState.responses[path] ?? [] })),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    dataSource,
    headerExtra,
    title,
    toolbarExtra,
  }: {
    dataSource: unknown[]
    headerExtra?: React.ReactNode
    title?: React.ReactNode
    toolbarExtra?: React.ReactNode
  }) => (
    <div data-testid="admin-table">
      {title ? <div>{title}</div> : null}
      {headerExtra ? <div>{headerExtra}</div> : null}
      {toolbarExtra ? <div>{toolbarExtra}</div> : null}
      {`rows:${dataSource.length}`}
    </div>
  ),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function setSnapshot(permissionKeys: string[], visibleMenuIds: string[] = [], visibleMenus: PermissionSnapshot['visibleMenus'] = []) {
  testState.snapshot = {
    permissionKeys,
    visibleMenuIds,
    visibleMenus,
  }
}

function setDefaultResponses() {
  testState.responses = {
    '/access/users': [],
    '/access/roles': [],
    '/access/teams': [],
    '/access/policies': [],
    '/access/scope-grants': [],
    '/applications': [],
  }
}

async function renderWithProviders(node: React.ReactNode, route: string) {
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

  await act(async () => {
    await Promise.resolve()
  })

  return container
}

function getButtonTexts(container: HTMLElement) {
  return Array.from(container.querySelectorAll('button'))
    .map((node) => node.textContent?.trim() ?? '')
    .filter(Boolean)
}

describe('frontend access authorization splits', () => {
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
    setSnapshot([])
    setDefaultResponses()
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
    vi.clearAllMocks()
  })

  it('renders an empty redirect shell for the access parent route when a child is accessible', async () => {
    setSnapshot(['access.roles.view'], ['access'], [{ id: 'access', path: '/access' }])

    const container = await renderWithProviders(<AccessCenterPage />, '/access/roles')

    expect(container.textContent?.trim() ?? '').toBe('')
  })

  it('shows the role page and hides manage actions for view-only role access', async () => {
    setSnapshot(['access.roles.view'], ['access'], [{ id: 'access', path: '/access' }])

    const container = await renderWithProviders(<AccessRolesPage />, '/access/roles')

    expect(container.textContent).toContain('角色管理')
    expect(getButtonTexts(container)).not.toContain('添加角色')
  })

  it('shows the role create action when the manage permission is present', async () => {
    setSnapshot(['access.roles.view', 'access.roles.manage'], ['access'], [{ id: 'access', path: '/access' }])

    const container = await renderWithProviders(<AccessRolesPage />, '/access/roles')

    expect(container.textContent).toContain('角色管理')
    expect(getButtonTexts(container)).toContain('添加角色')
  })

  it('blocks the scope-grants page without the dedicated view permission', async () => {
    const container = await renderWithProviders(<AccessScopeGrantsPage />, '/access/scope-grants')

    expect(container.textContent).toContain('当前账号没有授权范围页面权限。')
  })

  it('keeps scope-grants mutations hidden for view-only access', async () => {
    setSnapshot(['access.scope-grants.view'])

    const container = await renderWithProviders(<AccessScopeGrantsPage />, '/access/scope-grants')

    expect(container.textContent).toContain('授权范围')
    expect(getButtonTexts(container)).not.toContain('新建授权项')
  })

  it('shows the scope-grants create action when the manage permission is present', async () => {
    setSnapshot(['access.scope-grants.view', 'access.scope-grants.manage'])

    const container = await renderWithProviders(<AccessScopeGrantsPage />, '/access/scope-grants')

    expect(container.textContent).toContain('授权范围')
    expect(getButtonTexts(container)).toContain('新建授权项')
  })
})
