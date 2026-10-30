import { describe, expect, it } from 'vitest'
import {
  buildRelatedResourcePath,
  buildVolumeDetailPath,
  buildWorkloadDetailPath,
  buildWorkloadListPagePath,
  localizeRelatedResourceKind,
} from './workloads-model'

describe('workload relation paths', () => {
  it('links network and controller relations to their detail pages', () => {
    expect(
      buildRelatedResourcePath(
        { kind: 'Ingress', name: 'api', namespace: 'team-a' },
        'selected-ns',
      ),
    ).toBe('/network/ingresses/api?namespace=team-a')
    expect(
      buildRelatedResourcePath(
        { kind: 'ReplicaSet', name: 'api-7d9', namespace: 'team-a' },
        'selected-ns',
        'cluster-a',
      ),
    ).toBe('/workloads/replicasets/api-7d9?clusterId=cluster-a&namespace=selected-ns')
    expect(
      buildRelatedResourcePath(
        { kind: 'ReplicationController', name: 'legacy', namespace: 'team-a' },
        null,
        'cluster-a',
      ),
    ).toBe('/workloads/replicationcontrollers/legacy?clusterId=cluster-a&namespace=team-a')
    expect(localizeRelatedResourceKind('ReplicationController', 'zh_CN')).toBe(
      'ReplicationController',
    )
    expect(
      buildRelatedResourcePath({ kind: 'HTTPRoute', name: 'api', namespace: 'team-a' }, null),
    ).toBe('/network/gateway-api/httproutes/api?namespace=team-a')
    expect(
      buildRelatedResourcePath({ kind: 'GRPCRoute', name: 'rpc', namespace: 'team-a' }, null),
    ).toBe('/network/gateway-api/grpcroutes/rpc?namespace=team-a')
  })

  it('keeps cluster scope in non-workload relations and volume targets', () => {
    expect(
      buildRelatedResourcePath(
        { kind: 'ServiceAccount', name: 'runner', namespace: 'team-a' },
        null,
        'cluster-a',
      ),
    ).toBe('/platform-access-control/serviceaccounts/runner?clusterId=cluster-a&namespace=team-a')
    expect(
      buildVolumeDetailPath(
        { name: 'config', type: 'ConfigMap', sourceName: 'api-config', readOnly: true },
        'team-a',
        'cluster-a',
      ),
    ).toBe('/configuration/configmaps/api-config?clusterId=cluster-a&namespace=team-a')
  })

  it('keeps cluster and namespace scope in workload detail links', () => {
    expect(buildWorkloadDetailPath('pods', 'api/server', null, 'team/a', 'cluster/a')).toBe(
      '/workloads/pods/api%2Fserver?clusterId=cluster%2Fa&namespace=team%2Fa',
    )
  })

  it('builds scoped workload list and event detail routes', () => {
    expect(buildWorkloadListPagePath('pods', 'team/a', 'cluster/a')).toBe(
      '/workloads/pods?clusterId=cluster%2Fa&namespace=team%2Fa',
    )
  })
})
