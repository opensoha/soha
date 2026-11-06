/** @vitest-environment jsdom */

import { transferableAbortController } from 'node:util'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { PlatformAccessControlClusterRoleBindingDetailPage } from './clusterrolebindings/detail-page'
import { PlatformAccessControlClusterRoleDetailPage } from './clusterroles/detail-page'
import { PlatformAccessControlRoleBindingDetailPage } from './rolebindings/detail-page'
import { PlatformAccessControlRoleDetailPage } from './roles/detail-page'
import { PlatformAccessControlServiceAccountDetailPage } from './serviceaccounts/detail-page'

const deleteRequest = vi.hoisted(() => vi.fn<() => Promise<unknown>>())

vi.mock('@/services/api-client', () => ({
  api: {
    delete: deleteRequest,
    get: vi.fn(async (path: string) => {
      const url = new URL(path, 'http://localhost')
      return {
        data: url.pathname.endsWith('/detail')
          ? {
              name: 'reader',
              namespace: url.searchParams.get('namespace') ?? undefined,
              allowedActions: ['delete'],
            }
          : { content: '' },
      }
    }),
  },
}))

vi.mock('@/features/auth', () => ({
  hasPermission: () => true,
  hasAllowedAction: () => true,
  usePermissionSnapshot: () => ({ data: { data: {} }, error: null, isPending: false }),
}))
vi.mock('@/features/copilot', () => ({ useAIPageContext: () => undefined }))
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => ({
    clusterId: 'cluster-a',
    namespace: 'team-a',
    setClusterId: () => undefined,
  }),
}))
vi.mock('@/features/platform/cluster-capabilities', () => ({
  useClusterCapability: () => ({ disabled: false, isLoading: false }),
}))
vi.mock('@/i18n', async (original) => ({
  ...(await original<typeof import('@/i18n')>()),
  useI18n: () => ({ localeCode: 'en_US', t: (_key: string, fallback?: string) => fallback }),
}))
vi.mock('@/features/platform/shared/resource-detail-header', async (original) => ({
  ...(await original<typeof import('@/features/platform/shared/resource-detail-header')>()),
  ResourceDetailTabs: () => null,
}))

const cases = [
  ['serviceaccounts', <PlatformAccessControlServiceAccountDetailPage />],
  ['roles', <PlatformAccessControlRoleDetailPage />],
  ['clusterroles', <PlatformAccessControlClusterRoleDetailPage />],
  ['rolebindings', <PlatformAccessControlRoleBindingDetailPage />],
  ['clusterrolebindings', <PlatformAccessControlClusterRoleBindingDetailPage />],
] as const

const roots: Root[] = []
const clients: QueryClient[] = []

beforeAll(() => {
  // React Router's Node Request needs signals from the same realm, rather than jsdom's.
  const nativeController = transferableAbortController()
  vi.stubGlobal('AbortController', nativeController.constructor)
  vi.stubGlobal('AbortSignal', nativeController.signal.constructor)
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    }),
  })
})

afterAll(() => {
  vi.unstubAllGlobals()
})

beforeEach(() => {
  deleteRequest.mockReset()
})

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  for (const client of clients.splice(0)) client.clear()
  document.body.innerHTML = ''
})

async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function beginDelete(kind: string, page: ReactNode) {
  let resolveDelete!: (value: unknown) => void
  deleteRequest.mockImplementation(
    () =>
      new Promise((resolve) => {
        resolveDelete = resolve
      }),
  )
  const collection = `/platform-access-control/${kind}`
  const router = createMemoryRouter(
    [
      { path: `${collection}/:name`, element: page },
      { path: collection, element: <div>Collection</div> },
      { path: '/elsewhere', element: <div>Elsewhere</div> },
    ],
    { initialEntries: [`${collection}/reader?namespace=team-a`] },
  )
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <AntdApp>
          <RouterProvider router={router} />
        </AntdApp>
      </QueryClientProvider>,
    )
  })
  await flush()
  await flush()
  const deleteButton = Array.from(container.querySelectorAll('button')).find(
    (button) => button.textContent?.trim() === 'Delete',
  )
  expect(deleteButton).toBeDefined()
  await act(async () => deleteButton!.click())
  const confirm = document.querySelector<HTMLButtonElement>(
    '[role="dialog"] button[aria-label="Delete"]',
  )
  expect(confirm).not.toBeNull()
  await act(async () => confirm!.click())
  await flush()
  expect(deleteRequest).toHaveBeenCalledTimes(1)
  expect(confirm!.getAttribute('aria-busy')).toBe('true')
  return {
    router,
    collection,
    finish: async () => {
      await act(async () => resolveDelete(undefined))
      await flush()
      await flush()
    },
  }
}

describe.each(cases)('%s delete completion navigation', (kind, page) => {
  it('returns to its collection when the deleted detail is still active', async () => {
    const { router, collection, finish } = await beginDelete(kind, page)
    await finish()
    expect(router.state.location.pathname).toBe(collection)
  })

  it('keeps the destination after leaving while deletion is pending', async () => {
    const { router, finish } = await beginDelete(kind, page)
    await act(async () => {
      await router.navigate('/elsewhere')
    })
    await finish()
    expect(router.state.location.pathname).toBe('/elsewhere')
  })

  it('keeps the new scope after changing cluster while deletion is pending', async () => {
    const { router, collection, finish } = await beginDelete(kind, page)
    const destination = `${collection}/reader?clusterId=cluster-b&namespace=team-b`
    await act(async () => {
      await router.navigate(destination)
    })
    await finish()
    expect(router.state.location.pathname + router.state.location.search).toBe(destination)
  })
})
