/** @vitest-environment jsdom */
import { ApiError } from '@/services/api-error'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { observabilityKeys } from '../keys'
import { AlertIntegrationsPage } from './page'

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api }))
vi.mock('@/features/auth', () => ({
  hasPermission: () => true,
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))
vi.mock('@/components/admin-table', () => ({
  AdminTable: ({ headerExtra }: { headerExtra: ReactNode }) => <>{headerExtra}</>,
}))

let container: HTMLDivElement
let root: Root
let client: QueryClient
const token = 'qa-once-returned-token'
const result = {
  id: 'integration-qa',
  name: 'Integration QA',
  token,
  enabled: true,
  status: 'pending',
  integrationType: 'alertmanager_v1',
}

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
    value: () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    }),
  })
  const getStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getStyle(element))
})
beforeEach(() => {
  vi.useFakeTimers()
  api.get.mockReset().mockResolvedValue({ data: [] })
  api.post.mockReset().mockResolvedValue({ data: result })
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
})
afterEach(async () => {
  try {
    await act(async () => root.unmount())
    client.clear()
    // Run delayed form feedback while jsdom still owns its window.
    await act(async () => vi.runOnlyPendingTimersAsync())
    expect(vi.getTimerCount()).toBe(0)
  } finally {
    vi.useRealTimers()
    container.remove()
  }
})

async function settle(action: () => void = () => {}) {
  await act(async () => {
    action()
    await vi.advanceTimersByTimeAsync(20)
  })
}
function button(label: string) {
  const item = Array.from(document.querySelectorAll('button')).find(
    (node) => node.textContent?.replace(/\s/g, '') === label,
  )
  expect(item, label).toBeTruthy()
  return item!
}
async function renderAndSubmit() {
  await act(async () =>
    root.render(
      <App>
        <QueryClientProvider client={client}>
          <AlertIntegrationsPage />
        </QueryClientProvider>
      </App>,
    ),
  )
  await settle()
  await settle(() => button('新建集成').click())
  const name = document.querySelector<HTMLInputElement>('#name')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      name,
      'Integration QA',
    )
    name.dispatchEvent(new Event('input', { bubbles: true }))
    name.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await settle(() => button('保存').click())
}
function tokenValues() {
  return Array.from(document.querySelectorAll('input'))
    .map((node) => node.value)
    .filter((value) => value === token)
}

describe('integration private result ownership', () => {
  it('keeps a one-time token out of mutation receipts and clears its private display on close', async () => {
    await renderAndSubmit()
    await vi.waitFor(() => expect(api.post).toHaveBeenCalledOnce())
    await settle()
    expect(tokenValues()).toEqual([token])
    expect(
      JSON.stringify(
        client
          .getMutationCache()
          .getAll()
          .map((mutation) => mutation.state.data),
      ),
    ).not.toContain(token)
    await settle(() => button('完成').click())
    expect(tokenValues()).toEqual([])
  })

  it('does not reveal a late token after the source read is refused', async () => {
    let complete!: (value: { data: typeof result }) => void
    api.post.mockImplementation(
      () =>
        new Promise((resolve) => {
          complete = resolve
        }),
    )
    await renderAndSubmit()
    expect(api.post).toHaveBeenCalledOnce()
    api.get.mockRejectedValue(new ApiError(403, 'refused', { method: 'GET' }))
    await act(async () => {
      await client.refetchQueries({ queryKey: observabilityKeys.integrations.all })
    })
    await settle(() => complete({ data: result }))
    expect(tokenValues()).toEqual([])
    expect(document.body.textContent).not.toContain('集成 Token')
    expect(
      JSON.stringify(
        client
          .getMutationCache()
          .getAll()
          .map((mutation) => mutation.state.data),
      ),
    ).not.toContain(token)
  })
})
