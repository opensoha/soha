import { describe, expect, it } from 'vitest'
import type { ApplicationRuntimeWorkload, ReleaseTarget } from '../types'
import { visibleServiceInventory } from './helm-service-inventory'
import type { ServiceCard } from './service-cards'

const workload: ApplicationRuntimeWorkload = {
  applicationEnvironmentId: 'environment',
  clusterId: 'cluster',
  namespace: 'demo',
  targetId: 'helm',
  workloadKind: 'Deployment',
  workloadName: 'cart',
  desiredReplicas: 1,
  readyReplicas: 1,
  updatedReplicas: 1,
  availableReplicas: 1,
  source: {
    resource: {
      clusterId: 'cluster',
      namespace: 'demo',
      apiVersion: 'apps/v1',
      kind: 'Deployment',
      name: 'cart',
      uid: 'cart-uid',
      scopeMode: 'namespace',
    },
    specDigest: 'snapshot',
    observedAt: '2026-10-25T00:00:00Z',
  },
}

const nativeWorkload: ApplicationRuntimeWorkload = {
  ...workload,
  targetId: 'native',
  sourceStatus: 'linked',
}

const release: ServiceCard = {
  id: 'release',
  kind: 'Helm Release',
  name: 'Store',
  workloads: [workload],
}
const native: ServiceCard = {
  id: 'cart-service',
  kind: 'Kubernetes 工作负载',
  name: 'Cart',
  service: {
    id: 'cart-service',
    applicationId: 'app',
    key: 'cart',
    name: 'Cart',
    serviceKind: 'kubernetes_workload',
    enabled: true,
    createdAt: 'now',
    updatedAt: 'now',
  },
  workloads: [nativeWorkload],
}
const targets: ReleaseTarget[] = [
  {
    id: 'helm',
    clusterId: 'cluster',
    namespace: 'demo',
    workloadKind: 'HelmRelease',
    workloadName: 'store',
    enabled: false,
    metadata: { importSource: 'helm', serviceId: 'release' },
  },
  {
    id: 'native',
    clusterId: 'cluster',
    namespace: 'demo',
    workloadKind: 'Deployment',
    workloadName: 'cart',
    enabled: false,
    metadata: {
      importSource: 'kubernetes',
      ownershipMode: 'observe_only',
      serviceId: 'cart-service',
    },
  },
]

describe('Helm service inventory', () => {
  it('shows a release once when another service only observes its verified workload', () => {
    const { visibleCards, nestedServices } = visibleServiceInventory([release, native], targets)
    expect(visibleCards).toEqual([release])
    expect(nestedServices.get(release.id)).toEqual([native])
  })

  it.each([
    { ...nativeWorkload, sourceStatus: 'replaced' as const },
    { ...nativeWorkload, applicationEnvironmentId: 'other' },
    {
      ...nativeWorkload,
      source: {
        ...nativeWorkload.source!,
        resource: { ...nativeWorkload.source!.resource, uid: 'different-uid' },
      },
    },
  ])('keeps a service visible when the live association cannot be confirmed', (unverified) => {
    const standalone = { ...native, workloads: [unverified] }
    expect(visibleServiceInventory([release, standalone], targets).visibleCards).toEqual([
      release,
      standalone,
    ])
  })

  it('keeps services with an additional independent delivery target visible', () => {
    const independentTarget = {
      ...targets[1],
      id: 'manifest',
      metadata: { serviceId: native.id },
    }
    expect(
      visibleServiceInventory([release, native], [...targets, independentTarget]).visibleCards,
    ).toEqual([release, native])
  })
})
