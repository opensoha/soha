/** @vitest-environment jsdom */

import { act } from 'react'
import type { ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PermissionSnapshot } from '@/types'
import { SettingsCenterPage } from './settings-pages'

const testState = vi.hoisted(() => ({
  snapshot: {
    permissionKeys: ['settings.ai.view'],
    visibleMenuIds: ['settings'],
    visibleMenus: [{ id: 'settings', path: '/settings' }],
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
    get: vi.fn((path: string) => Promise.resolve({ data: testState.responses[path] ?? {} })),
    put: vi.fn(),
    post: vi.fn(),
    delete: vi.fn(),
    upload: vi.fn(),
  },
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({ title, dataSource }: { title?: ReactNode; dataSource: unknown[] }) => (
    <div data-testid="admin-table">
      {title ? <div>{title}</div> : null}
      <div>{`rows:${dataSource.length}`}</div>
    </div>
  ),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function setDefaultResponses() {
  testState.responses = {
    '/settings/ai': {
      provider: {
        enabled: true,
        baseUrl: 'https://api.example.com',
        apiKey: 'secret',
        model: 'gpt-test',
      },
      skillsRegistry: [],
    },
    '/copilot/data-sources': [],
    '/copilot/analysis-profiles': [],
    '/copilot/automation-policies': [],
    '/copilot/data-source-capabilities': [],
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
    await new Promise((resolve) => setTimeout(resolve, 0))
  })

  return container
}

describe('settings ai page rendering', () => {
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

  it('renders AI settings content on /settings/ai without collapsing to a blank page', async () => {
    const container = await renderWithProviders(<SettingsCenterPage />, '/settings/ai')

    expect(container.textContent).toContain('设置中心')
    expect(container.textContent).toContain('AI 设置')
    expect(container.textContent).toContain('Base URL')
    expect(container.textContent).toContain('Skills Registry')
  })
})
