import { beforeEach, describe, expect, it, vi } from 'vitest'
import type {
  EndpointDevice,
  EndpointDeviceInput,
  NetworkAccessGrant,
  NetworkAccessGrantInput,
  NetworkAccessGrantSecret,
  NetworkAccessPolicy,
  NetworkAccessPolicyInput,
  NetworkConflictAnalysisRequest,
  NetworkConflictAnalysisResult,
  NetworkGateway,
  NetworkGatewayInput,
  NetworkMihomoProfile,
  NetworkMihomoProfileInput,
  NetworkNASBinding,
  NetworkNASBindingInput,
  NetworkPolicyPreviewRequest,
  NetworkPolicyPreviewResult,
  NetworkPolicySnapshot,
  NetworkResource,
  NetworkResourceInput,
  NetworkRuntimeEnrollment,
  NetworkRuntimeEnrollmentInput,
  NetworkRuntimeEnrollmentSecret,
  NetworkSession,
  NetworkSessionActionInput,
  NetworkSessionActionPlan,
  NetworkSessionCommand,
  NetworkSite,
  NetworkSiteInput,
  NetworkSiteProfileBinding,
  NetworkSiteProfileBindingInput,
  NetworkSpace,
  NetworkSpaceInput,
  NetworkTelemetrySummary,
} from '@opensoha/contracts/gen/ts/sohaapi'
import {
  analyzeNetworkAccessConflicts,
  compileNetworkAccessPolicySnapshot,
  createNetworkAccessGrant,
  createNetworkAccessPolicy,
  createNetworkGateway,
  createNetworkMihomoProfile,
  createNetworkNASBinding,
  createNetworkResource,
  createNetworkRuntimeEnrollment,
  createNetworkSite,
  createNetworkSiteProfileBinding,
  createNetworkSpace,
  deleteNetworkAccessPolicy,
  deleteNetworkMihomoProfile,
  deleteNetworkNASBinding,
  deleteNetworkResource,
  deleteNetworkSite,
  deleteNetworkSiteProfileBinding,
  deleteNetworkSpace,
  getNetworkAccessPolicySnapshot,
  getNetworkTelemetrySummary,
  listNetworkAccessPolicies,
  listNetworkAccessGrants,
  listNetworkNASBindings,
  listEndpointDevices,
  listNetworkGateways,
  listNetworkMihomoProfiles,
  listNetworkResources,
  listNetworkRuntimeEnrollments,
  listNetworkSites,
  listNetworkSiteProfileBindings,
  listNetworkSessions,
  listNetworkSpaces,
  executeNetworkSessionAction,
  planNetworkSessionAction,
  previewNetworkAccessPolicy,
  revokeNetworkRuntimeEnrollment,
  revokeNetworkAccessGrant,
  updateNetworkAccessPolicy,
  updateNetworkGateway,
  updateNetworkMihomoProfile,
  updateNetworkNASBinding,
  updateEndpointDevice,
  updateNetworkResource,
  updateNetworkSite,
  updateNetworkSiteProfileBinding,
  updateNetworkSpace,
} from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const timestamps = { createdAt: '2026-09-02T00:00:00Z', updatedAt: '2026-09-02T00:00:00Z' }
const device = {
  id: 'device/id',
  name: 'Laptop',
  ownerUserId: 'user-1',
  status: 'active',
  platform: 'windows',
  postureStatus: 'compliant',
  postureVersion: 2,
  ...timestamps,
} as EndpointDevice
const site = { id: 'site/id', name: 'HQ', status: 'active', ...timestamps } as NetworkSite
const space = {
  id: 'space/id',
  siteId: site.id,
  name: 'Office',
  status: 'active',
  cidrs: ['10.0.0.0/24'],
  ...timestamps,
} as NetworkSpace
const resource = {
  id: 'resource/id',
  spaceId: space.id,
  name: 'Git',
  kind: 'fqdn',
  target: 'git.example.com',
  protected: true,
  protocol: 'tcp',
  ports: [443],
  pathMode: 'wireguard_ztna',
  ...timestamps,
} as NetworkResource
const gateway = {
  id: 'gateway/id',
  runtimeId: 'gateway-b',
  siteId: 'site-b',
  name: 'Branch B',
  administrativeStatus: 'active',
  status: 'online',
  publicEndpointHost: 'vpn-b.example.com',
  publicEndpointPort: 51821,
  overlayCidr: '100.96.1.0/24',
  routingMode: 'routed',
  hubGatewayId: 'gateway-a',
  advertisedCidrs: ['10.20.0.0/16'],
  mtu: 1420,
  persistentKeepaliveSeconds: 25,
  dnsServers: ['10.20.0.53'],
  ...timestamps,
} as NetworkGateway
const enrollment = {
  id: 'enrollment/id',
  challengeId: 'challenge-1',
  runtimeId: 'endpoint-1',
  runtimeKind: 'endpoint',
  deviceId: device.id,
  subjectId: device.ownerUserId,
  status: 'pending',
  expiresAt: '2026-09-02T00:10:00Z',
  createdBy: 'operator-1',
  createdAt: timestamps.createdAt,
} as NetworkRuntimeEnrollment
const nasBinding = {
  id: 'nas-binding/id',
  nasId: 'nas-1',
  runtimeId: 'radius-1',
  siteId: site.id,
  name: 'HQ Wi-Fi',
  status: 'active',
  coaSupported: true,
  disconnectSupported: true,
  ...timestamps,
} as NetworkNASBinding
const profileBinding = {
  id: 'profile-binding/id',
  siteId: site.id,
  accessProfile: 'restricted',
  vlanId: 30,
  filterId: 'soha-restricted',
  sessionTimeoutSeconds: 900,
  ...timestamps,
} as NetworkSiteProfileBinding
const session = {
  id: 'session/id',
  subjectId: 'user-1',
  deviceId: device.id,
  siteId: site.id,
  nasId: nasBinding.nasId,
  mode: 'internal_direct',
  path: 'site_direct',
  accessProfile: 'full',
  status: 'active',
  policyVersion: 7,
  networkLeaseIds: [],
  resourceLeaseIds: [],
  startedAt: timestamps.createdAt,
  expiresAt: '2026-09-02T01:00:00Z',
} as NetworkSession
const mihomoProfile = {
  id: 'mihomo/profile',
  deviceId: device.id,
  name: 'Managed proxy',
  mode: 'managed_follow',
  status: 'active',
  subscriptionConfigured: true,
  revision: 3,
  mixedPort: 7890,
  controllerPort: 9090,
  dnsMode: 'fake_ip',
  fakeIpRange: '198.18.0.0/15',
  selectorGroup: 'PROXY',
  selectedProxy: 'edge-hk',
  bypassCidrs: ['10.0.0.0/8'],
  bypassHosts: ['control.internal'],
  failClosed: true,
  ...timestamps,
} as NetworkMihomoProfile
const telemetrySummary = {
  from: '2026-09-02T00:00:00Z',
  to: '2026-09-02T01:00:00Z',
  eventCount: 12,
  heartbeatCount: 4,
  radiusAccountingCount: 2,
  networkFlowCount: 1,
  connectionSummaryCount: 2,
  proxyFlowCount: 3,
  uploadBytes: 100,
  downloadBytes: 200,
  activeConnections: 5,
  producers: [
    {
      producerId: 'endpoint-1',
      producerKind: 'endpoint',
      lastSeenAt: '2026-09-02T00:59:00Z',
      gapCount: 0,
      regressionCount: 0,
    },
  ],
  proxyFlows: [
    {
      producerId: 'endpoint-1',
      engine: 'mihomo',
      profileId: mihomoProfile.id,
      profileRevision: mihomoProfile.revision,
      mode: mihomoProfile.mode,
      selectedProxy: mihomoProfile.selectedProxy ?? '',
      uploadBytes: 100,
      downloadBytes: 200,
      activeConnections: 5,
      lastOccurredAt: '2026-09-02T00:59:00Z',
    },
  ],
} as NetworkTelemetrySummary

