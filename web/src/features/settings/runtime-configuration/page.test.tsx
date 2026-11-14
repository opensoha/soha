/** @vitest-environment jsdom */

import { ApiError } from '@/services/api-error'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { act, type ComponentProps } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterAll, afterEach, beforeAll, expect, it, vi } from 'vitest'
import { RuntimeConfigurationPage } from './page'

const feedbackRenders = vi.hoisted(() => ({ count: 0 }))
vi.mock('@/components/request-feedback', async () => {
  const actual = await vi.importActual<typeof import('@/components/request-feedback')>(
    '@/components/request-feedback',
  )
  return {
    ...actual,
    ReadFeedback: (props: ComponentProps<typeof actual.ReadFeedback>) => {
      if (props.label === '运行时配置' && ++feedbackRenders.count > 30)
        throw new Error('Configuration refusal caused a render loop')
      return <actual.ReadFeedback {...props} />
    },
  }
})

vi.mock('@/features/auth', async () => ({
  ...(await vi.importActual<typeof import('@/features/auth')>('@/features/auth')),
  usePermissionSnapshot: () => ({
    data: {
      data: {
        permissionKeys: ['settings.runtime-config.view'],
        visibleMenuIds: [],
        visibleMenus: [],
      },
    },
    isLoading: false,
  }),
}))
vi.mock('@/services/api-client', () => ({
  api: {
    get: vi.fn(async () => {
      throw new ApiError(403, 'Configuration refused', { method: 'GET' })
    }),
    getEnvelope: vi.fn(async () => ({ items: [] })),
  },
}))

let root: Root | undefined
let client: QueryClient | undefined
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
    configurable: true,
    value: vi.fn(() => ({
      matches: false,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
    })),
  })
})
afterAll(() => vi.unstubAllGlobals())
afterEach(async () => {
  await act(async () => root?.unmount())
  client?.clear()
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

it('settles a refused configuration read without repeatedly resetting the empty draft', async () => {
  const container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () =>
    root?.render(
      <App>
        <QueryClientProvider client={client!}>
          <RuntimeConfigurationPage />
        </QueryClientProvider>
      </App>,
    ),
  )
  await vi.waitFor(async () => {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(container.textContent).toContain('运行时配置无访问权限')
  })
  expect(feedbackRenders.count).toBeLessThan(20)
  expect(container.textContent).toContain('Configuration refused')
  expect(container.querySelector('.soha-runtime-config-item')).toBeNull()
})
