/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DockerProjectLogsPanel } from './logs-panel'
import { DockerProjectTerminalPanel } from './terminal-panel'

const state = vi.hoisted(() => ({
  ticket: vi.fn(),
  terminals: [] as { dispose: ReturnType<typeof vi.fn> }[],
}))
vi.mock('@/features/auth', () => ({
  buildSameOriginStreamURL: (path: string) => new URL(path, 'ws://localhost'),
  withStreamTicket: state.ticket,
}))
vi.mock('@/features/observability', () => ({
  LogExplorer: () => <div data-testid="log-explorer" />,
}))
vi.mock('@/components/status-tag', () => ({
  StatusTag: ({ value }: { value: string }) => <span data-testid="session-state">{value}</span>,
}))
vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    dispose = vi.fn()
    constructor() {
      state.terminals.push(this)
    }
    loadAddon() {}
    open() {}
    writeln() {}
    write() {}
    onData() {}
  },
}))
vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    fit() {}
  },
}))

class Socket {
  static OPEN = 1
  static instances: Socket[] = []
  readyState = 1
  onopen?: () => void
  onclose?: () => void
  onerror?: () => void
  send = vi.fn()
  close = vi.fn()
  constructor() {
    Socket.instances.push(this)
  }
}
let root: Root, container: HTMLDivElement
const base = {
  enabled: true,
  projectId: 'project-1',
  serviceName: 'service',
  serviceOptions: [{ label: 'service', value: 'service' }],
  onServiceChange: () => {},
}
async function render(node: ReactNode) {
  await act(async () => {
    root.render(node)
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
}
async function click(label: string) {
  const button = [...container.querySelectorAll('button')].find(
    (node) => node.textContent?.replace(/\s/g, '') === label,
  )!
  expect(button).toBeDefined()
  await act(async () => {
    button.click()
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
}
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  state.ticket.mockReset().mockResolvedValue('ws://localhost/terminal')
  state.terminals.length = 0
  Socket.instances.length = 0
  vi.stubGlobal('WebSocket', Socket)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  )
  window.matchMedia = vi.fn(() => ({
    matches: false,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent() {},
  })) as unknown as typeof window.matchMedia
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  vi.unstubAllGlobals()
})

it.each([
  { servicesLoading: true, servicesError: false, title: '正在读取服务' },
  { servicesLoading: false, servicesError: true, title: '服务列表读取失败' },
  { servicesLoading: false, servicesError: false, title: '没有可用服务' },
])('shows $title instead of starting an untargeted runtime', async ({ title, ...status }) => {
  for (const Panel of [DockerProjectLogsPanel, DockerProjectTerminalPanel]) {
    await render(<Panel {...base} serviceName="" serviceOptions={[]} {...status} />)
    expect(container.textContent).toContain(title)
    if (status.servicesError) expect(container.textContent).not.toContain('没有可用服务')
    expect(container.querySelector('[data-testid="log-explorer"]')).toBeNull()
    expect(state.ticket).not.toHaveBeenCalled()
  }
})

it('keeps ticket failure feedback until an explicit retry and updates state after disconnect', async () => {
  state.ticket.mockRejectedValueOnce(new Error('private transport details'))
  await render(<DockerProjectTerminalPanel {...base} />)
  expect(
    [...container.querySelectorAll('button')].find(
      (button) => button.textContent?.replace(/\s/g, '') === '断开',
    )?.disabled,
  ).toBe(true)
  await click('连接')
  expect(container.textContent).toContain('获取连接凭据失败')
  expect(container.textContent).not.toContain('private transport details')
  expect(Socket.instances).toHaveLength(0)
  await click('重试连接')
  expect(state.ticket).toHaveBeenCalledTimes(2)
  expect(Socket.instances).toHaveLength(1)
  await act(async () => Socket.instances[0].onopen?.())
  await click('断开')
  expect(Socket.instances[0].close).toHaveBeenCalledOnce()
  expect(container.querySelector('[data-testid="session-state"]')?.textContent).toBe('closed')
  expect(container.textContent).not.toContain('获取连接凭据失败')
})

it('preserves an unchanged session and releases it when the project scope changes', async () => {
  await render(<DockerProjectTerminalPanel {...base} />)
  await click('连接')
  await act(async () => Socket.instances[0].onopen?.())
  await render(<DockerProjectTerminalPanel {...base} />)
  expect(Socket.instances[0].close).not.toHaveBeenCalled()
  await render(<DockerProjectTerminalPanel {...base} projectId="project-2" />)
  expect(Socket.instances[0].close).toHaveBeenCalledOnce()
  expect(state.terminals[0].dispose).toHaveBeenCalledOnce()
  expect(state.ticket).toHaveBeenCalledTimes(1)
  expect(container.querySelector('[data-testid="session-state"]')?.textContent).toBe('idle')
})

it('does not establish a connection from a ticket that arrives after changing scope', async () => {
  let finish!: (url: string) => void
  state.ticket.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  await render(<DockerProjectTerminalPanel {...base} />)
  await click('连接')
  await render(<DockerProjectTerminalPanel {...base} projectId="project-2" />)
  await act(async () => {
    finish('ws://localhost/old-project')
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
  expect(Socket.instances).toHaveLength(0)
  expect(state.terminals).toHaveLength(0)
})

it('explains socket failure without automatically reconnecting', async () => {
  await render(<DockerProjectTerminalPanel {...base} />)
  await click('连接')
  await act(async () => Socket.instances[0].onerror?.())
  expect(container.textContent).toContain('Shell 连接失败')
  expect(container.textContent).toContain('重试连接')
  expect(state.ticket).toHaveBeenCalledTimes(1)
})
