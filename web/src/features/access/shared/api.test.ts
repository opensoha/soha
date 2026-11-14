import { beforeEach, describe, expect, it, vi } from 'vitest'
import { accessApi } from './api'

const apiMock = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMock }))

describe('access api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps list responses before returning them to query consumers', async () => {
    const users = [{ id: 'user-1', username: 'admin' }]
    apiMock.get.mockResolvedValueOnce({ data: users })

    await expect(accessApi.users.list()).resolves.toBe(users)
    expect(apiMock.get).toHaveBeenCalledWith('/access/users')
  })

  it('uses explicit encoded resource endpoints for mutations', async () => {
    apiMock.put.mockResolvedValueOnce({ data: {} })
    apiMock.delete.mockResolvedValueOnce({ data: {} })

    await accessApi.roles.update({ id: 'ops/admin', values: { name: 'Ops' } })
    await accessApi.teams.delete('platform/core')

    expect(apiMock.put).toHaveBeenCalledWith('/access/roles/ops%2Fadmin', { name: 'Ops' })
    expect(apiMock.delete).toHaveBeenCalledWith('/access/teams/platform%2Fcore')
  })

  it('uses the selected user or organization scope for grants', async () => {
    apiMock.get.mockResolvedValue({ data: [] })
    apiMock.delete.mockResolvedValueOnce({ data: {} })

    await accessApi.scopeGrants.listAll()
    await accessApi.scopeGrants.list({ subjectType: 'user', subjectId: 'user/1' })
    await accessApi.scopeGrants.delete({ subjectType: 'team', subjectId: 'team/1', id: 'grant/1' })

    expect(apiMock.get).toHaveBeenCalledWith('/access/scope-grants')
    expect(apiMock.get).toHaveBeenCalledWith('/access/users/user%2F1/scope-grants')
    expect(apiMock.delete).toHaveBeenCalledWith(
      '/access/teams/team%2F1/scope-grants/grant%2F1',
    )
  })

  it('normalizes absent login provider settings to an empty option list', async () => {
    apiMock.get.mockResolvedValueOnce({ data: {} })

    await expect(accessApi.dependencies.loginProviders()).resolves.toEqual([])
    expect(apiMock.get).toHaveBeenCalledWith('/settings/identity')
  })

  it('resets MFA through the encoded identity user endpoint without exposing secrets', async () => {
    const result = {
      userId: 'user/1',
      revokedCredentialCount: 2,
      recoveryCodesRevoked: 1,
      sessionsRevoked: 3,
      resetAt: '2026-07-27T00:00:00Z',
    }
    apiMock.post.mockResolvedValueOnce({ data: result })

    await expect(
      accessApi.users.resetMFA({
        id: 'user/1',
        input: { reason: 'Administrative reset', revokeSessions: true },
      }),
    ).resolves.toBe(result)
    expect(apiMock.post).toHaveBeenCalledWith('/identity/users/user%2F1/mfa/reset', {
      reason: 'Administrative reset',
      revokeSessions: true,
    })
  })
})
