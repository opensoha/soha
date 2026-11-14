import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AuditLog, OnlineUser } from '@/features/system'
import {
  IDENTITY_AUDIT_ACTION_PREFIXES,
  IDENTITY_OVERVIEW_AUDIT_LIMIT,
  listIdentityOverviewAudit,
  listIdentityOverviewSessions,
} from './api'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const session: OnlineUser = {
  id: 'session-1',
  userId: 'user-1',
  userName: 'Yamabuki',
  email: 'yamabuki@example.com',
  providerType: 'oidc',
  status: 'active',
  loginTime: '2026-07-10T00:00:00Z',
  lastSeenAt: '2026-07-10T00:05:00Z',
  expiry: '2026-07-10T08:00:00Z',
}

const audit: AuditLog = {
  id: 'audit-1',
  createdAt: '2026-07-10T00:00:00Z',
  actorId: 'user-1',
  actorName: 'Yamabuki',
  action: 'identity.provider.read',
  resourceKind: 'provider',
  resourceName: 'grafana',
  result: 'success',
  summary: 'Provider read',
}

describe('identity overview api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses the canonical online-user read model wire contract', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: [session] })

    await expect(listIdentityOverviewSessions()).resolves.toEqual([session])
    expect(apiMocks.get).toHaveBeenCalledWith('/auth/sessions')
  })

  it('requests exactly eight identity-only audit events', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: [audit] })

    await expect(listIdentityOverviewAudit()).resolves.toEqual([audit])
    expect(apiMocks.get).toHaveBeenCalledWith(
      `/identity/audit/events?limit=${IDENTITY_OVERVIEW_AUDIT_LIMIT}&actionPrefixes=${encodeURIComponent(IDENTITY_AUDIT_ACTION_PREFIXES.join(','))}`,
    )
  })

  it('normalizes empty list envelopes at the capability boundary', async () => {
    apiMocks.get.mockResolvedValueOnce({}).mockResolvedValueOnce({})

    await expect(listIdentityOverviewSessions()).resolves.toEqual([])
    await expect(listIdentityOverviewAudit()).resolves.toEqual([])
  })
})
