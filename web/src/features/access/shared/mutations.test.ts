import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { accessApi } from './api'
import { accessMutations } from './mutations'

describe('access mutations', () => {
  afterEach(() => vi.restoreAllMocks())

  it('passes typed MFA reset input to the users API', async () => {
    const result = {
      userId: 'user-1',
      revokedCredentialCount: 2,
      recoveryCodesRevoked: 1,
      sessionsRevoked: 3,
      resetAt: '2026-07-27T00:00:00Z',
    }
    const reset = vi.spyOn(accessApi.users, 'resetMFA').mockResolvedValue(result)
    const observer = new MutationObserver(new QueryClient(), accessMutations.users.resetMFA())
    const variables = {
      id: 'user-1',
      input: { reason: 'Administrative reset', revokeSessions: true },
    }

    await expect(observer.mutate(variables)).resolves.toBe(result)
    expect(reset).toHaveBeenCalledWith(variables)
  })
})
