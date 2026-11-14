import { describe, expect, it } from 'vitest'
import type { AuditLog } from '@/features/system'
import { applicationLoginRecord } from './model'

export function loginLog(overrides: Partial<AuditLog> = {}): AuditLog {
  return {
    id: 'login-1',
    createdAt: '2026-09-14T09:00:00Z',
    actorId: 'user-1',
    actorName: 'Alice',
    action: 'oidc.authorize',
    result: 'success',
    resourceKind: 'IdentityProvider',
    resourceName: 'provider-1',
    summary: 'oidc.authorize',
    metadata: { applicationId: 'app-1' },
    ...overrides,
  }
}

describe('application login records', () => {
  it('keeps login stages and their actual outcomes without counting launches, refreshes or bypasses', () => {
    for (const action of [
      'identity.application.launch',
      'identity.provider.update',
      'oidc.token.refresh',
      'identity.login.success',
    ]) {
      expect(applicationLoginRecord(loginLog({ action }))).toBeNull()
    }
    expect(
      applicationLoginRecord(loginLog({ action: 'proxy.allow', metadata: { skipped: true } })),
    ).toBeNull()
    expect(applicationLoginRecord(loginLog())).toMatchObject({
      stage: 'oidcAuthorize',
      outcome: 'success',
      applicationId: 'app-1',
      actorName: 'Alice',
    })
    expect(
      applicationLoginRecord(loginLog({ result: 'deny', metadata: { reason: 'access denied' } })),
    ).toMatchObject({ outcome: 'failure', reason: 'access denied' })
    expect(applicationLoginRecord(loginLog({ action: 'identity.saml.sso' }))).toMatchObject({
      stage: 'saml',
    })
  })

  it('accepts only access events from Outposts and never invents the user or result', () => {
    const outpost = loginLog({
      action: 'outpost.event',
      actorName: '',
      actorId: '',
      result: 'denied',
      metadata: {
        event: { eventType: 'proxy_deny', applicationId: 'remote-app', reason: 'policy denied' },
      },
    })
    expect(applicationLoginRecord(outpost)).toMatchObject({
      applicationId: 'remote-app',
      outcome: 'failure',
      reason: 'policy denied',
      actorName: '',
      actorId: '',
    })
    expect(applicationLoginRecord({ ...outpost, result: 'reported' })?.outcome).toBe('reported')
    expect(
      applicationLoginRecord({ ...outpost, metadata: { event: { eventType: 'health' } } }),
    ).toBeNull()
    expect(
      applicationLoginRecord({
        ...outpost,
        metadata: { event: { eventType: 'proxy_allow', skipped: true } },
      }),
    ).toBeNull()
  })
})
