/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeAll, beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { useAuthStore } from '@/stores/auth-store'
import { ApiError } from '@/services/api-error'
import { authProfileApi } from './profile-api'
import { CredentialRequestNotSentError } from './governed-credentials-api'
import { useProfileKeyRotation } from './use-profile-key-rotation'
import type { PersonalAccessToken } from '@/features/copilot'

vi.mock('./profile-api', () => ({
  authProfileApi: { rotateGatewayToken: vi.fn(), tokenRotation: vi.fn() },
}))
const owner = {
  userId: 'rotation-user',
  userName: 'Test',
  email: '',
  roles: [],
  teams: [],
  projects: [],
  tags: [],
}
const token: PersonalAccessToken = {
  id: 'old-test',
  userId: owner.userId,
  name: 'CLI',
  tokenPrefix: 'test-prefix',
  permissionKeys: ['platform.clusters.view'],
  scopes: [],
  createdBy: owner.userId,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}
let root: Root
let container: HTMLDivElement
let client: QueryClient
const onValue = vi.fn()
const onRefresh = vi.fn()
function Subject() {
  const userId = useAuthStore((state) => state.user?.userId)
  const rotation = useProfileKeyRotation(userId || '', onValue, onRefresh)
  return (
    <>
      <button disabled={rotation.blocked} onClick={() => rotation.mutate(token)}>
        开始轮换
      </button>
      {rotation.feedback}
    </>
  )
}
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 10))
  })
}
async function mount() {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <Subject />
      </QueryClientProvider>,
    ),
  )
  await flush()
}
async function click(text: string) {
  await act(async () =>
    [...container.querySelectorAll('button')]
      .find((button) => button.textContent?.includes(text))!
      .click(),
  )
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
  useAuthStore.setState({ user: owner, accessToken: 'independent-browser-test' })
})
afterEach(async () => {
  if (root) await act(async () => root.unmount())
  client?.clear()
  document.body.innerHTML = ''
})
describe('personal Key rotation recovery', () => {
  it('unblocks a new request when preflight proves no rotation POST was sent', async () => {
    vi.mocked(authProfileApi.rotateGatewayToken).mockRejectedValue(
      new CredentialRequestNotSentError(new Error('governed_ai_unsupported')),
    )
    await mount()
    await click('开始轮换')
    expect(sessionStorage.length).toBe(0)
    expect(container.textContent).not.toContain('保留原轮换请求供核对')
    expect(container.querySelector('button')?.disabled).toBe(false)
  })
  it('retains an earlier unknown outcome when a retry fails before its POST', async () => {
    vi.mocked(authProfileApi.rotateGatewayToken).mockRejectedValueOnce(new ApiError(0, 'Lost'))
    await mount()
    await click('开始轮换')
    const saved = sessionStorage.getItem('soha-pat-rotation:rotation-user')
    vi.mocked(authProfileApi.rotateGatewayToken).mockRejectedValueOnce(
      new CredentialRequestNotSentError(new Error('Key list unavailable')),
    )
    await click('重试原轮换')
    expect(sessionStorage.getItem('soha-pat-rotation:rotation-user')).toBe(saved)
    expect(container.textContent).toContain('保留原轮换请求供核对')
  })
  it('persists the recovery ID before sending and keeps new plaintext out of mutation caches', async () => {
    vi.mocked(authProfileApi.rotateGatewayToken).mockImplementation(async (_id, input) => {
      const saved = JSON.parse(sessionStorage.getItem('soha-pat-rotation:rotation-user')!)
      expect(saved.requestId).toBe(input.rotationRequestId)
      const remaining = Date.parse(input.expiresAt!) - Date.now()
      expect(remaining).toBeGreaterThan(89 * 86400000)
      expect(remaining).toBeLessThan(91 * 86400000)
      return { data: { token: { ...token, id: 'new-test' }, value: 'sensitive-test-new-key' } }
    })
    await mount()
    await click('开始轮换')
    expect(onValue).toHaveBeenCalledWith('sensitive-test-new-key', 'test-prefix')
    expect(
      JSON.stringify(
        client
          .getMutationCache()
          .getAll()
          .map((mutation) => mutation.state),
      ),
    ).not.toContain('sensitive-test-new-key')
    expect(sessionStorage.length).toBe(0)
    expect(container.textContent).toContain('旧 Key 已撤销')
  })
  it('reuses the original request on retry and resolves an outcome without replaying plaintext', async () => {
    vi.mocked(authProfileApi.rotateGatewayToken).mockRejectedValue(
      new ApiError(0, 'Connection lost'),
    )
    await mount()
    await click('开始轮换')
    const first = vi.mocked(authProfileApi.rotateGatewayToken).mock.calls[0][1]
    await click('重试原轮换')
    expect(vi.mocked(authProfileApi.rotateGatewayToken).mock.calls[1][1]).toEqual(first)
    vi.mocked(authProfileApi.tokenRotation).mockResolvedValue({
      rotationRequestId: first.rotationRequestId!,
      tokenKind: 'personal_access_token',
      previousTokenId: token.id,
      replacementTokenId: 'replacement-test',
      status: 'completed',
      rotatedAt: '2026-01-01T00:00:00Z',
      newValueRetrievable: false,
    })
    await click('核对轮换结果')
    expect(onValue).not.toHaveBeenCalled()
    expect(container.textContent).toContain('replacement-test')
    expect(container.textContent).toContain('新明文不能恢复')
    expect(sessionStorage.length).toBe(0)
  })
  it('does not show a late plaintext response after the browser account changes', async () => {
    let release!: (response: Awaited<ReturnType<typeof authProfileApi.rotateGatewayToken>>) => void
    vi.mocked(authProfileApi.rotateGatewayToken).mockReturnValue(
      new Promise((resolve) => {
        release = resolve
      }),
    )
    await mount()
    await click('开始轮换')
    await act(async () => useAuthStore.setState({ user: { ...owner, userId: 'other-user' } }))
    await act(async () =>
      release({ data: { token: { ...token, id: 'new-test' }, value: 'sensitive-test-late-key' } }),
    )
    await flush()
    expect(onValue).not.toHaveBeenCalled()
    expect(container.textContent).not.toContain('sensitive-test-late-key')
    expect(sessionStorage.getItem('soha-pat-rotation:rotation-user')).not.toBeNull()
  })
})
