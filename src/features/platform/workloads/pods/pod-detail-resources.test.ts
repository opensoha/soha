import { describe, expect, it } from 'vitest'
import type { PodDetail } from '@/types'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { buildPodRelationships } from './pod-detail-resources'

const pod = {
  name: 'api-pod',
  namespace: 'team-a',
  phase: 'Running',
  nodeName: 'worker-a',
  serviceAccountName: 'runner',
  relatedResources: [
    {
      kind: 'ConfigMap',
      name: 'api-config',
      namespace: 'team-a',
      relations: ['uses-config', 'mounted'],
      details: ['Mount /etc/config'],
    },
  ],
} as PodDetail
function fixture(): KubernetesResourceGraph {
  const make = (id: string, kind: string, name = id) => ({
    id,
    resource: {
      kind,
      name,
      namespace: 'team-a',
      clusterId: 'cluster-a',
      apiVersion: 'v1',
      scopeMode: 'namespace' as const,
    },
  })
  return {
    rootId: 'pod',
    clusterId: 'cluster-a',
    generatedAt: '2026-10-30T10:00:00Z',
    warnings: [],
    evidence: [],
    nodes: [
      make('deploy', 'Deployment'),
      make('rs', 'ReplicaSet'),
      make('other-rs', 'ReplicaSet'),
      make('unrelated-rs', 'ReplicaSet'),
      make('pod', 'Pod', 'api-pod'),
      make('config', 'ConfigMap', 'api-config'),
    ],
    edges: [
      { id: 'owns-rs', sourceId: 'deploy', targetId: 'rs', relation: 'owns' },
      { id: 'owns-other', sourceId: 'deploy', targetId: 'other-rs', relation: 'owns' },
      { id: 'owns-pod', sourceId: 'rs', targetId: 'pod', relation: 'owns' },
      { id: 'uses-config', sourceId: 'pod', targetId: 'config', relation: 'uses-config' },
    ],
  }
}
describe('Pod detail relationship composition', () => {
  it('uses an explicit direct owner from Pod detail when graph collection omits the controller', () => {
    const owner = {
      kind: 'StatefulSet',
      name: 'database',
      namespace: 'team-a',
      relations: ['owner'],
    }
    expect(
      buildPodRelationships({ ...pod, relatedResources: [owner] }, 'cluster-a').ancestors,
    ).toMatchObject([
      { resource: { kind: 'StatefulSet', name: 'database' }, evidence: 'pod-related-resources' },
    ])
    expect(
      buildPodRelationships(
        { ...pod, relatedResources: [{ ...owner, namespace: undefined }] },
        'cluster-a',
      ).ancestors[0].resource.namespace,
    ).toBe('team-a')
    expect(
      buildPodRelationships(
        { ...pod, relatedResources: [{ ...owner, relations: ['selected-by'] }] },
        'cluster-a',
      ).ancestors,
    ).toEqual([])
    expect(
      buildPodRelationships(
        { ...pod, relatedResources: [{ ...owner, namespace: 'other-team' }] },
        'cluster-a',
      ).ancestors,
    ).toEqual([])
  })
  it('uses the actual owner chain and supplements scheduled node and service account from Pod fields', () => {
    const graph = fixture()
    const model = buildPodRelationships(pod, 'cluster-a', graph)
    expect(model.ancestors.map((item) => item.resource.kind)).toEqual(['Deployment', 'ReplicaSet'])
    expect(model.node?.resource).toMatchObject({
      kind: 'Node',
      name: 'worker-a',
      scopeMode: 'cluster',
      clusterId: 'cluster-a',
    })
    expect(model.references.map((item) => item.resource.kind)).toEqual([
      'ConfigMap',
      'ServiceAccount',
    ])
    expect(model.references.filter((item) => item.resource.name === 'api-config')).toHaveLength(1)
    expect(model.references.find((item) => item.resource.name === 'api-config')?.details).toEqual([
      'Mount /etc/config',
    ])
    expect(model.references.find((item) => item.resource.name === 'api-config')?.relations).toEqual(
      ['uses-config', 'mounted'],
    )
    expect(model.otherReplicaSets.map((item) => item.id)).toEqual(['other-rs'])
    expect(graph.nodes).toHaveLength(6)
  })
  it('does not invent owners, history or scheduled nodes when data is absent', () => {
    const model = buildPodRelationships(
      { ...pod, nodeName: '—', serviceAccountName: '' },
      'cluster-a',
    )
    expect(model.ancestors).toEqual([])
    expect(model.otherReplicaSets).toEqual([])
    expect(model.node).toBeUndefined()
    expect(model.references.map((item) => item.resource.kind)).toEqual(['ConfigMap'])
    expect(model.root.resource.name).toBe('api-pod')
  })
  it('stops ownership cycles and never treats inbound selector edges as ownership', () => {
    const graph = fixture()
    graph.edges.push(
      { id: 'cycle', sourceId: 'pod', targetId: 'deploy', relation: 'owns' },
      { id: 'selector', sourceId: 'unrelated-rs', targetId: 'pod', relation: 'selects' },
    )
    const model = buildPodRelationships(pod, 'cluster-a', graph)
    expect(model.ancestors.map((item) => item.resource.kind)).toEqual(['Deployment', 'ReplicaSet'])
  })
})