describe('network access api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('normalizes and encodes list filters', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })

    await listEndpointDevices({
      search: ' Laptop ',
      ownerUserId: 'user/1',
      status: 'active',
      limit: 200,
    })
    await listNetworkSites({ search: ' HQ ', status: 'active', limit: 200 })
    await listNetworkSpaces({ siteId: 'site/1', status: 'active', limit: 200 })
    await listNetworkResources({ spaceId: 'space/1', kind: 'fqdn', protected: true, limit: 200 })
    await listNetworkGateways({ siteId: 'site/1', status: 'online', limit: 200 })
    await listNetworkNASBindings({ siteId: 'site/1', runtimeId: 'radius/1', status: 'active' })
    await listNetworkSiteProfileBindings({ siteId: 'site/1', accessProfile: 'restricted' })
    await listNetworkSessions({
      siteId: 'site/1',
      runtimeId: 'radius/1',
      subjectId: 'user/1',
      deviceId: 'device/1',
      status: 'active',
      limit: 50,
    })

    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/network-access/devices?search=Laptop&status=active&ownerUserId=user%2F1&limit=200',
      '/network-access/sites?search=HQ&status=active&limit=200',
      '/network-access/spaces?status=active&siteId=site%2F1&limit=200',
      '/network-access/resources?kind=fqdn&spaceId=space%2F1&protected=true&limit=200',
      '/network-access/gateways?status=online&siteId=site%2F1&limit=200',
      '/network-access/nas-bindings?siteId=site%2F1&runtimeId=radius%2F1&status=active',
      '/network-access/site-profile-bindings?siteId=site%2F1&accessProfile=restricted',
      '/network-access/sessions?siteId=site%2F1&runtimeId=radius%2F1&subjectId=user%2F1&deviceId=device%2F1&status=active&limit=50',
    ])
  })

  it('unwraps lists and uses encoded ids for updates', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: [device] })
    apiMocks.put.mockResolvedValueOnce({ data: device })
    const input: EndpointDeviceInput = {
      name: device.name,
      status: 'active',
      postureStatus: 'non_compliant',
    }

    await expect(listEndpointDevices({})).resolves.toEqual([device])
    await expect(updateEndpointDevice({ deviceId: device.id, input })).resolves.toEqual(device)
    expect(apiMocks.put).toHaveBeenCalledWith('/network-access/devices/device%2Fid', input)
  })

  it('maps mihomo profile CRUD without returning the write-only subscription', async () => {
    const input = {
      deviceId: device.id,
      name: mihomoProfile.name,
      mode: 'managed_follow',
      status: 'active',
      subscriptionUrl: 'https://subscription.example/profile',
      mixedPort: 7890,
      controllerPort: 9090,
      dnsMode: 'fake_ip',
      fakeIpRange: '198.18.0.0/15',
      selectorGroup: 'PROXY',
      selectedProxy: 'edge-hk',
      bypassCidrs: ['10.0.0.0/8'],
      bypassHosts: ['control.internal'],
      failClosed: true,
    } as NetworkMihomoProfileInput
    apiMocks.get.mockResolvedValueOnce({ data: [mihomoProfile] })
    apiMocks.post.mockResolvedValueOnce({ data: mihomoProfile })
    apiMocks.put.mockResolvedValueOnce({ data: mihomoProfile })
    apiMocks.delete.mockResolvedValueOnce({ status: 'ok' })

    const profiles = await listNetworkMihomoProfiles({
      search: ' Managed ',
      deviceId: 'device/1',
      mode: 'managed_follow',
      status: 'active',
      limit: 200,
    })
    await expect(createNetworkMihomoProfile(input)).resolves.toEqual(mihomoProfile)
    await expect(
      updateNetworkMihomoProfile({ profileId: mihomoProfile.id, input }),
    ).resolves.toEqual(mihomoProfile)
    await expect(deleteNetworkMihomoProfile(mihomoProfile.id)).resolves.toBeUndefined()

    expect(profiles).toEqual([mihomoProfile])
    expect(profiles[0]).not.toHaveProperty('subscriptionUrl')
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/network-access/mihomo-profiles?search=Managed&deviceId=device%2F1&mode=managed_follow&status=active&limit=200',
    )
    expect(apiMocks.post).toHaveBeenCalledWith('/network-access/mihomo-profiles', input)
    expect(apiMocks.put).toHaveBeenCalledWith(
      '/network-access/mihomo-profiles/mihomo%2Fprofile',
      input,
    )
    expect(apiMocks.delete).toHaveBeenCalledWith('/network-access/mihomo-profiles/mihomo%2Fprofile')
  })

  it('queries only bounded aggregate telemetry', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: telemetrySummary })

    await expect(
      getNetworkTelemetrySummary({
        from: telemetrySummary.from,
        to: telemetrySummary.to,
        producerId: 'endpoint/1',
        limit: 25,
      }),
    ).resolves.toEqual(telemetrySummary)

    expect(apiMocks.get).toHaveBeenCalledWith(
      '/network-access/telemetry/summary?from=2026-09-02T00%3A00%3A00Z&to=2026-09-02T01%3A00%3A00Z&producerId=endpoint%2F1&limit=25',
    )
    expect(telemetrySummary).not.toHaveProperty('events')
    expect(telemetrySummary).not.toHaveProperty('payload')
    expect(telemetrySummary).not.toHaveProperty('destination')
  })

  it('maps site, space and resource CRUD without leaking transport envelopes', async () => {
    const cases = [
      {
        create: createNetworkSite,
        update: updateNetworkSite,
        remove: deleteNetworkSite,
        value: site,
        input: { name: 'HQ', status: 'active' } as NetworkSiteInput,
        idKey: 'siteId',
        path: 'sites',
      },
      {
        create: createNetworkSpace,
        update: updateNetworkSpace,
        remove: deleteNetworkSpace,
        value: space,
        input: {
          siteId: site.id,
          name: 'Office',
          status: 'active',
          cidrs: ['10.0.0.0/24'],
        } as NetworkSpaceInput,
        idKey: 'spaceId',
        path: 'spaces',
      },
      {
        create: createNetworkResource,
        update: updateNetworkResource,
        remove: deleteNetworkResource,
        value: resource,
        input: {
          spaceId: space.id,
          name: 'Git',
          kind: 'fqdn',
          target: 'git.example.com',
          protected: true,
          protocol: 'tcp',
          ports: [443],
          pathMode: 'wireguard_ztna',
        } as NetworkResourceInput,
        idKey: 'resourceId',
        path: 'resources',
      },
    ] as const

    for (const item of cases) {
      apiMocks.post.mockResolvedValueOnce({ data: item.value })
      apiMocks.put.mockResolvedValueOnce({ data: item.value })
      apiMocks.delete.mockResolvedValueOnce({ status: 'ok' })
      await expect(item.create(item.input as never)).resolves.toEqual(item.value)
      await expect(
        item.update({ [item.idKey]: item.value.id, input: item.input } as never),
      ).resolves.toEqual(item.value)
      await expect(item.remove(item.value.id)).resolves.toBeUndefined()
      expect(apiMocks.post).toHaveBeenLastCalledWith(`/network-access/${item.path}`, item.input)
      expect(apiMocks.put).toHaveBeenLastCalledWith(
        `/network-access/${item.path}/${encodeURIComponent(item.value.id)}`,
        item.input,
      )
      expect(apiMocks.delete).toHaveBeenLastCalledWith(
        `/network-access/${item.path}/${encodeURIComponent(item.value.id)}`,
      )
    }
  })

  it('maps gateway create and update with hub-and-spoke topology fields', async () => {
    const input: NetworkGatewayInput = {
      runtimeId: gateway.runtimeId,
      siteId: gateway.siteId,
      name: gateway.name,
      administrativeStatus: gateway.administrativeStatus,
      publicEndpointHost: gateway.publicEndpointHost,
      publicEndpointPort: gateway.publicEndpointPort,
      overlayCidr: gateway.overlayCidr,
      routingMode: gateway.routingMode,
      hubGatewayId: gateway.hubGatewayId,
      advertisedCidrs: gateway.advertisedCidrs,
      mtu: gateway.mtu,
      persistentKeepaliveSeconds: gateway.persistentKeepaliveSeconds,
      dnsServers: gateway.dnsServers,
    }
    apiMocks.post.mockResolvedValueOnce({ data: gateway })
    apiMocks.put.mockResolvedValueOnce({ data: gateway })

    await expect(createNetworkGateway(input)).resolves.toEqual(gateway)
    await expect(updateNetworkGateway({ gatewayId: gateway.id, input })).resolves.toEqual(gateway)

    expect(apiMocks.post).toHaveBeenCalledWith('/network-access/gateways', input)
    expect(apiMocks.put).toHaveBeenCalledWith('/network-access/gateways/gateway%2Fid', input)
  })

  it('posts policy preview and unwraps the decision', async () => {
    const input: NetworkPolicyPreviewRequest = {
      subjectUserId: 'user-1',
      deviceId: device.id,
      resourceId: resource.id,
      mode: 'external_direct_ztna',
    }
    const result = {
      decision: 'allow',
      path: 'access_proxy',
      policyVersion: 1,
      protected: true,
      networkLeaseRequired: false,
      resourceLeaseRequired: true,
      reasons: ['policy_allow'],
    } as NetworkPolicyPreviewResult
    apiMocks.post.mockResolvedValueOnce({ data: result })

    await expect(previewNetworkAccessPolicy(input)).resolves.toEqual(result)
    expect(apiMocks.post).toHaveBeenCalledWith('/network-access/policy/preview', input)
  })

  it('maps runtime enrollment lifecycle without persisting the one-time token', async () => {
    const input = {
      runtimeId: enrollment.runtimeId,
      runtimeKind: enrollment.runtimeKind,
      deviceId: enrollment.deviceId,
      subjectId: enrollment.subjectId,
      ttlSeconds: 600,
    } as NetworkRuntimeEnrollmentInput
    const secret = {
      enrollment,
      token: '0123456789abcdef0123456789abcdef',
    } as NetworkRuntimeEnrollmentSecret
    const revoked = { ...enrollment, status: 'revoked' } as NetworkRuntimeEnrollment
    apiMocks.get.mockResolvedValueOnce({ data: [enrollment] })
    apiMocks.post.mockResolvedValueOnce({ data: secret }).mockResolvedValueOnce({ data: revoked })

    await expect(listNetworkRuntimeEnrollments({ limit: 200 })).resolves.toEqual([enrollment])
    await expect(createNetworkRuntimeEnrollment(input)).resolves.toEqual(secret)
    await expect(revokeNetworkRuntimeEnrollment(enrollment.id)).resolves.toEqual(revoked)

    expect(apiMocks.get).toHaveBeenCalledWith('/network-access/enrollments?limit=200')
    expect(apiMocks.post).toHaveBeenNthCalledWith(1, '/network-access/enrollments', input)
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      2,
      '/network-access/enrollments/enrollment%2Fid/revoke',
    )
  })

  it('maps short-lived network access grant lifecycle without returning token material from lists', async () => {
    const input = {
      deviceId: 'device-1',
      siteId: 'site-1',
      networkSpaceId: 'space-1',
      mode: 'internal_ztna',
      resourceIds: ['resource-db'],
      ttlSeconds: 300,
    } as NetworkAccessGrantInput
    const grant = {
      id: 'grant/id',
      subjectId: 'user-1',
      policyVersion: 7,
      status: 'issued',
      createdBy: 'user-1',
      createdAt: timestamps.createdAt,
      expiresAt: timestamps.updatedAt,
      ...input,
    } as NetworkAccessGrant
    const secret = {
      grant,
      token: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA',
    } as NetworkAccessGrantSecret
    apiMocks.get.mockResolvedValueOnce({ data: [grant] })
    apiMocks.post.mockResolvedValueOnce({ data: secret }).mockResolvedValueOnce({
      data: { ...grant, status: 'revoked' },
    })

    await expect(
      listNetworkAccessGrants({ subjectId: 'user/1', status: 'issued', limit: 200 }),
    ).resolves.toEqual([grant])
    await expect(createNetworkAccessGrant(input)).resolves.toEqual(secret)
    await expect(revokeNetworkAccessGrant(grant.id)).resolves.toMatchObject({ status: 'revoked' })

    expect(apiMocks.get).toHaveBeenCalledWith(
      '/network-access/access-grants?subjectId=user%2F1&status=issued&limit=200',
    )
    expect(apiMocks.post).toHaveBeenNthCalledWith(1, '/network-access/access-grants', input)
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      2,
      '/network-access/access-grants/grant%2Fid/revoke',
    )
  })

  it('maps policy drafts, snapshot publication and conflict analysis', async () => {
    const input: NetworkAccessPolicyInput = {
      name: 'Engineering',
      enabled: true,
      priority: 100,
      effect: 'allow',
      subjects: { users: [], teams: ['team-1'], tags: [] },
      siteIds: ['site-1'],
      resourceIds: ['resource-1'],
      modes: ['internal_ztna'],
      deviceStatuses: ['active'],
      postureStatuses: ['compliant'],
      accessProfile: 'full',
    }
    const policy = { id: 'policy/id', version: 1, ...timestamps, ...input } as NetworkAccessPolicy
    const snapshot = {
      policyVersion: 7,
      contentHash: `sha256:${'a'.repeat(64)}`,
      policyCount: 1,
      protectedResourceCount: 1,
      publishedAt: timestamps.updatedAt,
    } as NetworkPolicySnapshot
    const conflictRequest: NetworkConflictAnalysisRequest = {
      runtimeRanges: [{ sourceType: 'wireguard_overlay', name: 'overlay', cidr: '10.0.0.0/16' }],
    }
    const conflictResult = {
      valid: false,
      rangesAnalyzed: 2,
      conflicts: [],
      warnings: [],
    } as NetworkConflictAnalysisResult

    apiMocks.get.mockResolvedValueOnce({ data: [policy] }).mockResolvedValueOnce({ data: snapshot })
    apiMocks.post
      .mockResolvedValueOnce({ data: policy })
      .mockResolvedValueOnce({ data: snapshot })
      .mockResolvedValueOnce({ data: conflictResult })
    apiMocks.put.mockResolvedValueOnce({ data: policy })
    apiMocks.delete.mockResolvedValueOnce({ status: 'ok' })

    await expect(
      listNetworkAccessPolicies({
        search: ' Engineering ',
        enabled: false,
        effect: 'allow',
        limit: 200,
      }),
    ).resolves.toEqual([policy])
    await expect(createNetworkAccessPolicy(input)).resolves.toEqual(policy)
    await expect(updateNetworkAccessPolicy({ policyId: policy.id, input })).resolves.toEqual(policy)
    await expect(deleteNetworkAccessPolicy(policy.id)).resolves.toBeUndefined()
    await expect(getNetworkAccessPolicySnapshot()).resolves.toEqual(snapshot)
    await expect(compileNetworkAccessPolicySnapshot()).resolves.toEqual(snapshot)
    await expect(analyzeNetworkAccessConflicts(conflictRequest)).resolves.toEqual(conflictResult)

    expect(apiMocks.get).toHaveBeenNthCalledWith(
      1,
      '/network-access/policies?search=Engineering&enabled=false&effect=allow&limit=200',
    )
    expect(apiMocks.put).toHaveBeenCalledWith('/network-access/policies/policy%2Fid', input)
    expect(apiMocks.delete).toHaveBeenCalledWith('/network-access/policies/policy%2Fid')
    expect(apiMocks.get).toHaveBeenNthCalledWith(2, '/network-access/policy/snapshot')
    expect(apiMocks.post).toHaveBeenNthCalledWith(2, '/network-access/policies/compile')
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      3,
      '/network-access/conflicts/analyze',
      conflictRequest,
    )
  })

  it('maps NAS and site profile binding CRUD', async () => {
    const cases = [
      {
        create: createNetworkNASBinding,
        update: updateNetworkNASBinding,
        remove: deleteNetworkNASBinding,
        value: nasBinding,
        input: {
          nasId: nasBinding.nasId,
          runtimeId: nasBinding.runtimeId,
          siteId: nasBinding.siteId,
          name: nasBinding.name,
          status: nasBinding.status,
          coaSupported: true,
          disconnectSupported: true,
        } as NetworkNASBindingInput,
        idKey: 'bindingId',
        path: 'nas-bindings',
      },
      {
        create: createNetworkSiteProfileBinding,
        update: updateNetworkSiteProfileBinding,
        remove: deleteNetworkSiteProfileBinding,
        value: profileBinding,
        input: {
          siteId: profileBinding.siteId,
          accessProfile: profileBinding.accessProfile,
          vlanId: profileBinding.vlanId,
          filterId: profileBinding.filterId,
          sessionTimeoutSeconds: profileBinding.sessionTimeoutSeconds,
        } as NetworkSiteProfileBindingInput,
        idKey: 'bindingId',
        path: 'site-profile-bindings',
      },
    ] as const

    for (const item of cases) {
      apiMocks.post.mockResolvedValueOnce({ data: item.value })
      apiMocks.put.mockResolvedValueOnce({ data: item.value })
      apiMocks.delete.mockResolvedValueOnce({ status: 'ok' })
      await expect(item.create(item.input as never)).resolves.toEqual(item.value)
      await expect(
        item.update({ [item.idKey]: item.value.id, input: item.input } as never),
      ).resolves.toEqual(item.value)
      await expect(item.remove(item.value.id)).resolves.toBeUndefined()
      expect(apiMocks.post).toHaveBeenLastCalledWith(`/network-access/${item.path}`, item.input)
      expect(apiMocks.put).toHaveBeenLastCalledWith(
        `/network-access/${item.path}/${encodeURIComponent(item.value.id)}`,
        item.input,
      )
      expect(apiMocks.delete).toHaveBeenLastCalledWith(
        `/network-access/${item.path}/${encodeURIComponent(item.value.id)}`,
      )
    }
  })

  it('keeps session action planning separate from execution', async () => {
    const input = {
      action: 'coa',
      targetAccessProfile: 'restricted',
      reasonCode: 'risk_change',
    } as NetworkSessionActionInput
    const plan = {
      sessionId: session.id,
      runtimeId: nasBinding.runtimeId,
      nasId: nasBinding.nasId,
      requestedAction: 'coa',
      effectiveAction: 'disconnect',
      currentAccessProfile: 'full',
      targetAccessProfile: 'restricted',
      reasonCode: input.reasonCode,
      willDisconnect: true,
      commandExpiresAt: '2026-09-02T00:05:00Z',
      planHash: `sha256:${'b'.repeat(64)}`,
    } as NetworkSessionActionPlan
    const command = {
      id: 'command-1',
      sessionId: session.id,
      runtimeId: nasBinding.runtimeId,
      nasId: nasBinding.nasId,
      action: plan.effectiveAction,
      targetAccessProfile: plan.targetAccessProfile,
      policyVersion: 7,
      status: 'pending',
      reasonCode: plan.reasonCode,
      effectiveAt: timestamps.updatedAt,
      expiresAt: plan.commandExpiresAt,
      createdAt: timestamps.createdAt,
    } as NetworkSessionCommand
    apiMocks.post.mockResolvedValueOnce({ data: plan }).mockResolvedValueOnce({ data: command })

    await expect(planNetworkSessionAction({ sessionId: session.id, input })).resolves.toEqual(plan)
    await expect(
      executeNetworkSessionAction({
        sessionId: session.id,
        input: { ...input, planHash: plan.planHash },
      }),
    ).resolves.toEqual(command)

    expect(apiMocks.post).toHaveBeenNthCalledWith(
      1,
      '/network-access/sessions/session%2Fid/actions/plan',
      input,
    )
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      2,
      '/network-access/sessions/session%2Fid/actions/execute',
      { ...input, planHash: plan.planHash },
    )
  })
})
