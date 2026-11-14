import { describe, expect, it } from 'vitest'
import { podTargetFromRecord } from './types'

describe('pod action target', () => {
  it('always uses the record namespace for list actions', () => {
    expect(
      podTargetFromRecord('cluster-a', {
        name: 'api-pod',
        namespace: 'record-namespace',
      }),
    ).toEqual({
      scope: { clusterId: 'cluster-a', namespace: 'record-namespace' },
      name: 'api-pod',
    })
  })
})
