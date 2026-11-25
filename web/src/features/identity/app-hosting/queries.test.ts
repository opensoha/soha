import { describe, expect, it, vi } from 'vitest'
import { appHostingAdminKeys } from './keys'
import { appHostingAdminQueries } from './queries'

vi.mock('./api', () => ({
  getAppHostingSettings: vi.fn(),
  listAdminHostedApps: vi.fn(),
  getAdminHostedApp: vi.fn(),
  listAdminHostedAppRevisions: vi.fn(),
  listAdminVisibilityApprovals: vi.fn(),
  listAppHostingAudienceCandidates: vi.fn(),
}))

describe('appHostingAdminQueries', () => {
  it('builds canonical queryOptions and guards empty appId queries', () => {
    const settingsOpts = appHostingAdminQueries.settings(true)
    expect(settingsOpts.queryKey).toEqual(appHostingAdminKeys.settings)
    expect(settingsOpts.enabled).toBe(true)

    const emptyDetailOpts = appHostingAdminQueries.appDetail('   ', true)
    expect(emptyDetailOpts.enabled).toBe(false)

    const validDetailOpts = appHostingAdminQueries.appDetail('app-1', true)
    expect(validDetailOpts.enabled).toBe(true)
    expect(validDetailOpts.queryKey).toEqual(appHostingAdminKeys.appDetail('app-1'))

    const emptyRevisionOpts = appHostingAdminQueries.appRevisions('', {}, true)
    expect(emptyRevisionOpts.enabled).toBe(false)

    const approvalOpts = appHostingAdminQueries.approvals({ status: 'pending' }, false)
    expect(approvalOpts.enabled).toBe(false)
    expect(approvalOpts.queryKey).toEqual(appHostingAdminKeys.approvalList({ status: 'pending' }))
  })
})
