import { describe, expect, it } from 'vitest'
import { buildScopeGrantPayload } from './scope-grant-editor'
import type { AccessScopeGrant } from './types'

const legacyGrant: AccessScopeGrant = {
  id: 'grant-1',
  subjectType: 'user',
  subjectId: 'user-1',
  businessLineId: 'payments',
  environmentIds: ['prod'],
  applicationIds: ['checkout'],
  scopeType: 'legacy',
  clusterIds: ['cluster-1'],
  namespaces: ['payments'],
  namespaceSelector: 'team=payments',
  resourceGroups: ['apps'],
  resourceKinds: ['Deployment'],
  role: 'developer',
  effect: 'allow',
  enabled: true,
  createdAt: '2026-08-13T08:00:00Z',
  updatedAt: '2026-08-13T08:00:00Z',
}

describe('scope grant payloads', () => {
  it('preserves hidden restrictions when editing a legacy grant', () => {
    expect(
      buildScopeGrantPayload(
        {
          applicationIds: ['checkout'],
          businessLineId: ' payments ',
          effect: 'allow',
          enabled: false,
          environmentIds: ['prod'],
          role: 'developer',
          scopeType: 'legacy',
          subjectId: 'user-1',
          subjectType: 'user',
        },
        legacyGrant,
      ),
    ).toMatchObject({
      businessLineId: 'payments',
      clusterIds: ['cluster-1'],
      namespaceSelector: 'team=payments',
      namespaces: ['payments'],
      resourceGroups: ['apps'],
      resourceKinds: ['Deployment'],
    })
  })

  it('locks application grants while allowing an environment deny override', () => {
    expect(
      buildScopeGrantPayload(
        {
          applicationIds: ['other-app'],
          businessLineId: 'other-line',
          effect: 'deny',
          enabled: true,
          environmentIds: ['env-prod'],
          role: 'readonly',
          scopeType: 'platform',
        },
        null,
        { id: 'user-1', type: 'user' },
        { id: 'app-1', businessLineId: 'payments' },
      ),
    ).toMatchObject({
      applicationIds: ['app-1'],
      businessLineId: 'payments',
      effect: 'deny',
      environmentIds: ['env-prod'],
      scopeType: 'delivery',
      subjectId: 'user-1',
      subjectType: 'user',
    })
  })
})
