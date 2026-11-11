import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type {
  NetworkAccessGrantInput,
  NetworkConflictAnalysisRequest,
  NetworkGatewayInput,
  NetworkAccessPolicyInput,
  NetworkPolicyPreviewRequest,
  NetworkSiteInput,
  NetworkRuntimeEnrollmentInput,
  NetworkSessionActionInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { networkAccessKeys } from './keys'
import { networkAccessMutations } from './mutations'

const apiMocks = vi.hoisted(() => ({
  analyzeNetworkAccessConflicts: vi.fn(),
  compileNetworkAccessPolicySnapshot: vi.fn(),
  createNetworkAccessPolicy: vi.fn(),
  createNetworkAccessGrant: vi.fn(),
  createNetworkGateway: vi.fn(),
  createNetworkSite: vi.fn(),
  createNetworkRuntimeEnrollment: vi.fn(),
  executeNetworkSessionAction: vi.fn(),
  planNetworkSessionAction: vi.fn(),
  previewNetworkAccessPolicy: vi.fn(),
  revokeNetworkRuntimeEnrollment: vi.fn(),
  revokeNetworkAccessGrant: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...apiMocks,
}))

describe('network access mutation options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('invalidates the bounded network cache after a successful write', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const input = { name: 'HQ', status: 'active' } as NetworkSiteInput
    apiMocks.createNetworkSite.mockResolvedValueOnce({ id: 'site-1', ...input })
    const observer = new MutationObserver(
      queryClient,
      networkAccessMutations.sites.create(queryClient),
    )

    await observer.mutate(input)
    expect(invalidate).toHaveBeenCalledOnce()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: networkAccessKeys.all })
  })

  it('invalidates gateway lists after topology writes', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const input = {
      runtimeId: 'gateway-b',
      siteId: 'site-b',
      name: 'Branch B',
      administrativeStatus: 'active',
      publicEndpointHost: 'vpn-b.example.com',
      publicEndpointPort: 51821,
      overlayCidr: '100.96.1.0/24',
      routingMode: 'routed',
      hubGatewayId: 'gateway-a',
      advertisedCidrs: ['10.20.0.0/16'],
      mtu: 1420,
      persistentKeepaliveSeconds: 25,
      dnsServers: ['10.20.0.53'],
    } as NetworkGatewayInput
    apiMocks.createNetworkGateway.mockResolvedValueOnce({ id: 'gateway-b', ...input })

    await new MutationObserver(
      queryClient,
      networkAccessMutations.gateways.create(queryClient),
    ).mutate(input)

    expect(invalidate).toHaveBeenCalledWith({ queryKey: networkAccessKeys.all })
  })

  it('keeps side-effect-free policy preview out of cache invalidation', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const input = {
      subjectUserId: 'user-1',
      deviceId: 'device-1',
      resourceId: 'resource-1',
      mode: 'external_vpn_ztna',
    } as NetworkPolicyPreviewRequest
    apiMocks.previewNetworkAccessPolicy.mockResolvedValueOnce({ decision: 'deny' })
    const observer = new MutationObserver(queryClient, networkAccessMutations.policy.preview())

    await observer.mutate(input)
    expect(invalidate).not.toHaveBeenCalled()
  })

  it('invalidates enrollment lists after create and revoke', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const input = {
      runtimeId: 'endpoint-1',
      runtimeKind: 'endpoint',
      deviceId: 'device-1',
      subjectId: 'user-1',
    } as NetworkRuntimeEnrollmentInput
    apiMocks.createNetworkRuntimeEnrollment.mockResolvedValueOnce({
      enrollment: { id: 'enrollment-1' },
      token: 'one-time-token',
    })
    apiMocks.revokeNetworkRuntimeEnrollment.mockResolvedValueOnce({
      id: 'enrollment-1',
      status: 'revoked',
    })

    await new MutationObserver(
      queryClient,
      networkAccessMutations.enrollments.create(queryClient),
    ).mutate(input)
    await new MutationObserver(
      queryClient,
      networkAccessMutations.enrollments.revoke(queryClient),
    ).mutate('enrollment-1')

    expect(invalidate).toHaveBeenCalledTimes(2)
  })

  it('drops one-time access grants from mutation cache when reset and invalidates grant lists', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const input = {
      deviceId: 'device-1',
      siteId: 'site-1',
      networkSpaceId: 'space-1',
      mode: 'internal_ztna',
      resourceIds: ['resource-db'],
      ttlSeconds: 300,
    } as NetworkAccessGrantInput
    apiMocks.createNetworkAccessGrant.mockResolvedValueOnce({
      grant: { id: 'grant-1' },
      token: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA',
    })
    apiMocks.revokeNetworkAccessGrant.mockResolvedValueOnce({
      id: 'grant-1',
      status: 'revoked',
    })
    const observer = new MutationObserver(
      queryClient,
      networkAccessMutations.accessGrants.create(queryClient),
    )

    await observer.mutate(input)
    observer.reset()
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new MutationObserver(
      queryClient,
      networkAccessMutations.accessGrants.revoke(queryClient),
    ).mutate('grant-1')

    expect(invalidate).toHaveBeenCalledTimes(2)
    expect(
      JSON.stringify(
        queryClient
          .getMutationCache()
          .getAll()
          .map((item) => item.state.data),
      ),
    ).not.toContain('AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA')
  })

  it('invalidates policy caches only for draft writes and snapshot publication', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const policy = {
      name: 'Engineering',
      enabled: true,
      priority: 100,
      effect: 'allow',
      subjects: { users: [], teams: ['team-1'], tags: [] },
      siteIds: [],
      resourceIds: [],
      modes: ['internal_ztna'],
      deviceStatuses: ['active'],
      postureStatuses: ['compliant'],
      accessProfile: 'full',
    } as NetworkAccessPolicyInput
    apiMocks.createNetworkAccessPolicy.mockResolvedValueOnce({ id: 'policy-1', ...policy })
    apiMocks.compileNetworkAccessPolicySnapshot.mockResolvedValueOnce({ policyVersion: 1 })
    await new MutationObserver(
      queryClient,
      networkAccessMutations.policy.create(queryClient),
    ).mutate(policy)
    await new MutationObserver(
      queryClient,
      networkAccessMutations.policy.compile(queryClient),
    ).mutate(undefined)
    expect(invalidate).toHaveBeenCalledTimes(2)

    const conflictRequest = { runtimeRanges: [] } as NetworkConflictAnalysisRequest
    apiMocks.analyzeNetworkAccessConflicts.mockResolvedValueOnce({ valid: true, conflicts: [] })
    await new MutationObserver(
      queryClient,
      networkAccessMutations.policy.analyzeConflicts(),
    ).mutate(conflictRequest)
    expect(invalidate).toHaveBeenCalledTimes(2)
  })

  it('invalidates sessions only after an action is executed', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const input = {
      action: 'coa',
      targetAccessProfile: 'restricted',
      reasonCode: 'risk_change',
    } as NetworkSessionActionInput
    const planHash = `sha256:${'a'.repeat(64)}`
    apiMocks.planNetworkSessionAction.mockResolvedValueOnce({ planHash })
    apiMocks.executeNetworkSessionAction.mockResolvedValueOnce({
      id: 'command-1',
      status: 'pending',
    })

    await new MutationObserver(queryClient, networkAccessMutations.sessions.plan()).mutate({
      sessionId: 'session-1',
      input,
    })
    expect(invalidate).not.toHaveBeenCalled()

    await new MutationObserver(
      queryClient,
      networkAccessMutations.sessions.execute(queryClient),
    ).mutate({ sessionId: 'session-1', input: { ...input, planHash } })
    expect(invalidate).toHaveBeenCalledOnce()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: networkAccessKeys.all })
  })
})
