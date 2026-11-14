/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { VirtualMachineAccessBinding } from '@opensoha/contracts/gen/ts/sohaapi'
import { VMSSHTerminal } from './ssh-terminal'
const state = vi.hoisted(() => ({
  create: vi.fn(),
  close: vi.fn(),
  write: vi.fn(),
  input: (_data: string) => {},
  resize: (_size: { cols: number; rows: number }) => {},
}))
vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 80
    rows = 24
    loadAddon() {}
    open() {}
    focus() {}
    dispose() {}
    write(data: unknown) {
      state.write(data)
    }
    onData(callback: typeof state.input) {
      state.input = callback
    }
    onResize(callback: typeof state.resize) {
      state.resize = callback
    }
  },
}))
vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    fit() {}
  },
}))
vi.mock('@/features/auth', () => ({
  buildSameOriginStreamURL: () => new URL('ws://localhost/stream'),
  withStreamTicket: async (url: string) => url + '?ticket=one-use',
}))
vi.mock('@/theme/app-theme', () => ({ readTerminalThemeColors: () => ({}) }))
vi.mock('../queries', () => ({
  virtualizationQueries: {
    sshSession: (id: string) => ({
      queryKey: ['ssh', id],
      queryFn: async () => ({ id, status: 'pending' }),
      enabled: Boolean(id),
    }),
  },
}))
vi.mock('../mutations', () => ({
  virtualizationMutations: {
    createSSHSession: () => ({ mutationFn: state.create }),
    closeSSHSession: () => ({ mutationFn: state.close }),
  },
}))
vi.mock('../virtualization-api', () => ({ virtualizationApi: { closeSSHSession: state.close } }))
class Socket {
  static OPEN = 1
  static instances: Socket[] = []
  readyState = 1
  send = vi.fn()
  close = vi.fn()
  onopen?: () => void
  onmessage?: (event: { data: string }) => void
  onclose?: () => void
  onerror?: () => void
  constructor() {
    Socket.instances.push(this)
  }
}
let root: Root, node: HTMLDivElement, client: QueryClient
const binding = { vmId: 'vm-one', revision: 2 } as VirtualMachineAccessBinding
beforeEach(() => {
  Object.assign(globalThis, {
    IS_REACT_ACT_ENVIRONMENT: true,
    WebSocket: Socket,
    ResizeObserver: class {
      observe() {}
      disconnect() {}
    },
  })
  vi.clearAllMocks()
  Socket.instances = []
  state.create.mockResolvedValue({ id: 'session-one' })
  state.close.mockResolvedValue({ id: 'session-one', status: 'closed' })
  node = document.createElement('div')
  document.body.appendChild(node)
  root = createRoot(node)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  node.remove()
})
async function connect() {
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <VMSSHTerminal binding={binding} enabled />
      </QueryClientProvider>,
    )
  })
  await act(async () => {
    node.querySelector('button')?.click()
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
}
it('preserves terminal bytes and routes resize through the same authorized session', async () => {
  await connect()
  const socket = Socket.instances[0]
  expect(socket).toBeDefined()
  expect(state.create).toHaveBeenCalledWith(
    { id: 'vm-one', input: { bindingRevision: 2, cols: 80, rows: 24 } },
    expect.anything(),
  )
  await act(async () => {
    socket.onopen?.()
    socket.onmessage?.({
      data: JSON.stringify({ type: 'output', data: 'AP8=', dataEncoding: 'base64' }),
    })
    state.input('中')
    state.resize({ cols: 100, rows: 30 })
  })
  expect(state.write).toHaveBeenCalledWith(Uint8Array.from([0, 255]))
  expect(socket.send.mock.calls.map(([data]) => JSON.parse(data))).toEqual([
    { type: 'input', dataEncoding: 'base64', data: '5Lit' },
    { type: 'resize', cols: 100, rows: 30 },
  ])
  await act(async () => {
    Array.from(node.querySelectorAll('button'))
      .find((button) => button.textContent === '关闭会话')
      ?.click()
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(state.close).toHaveBeenCalledWith('session-one', expect.anything())
})
it('closes a late creation reply after the terminal is unmounted', async () => {
  let reply: (value: { id: string }) => void = () => undefined
  state.create.mockImplementation(
    () =>
      new Promise((resolve) => {
        reply = resolve
      }),
  )
  await connect()
  await act(async () => root.unmount())
  await act(async () => {
    reply({ id: 'late-session' })
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(state.close).toHaveBeenCalledWith('late-session')
  expect(Socket.instances).toHaveLength(0)
})

it('explains a disconnected shell while retaining the unknown guest result', async () => {
  await connect()
  await act(async () => Socket.instances[0].onopen?.())
  await act(async () => {
    Array.from(node.querySelectorAll('button'))
      .find((button) => button.textContent === '关闭会话')
      ?.click()
    await new Promise((resolve) => setTimeout(resolve, 20))
    client.setQueryData(['ssh', 'session-one'], {
      id: 'session-one',
      status: 'unknown',
      reason: 'SSH connection ended without a terminal receipt; guest effects require verification',
    })
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(node.textContent).toContain('连接已断开 · 执行结果待核验')
  expect(node.textContent).toContain('已启动的命令或后台进程是否结束仍需核验')
  expect(client.getQueryData(['ssh', 'session-one'])).toMatchObject({ status: 'unknown' })
  expect(Socket.instances[0].close).toHaveBeenCalled()
})
