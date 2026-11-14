/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { VMConsole } from './vm-console'
import { virtualizationKeys } from './keys'

const state = vi.hoisted(() => ({
  read: vi.fn(),
  ticket: vi.fn(),
  sessions: [] as {
    disconnect: ReturnType<typeof vi.fn>
    dispatchEvent: (event: Event) => boolean
  }[],
}))
vi.mock('./virtualization-api', () => ({ virtualizationApi: { vmConsoleURL: state.read } }))
vi.mock('@/features/auth', () => ({
  buildSameOriginStreamURL: (url: string) => url,
  withStreamTicket: state.ticket,
}))
vi.mock('@novnc/novnc', () => ({
  default: class extends EventTarget {
    scaleViewport = false
    resizeSession = false
    disconnect = vi.fn()
    constructor() {
      super()
      state.sessions.push(this)
    }
  },
}))

let root: Root
let client: QueryClient
let container: HTMLDivElement
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 40))
  })
}
async function render(vmId = 'vm-1') {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <App>
          <VMConsole vmId={vmId} />
        </App>
      </QueryClientProvider>,
    ),
  )
  await settle()
}
beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  window.matchMedia = vi.fn(() => ({
    matches: false,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })) as unknown as typeof window.matchMedia
  state.sessions.length = 0
  state.read
    .mockReset()
    .mockResolvedValue({ ready: true, type: 'vnc', url: 'ws://localhost/console' })
  state.ticket.mockReset().mockResolvedValue('ws://localhost/console?ticket=synthetic')
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  container.remove()
})

describe('VM console session continuity', () => {
  it('keeps its connected session during rerender and VM record refresh, then cleans up on leaving', async () => {
    await render()
    expect(state.sessions).toHaveLength(1)
    await act(async () => state.sessions[0].dispatchEvent(new Event('connect')))
    expect(container.textContent).toContain('已连接')
    await render()
    await act(async () => {
      await client.invalidateQueries({ queryKey: virtualizationKeys.vmDetail('vm-1') })
    })
    expect(state.read).toHaveBeenCalledTimes(1)
    expect(state.sessions).toHaveLength(1)
    expect(state.sessions[0].disconnect).not.toHaveBeenCalled()
    await act(async () => root.render(null))
    expect(state.sessions[0].disconnect).toHaveBeenCalledTimes(1)
  })
  it('requests a new ticket only after explicit reconnection and disconnects the prior attempt', async () => {
    await render()
    await act(async () =>
      state.sessions[0].dispatchEvent(
        new CustomEvent('securityfailure', { detail: { reason: 'Ticket expired' } }),
      ),
    )
    expect(container.textContent).toContain('Ticket expired')
    const reconnect = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('重新连接'),
    )!
    await act(async () => reconnect.click())
    await settle()
    expect(state.read).toHaveBeenCalledTimes(2)
    expect(state.ticket).toHaveBeenCalledTimes(2)
    expect(state.sessions).toHaveLength(2)
    expect(state.sessions[0].disconnect).toHaveBeenCalledTimes(1)
  })
  it('isolates the previous session when the VM identity changes', async () => {
    await render()
    await act(async () => state.sessions[0].dispatchEvent(new Event('connect')))
    await render('vm-2')
    expect(state.read).toHaveBeenCalledTimes(2)
    expect(state.sessions).toHaveLength(2)
    expect(state.sessions[0].disconnect).toHaveBeenCalledTimes(1)
  })
  it('does not create a session for an unavailable provider or a failed ticket', async () => {
    state.read.mockResolvedValue({ ready: false, message: 'Provider unavailable' })
    await render()
    expect(state.sessions).toHaveLength(0)
    expect(state.ticket).not.toHaveBeenCalled()
    expect(container.textContent).toContain('Provider unavailable')
    state.read.mockResolvedValue({ ready: true, type: 'vnc', url: 'ws://localhost/console' })
    state.ticket.mockRejectedValue(new Error('Ticket denied'))
    const retry = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('重新检查控制台'),
    )!
    await act(async () => retry.click())
    await settle()
    expect(state.sessions).toHaveLength(0)
    expect(container.textContent).toContain('Ticket denied')
  })
})
