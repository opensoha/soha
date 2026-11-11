import { describe, expect, it } from 'vitest'
import { clusterTypeOptions, formatClusterType } from './presentation'

describe('cluster type presentation', () => {
  it('includes K3s and EKS in the selectable cluster types', () => {
    expect(clusterTypeOptions.map(({ value }) => value)).toEqual(
      expect.arrayContaining(['k3s', 'eks']),
    )
    expect(formatClusterType('k3s', 'zh_CN')).toBe('K3s')
    expect(formatClusterType('eks', 'en_US')).toBe('EKS')
  })
})
