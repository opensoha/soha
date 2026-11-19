/** @vitest-environment jsdom */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAuthStore } from '@/stores/auth-store'
import { streamWorkbenchMessage } from './stream'

const auth = vi.hoisted(() => ({
  getStoredAccessToken: vi.fn<() => string | null>(),
  refreshAuthSession: vi.fn<() => Promise<boolean>>(),
}))

vi.mock('@/features/auth/auth-api', async (original) => ({
  ...(await original<typeof import('@/features/auth/auth-api')>()),
  getStoredAccessToken: auth.getStoredAccessToken,
  refreshAuthSession: auth.refreshAuthSession,
}))

describe('workbench stream session ownership', () => {
  beforeEach(() => {
    auth.getStoredAccessToken.mockReset()
    auth.refreshAuthSession.mockReset()
    auth.getStoredAccessToken.mockReturnValue('old-token')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    useAuthStore.getState().clearAuth()
  })

  it.each(['response', 'refresh'] as const)(
    'does not replay an old stream after the session changes during its %s',
    async (stage) => {
      let respond!: (value: Response) => void
      let refreshed!: (value: boolean) => void
      const response = new Promise<Response>((resolve) => {
        respond = resolve
      })
      const refresh = new Promise<boolean>((resolve) => {
        refreshed = resolve
      })
      const fetchMock = vi.fn(() => response)
      vi.stubGlobal('fetch', fetchMock)
      auth.refreshAuthSession.mockReturnValue(refresh)
      const request = streamWorkbenchMessage('/stream', { content: 'hello' }, vi.fn())
      if (stage === 'refresh') {
        respond(new Response(null, { status: 401 }))
        await vi.waitFor(() => expect(auth.refreshAuthSession).toHaveBeenCalledOnce())
      }
      useAuthStore.getState().clearAuth()
      if (stage === 'response') respond(new Response(null, { status: 401 }))
      else refreshed(true)
      await expect(request).rejects.toMatchObject({ name: 'AbortError' })
      expect(fetchMock).toHaveBeenCalledOnce()
      if (stage === 'response') expect(auth.refreshAuthSession).not.toHaveBeenCalled()
    },
  )

  it('cancels a late retry response when the original session has changed', async () => {
    let respond!: (value: Response) => void
    const retry = new Promise<Response>((resolve) => {
      respond = resolve
    })
    const cancel = vi.fn()
    const body = new ReadableStream<Uint8Array>({ cancel })
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(new Response(null, { status: 401 }))
      .mockReturnValueOnce(retry)
    vi.stubGlobal('fetch', fetchMock)
    auth.refreshAuthSession.mockResolvedValue(true)
    const received = vi.fn()
    const request = streamWorkbenchMessage('/stream', { content: 'hello' }, received)
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    useAuthStore.getState().clearAuth()
    respond(new Response(body))
    await expect(request).rejects.toMatchObject({ name: 'AbortError' })
    expect(cancel).toHaveBeenCalledOnce()
    expect(received).not.toHaveBeenCalled()
    expect(body.locked).toBe(false)
  })

  it('stops delivering stream events after the owning session changes', async () => {
    let controller!: ReadableStreamDefaultController<Uint8Array>
    const cancel = vi.fn()
    const body = new ReadableStream<Uint8Array>({
      start(value) {
        controller = value
      },
      cancel,
    })
    const reader = vi.spyOn(body, 'getReader')
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(body)),
    )
    const received = vi.fn()
    const request = streamWorkbenchMessage('/stream', { content: 'hello' }, received)
    await vi.waitFor(() => expect(reader).toHaveBeenCalledOnce())
    useAuthStore.getState().clearAuth()
    controller.enqueue(new TextEncoder().encode('data: {"type":"message.delta"}\n\n'))
    await expect(request).rejects.toMatchObject({ name: 'AbortError' })
    expect(received).not.toHaveBeenCalled()
    expect(cancel).toHaveBeenCalledOnce()
    expect(body.locked).toBe(false)
  })
})
