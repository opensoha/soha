/** @vitest-environment jsdom */

import { act } from 'react'
import type { ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PermissionSnapshot } from '@/types'
import { AccessUsersPage } from './access-pages'

const testState = vi.hoisted(() => ({
  snapshot: {
    permissionKeys: ['access.users.view'],
    visibleMenuIds: ['access'],
    visibleMenus: [{ id: 'access', path: '/access' }],
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
  AdminTable: ({ columns, dataSource, title, toolbar, toolbarExtra }: { columns: any[]; dataSource: Array<Record<string, unknown>>; title?: ReactNode; toolbar?: ReactNode; toolbarExtra?: ReactNode }) => (
    <div data-testid="admin-table">
      {title ? <div>{title}</div> : null}
      {toolbar ? <div>{toolbar}</div> : null}
      {toolbarExtra ? <div>{toolbarExtra}</div> : null}
      <div data-testid="headers">
        {columns.map((column, index) => (
          <span key={`${String(column?.key ?? column?.dataIndex ?? index)}`}>{typeof column?.title === 'string' ? column.title : ''}</span>
        ))}
      </div>
      {dataSource.map((record, rowIndex) => (
        <div key={String(record.id ?? rowIndex)} data-testid={`row-${String(record.id ?? rowIndex)}`}>
          {columns.map((column, columnIndex) => {
            const dataIndex = column?.dataIndex
            const value = typeof dataIndex === 'string' ? record[dataIndex] : undefined
            const content = column?.render ? column.render(value, record, rowIndex) : String(value ?? '')
            return (
              <div key={`${String(column?.key ?? dataIndex ?? columnIndex)}:${columnIndex}`} data-testid={`cell-${rowIndex}-${columnIndex}`}>
                {content}
              </div>
            )
          })}
        </div>
      ))}
    </div>
  ),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function setDefaultResponses() {
  testState.responses = {
    '/access/users': [
      {
        id: 'u-admin',
        username: 'admin',
        displayName: 'Admin',
        email: 'admin@kubecrux.local',
        status: 'active',
        roles: ['admin'],
        teams: [],
        tags: [],
        projects: [],
      },
    ],
    '/access/roles': [{ id: 'admin', name: 'admin' }],
    '/access/teams': [],
  }
}

async function renderWithProviders(node: ReactNode, route: string) {
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
    await Promise.resolve()
  })

  return container
}

async function waitForRow(container: HTMLElement, testId: string) {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    if (container.querySelector(`[data-testid="${testId}"]`)) {
      return
    }
    await act(async () => {
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
  }
}

describe('access users page columns', () => {
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

  it('renders avatar ahead of a standalone username column', async () => {
    const container = await renderWithProviders(<AccessUsersPage />, '/access/users')
    await waitForRow(container, 'row-u-admin')

    const headerText = container.querySelector('[data-testid="headers"]')?.textContent ?? ''
    expect(headerText).toContain('头像')
    expect(headerText).toContain('用户名')
    expect(headerText).toContain('显示名')

    const avatarCell = container.querySelector('[data-testid="cell-0-0"]')
    const usernameCell = container.querySelector('[data-testid="cell-0-1"]')
    const displayNameCell = container.querySelector('[data-testid="cell-0-2"]')

    expect(avatarCell).not.toBeNull()
    expect(usernameCell?.textContent).toContain('admin')
    expect(displayNameCell?.textContent).toContain('Admin')
  })

  it('filters users from the toolbar search input', async () => {
    testState.responses['/access/users'] = [
      {
        id: 'u-admin',
        username: 'admin',
        displayName: 'Admin',
        email: 'admin@kubecrux.local',
        status: 'active',
        roles: ['admin'],
        teams: [],
        tags: [],
        projects: [],
      },
      {
        id: 'u-dev',
        username: 'developer',
        displayName: 'Dev User',
        email: 'dev@kubecrux.local',
        status: 'active',
        roles: ['developer'],
        teams: ['platform'],
        tags: [],
        projects: [],
      },
    ]

    const container = await renderWithProviders(<AccessUsersPage />, '/access/users')
    const searchInput = container.querySelector('input[placeholder="搜索用户名、显示名、邮箱、角色或用户组"]') as HTMLInputElement | null

    expect(searchInput).not.toBeNull()

    await act(async () => {
      const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')?.set
      nativeInputValueSetter?.call(searchInput, 'developer')
      searchInput!.dispatchEvent(new Event('input', { bubbles: true }))
    })

    expect(container.querySelector('[data-testid="row-u-admin"]')).toBeNull()
    expect(container.querySelector('[data-testid="row-u-dev"]')).not.toBeNull()
  })
})
