/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { NetworkAccessGrantPane } from './access-grant-pane'

const apiMocks = vi.hoisted(() => ({
  createNetworkAccessGrant: vi.fn(),
  listNetworkAccessGrants: vi.fn(),
  revokeNetworkAccessGrant: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...apiMocks,
}))

const roots: Root[] = []

beforeAll(() => {
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
    value: vi.fn(() => ({
      matches: false,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
  const getComputedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
})

beforeEach(() => {
  vi.clearAllMocks()
  apiMocks.listNetworkAccessGrants.mockResolvedValue([])
  apiMocks.createNetworkAccessGrant.mockResolvedValue({
    grant: {
      id: 'grant-1',
      subjectId: 'user-1',
      deviceId: 'device-1',
      siteId: 'site-1',
      networkSpaceId: 'space-1',
      mode: 'internal_ztna',
      resourceIds: ['resource-db'],
      policyVersion: 7,
      status: 'issued',
      createdBy: 'user-1',
      createdAt: '2026-09-03T00:00:00Z',
      expiresAt: '2026-09-03T00:05:00Z',
    },
    token: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA',
  })
})

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function settle(queryClient: QueryClient) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => {
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    if (queryClient.isFetching() === 0 && queryClient.isMutating() === 0) return
  }
}

function setInputValue(id: string, value: string) {
  const input = document.querySelector(id)
  if (!(input instanceof HTMLInputElement || input instanceof HTMLTextAreaElement)) {
    throw new Error(`Input not found: ${id}`)
  }
  Object.getOwnPropertyDescriptor(Object.getPrototypeOf(input), 'value')?.set?.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
  input.dispatchEvent(new Event('change', { bubbles: true }))
}

async function clickButton(text: string) {
  const button = Array.from(document.querySelectorAll('button')).find(
    (candidate) => candidate.textContent?.replace(/\s+/g, '') === text.replace(/\s+/g, ''),
  )
  if (!(button instanceof HTMLButtonElement)) throw new Error(`Button not found: ${text}`)
  await act(async () => button.click())
}

describe('network access grant pane', () => {
  it('shows a grant token once and clears it from the mutation cache on close', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
    })
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    roots.push(root)
    await act(async () => {
      root.render(
        <AntdApp>
          <QueryClientProvider client={queryClient}>
            <NetworkAccessGrantPane canCreate canRevoke />
          </QueryClientProvider>
        </AntdApp>,
      )
    })
    await settle(queryClient)

    await clickButton('创建访问授权')
    await act(async () => {
      setInputValue('#deviceId', ' device-1 ')
      setInputValue('#siteId', ' site-1 ')
      setInputValue('#networkSpaceId', ' space-1 ')
      setInputValue('#resourceIdsText', ' resource-db, resource-api ')
    })
    await clickButton('创建')
    await settle(queryClient)

    expect(apiMocks.createNetworkAccessGrant).toHaveBeenCalledWith(
      {
        deviceId: 'device-1',
        siteId: 'site-1',
        networkSpaceId: 'space-1',
        mode: 'internal_ztna',
        resourceIds: ['resource-api', 'resource-db'],
        ttlSeconds: 300,
      },
      expect.anything(),
    )
    expect(document.body.textContent).toContain('AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA')

    await clickButton('我已保存')
    await settle(queryClient)

    expect(document.body.textContent).not.toContain('AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA')
    expect(
      JSON.stringify(
        queryClient
          .getMutationCache()
          .getAll()
          .map((item) => item.state.data),
      ),
    ).not.toContain('AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA')
  })
})
