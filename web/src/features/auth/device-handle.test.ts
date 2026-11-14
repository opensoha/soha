/** @vitest-environment jsdom */
import { beforeEach, describe, expect, it } from 'vitest'
import { captureDeviceHandle, clearDeviceHandle } from './device-handle'
const handle = 'a'.repeat(43)
beforeEach(() => {
  sessionStorage.clear()
  history.replaceState(null, '', '/auth/device')
})
describe('device request locator', () => {
  it('removes the fragment before login and recovers only within this tab and TTL', () => {
    history.replaceState(null, '', `/auth/device#handle=${handle}`)
    expect(captureDeviceHandle(1000)).toEqual({ value: handle, expiresAt: 601000 })
    expect(location.hash).toBe('')
    history.replaceState(null, '', '/login?return_to=%2Fauth%2Fdevice')
    expect(location.search).not.toContain(handle)
    history.replaceState(null, '', '/auth/device')
    expect(captureDeviceHandle(2000)?.value).toBe(handle)
    expect(captureDeviceHandle(601001)).toBeNull()
    expect(sessionStorage.length).toBe(0)
  })
  it('rejects malformed, duplicate and extra fragment parameters without reusing an older handle', () => {
    for (const fragment of [
      'handle=123456',
      `handle=${handle}&handle=${handle}`,
      `handle=${handle}&userCode=000042`,
      'userCode=000042',
      'unrecognized',
    ]) {
      history.replaceState(null, '', `/auth/device#handle=${handle}`)
      captureDeviceHandle(1000)
      history.replaceState(null, '', `/auth/device#${fragment}`)
      expect(captureDeviceHandle(1001)).toBeNull()
      expect(location.hash).toBe('')
    }
  })
  it('clears completed locators without placing them in general auth persistence', () => {
    history.replaceState(null, '', `/auth/device#handle=${handle}`)
    captureDeviceHandle()
    expect(localStorage.getItem('soha-auth') || '').not.toContain(handle)
    clearDeviceHandle()
    expect(captureDeviceHandle()).toBeNull()
  })
})
