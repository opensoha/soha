/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { NetworkEnrollmentPane } from './enrollment-pane'

const apiMocks = vi.hoisted(() => ({
  createNetworkRuntimeEnrollment: vi.fn(),
  listNetworkRuntimeEnrollments: vi.fn(),
  revokeNetworkRuntimeEnrollment: vi.fn(),
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
  apiMocks.listNetworkRuntimeEnrollments.mockResolvedValue([])
  apiMocks.createNetworkRuntimeEnrollment.mockResolvedValue({
    enrollment: {
      id: 'enrollment-1',
      challengeId: 'challenge-1',
      runtimeId: 'endpoint-1',
      runtimeKind: 'endpoint',
      deviceId: 'device-1',
      subjectId: 'user-1',
      status: 'pending',
      expiresAt: '2026-09-02T00:10:00Z',
      createdBy: 'operator-1',
      createdAt: '2026-09-02T00:00:00Z',
    },
    token: 'one-time-enrollment-token-123456',
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

function setInputValue(input: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(input, value)
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

describe('network runtime enrollment pane', () => {
  it('clears the one-time token from the mutation cache when its modal closes', async () => {
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
            <NetworkEnrollmentPane canCreate canRevoke />
          </QueryClientProvider>
        </AntdApp>,
      )
    })
    await settle(queryClient)

    await clickButton('创建注册令牌')
    await act(async () => {
      setInputValue(document.querySelector('#runtimeId') as HTMLInputElement, ' endpoint-1 ')
      setInputValue(document.querySelector('#deviceId') as HTMLInputElement, ' device-1 ')
      setInputValue(document.querySelector('#subjectId') as HTMLInputElement, ' user-1 ')
    })
    await clickButton('创建')
    await settle(queryClient)

    expect(apiMocks.createNetworkRuntimeEnrollment).toHaveBeenCalledWith(
      {
        runtimeId: 'endpoint-1',
        runtimeKind: 'endpoint',
        deviceId: 'device-1',
        subjectId: 'user-1',
        ttlSeconds: 600,
      },
      expect.anything(),
    )
    expect(document.body.textContent).toContain('one-time-enrollment-token-123456')

    await clickButton('我已保存')
    await settle(queryClient)

    expect(document.body.textContent).not.toContain('one-time-enrollment-token-123456')
    expect(
      JSON.stringify(
        queryClient
          .getMutationCache()
          .getAll()
          .map((item) => item.state.data),
      ),
    ).not.toContain('one-time-enrollment-token-123456')
  })
})
