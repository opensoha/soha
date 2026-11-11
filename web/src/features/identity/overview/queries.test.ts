import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { identityOverviewKeys } from './keys'
import { identityOverviewQueries } from './queries'

const apiMocks = vi.hoisted(() => ({
  listIdentityOverviewAudit: vi.fn(),
  listIdentityOverviewSessions: vi.fn(),
}))

vi.mock('./api', () => ({
  IDENTITY_OVERVIEW_AUDIT_LIMIT: 8,
  ...apiMocks,
}))

describe('identity overview query options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('binds session and audit fetchers to their canonical keys', async () => {
    apiMocks.listIdentityOverviewSessions.mockResolvedValueOnce([{ id: 'session-1' }])
    apiMocks.listIdentityOverviewAudit.mockResolvedValueOnce([{ id: 'audit-1' }])
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    await expect(queryClient.fetchQuery(identityOverviewQueries.sessions())).resolves.toEqual([
      { id: 'session-1' },
    ])
    await expect(queryClient.fetchQuery(identityOverviewQueries.audit())).resolves.toEqual([
      { id: 'audit-1' },
    ])

    expect(apiMocks.listIdentityOverviewSessions).toHaveBeenCalledOnce()
    expect(apiMocks.listIdentityOverviewAudit).toHaveBeenCalledOnce()
    expect(queryClient.getQueryData(identityOverviewKeys.sessions())).toEqual([{ id: 'session-1' }])
    expect(queryClient.getQueryData(identityOverviewKeys.audit())).toEqual([{ id: 'audit-1' }])
  })
})
