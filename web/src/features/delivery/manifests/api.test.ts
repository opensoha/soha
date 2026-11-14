import { beforeEach, describe, expect, it, vi } from 'vitest'
import { manifestApi } from './api'
import type { ManifestEnvironmentBindingUpdateInput, ManifestPackageInput } from './types'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  postWithHeaders: vi.fn(),
  put: vi.fn(),
}))
vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('manifestApi', () => {
  beforeEach(() => vi.clearAllMocks())

  it('encodes scope filters and identifiers', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })
    await manifestApi.list({
      applicationId: 'app/a',
      serviceId: 'service/a',
      clusterId: 'dev cluster',
      namespace: 'team-a',
    })
    await manifestApi.revisions('package/a')

    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/delivery/manifest-packages?applicationId=app%2Fa&serviceId=service%2Fa&clusterId=dev+cluster&namespace=team-a',
      '/delivery/manifest-packages/package%2Fa/revisions',
    ])
  })

  it('preserves package and publish payloads', async () => {
    apiMocks.post.mockResolvedValue({ data: { id: 'manifest-1' } })
    const input = {
      name: 'Ingress',
      applicationId: 'app-1',
      renderer: 'raw_yaml',
      files: [],
      bindings: [],
    } satisfies ManifestPackageInput

    await manifestApi.create(input)
    await manifestApi.publish('manifest/1', 'promote host')

    expect(apiMocks.post).toHaveBeenNthCalledWith(1, '/delivery/manifest-packages', input)
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      2,
      '/delivery/manifest-packages/manifest%2F1/publish',
      { note: 'promote host' },
    )
  })

  it('uses durable action routes and an idempotency key for Git sync', async () => {
    apiMocks.post.mockResolvedValue({ data: { task: { id: 'task-1' } } })
    apiMocks.postWithHeaders.mockResolvedValue({ data: { run: { id: 'run-1' } } })

    await manifestApi.setDesiredRevision('binding/1', {
      desiredRevision: 3,
      expectedGeneration: 2,
      reconcilePolicy: 'continuous',
    })
    await manifestApi.sync('package/1', 5)
    await manifestApi.decideIntent('intent/1', 'accept', {
      expectedCurrentRevision: 3,
      expectedPackageUpdatedAt: '2026-07-30T00:00:00Z',
    })

    expect(apiMocks.post).toHaveBeenNthCalledWith(
      1,
      '/delivery/manifest-bindings/binding%2F1/desired-revision',
      { desiredRevision: 3, expectedGeneration: 2, reconcilePolicy: 'continuous' },
    )
    expect(apiMocks.postWithHeaders.mock.calls[0][0]).toBe(
      '/delivery/manifest-packages/package%2F1/sync',
    )
    expect(apiMocks.postWithHeaders.mock.calls[0][2]['Idempotency-Key']).toMatch(
      /^manifest-web-package%2F1-/,
    )
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      2,
      '/delivery/manifest-delivery-intents/intent%2F1/accept',
      {
        expectedCurrentRevision: 3,
        expectedPackageUpdatedAt: '2026-07-30T00:00:00Z',
      },
    )
  })

  it('preserves the complete environment binding update', async () => {
    apiMocks.put.mockResolvedValue({ data: { id: 'binding-1' } })
    const input = {
      applicationEnvironmentId: 'environment-1',
      clusterId: 'cluster-1',
      namespace: 'demo',
      overlay: { replicas: '3' },
      rolloutStrategyId: 'strategy-1',
      verificationPolicyId: 'policy-1',
      driftPolicy: 'report',
      deletionPolicy: 'orphan',
      enabled: false,
      expectedVersion: 4,
    } satisfies ManifestEnvironmentBindingUpdateInput

    await manifestApi.updateBinding('binding/1', input)

    expect(apiMocks.put).toHaveBeenCalledWith('/delivery/manifest-bindings/binding%2F1', input)
  })

  it('loads every deployment page for a manifest package', async () => {
    apiMocks.get
      .mockResolvedValueOnce({
        data: {
          items: Array.from({ length: 100 }, (_, index) => ({ id: `deployment-${index}` })),
          total: 101,
          page: 1,
          pageSize: 100,
        },
      })
      .mockResolvedValueOnce({
        data: { items: [{ id: 'deployment-100' }], total: 101, page: 2, pageSize: 100 },
      })

    const result = await manifestApi.deployments('package/1')

    expect(result.items).toHaveLength(101)
    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/delivery/manifest-deployments?packageId=package%2F1&page=1&pageSize=100',
      '/delivery/manifest-deployments?packageId=package%2F1&page=2&pageSize=100',
    ])
  })
})
