import { describe, expect, it } from 'vitest'
import { assessPersistentVolumeClaim } from './lifecycle-model'

describe('assessPersistentVolumeClaim', () => {
  it('flags ReadWriteOnce references across nodes as an investigation hint', () => {
    const assessment = assessPersistentVolumeClaim({
      status: 'Bound',
      volumeName: 'pvc-1',
      accessModes: ['ReadWriteOnce'],
      pods: [
        { name: 'api-0', namespace: 'team-a', nodeName: 'node-a' },
        { name: 'api-1', namespace: 'team-a', nodeName: 'node-b' },
      ],
    })

    expect(assessment.currentStep).toBe(2)
    expect(assessment.risks).toContain('multi-node-rwo')
  })

  it.each([undefined, []])(
    'does not infer an unmounted claim from absent visible references: %j',
    (pods) => {
      const assessment = assessPersistentVolumeClaim({ status: 'Bound', pods })
      expect(assessment.currentStep).toBe(1)
      expect(assessment.risks).toEqual([])
    },
  )

  it.each(['Pending', 'Lost', 'Unknown'])(
    'does not infer binding from volume name or references for %s',
    (status) => {
      const assessment = assessPersistentVolumeClaim({
        status,
        volumeName: 'requested-pv',
        pods: [{ name: 'pending-pod', namespace: 'team-a' }],
      })
      expect(assessment.currentStep).toBe(0)
      expect(assessment.risks).toEqual(status === 'Unknown' ? [] : [status.toLowerCase()])
    },
  )

  it('preserves truncation without treating an empty returned range as unmounted', () => {
    expect(
      assessPersistentVolumeClaim({ status: 'Bound', pods: [], podsTruncated: true }).risks,
    ).toEqual(['truncated'])
  })

  it('requires two known nodes and RWO before suggesting a multi-node investigation', () => {
    const pods = [
      { name: 'a', namespace: 'ns', nodeName: 'node-a' },
      { name: 'b', namespace: 'ns', nodeName: 'node-a' },
      { name: 'c', namespace: 'ns' },
    ]
    expect(
      assessPersistentVolumeClaim({ status: 'Bound', accessModes: ['ReadWriteOnce'], pods }).risks,
    ).toEqual([])
    expect(
      assessPersistentVolumeClaim({
        status: 'Bound',
        accessModes: ['ReadWriteMany'],
        pods: [...pods, { name: 'd', namespace: 'ns', nodeName: 'node-b' }],
      }).risks,
    ).toEqual([])
  })
})
