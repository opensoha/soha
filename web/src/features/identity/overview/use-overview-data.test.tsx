import { ApiError } from '@/services/api-error'
/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { useIdentityOverviewData } from './use-overview-data'

const testState = vi.hoisted(() => ({
  permissionKeys: [] as string[],
  applications: vi.fn(),
  providers: vi.fn(),
  outposts: vi.fn(),
  sessions: vi.fn(),
  audit: vi.fn(),
  runtime: vi.fn(),
}))

vi.mock('@/features/auth', () => ({
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, key: string) =>
    snapshot?.permissionKeys?.includes(key) ?? false,
  usePermissionSnapshot: () => ({
    data: { data: { permissionKeys: testState.permissionKeys } },
  }),
}))

vi.mock('../applications', () => ({
  identityApplicationQueries: {
    list: () => ({
      queryKey: ['test', 'identity-overview', 'applications'],
      queryFn: testState.applications,
    }),
  },
}))

vi.mock('../providers', () => ({
  identityProviderQueries: {
    list: () => ({
      queryKey: ['test', 'identity-overview', 'providers'],
      queryFn: testState.providers,
    }),
  },
}))

vi.mock('../outposts', () => ({
  identityOutpostQueries: {
    list: () => ({
      queryKey: ['test', 'identity-overview', 'outposts'],
      queryFn: testState.outposts,
    }),
  },
}))

vi.mock('./queries', () => ({
  identityOverviewQueries: {
    sessions: () => ({
      queryKey: ['test', 'identity-overview', 'sessions'],
      queryFn: testState.sessions,
    }),
    audit: () => ({
      queryKey: ['test', 'identity-overview', 'audit'],
      queryFn: testState.audit,
    }),
  },
}))

vi.mock('../runtime', () => ({
  identityRuntimeQueries: {
    capabilities: () => ({
      queryKey: ['test', 'identity-overview', 'runtime'],
      queryFn: testState.runtime,
    }),
  },
}))

const roots: Root[] = []
const containers: HTMLElement[] = []

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})

beforeEach(() => {
  vi.clearAllMocks()
  testState.permissionKeys = []
  testState.applications.mockResolvedValue([])
  testState.providers.mockResolvedValue([])
  testState.outposts.mockResolvedValue([])
  testState.sessions.mockResolvedValue([])
  testState.audit.mockResolvedValue([])
  testState.runtime.mockResolvedValue({})
})

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  for (const container of containers.splice(0)) container.remove()
})

async function settle(queryClient: QueryClient) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => {
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    if (queryClient.isFetching() === 0) return
  }
}

async function renderProbe() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)
  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  function Probe() {
    const overview = useIdentityOverviewData()
    return (
      <div>
        <span data-testid="applications">
          {overview.applications.map((item) => item.name).join(',')}
        </span>
        <span data-testid="permissions">
          {Object.entries(overview.permissions)
            .filter(([, allowed]) => allowed)
            .map(([key]) => key)
            .join(',')}
        </span>
        <button onClick={overview.refreshAll}>refresh</button>
      </div>
    )
  }

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <Probe />
      </QueryClientProvider>,
    )
  })
  await settle(queryClient)
  return { container, queryClient }
}

async function clickRefresh(container: HTMLElement, queryClient: QueryClient) {
  await act(async () => {
    container.querySelector('button')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })
  await settle(queryClient)
}

describe('useIdentityOverviewData', () => {
  it('loads and refreshes all five authorized read models', async () => {
    testState.permissionKeys = [
      'identity.applications.view',
      'identity.providers.view',
      'identity.outposts.view',
      'identity.policies.view',
      'system.online-users.view',
      'identity.audit.view',
    ]
    const { container, queryClient } = await renderProbe()

    expect(testState.applications).toHaveBeenCalledOnce()
    expect(testState.providers).toHaveBeenCalledOnce()
    expect(testState.outposts).toHaveBeenCalledOnce()
    expect(testState.sessions).toHaveBeenCalledOnce()
    expect(testState.audit).toHaveBeenCalledOnce()

    await clickRefresh(container, queryClient)

    expect(testState.applications).toHaveBeenCalledTimes(2)
    expect(testState.providers).toHaveBeenCalledTimes(2)
    expect(testState.outposts).toHaveBeenCalledTimes(2)
    expect(testState.sessions).toHaveBeenCalledTimes(2)
    expect(testState.audit).toHaveBeenCalledTimes(2)
  })

  it('does not fetch or imperatively refresh unauthorized queries', async () => {
    const { container, queryClient } = await renderProbe()
    await clickRefresh(container, queryClient)

    expect(testState.applications).not.toHaveBeenCalled()
    expect(testState.providers).not.toHaveBeenCalled()
    expect(testState.outposts).not.toHaveBeenCalled()
    expect(testState.sessions).not.toHaveBeenCalled()
    expect(testState.audit).not.toHaveBeenCalled()
  })

  it('uses the System permissions for sessions and audit only', async () => {
    testState.permissionKeys = ['system.online-users.view', 'system.audit.view']
    const { container } = await renderProbe()

    expect(container.querySelector('[data-testid="permissions"]')?.textContent).toBe(
      'sessions,audit',
    )
    expect(testState.sessions).toHaveBeenCalledOnce()
    expect(testState.audit).toHaveBeenCalledOnce()
    expect(testState.applications).not.toHaveBeenCalled()
    expect(testState.providers).not.toHaveBeenCalled()
    expect(testState.outposts).not.toHaveBeenCalled()
  })
  it('keeps transient cached application reads but removes denied cache', async () => {
    testState.permissionKeys = ['identity.applications.view']
    testState.applications.mockResolvedValue([{ id: 'cached', name: 'Cached application' }])
    const { container, queryClient } = await renderProbe()
    expect(container.querySelector('[data-testid="applications"]')?.textContent).toBe(
      'Cached application',
    )
    testState.applications.mockRejectedValue(
      new ApiError(503, 'Unavailable', { method: 'GET', path: '/identity/applications' }),
    )
    await clickRefresh(container, queryClient)
    expect(container.querySelector('[data-testid="applications"]')?.textContent).toBe(
      'Cached application',
    )
    testState.applications.mockRejectedValue(
      new ApiError(403, 'Denied', { method: 'GET', path: '/identity/applications' }),
    )
    await clickRefresh(container, queryClient)
    expect(container.querySelector('[data-testid="applications"]')?.textContent).toBe('')
  })
})
