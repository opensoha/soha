import { describe, expect, it } from 'vitest'
import { appHostingAdminKeys, appHostingAdminMutationKeys } from './keys'

describe('appHostingAdminKeys', () => {
  it('normalizes whitespace and defaults for list, detail, revision, and candidate keys', () => {
    expect(
      appHostingAdminKeys.appList({
        query: '  weekly-report  ',
        status: 'online',
        ownerUserId: '  user-1 ',
        teamId: '  team-rd ',
      }),
    ).toEqual([
      'identity-app-hosting',
      'apps',
      'list',
      {
        query: 'weekly-report',
        status: 'online',
        ownerUserId: 'user-1',
        teamId: 'team-rd',
        limit: 20,
        offset: 0,
      },
    ])

    expect(
      appHostingAdminKeys.approvalList({
        status: 'pending',
        appId: '  app-1 ',
        limit: 50,
        offset: -4,
      }),
    ).toEqual([
      'identity-app-hosting',
      'approvals',
      'list',
      {
        status: 'pending',
        appId: 'app-1',
        limit: 50,
        offset: 0,
      },
    ])

    expect(appHostingAdminKeys.appDetail('  app-99 ')).toEqual([
      'identity-app-hosting',
      'apps',
      'detail',
      'app-99',
    ])

    expect(appHostingAdminKeys.appRevisions(' app-99 ', { limit: 10, offset: 20 })).toEqual([
      'identity-app-hosting',
      'apps',
      'revisions',
      'app-99',
      { limit: 10, offset: 20 },
    ])

    expect(appHostingAdminKeys.audienceCandidateSearch('  alice  ', 15)).toEqual([
      'identity-app-hosting',
      'audience-candidates',
      'alice',
      15,
    ])

    expect(appHostingAdminMutationKeys.rotateGatewayToken).toEqual([
      'identity-app-hosting',
      'settings',
      'rotate-gateway-token',
    ])
  })
})
