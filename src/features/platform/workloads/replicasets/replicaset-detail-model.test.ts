import { describe, expect, it } from 'vitest'
import {
  readReplicaSetConfiguration,
  replicaSetHealth,
  replicaSetOwner,
  replicaSetRevision,
  replicaSetTemplateState,
} from './replicaset-detail-model'
import type { ReplicaSetDetail } from './types'
const detail: ReplicaSetDetail = {
  name: 'app-abc',
  namespace: 'team',
  createdAt: '2026-11-01',
  desiredReplicas: 3,
  readyReplicas: 3,
  availableReplicas: 3,
  annotations: { 'deployment.kubernetes.io/revision': '7' },
  relatedResources: [{ kind: 'Deployment', name: 'app', namespace: 'team', relation: 'owner' }],
}
const yaml =
  'apiVersion: apps/v1\nkind: ReplicaSet\nmetadata: {name: app-abc, namespace: team, generation: 0}\nspec: {minReadySeconds: 0, template: {spec: {initContainers: [{name: init, image: init:1}], containers: [{name: app, image: app:7}]}}}\nstatus: {replicas: 0, fullyLabeledReplicas: 0, observedGeneration: 0, conditions: [{type: ReplicaFailure, status: "True", reason: FailedCreate, message: exceeded quota, lastTransitionTime: "2026-11-01T01:02:03Z"}]}\n'
describe('ReplicaSet native state and ancestry', () => {
  it('requires availability as well as readiness and preserves zero and unknown', () => {
    expect(replicaSetHealth(detail)).toBe('healthy')
    expect(replicaSetHealth({ ...detail, availableReplicas: 1 })).toBe('warning')
    expect(replicaSetHealth({ desiredReplicas: 3, readyReplicas: 3 })).toBe('unknown')
    expect(replicaSetHealth({ desiredReplicas: 0, readyReplicas: 0, availableReplicas: 0 })).toBe(
      'inactive',
    )
    expect(replicaSetHealth(detail, readReplicaSetConfiguration(yaml, 'app-abc', 'team'))).toBe(
      'failed',
    )
  })
  it('uses only same-namespace Deployment owner references and raw revision annotations', () => {
    expect(replicaSetOwner(detail)?.name).toBe('app')
    expect(replicaSetRevision(detail)).toBe('7')
    expect(
      replicaSetOwner({
        ...detail,
        relatedResources: [
          { kind: 'Deployment', name: 'app', namespace: 'other', relation: 'owner' },
        ],
      }),
    ).toBeUndefined()
    expect(
      replicaSetRevision({ ...detail, annotations: { resourceVersion: '12345' } }),
    ).toBeUndefined()
    expect(replicaSetTemplateState({ ...detail, relatedResources: [] })).toBe('standalone')
  })
  it('requires the exact parent status and history membership to classify a revision', () => {
    const record = {
      name: detail.name,
      namespace: detail.namespace,
      revision: '7',
      replicas: 3,
      readyReplicas: 3,
    }
    const status = {
      name: 'app',
      namespace: 'team',
      revision: '7',
      status: 'healthy',
      message: '',
      desiredReplicas: 3,
      updatedReplicas: 3,
      readyReplicas: 3,
      availableReplicas: 3,
      observedGeneration: 1,
    }
    expect(replicaSetTemplateState(detail, [record], status)).toBe('current')
    expect(replicaSetTemplateState(detail, [record], { ...status, revision: '8' })).toBe('retained')
    for (const parent of [
      undefined,
      { ...status, name: 'other' },
      { ...status, namespace: 'other' },
      { ...status, revision: '' },
    ])
      expect(replicaSetTemplateState(detail, [record], parent)).toBe('unknown')
    for (const records of [
      undefined,
      [],
      [{ ...record, namespace: 'other' }],
      [{ ...record, revision: '8' }],
    ])
      expect(replicaSetTemplateState(detail, records, status)).toBe('unknown')
  })
})
describe('ReplicaSet native configuration', () => {
  it('preserves zero configuration, init/regular images and failure conditions', () => {
    expect(readReplicaSetConfiguration(yaml, 'app-abc', 'team')).toMatchObject({
      generation: 0,
      currentReplicas: 0,
      observedGeneration: 0,
      minReadySeconds: 0,
      containers: [
        { name: 'init', image: 'init:1', init: true },
        { name: 'app', image: 'app:7', init: false },
      ],
      conditions: [{ type: 'ReplicaFailure', reason: 'FailedCreate', message: 'exceeded quota' }],
    })
  })
  it('rejects wrong kind, name, namespace, missing Pod template and oversized or invalid YAML', () => {
    for (const content of [
      yaml.replace('kind: ReplicaSet', 'kind: Deployment'),
      yaml.replace('name: app-abc', 'name: other'),
      yaml.replace('namespace: team', 'namespace: other'),
      'kind: ReplicaSet\nmetadata: {name: app-abc, namespace: team}\nspec: {}',
      'spec: [bad',
      ' '.repeat(4 * 1024 * 1024 + 1),
    ])
      expect(() => readReplicaSetConfiguration(content, 'app-abc', 'team')).toThrow()
  })
})
