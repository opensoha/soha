/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { beforeAll, beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { useAuthStore } from '@/stores/auth-store'
import { CLIDevicePage } from './cli-device-page'
import { cliAuthApi } from './cli-auth-api'
import { restoreAuthSession } from './auth-api'

vi.mock('./cli-auth-api', () => ({ cliAuthApi: { inspect: vi.fn(), decide: vi.fn() } }))
vi.mock('./auth-api', () => ({ restoreAuthSession: vi.fn(async () => 'unauthenticated') }))
vi.mock('./device-step-up', () => ({
  DeviceStepUp: ({ onVerified }: { onVerified: () => void }) => (
    <button onClick={onVerified}>完成已有 MFA</button>
  ),
}))
const user = {
  userId: 'u-test',
  userName: 'Test user',
  email: '',
  roles: [],
  teams: [],
  projects: [],
  tags: [],
}
const confirmation = () => ({
  grantId: 'grant-test',
  confirmationId: 'c'.repeat(43),
  deviceName: 'My terminal',
  clientVersion: 'task115',
  user,
  permissionKeys: ['platform.clusters.view'],
  expiresAt: new Date(Date.now() + 60000).toISOString(),
  confirmationExpiresAt: new Date(Date.now() + 30000).toISOString(),
  sessionAbsoluteExpiresIn: 2592000,
})
let root: Root
let container: HTMLDivElement
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 10))
  })
}
async function mount() {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  await act(async () =>
    root.render(
      <MemoryRouter>
        <CLIDevicePage />
      </MemoryRouter>,
    ),
  )
  await flush()
}
function button(text: string) {
  return [...container.querySelectorAll('button')].find((element) =>
    element.textContent?.replace(/\s/g, '').includes(text.replace(/\s/g, '')),
  )!
}
async function enterCode(value = '000042') {
  const input = container.querySelector('input')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await act(async () => {
    container
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
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
  history.replaceState(null, '', `/auth/device#handle=${'h'.repeat(43)}`)
  useAuthStore.setState({ user, accessToken: 'browser-test' })
  vi.mocked(cliAuthApi.inspect).mockImplementation(async () => confirmation())
  vi.mocked(cliAuthApi.decide).mockResolvedValue({
    grantId: 'grant-test',
    status: 'approved',
    expiresAt: new Date(Date.now() + 60000).toISOString(),
  })
})
afterEach(async () => {
  if (root) await act(async () => root.unmount())
  vi.useRealTimers()
  vi.restoreAllMocks()
  document.body.innerHTML = ''
  useAuthStore.setState({ user: null, accessToken: null })
})
describe('CLI browser authorization', () => {
  it('shows only server-confirmed expiry and changes the live announcement by minute', async () => {
    let now = Date.now()
    vi.spyOn(Date, 'now').mockImplementation(() => now)
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    vi.mocked(cliAuthApi.inspect).mockImplementation(async () => ({
      ...confirmation(),
      expiresAt: new Date(Date.now() + 120000).toISOString(),
    }))
    await mount()
    const timer = () => container.querySelector('[role="timer"]')!
    const announcement = () => container.querySelector('.soha-cli-device-sr-only')!.textContent
    expect(container.querySelector('[role="timer"]')).toBeNull()
    expect(container.querySelector('.soha-cli-device-sr-only')).toBeNull()
    expect(container.textContent).toContain('登录请求实际有效期以终端为准')
    await act(async () => {
      now += 61000
      vi.advanceTimersByTime(61000)
    })
    expect(container.querySelector('[role="timer"]')).toBeNull()
    await enterCode()
    expect(timer().textContent).toContain('授权剩余时间：2 分 00 秒')
    expect(timer().getAttribute('aria-live')).toBe('off')
    expect(container.textContent).not.toContain('登录请求实际有效期以终端为准')
    const initial = announcement()
    await act(async () => {
      now += 1000
      vi.advanceTimersByTime(1000)
    })
    expect(timer().textContent).toContain('1 分 59 秒')
    expect(announcement()).toBe(initial)
    await act(async () => {
      now += 59000
      vi.advanceTimersByTime(59000)
    })
    expect(announcement()).toContain('不到 1 分钟')
    expect(timer().textContent).toContain('授权剩余时间：1 分 00 秒')
    expect(container.textContent).not.toContain('登录请求实际有效期以终端为准')
    await act(async () => {
      now += 60000
      vi.advanceTimersByTime(60000)
    })
    expect(container.textContent).toContain('登录请求已过期')
    expect(container.querySelector('[role="timer"]')).toBeNull()
    expect(button('批准此终端登录')).toBeUndefined()
    expect(cliAuthApi.decide).not.toHaveBeenCalled()
  })
  it('pastes a spaced ASCII code without truncating its leading zeroes', async () => {
    await mount()
    const paste = new Event('paste', { bubbles: true, cancelable: true })
    Object.defineProperty(paste, 'clipboardData', { value: { getData: () => '000 042' } })
    await act(async () => container.querySelector('input')!.dispatchEvent(paste))
    expect(paste.defaultPrevented).toBe(true)
    expect(container.querySelector('input')!.value).toBe('000042')
    await act(async () =>
      container
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
    await flush()
    expect(cliAuthApi.inspect).toHaveBeenCalledWith(
      expect.objectContaining({ userCode: '000042' }),
      expect.any(AbortSignal),
    )
  })
  it('requires the request-bound link and never looks up a global six-digit code', async () => {
    history.replaceState(null, '', '/auth/device')
    await mount()
    expect(container.textContent).toContain('完整登录地址')
    expect(container.querySelector('input')).toBeNull()
    expect(cliAuthApi.inspect).not.toHaveBeenCalled()
    expect(cliAuthApi.decide).not.toHaveBeenCalled()
  })
  it('preserves leading zeroes, shows the exact grant and requires an explicit decision', async () => {
    vi.mocked(cliAuthApi.inspect).mockResolvedValueOnce({
      ...confirmation(),
      sessionAbsoluteExpiresIn: 1200,
    })
    await mount()
    await enterCode()
    expect(location.hash).toBe('')
    expect(cliAuthApi.inspect).toHaveBeenCalledWith(
      { verificationHandle: 'h'.repeat(43), userCode: '000042' },
      expect.any(AbortSignal),
    )
    expect(container.textContent).toContain('My terminal')
    expect(container.textContent).toContain('Test user')
    expect(container.textContent).toContain('platform.clusters.view')
    expect(container.textContent).toContain('20 分钟')
    expect(cliAuthApi.decide).not.toHaveBeenCalled()
    await act(async () => button('拒绝').click())
    await flush()
    expect(cliAuthApi.decide).toHaveBeenCalledWith(
      expect.objectContaining({
        decision: 'deny',
        userCode: '000042',
        confirmationId: 'c'.repeat(43),
      }),
      expect.any(AbortSignal),
    )
    expect(sessionStorage.length).toBe(0)
  })
  it('recovers an unknown result using the exact original decision and no new confirmation', async () => {
    vi.mocked(cliAuthApi.decide).mockRejectedValueOnce(new ApiError(0, 'Connection lost'))
    await mount()
    await enterCode()
    await act(async () => button('批准此终端登录').click())
    await flush()
    expect(container.textContent).toContain('原决定的结果尚未确认')
    expect(button('批准此终端登录')).toBeUndefined()
    await act(async () => button('核对原决定').click())
    await flush()
    expect(cliAuthApi.decide).toHaveBeenCalledTimes(2)
    expect(vi.mocked(cliAuthApi.decide).mock.calls[1][0]).toEqual(
      vi.mocked(cliAuthApi.decide).mock.calls[0][0],
    )
    expect(cliAuthApi.inspect).toHaveBeenCalledTimes(1)
    expect(container.textContent).toContain('已批准登录')
  })
  it('closes an expired server grant before the local handle timeout', async () => {
    vi.mocked(cliAuthApi.inspect).mockRejectedValueOnce(
      new ApiError(400, 'Device authorization expired', { code: 'expired_token' }),
    )
    await mount()
    await enterCode()
    expect(container.textContent).toContain('登录请求已过期')
    expect(container.querySelector('form')).toBeNull()
    expect(button('批准此终端登录')).toBeUndefined()
    expect(sessionStorage.length).toBe(0)
    expect(cliAuthApi.decide).not.toHaveBeenCalled()
  })
  it('switches a request in the same tab and discards the original confirmation', async () => {
    await mount()
    await enterCode()
    expect(container.textContent).toContain('My terminal')
    await act(async () => {
      history.replaceState(null, '', `/auth/device#handle=${'n'.repeat(43)}`)
      window.dispatchEvent(new HashChangeEvent('hashchange'))
    })
    expect(container.textContent).not.toContain('My terminal')
    expect(button('批准此终端登录')).toBeUndefined()
    expect(location.hash).toBe('')
    await enterCode('000043')
    expect(vi.mocked(cliAuthApi.inspect).mock.calls.at(-1)?.[0]).toEqual({
      verificationHandle: 'n'.repeat(43),
      userCode: '000043',
    })
    expect(cliAuthApi.decide).not.toHaveBeenCalled()
  })
  it('ignores an old inspection response after a new link arrives during the request', async () => {
    let finishOld!: (value: ReturnType<typeof confirmation>) => void
    vi.mocked(cliAuthApi.inspect).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishOld = resolve
        }),
    )
    await mount()
    await enterCode()
    const oldSignal = vi.mocked(cliAuthApi.inspect).mock.calls[0][1]
    await act(async () => {
      history.replaceState(null, '', `/auth/device#handle=${'n'.repeat(43)}`)
      window.dispatchEvent(new HashChangeEvent('hashchange'))
    })
    expect(oldSignal?.aborted).toBe(true)
    vi.mocked(cliAuthApi.inspect).mockResolvedValueOnce({
      ...confirmation(),
      deviceName: 'New terminal',
    })
    await enterCode('000043')
    await act(async () => finishOld({ ...confirmation(), deviceName: 'Old terminal' }))
    expect(container.textContent).toContain('New terminal')
    expect(container.textContent).not.toContain('Old terminal')
    expect(button('批准此终端登录')).toBeDefined()
    expect(cliAuthApi.decide).not.toHaveBeenCalled()
  })
  it('requires existing MFA before reinspection and clears confirmation on account changes', async () => {
    vi.mocked(cliAuthApi.inspect).mockRejectedValueOnce(
      new ApiError(403, 'Step up', { code: 'interactive_auth_required' }),
    )
    await mount()
    await enterCode()
    expect(button('完成已有 MFA')).toBeDefined()
    expect(cliAuthApi.decide).not.toHaveBeenCalled()
    await act(async () => button('完成已有 MFA').click())
    await enterCode()
    expect(container.textContent).toContain('My terminal')
    await act(async () =>
      useAuthStore.setState({ user: { ...user, userId: 'another', userName: 'Another user' } }),
    )
    expect(container.textContent).not.toContain('My terminal')
    expect(button('批准此终端登录')).toBeUndefined()
  })
  it('returns to the device page after login without sending the handle in return_to', async () => {
    useAuthStore.setState({ user: null, accessToken: null })
    await mount()
    expect(restoreAuthSession).toHaveBeenCalled()
    expect(button('登录并继续')).toBeDefined()
    expect(location.search).not.toContain('h'.repeat(43))
    expect(location.hash).toBe('')
    expect(JSON.stringify(useAuthStore.getState())).not.toContain('h'.repeat(43))
  })
})
