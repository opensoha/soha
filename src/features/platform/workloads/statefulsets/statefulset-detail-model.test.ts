import { describe, it, expect } from 'vitest'
import { readStatefulSetConfiguration, readStatefulSetRevision } from './statefulset-configuration'
import { statefulSetState } from './statefulset-detail-model'
const yaml = `apiVersion: apps/v1
kind: StatefulSet
metadata:
 name: db
 namespace: ops
 uid: uid-new
 generation: 4
spec:
 serviceName: db-headless
 podManagementPolicy: OrderedReady
 updateStrategy:
  type: RollingUpdate
  rollingUpdate:
   partition: 2
 persistentVolumeClaimRetentionPolicy:
  whenDeleted: Retain
  whenScaled: Delete
 template:
  spec:
   initContainers:
   - name: init
     image: prepare:v2
   containers:
   - name: db
     image: db:v2
 volumeClaimTemplates:
 - metadata:
    name: data
   spec:
    accessModes: [ReadWriteOnce]
    storageClassName: fast
    resources:
     requests:
      storage: 20Gi
status:
 observedGeneration: 4
 updatedReplicas: 1
`
const detail = {
  name: 'db',
  namespace: 'ops',
  createdAt: '2026-10-01',
  desiredReplicas: 3,
  readyReplicas: 3,
}
describe('StatefulSet observed configuration', () => {
  it('reads native template, partition, storage and independent retention semantics', () => {
    const config = readStatefulSetConfiguration(yaml, 'db', 'ops')
    expect(config.claims[0]).toMatchObject({
      name: 'data',
      requested: '20Gi',
      storageClass: 'fast',
    })
    expect(config.containers).toEqual([
      { name: 'init', image: 'prepare:v2', init: true },
      { name: 'db', image: 'db:v2', init: false },
    ])
    expect(config.retention).toEqual({ whenDeleted: 'Retain', whenScaled: 'Delete' })
    expect(statefulSetState(detail, config)).toBe('healthy')
    expect(statefulSetState(detail, { ...config, partition: 0 })).toBe('progressing')
    expect(statefulSetState(detail, { ...config, partition: 2, updatedReplicas: 0 })).toBe(
      'progressing',
    )
    expect(
      statefulSetState(detail, { ...config, partition: 5, ordinalStart: 5, updatedReplicas: 0 }),
    ).toBe('healthy')
    expect(
      statefulSetState(detail, { ...config, partition: 2, ordinalStart: 5, updatedReplicas: 1 }),
    ).toBe('healthy')
    expect(statefulSetState(detail, { ...config, strategy: 'OnDelete' })).toBe('healthy')
    expect(statefulSetState({ ...detail, desiredReplicas: 0, readyReplicas: 0 }, config)).toBe(
      'scaled-to-zero',
    )
    expect(statefulSetState({ ...detail, readyReplicas: undefined }, config)).toBe('unknown')
    expect(statefulSetState({ ...detail, readyReplicas: 1 }, config)).toBe('degraded')
    expect(statefulSetState(detail, { ...config, observedGeneration: 3 })).toBe('progressing')
  })
  it('rejects wrong identity, malformed or over-size documents', () => {
    expect(() => readStatefulSetConfiguration(yaml, 'other', 'ops')).toThrow(/identity/)
    expect(() => readStatefulSetConfiguration(yaml, 'db', 'other')).toThrow(/identity/)
    expect(() =>
      readStatefulSetConfiguration('x'.repeat(4 * 1024 * 1024 + 1), 'db', 'ops'),
    ).toThrow(/size/)
  })
  it('uses only the selected retained ControllerRevision template', () => {
    const record = { name: 'db-a', namespace: 'ops', revision: '3', replicas: 0, readyReplicas: 0 }
    const content =
      'kind: ControllerRevision\nmetadata:\n name: db-a\n namespace: ops\nrevision: 3\ndata:\n spec:\n  template:\n   spec:\n    containers: [{name: db, image: db:v1}]\n'
    expect(readStatefulSetRevision(content, record).template).toContain('db:v1')
    expect(() => readStatefulSetRevision(content, { ...record, name: 'other' })).toThrow(/identity/)
    expect(() => readStatefulSetRevision(content, { ...record, revision: '4' })).toThrow(/identity/)
  })
})
