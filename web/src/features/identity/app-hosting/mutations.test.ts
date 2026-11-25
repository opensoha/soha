import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { appHostingAdminKeys, appHostingAdminMutationKeys } from './keys'
import { appHostingAdminMutations } from './mutations'

vi.mock('./api', () => ({
  updateAppHostingSettings: vi.fn(),
  rotateAppHostingGatewayToken: vi.fn(),
  suspendAdminHostedApp: vi.fn(),
  unsuspendAdminHostedApp: vi.fn(),
  transferAdminHostedAppOwnership: vi.fn(),
  deleteAdminHostedApp: vi.fn(),
  approveAdminVisibilityApproval: vi.fn(),
  rejectAdminVisibilityApproval: vi.fn(),
}))

describe('appHostingAdminMutations', () => {
  it('invalidates approvals and apps caches on approval decisions and lifecycle actions', async () => {
    const queryClient = new QueryClient()
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')

    const approveOpts = appHostingAdminMutations.approveVisibility(queryClient)
    expect(approveOpts.mutationKey).toEqual(appHostingAdminMutationKeys.approveVisibility)
    await approveOpts.onSuccess?.(
      {} as never,
      { approvalId: 'apr-1' },
      undefined as never,
      {} as never,
    )
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: appHostingAdminKeys.approvals })
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: appHostingAdminKeys.apps })

    invalidateSpy.mockClear()
    const settingsOpts = appHostingAdminMutations.updateSettings(queryClient)
    await settingsOpts.onSuccess?.({} as never, { enabled: true }, undefined as never, {} as never)
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: appHostingAdminKeys.settings })
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: appHostingAdminKeys.approvals })
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: appHostingAdminKeys.apps })
  })
})
