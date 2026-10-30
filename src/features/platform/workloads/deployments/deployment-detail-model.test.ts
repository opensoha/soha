import { describe, expect, it } from 'vitest'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import type { DeploymentDetail, RolloutHistory } from '@/types'
import { buildDeploymentRelationships } from './deployment-detail-model'

const detail: DeploymentDetail = {
  name: 'api',
  namespace: 'ns',
  desiredReplicas: 3,
  updatedReplicas: 2,
  readyReplicas: 2,
  availableReplicas: 2,
  observedGeneration: 1,
  strategy: 'RollingUpdate',
}
const scope = { clusterId: 'a', namespace: 'ns' }
const history: RolloutHistory[] = [
  { name: 'api-new', namespace: 'ns', revision: '12', replicas: 2, readyReplicas: 1 },
  { name: 'api-old', namespace: 'ns', revision: '11', replicas: 1, readyReplicas: 1 },
  { name: 'api-zero', namespace: 'ns', revision: '10', replicas: 0, readyReplicas: 0 },
]
const graph = {
  rootId: 'deployment',
  clusterId: 'a',
  generatedAt: '',
  evidence: [],
  warnings: [],
  nodes: [
    ['deployment', 'Deployment', 'api'],
    ['new', 'ReplicaSet', 'api-new'],
    ['old', 'ReplicaSet', 'api-old'],
    ['unrelated', 'ReplicaSet', 'other'],
    ['new-pod', 'Pod', 'api-new-1'],
    ['old-pod', 'Pod', 'api-old-1'],
    ['config', 'ConfigMap', 'api-config'],
  ].map(([id, kind, name]) => ({
    id,
    resource: {
      clusterId: 'a',
      namespace: 'ns',
      scopeMode: 'namespace',
      apiVersion: 'v1',
      kind,
      name,
    },
  })),
  edges: [
    ['deployment', 'new', 'owns'],
    ['deployment', 'old', 'owns'],
    ['new', 'new-pod', 'owns'],
    ['old', 'old-pod', 'owns'],
    ['deployment', 'config', 'uses-config'],
    ['new', 'config', 'uses-config'],
  ].map(([sourceId, targetId, relation], index) => ({
    id: String(index),
    sourceId,
    targetId,
    relation,
  })),
} as KubernetesResourceGraph

describe('Deployment relationship evidence', () => {
  it('keeps active old revisions visible and assigns Pods only through owns edges', () => {
    const model = buildDeploymentRelationships(detail, scope, history, '12', graph)
    expect(model.sets.filter((set) => !set.historical).map((set) => set.record?.revision)).toEqual([
      '12',
      '11',
    ])
    expect(model.podOwners.get('ns/api-new-1')?.record?.revision).toBe('12')
    expect(model.podOwners.get('ns/api-old-1')?.record?.revision).toBe('11')
    expect(model.sets.some((set) => set.node.resource.name === 'other')).toBe(false)
    expect(model.references.map((node) => node.resource.name)).toEqual(['api-config'])
  })
  it('does not infer Pod ownership from name prefixes or a different root scope', () => {
    const model = buildDeploymentRelationships(detail, scope, history, '12')
    expect(model.podOwners.size).toBe(0)
    expect(model.sets.every((set) => set.pods.length === 0)).toBe(true)
    const foreign = buildDeploymentRelationships(
      detail,
      { clusterId: 'b', namespace: 'ns' },
      history,
      '12',
      graph,
    )
    expect(foreign.podOwners.size).toBe(0)
    expect(foreign.references).toHaveLength(0)
  })
})
