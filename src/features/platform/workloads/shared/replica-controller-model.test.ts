import { describe, it, expect } from 'vitest'
import {
  readReplicaControllerConfiguration as read,
  replicaControllerHealth as health,
} from './replica-controller-model'
const yaml =
  'apiVersion: v1\nkind: ReplicationController\nmetadata: {name: legacy, namespace: team, generation: 0}\nspec: {minReadySeconds: 0, template: {spec: {initContainers: [{name: init, image: init:1}], containers: [{name: app, image: app:1}]}}}\nstatus: {replicas: 0, fullyLabeledReplicas: 0, observedGeneration: 0}'
describe('RC native configuration and observed health', () => {
  it('preserves zero and native init/regular templates', () => {
    expect(read(yaml, 'legacy', 'team', 'ReplicationController')).toMatchObject({
      generation: 0,
      currentReplicas: 0,
      observedGeneration: 0,
      minReadySeconds: 0,
      fullyLabeledReplicas: 0,
      containers: [
        { name: 'init', image: 'init:1', init: true },
        { name: 'app', image: 'app:1', init: false },
      ],
    })
  })
  it('allows the optional RC template but retains identity and parse bounds', () => {
    expect(
      read(
        'kind: ReplicationController\nmetadata: {name: legacy, namespace: team}\nspec: {}',
        'legacy',
        'team',
        'ReplicationController',
      ).containers,
    ).toEqual([])
    for (const content of [
      yaml.replace('kind: ReplicationController', 'kind: ReplicaSet'),
      yaml.replace('name: legacy', 'name: other'),
      yaml.replace('namespace: team', 'namespace: other'),
      'spec: [bad',
      'x'.repeat(4 * 1024 * 1024 + 1),
    ])
      expect(() => read(content, 'legacy', 'team', 'ReplicationController')).toThrow()
  })
  it('requires current, readiness and availability; distinguishes zero and failure', () => {
    const counts = {
      desiredReplicas: 3,
      currentReplicas: 3,
      readyReplicas: 3,
      availableReplicas: 3,
    }
    expect(health(counts, undefined, true)).toBe('healthy')
    for (const key of ['currentReplicas', 'readyReplicas', 'availableReplicas'] as const) {
      expect(health({ ...counts, [key]: 1 }, undefined, true)).toBe('warning')
      expect(health({ ...counts, [key]: undefined }, undefined, true)).toBe('unknown')
    }
    expect(health({ desiredReplicas: 0 }, undefined, true)).toBe('inactive')
    expect(
      health(
        counts,
        {
          containers: [],
          conditions: [{ type: 'ReplicaFailure', status: 'True', reason: 'FailedCreate' }],
        },
        true,
      ),
    ).toBe('failed')
  })
})
