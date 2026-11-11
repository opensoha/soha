/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import {
  ApiError,
  emitApiError,
  claimApiErrorPresentation,
  apiErrorNotificationKey,
} from '@/services/api-error'
import { GlobalApiErrorHandler } from './global-api-error-handler'

const notifications = vi.hoisted(() => ({ warning: vi.fn(), error: vi.fn(), destroy: vi.fn() }))
vi.mock('antd', () => ({ App: { useApp: () => ({ notification: notifications }) } }))
let container: HTMLDivElement
let root: ReturnType<typeof createRoot>
function Location() {
  const location = useLocation()
  return <output>{location.pathname}</output>
}
async function mount(path = '/access/roles') {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[path]}>
        <GlobalApiErrorHandler />
        <Location />
      </MemoryRouter>,
    )
  })
}
async function flushNotifications() {
  await act(async () => vi.advanceTimersByTime(40))
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-09-20T00:00:00Z'))
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) =>
    window.setTimeout(() => callback(performance.now()), 16),
  )
  vi.stubGlobal('cancelAnimationFrame', (frame: number) => window.clearTimeout(frame))
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})
it('deduplicates infrastructure feedback while retaining the request ID', async () => {
  await mount()
  const error = new ApiError(503, 'temporarily unavailable', {
    path: '/access/roles',
    method: 'POST',
    requestId: 'req-503',
  })
  await act(async () => {
    emitApiError(error)
    emitApiError(error)
  })
  await flushNotifications()
  expect(notifications.error).toHaveBeenCalledTimes(1)
  expect(notifications.error).toHaveBeenCalledWith(
    expect.objectContaining({ description: expect.stringContaining('req-503') }),
  )
  await act(async () => {
    vi.advanceTimersByTime(8000)
    emitApiError(error)
  })
  await flushNotifications()
  expect(notifications.error).toHaveBeenCalledTimes(2)
})
it('keeps permission rejection separate from client validation feedback', async () => {
  await mount()
  await act(async () => {
    emitApiError(new ApiError(403, 'denied', { path: '/access/roles' }))
    emitApiError(new ApiError(409, 'name exists', { path: '/access/roles' }))
  })
  await flushNotifications()
  expect(notifications.warning).toHaveBeenCalledTimes(1)
  expect(notifications.error).not.toHaveBeenCalled()
  expect(container.textContent).toBe('/access/roles')
})
it.each(['/access/roles', '/login'])(
  'handles session expiry from %s without losing the login destination',
  async (path) => {
    await mount(path)
    await act(async () => emitApiError(new ApiError(401, 'expired')))
    expect(container.textContent).toBe('/login')
    expect(notifications.warning).toHaveBeenCalledTimes(1)
  },
)

it('hands a displayed request error to inline feedback and restores fallback after all owners leave', async () => {
  await mount()
  const error = new ApiError(503, 'save unavailable', { path: '/records', method: 'PUT' })
  emitApiError(error)
  await flushNotifications()
  expect(notifications.error).toHaveBeenCalledOnce()
  const release = claimApiErrorPresentation(error)!
  const releaseOther = claimApiErrorPresentation(error)!
  expect(notifications.destroy).toHaveBeenCalledWith(apiErrorNotificationKey(error))
  release()
  emitApiError(error)
  expect(notifications.error).toHaveBeenCalledOnce()
  releaseOther()
  emitApiError(error)
  await flushNotifications()
  expect(notifications.error).toHaveBeenCalledTimes(2)
})

it('does not let inline feedback suppress authentication or an unrelated request', async () => {
  await mount()
  const error = new ApiError(403, 'denied', { path: '/records/first' })
  const release = claimApiErrorPresentation(error)!
  try {
    await act(async () => {
      emitApiError(error)
      emitApiError(new ApiError(403, 'denied', { path: '/records/second' }))
      expect(claimApiErrorPresentation(new ApiError(401, 'expired'))).toBeUndefined()
    })
    await flushNotifications()
    await act(async () => emitApiError(new ApiError(401, 'expired')))
    expect(notifications.warning).toHaveBeenCalledTimes(2)
    expect(container.textContent).toBe('/login')
  } finally {
    release()
  }
})

it('cancels queued fallback when inline feedback claims the query result', async () => {
  await mount()
  const error = new ApiError(503, 'read unavailable', { path: '/records' })
  emitApiError(error)
  const release = claimApiErrorPresentation(error)!
  try {
    await flushNotifications()
    expect(notifications.error).not.toHaveBeenCalled()
  } finally {
    release()
  }
})
