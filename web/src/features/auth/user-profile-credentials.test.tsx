/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { App, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { beforeAll, beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { useAuthStore } from '@/stores/auth-store'
import { permissionSnapshotQueryKey } from './permission-snapshot'
import { UserProfilePage } from './user-profile-page'
import { authProfileApi } from './profile-api'

vi.mock('./profile-api', () => ({
  authProfileApi: {
    get: vi.fn(),
    listIdentityProviders: vi.fn(),
    listGatewayTokens: vi.fn(),
    createGatewayToken: vi.fn(),
    revokeGatewayToken: vi.fn(),
    rotateGatewayToken: vi.fn(),
    tokenRotation: vi.fn(),
    update: vi.fn(),
    changePassword: vi.fn(),
    beginIdentityLink: vi.fn(),
  },
}))
const user = {
  userId: 'profile-user',
  userName: 'Test',
  email: '',
  roles: [],
  teams: [],
  projects: [],
  tags: [],
}
const profile = {
  userId: user.userId,
  username: 'test',
  displayName: 'Test',
  email: 'test@example.test',
  status: 'active',
  roles: [],
  teams: [],
  projects: [],
  tags: [],
  identities: [],
  sessions: [],
}
const token = {
  id: 'pat-new',
  userId: user.userId,
  name: 'test',
  tokenPrefix: 'prefix-test',
  permissionKeys: ['ai.gateway.invoke'],
  scopes: [],
  createdBy: user.userId,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}
let root: Root
let client: QueryClient
let container: HTMLDivElement
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
async function mount(
  permissionKeys = ['ai.gateway.view', 'ai.gateway.invoke', 'platform.clusters.view'],
) {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(permissionSnapshotQueryKey, {
    data: { permissionKeys, visibleMenuIds: [], visibleMenus: [] },
  })
  await act(async () =>
    root.render(
      <ConfigProvider locale={zhCN}>
        <App>
          <QueryClientProvider client={client}>
            <MemoryRouter>
              <UserProfilePage />
            </MemoryRouter>
          </QueryClientProvider>
        </App>
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
  sessionStorage.clear()
  useAuthStore.setState({ user, accessToken: 'browser-test' })
  vi.mocked(authProfileApi.get).mockResolvedValue({ data: profile })
  vi.mocked(authProfileApi.listIdentityProviders).mockResolvedValue({ data: [] })
  vi.mocked(authProfileApi.listGatewayTokens).mockResolvedValue({ data: [] })
  vi.mocked(authProfileApi.createGatewayToken).mockResolvedValue({
    data: { token, value: 'sensitive-created-pat' },
  })
})
afterEach(async () => {
  if (root) await act(async () => root.unmount())
  client?.clear()
  document.body.innerHTML = ''
  useAuthStore.setState({ user: null, accessToken: null })
})
describe('personal credential entry points', () => {
  it('creates an LLM Key with only Gateway invocation and a future 90-day expiry', async () => {
    await mount()
    await click('生成 LLM Key')
    await click('生成 key')
    expect(authProfileApi.createGatewayToken).toHaveBeenCalledTimes(1)
    const input = vi.mocked(authProfileApi.createGatewayToken).mock.calls[0][0]
    expect(input.permissionKeys).toEqual(['ai.gateway.invoke'])
    expect(Date.parse(input.expiresAt!) - Date.now()).toBeGreaterThan(89 * 86400000)
    expect(Date.parse(input.expiresAt!) - Date.now()).toBeLessThan(91 * 86400000)
    expect(
      JSON.stringify(
        client
          .getMutationCache()
          .getAll()
          .map((item) => item.state),
      ),
    ).not.toContain('sensitive-created-pat')
  })
  it('requires explicit CLI permissions and closes the form when the account changes', async () => {
    await mount()
    await click('生成 CLI PAT')
    await click('生成 key')
    expect(authProfileApi.createGatewayToken).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('请至少选择一项权限')
    await act(async () => useAuthStore.setState({ user: { ...user, userId: 'another-user' } }))
    await flush()
    expect(document.querySelector('.ant-modal:not(.ant-modal-hidden)')).toBeNull()
  })
  it('keeps credential creation disabled without the existing issue permission', async () => {
    await mount(['ai.gateway.view'])
    expect(button('生成 LLM Key').disabled).toBe(true)
    expect(button('生成 CLI PAT').disabled).toBe(true)
    expect(authProfileApi.createGatewayToken).not.toHaveBeenCalled()
  })
})
