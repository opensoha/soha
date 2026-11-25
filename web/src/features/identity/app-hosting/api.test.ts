import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  approveAdminVisibilityApproval,
  deleteAdminHostedApp,
  getAdminHostedApp,
  getAppHostingSettings,
  listAdminHostedAppRevisions,
  listAdminHostedApps,
  listAdminVisibilityApprovals,
  listAppHostingAudienceCandidates,
  rejectAdminVisibilityApproval,
  rotateAppHostingGatewayToken,
  suspendAdminHostedApp,
  transferAdminHostedAppOwnership,
  unsuspendAdminHostedApp,
  updateAppHostingSettings,
} from './api'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('identity app-hosting admin api', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('reads and updates settings and rotates gateway token', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: { enabled: true } })
    await expect(getAppHostingSettings()).resolves.toEqual({ enabled: true })
    expect(apiMocks.get).toHaveBeenCalledWith('/app-hosting/settings')

    apiMocks.put.mockResolvedValueOnce({
      data: { enabled: true, organizationVisibilityPolicy: 'require_approval' },
    })
    await updateAppHostingSettings({
      enabled: true,
      organizationVisibilityPolicy: 'require_approval',
    })
    expect(apiMocks.put).toHaveBeenCalledWith('/app-hosting/settings', {
      enabled: true,
      organizationVisibilityPolicy: 'require_approval',
    })

    apiMocks.post.mockResolvedValueOnce({
      data: { token: 'gw_sec_123', rotatedAt: '2026-11-21T00:00:00Z' },
    })
    await rotateAppHostingGatewayToken({ reason: '  quarterly rotation  ' })
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/settings/gateway-token/rotate', {
      reason: 'quarterly rotation',
    })
  })

  it('encodes scope=all and filter query parameters for apps, revisions, candidates, and approvals', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: { items: [], total: 0 } })
    await listAdminHostedApps({
      query: '  board ',
      status: 'suspended_by_admin',
      ownerUserId: 'u-1',
      teamId: 't-2',
      limit: 20,
      offset: 40,
    })
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/app-hosting/apps?scope=all&query=board&status=suspended_by_admin&ownerUserId=u-1&teamId=t-2&limit=20&offset=40',
    )

    apiMocks.get.mockResolvedValueOnce({ data: { id: 'app/1' } })
    await getAdminHostedApp(' app/1 ')
    expect(apiMocks.get).toHaveBeenCalledWith('/app-hosting/apps/app%2F1')

    apiMocks.get.mockResolvedValueOnce({ data: { items: [], total: 0 } })
    await listAdminHostedAppRevisions('app/1', { limit: 10, offset: 0 })
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/app-hosting/apps/app%2F1/revisions?limit=10&offset=0',
    )

    apiMocks.get.mockResolvedValueOnce({ data: { users: [], teams: [] } })
    await listAppHostingAudienceCandidates(' 张三 ', 15)
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/app-hosting/audience-candidates?query=%E5%BC%A0%E4%B8%89&limit=15',
    )

    apiMocks.get.mockResolvedValueOnce({ data: { items: [], total: 0 } })
    await listAdminVisibilityApprovals({ status: 'pending', limit: 20, offset: 0 })
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/app-hosting/approvals?scope=all&status=pending&limit=20&offset=0',
    )
  })

  it('sends lifecycle, ownership transfer, confirmation delete, and approval decision payloads', async () => {
    apiMocks.post.mockResolvedValue({ data: {} })

    await suspendAdminHostedApp({ appId: 'app-1', reason: '  abuse traffic ' })
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/apps/app-1/suspend', {
      reason: 'abuse traffic',
    })

    await unsuspendAdminHostedApp({ appId: 'app-1', reason: '  resolved ' })
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/apps/app-1/unsuspend', {
      reason: 'resolved',
    })

    await transferAdminHostedAppOwnership({
      appId: 'app-1',
      newOwnerUserId: ' user-2 ',
      reason: ' employee offboarding ',
    })
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/apps/app-1/transfer', {
      newOwnerUserId: 'user-2',
      reason: 'employee offboarding',
    })

    await deleteAdminHostedApp({
      appId: 'app-1',
      confirmationName: 'Exact App Name',
      reason: ' cleanup ',
    })
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/apps/app-1/delete', {
      confirmationName: 'Exact App Name',
      reason: 'cleanup',
    })

    await approveAdminVisibilityApproval({ approvalId: 'apr-1', comment: ' looks good ' })
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/approvals/apr-1/approve', {
      comment: 'looks good',
    })

    await rejectAdminVisibilityApproval({ approvalId: 'apr-2', comment: ' narrow scope first ' })
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/approvals/apr-2/reject', {
      comment: 'narrow scope first',
    })
  })
})
