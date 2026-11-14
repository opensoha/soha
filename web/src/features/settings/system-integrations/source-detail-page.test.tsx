/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import type { PermissionSnapshot } from '@/types'
import { SourceConnectionDetailPage } from './source-detail-page'

const apiPostMock = vi.hoisted(() => vi.fn(() => Promise.resolve({ data: { id: 'gitlab-main' } })))

vi.mock('@/features/auth/permission-snapshot', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth/permission-snapshot')>(
    '@/features/auth/permission-snapshot',
  )
  return {
    ...actual,
    usePermissionSnapshot: () => ({
      data: {
        data: {
          permissionKeys: [
            'settings.system-integrations.view',
            'settings.system-integrations.create',
          ],
          visibleMenuIds: [],
          visibleMenus: [],
        } satisfies PermissionSnapshot,
      },
      isLoading: false,
    }),
  }
})

vi.mock('@/services/api-client', () => ({
  api: {
    get: vi.fn(),
    getEnvelope: vi.fn(() => Promise.resolve({ items: [] })),
    patch: vi.fn(),
    post: apiPostMock,
  },
}))

let root: ReturnType<typeof createRoot> | undefined
let container: HTMLDivElement | undefined

async function flush() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

beforeAll(() => {
  const getComputedStyle = window.getComputedStyle.bind(window)
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
  vi.stubGlobal('getComputedStyle', (element: Element) => getComputedStyle(element))
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('ResizeObserver', ResizeObserverMock)
})
afterAll(() => vi.unstubAllGlobals())

afterEach(() => {
  act(() => root?.unmount())
  container?.remove()
  root = undefined
  container = undefined
  vi.clearAllMocks()
})

describe('SourceConnectionDetailPage', () => {
  it('selects a provider before saving its configuration from the form footer', async () => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
    })

    await act(async () => {
      root?.render(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={['/settings/source-control/new']}>
            <AntdApp>
              <Routes>
                <Route
                  path="/settings/source-control/new"
                  element={<SourceConnectionDetailPage />}
                />
                <Route
                  path="/settings/source-control/:integrationId"
                  element={<SourceConnectionDetailPage />}
                />
                <Route path="/settings/source-control" element={<div>list</div>} />
              </Routes>
            </AntdApp>
          </MemoryRouter>
        </QueryClientProvider>,
      )
    })

    const modal = document.body.querySelector<HTMLElement>('.ant-modal')
    expect(modal).not.toBeNull()
    expect(modal?.textContent).toContain('新增 Git')
    expect(modal?.textContent).toContain('选择 Provider')
    expect(modal?.textContent).toContain('连接配置')
    expect(modal?.textContent).not.toContain('全局代码源连接可被')
    expect(
      modal?.querySelector('.soha-step-form__content > div:not([hidden])')?.textContent,
    ).toContain('GitLab')

    const nextButton = Array.from(modal?.querySelectorAll('button') ?? []).find(
      (button) => button.textContent?.trim() === '下一步',
    )
    expect(nextButton).toBeDefined()
    expect(nextButton?.disabled).toBe(false)
    await act(async () => nextButton?.click())
    await flush()
    expect(modal?.textContent).not.toContain('请选择 Git Provider')
    expect(
      modal?.querySelector('.soha-step-form__content > div:first-child')?.hasAttribute('hidden'),
    ).toBe(true)

    const tokenInput = modal?.querySelector<HTMLInputElement>('input[id$="token"]')
    expect(tokenInput).not.toBeNull()
    const valueSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
    await act(async () => {
      valueSetter?.call(tokenInput, 'secret')
      tokenInput?.dispatchEvent(new Event('input', { bubbles: true }))
      tokenInput?.dispatchEvent(new Event('change', { bubbles: true }))
    })

    const saveButton = Array.from(
      modal?.querySelectorAll<HTMLButtonElement>('.soha-step-form__actions button') ?? [],
    ).find((button) => button.textContent?.replace(/\s/g, '') === '保存')
    expect(saveButton).toBeDefined()
    await act(async () => saveButton?.click())
    await flush()

    expect(apiPostMock).toHaveBeenCalledWith(
      '/system-integrations',
      expect.objectContaining({
        category: 'source_control',
        providerType: 'gitlab',
        credentials: [{ key: 'token', value: 'secret' }],
      }),
    )
  })
})
