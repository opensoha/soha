import { describe, expect, it } from 'vitest'
import { buildKubernetesEventResourcePath, buildKubernetesResourcePath } from './resource-ref'

describe('Kubernetes resource reference routes', () => {
  it.each([
    ['Deployment', 'apps/v1', '/workloads/deployments/api?clusterId=cluster-a&namespace=team-a'],
    ['Service', 'v1', '/network/services/api?namespace=team-a'],
    ['HorizontalPodAutoscaler', 'autoscaling/v2', '/configuration/hpas/api?namespace=team-a'],
    ['PersistentVolumeClaim', 'v1', '/storage/persistentvolumeclaims/api?namespace=team-a'],
    ['ServiceAccount', 'v1', '/platform-access-control/serviceaccounts/api?namespace=team-a'],
    ['Node', 'v1', '/cluster-resources/nodes/api?clusterId=cluster-a'],
  ])('maps %s references to a detail route', (kind, apiVersion, expected) => {
    expect(
      buildKubernetesResourcePath({
        apiVersion,
        clusterId: 'cluster-a',
        kind,
        name: 'api',
        namespace: kind === 'Node' ? undefined : 'team-a',
        scopeMode: kind === 'Node' ? 'cluster' : 'namespace',
      }),
    ).toBe(expected)
  })

  it('returns null when the console has no stable detail route', () => {
    expect(
      buildKubernetesResourcePath({
        apiVersion: 'example.io/v1',
        clusterId: 'cluster-a',
        kind: 'Widget',
        name: 'sample',
        namespace: 'team-a',
        scopeMode: 'namespace',
      }),
    ).toBeNull()
  })

  it('uses the same resource mapping for Kubernetes events', () => {
    expect(
      buildKubernetesEventResourcePath(
        { involvedKind: 'Node', involvedName: 'worker-1' },
        'cluster-a',
      ),
    ).toBe('/cluster-resources/nodes/worker-1?clusterId=cluster-a')
    expect(buildKubernetesEventResourcePath({ involvedKind: 'Pod' }, 'cluster-a')).toBeNull()
  })
})
