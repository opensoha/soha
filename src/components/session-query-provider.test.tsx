/** @vitest-environment jsdom */

import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { SessionQueryProvider } from './session-query-provider'
import { useAuthStore } from '@/stores/auth-store'

it('isolates fresh permission and resource data across sessions while keeping token refresh stable', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const observed: QueryClient[] = []
  function CaptureClient() {
    observed.push(useQueryClient())
    return null
  }

  useAuthStore.getState().setTokens('first-session')
  const root = createRoot(document.createElement('div'))
  try {
    await act(async () => {
      root.render(
        <SessionQueryProvider>
          <CaptureClient />
        </SessionQueryProvider>,
      )
    })
    const original = observed.at(-1)!
    const permissions = ['auth', 'permissions']
    const secret = ['secret', 'cluster', 'namespace', 'name']
    original.setQueryData(permissions, ['read-secret'])
    original.setQueryData(secret, 'previous-session-data')

    await act(async () => useAuthStore.getState().setTokens('refreshed-token'))
    expect(observed.at(-1)).toBe(original)

    await act(async () => {
      useAuthStore.getState().clearAuth()
      useAuthStore.getState().setTokens('second-session')
    })
    const current = observed.at(-1)!
    expect(current).not.toBe(original)
    expect(current.getQueryData(permissions)).toBeUndefined()
    expect(current.getQueryData(secret)).toBeUndefined()
    expect(original.getQueryData(secret)).toBeUndefined()
  } finally {
    await act(async () => root.unmount())
    useAuthStore.getState().clearAuth()
    vi.unstubAllGlobals()
  }
})
