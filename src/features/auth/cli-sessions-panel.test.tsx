/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeAll, beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { useAuthStore } from '@/stores/auth-store'
import { ApiError } from '@/services/api-error'
import { ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { CLISessionsPanel } from './cli-sessions-panel'
import { cliAuthApi } from './cli-auth-api'

vi.mock('./cli-auth-api', () => ({
  cliAuthApi: { sessions: vi.fn(), revokeSession: vi.fn() },
}))
const user = {
  userId: 'sessions-user',
  userName: 'Test',
  email: '',
  roles: [],
  teams: [],
  projects: [],
  tags: [],
}
const session = (id: string, status: 'active' | 'revoked' = 'active') => ({
  id,
  deviceName: id,
  clientVersion: 'task115',
  status,
  current: false,
  createdAt: '2026-01-01T00:00:00Z',
  lastSeenAt: '2026-01-01T00:00:00Z',
  expiresAt: '2026-12-01T00:00:00Z',
  absoluteExpiresAt: '2026-12-20T00:00:00Z',
})
let root: Root
let container: HTMLDivElement
let client: QueryClient
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
async function mount() {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () =>
    root.render(
      <ConfigProvider locale={zhCN}>
        <QueryClientProvider client={client}>
          <CLISessionsPanel />
        </QueryClientProvider>
      </ConfigProvider>,
    ),
  )
  await flush()
}
function button(text: string) {
  return [...document.querySelectorAll('button')].find((item) =>
    item.textContent?.replace(/\s/g, '').includes(text.replace(/\s/g, '')),
  )!
}
async function click(text: string) {
  await act(async () => button(text).click())
  await flush()
}
beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
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
})
beforeEach(() => {
  vi.clearAllMocks()
  useAuthStore.setState({ user, accessToken: 'browser-test' })
  vi.mocked(cliAuthApi.revokeSession).mockResolvedValue(undefined)
})
afterEach(async () => {
  if (root) await act(async () => root.unmount())
  client?.clear()
  document.body.innerHTML = ''
  useAuthStore.setState({ user: null, accessToken: null })
})
describe('personal CLI sessions', () => {
  it('uses server cursors, disables revoked rows and resets the cursor on account changes', async () => {
    vi.mocked(cliAuthApi.sessions).mockImplementation(async (cursor) =>
      cursor
        ? { items: [session('second-terminal', 'revoked')] }
        : { items: [session('first-terminal')], nextCursor: 'opaque-next' },
    )
    await mount()
    expect(container.textContent).toContain('first-terminal')
    expect(button('上一页').disabled).toBe(true)
    await click('下一页')
    expect(cliAuthApi.sessions).toHaveBeenLastCalledWith('opaque-next')
    expect(container.textContent).toContain('second-terminal')
    expect(button('撤销').disabled).toBe(true)
    expect(button('下一页').disabled).toBe(true)
    const before = vi.mocked(cliAuthApi.sessions).mock.calls.length
    await act(async () => useAuthStore.setState({ user: { ...user, userId: 'other-user' } }))
    await flush()
    expect(vi.mocked(cliAuthApi.sessions).mock.calls.slice(before)).toEqual([[undefined]])
    expect(container.textContent).toContain('第 1 页')
    expect(container.textContent).not.toContain('second-terminal')
  })
  it('requires confirmation, refreshes after revocation and retains read error feedback', async () => {
    vi.mocked(cliAuthApi.sessions).mockResolvedValue({ items: [session('terminal-to-revoke')] })
    await mount()
    await click('撤销')
    expect(cliAuthApi.revokeSession).not.toHaveBeenCalled()
    await click('确定')
    expect(cliAuthApi.revokeSession).toHaveBeenCalledWith('terminal-to-revoke', expect.anything())
    expect(cliAuthApi.sessions).toHaveBeenCalledTimes(2)
    expect(container.textContent).toContain('CLI 会话已撤销')
    vi.mocked(cliAuthApi.sessions).mockRejectedValue(new ApiError(503, 'Temporarily unavailable'))
    await act(async () => client.invalidateQueries())
    await flush()
    expect(document.body.textContent).toContain('Temporarily unavailable')
    expect(button('下一页').disabled).toBe(true)
  })
})
