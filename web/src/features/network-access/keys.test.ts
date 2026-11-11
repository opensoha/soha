import { describe, expect, it } from 'vitest'
import { networkAccessKeys } from './keys'

describe('network access query keys', () => {
  it('normalizes list filters into stable keys', () => {
    expect(
      networkAccessKeys.devices.list({ search: ' Laptop ', ownerUserId: ' user-1 ', limit: 200 }),
    ).toEqual([
      'network-access',
      'devices',
      'list',
      { limit: 200, ownerUserId: 'user-1', search: 'Laptop', siteId: '', status: '' },
    ])
    expect(networkAccessKeys.resources.list({ spaceId: ' space-1 ', protected: false })).toEqual([
      'network-access',
      'resources',
      'list',
      { kind: '', limit: undefined, protected: false, search: '', spaceId: 'space-1' },
    ])
    expect(
      networkAccessKeys.policies.list({ search: ' Engineering ', enabled: false, effect: 'allow' }),
    ).toEqual([
      'network-access',
      'policies',
      'list',
      { effect: 'allow', enabled: false, limit: undefined, search: 'Engineering' },
    ])
    expect(networkAccessKeys.policySnapshot).toEqual(['network-access', 'policy-snapshot'])
    expect(networkAccessKeys.enrollments.list({ limit: 200 })).toEqual([
      'network-access',
      'enrollments',
      'list',
      { limit: 200 },
    ])
    expect(
      networkAccessKeys.accessGrants.list({
        subjectId: ' user-1 ',
        deviceId: ' device-1 ',
        status: 'issued',
        limit: 200,
      }),
    ).toEqual([
      'network-access',
      'access-grants',
      'list',
      { deviceId: 'device-1', limit: 200, status: 'issued', subjectId: 'user-1' },
    ])
    expect(
      networkAccessKeys.nasBindings.list({ siteId: ' site-1 ', runtimeId: ' radius-1 ' }),
    ).toEqual([
      'network-access',
      'nas-bindings',
      'list',
      { limit: undefined, runtimeId: 'radius-1', siteId: 'site-1', status: '' },
    ])
    expect(
      networkAccessKeys.siteProfileBindings.list({
        siteId: ' site-1 ',
        accessProfile: 'restricted',
      }),
    ).toEqual([
      'network-access',
      'site-profile-bindings',
      'list',
      { accessProfile: 'restricted', limit: undefined, siteId: 'site-1' },
    ])
    expect(
      networkAccessKeys.sessions.list({
        deviceId: ' device-1 ',
        subjectId: ' user-1 ',
        status: 'active',
      }),
    ).toEqual([
      'network-access',
      'sessions',
      'list',
      {
        deviceId: 'device-1',
        limit: undefined,
        runtimeId: '',
        siteId: '',
        status: 'active',
        subjectId: 'user-1',
      },
    ])
  })
})
