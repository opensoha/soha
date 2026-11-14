/** @vitest-environment jsdom */

import { ApiError } from '@/services/api-error'

import { restoreAuthSession } from '@/features/auth/auth-api'
import { useAuthStore } from '@/stores/auth-store'
import type { User } from '@/types'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AuthGuard } from './auth-guard'

vi.mock('@/features/auth/auth-api', () => ({
  restoreAuthSession: vi.fn(),
}))

const permissionState = vi.hoisted(() => ({ error: null as Error | null }))

vi.mock('@/features/auth/permission-snapshot', () => ({
  permissionSnapshotQueryKey: ['access/permission-snapshot'],
  usePermissionSnapshot: () => ({
    data: {
      data: {
        permissionKeys: [],
        visibleMenuIds: [],
        visibleMenus: [],
      },
    },
    isLoading: false,
    isFetching: false,
    isError: Boolean(permissionState.error),
    error: permissionState.error,
  }),
}))

vi.mock('@/routes/meta', () => ({
  canAccessRoute: vi.fn(() => true),
  findFirstAccessiblePath: vi.fn(() => '/'),
  findPreferredWorkspace: vi.fn(() => 'resource'),
  getRouteMeta: vi.fn(() => undefined),
}))

vi.mock('@/stores/preferences-store', () => ({
  usePreferencesStore: (selector: (state: { currentWorkspace: string }) => unknown) =>
    selector({ currentWorkspace: 'resource' }),
}))

const user: User = {
  userId: 'user-1',
  userName: 'opensoha',
  email: 'opensoha@soha.local',
  roles: [],
  teams: [],
  projects: [],
  tags: [],
}

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

async function flushReact() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

async function renderGuard() {
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
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/clusters']}>
          <Routes>
            <Route element={<AuthGuard />}>
              <Route path="/clusters" element={<div>protected page</div>} />
            </Route>
            <Route path="/login" element={<div>login page</div>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })
  await flushReact()

  return container
}

describe('auth guard', () => {
  beforeEach(() => {
    permissionState.error = null
    vi.useFakeTimers()
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    useAuthStore.getState().clearAuth()
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
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.clearAllMocks()
    useAuthStore.getState().clearAuth()
  })

  it('waits and retries when session restore is temporarily unavailable', async () => {
    vi.mocked(restoreAuthSession)
      .mockResolvedValueOnce('unavailable')
      .mockImplementationOnce(async () => {
        useAuthStore.getState().setUser(user)
        useAuthStore.getState().setTokens('new-access-token')
        return 'authenticated'
      })

    const container = await renderGuard()

    expect(container.textContent).toContain('正在恢复登录状态')
    expect(container.textContent).toContain('后端服务暂时不可用')
    expect(container.textContent).not.toContain('login page')

    await act(async () => {
      vi.advanceTimersByTime(2_000)
    })
    await flushReact()

    expect(restoreAuthSession).toHaveBeenCalledTimes(2)
    expect(container.textContent).toContain('protected page')
    expect(container.textContent).not.toContain('login page')
  })

  it.each([401, 403])('does not trust cached permissions after status %s', async (status) => {
    useAuthStore.getState().setUser(user)
    useAuthStore.getState().setTokens('synthetic-access')
    permissionState.error = new ApiError(status, 'Permission snapshot refused', { method: 'GET' })
    const container = await renderGuard()
    expect(container.textContent).toContain(status === 401 ? '权限登录状态失效' : '权限无访问权限')
    expect(container.textContent).not.toContain('protected page')
  })

  it('redirects to login when session restore is rejected', async () => {
    vi.mocked(restoreAuthSession).mockResolvedValueOnce('unauthenticated')

    const container = await renderGuard()

    expect(container.textContent).toContain('login page')
  })
})
