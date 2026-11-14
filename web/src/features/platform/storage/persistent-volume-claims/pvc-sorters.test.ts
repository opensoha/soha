import { describe, expect, it } from 'vitest'
import { compareRequestedStorage } from './pvc-sorters'

describe('PVC requested capacity sorting', () => {
  it('compares binary, decimal and exponent quantities by capacity', () => {
    expect(compareRequestedStorage('900Mi', '1Gi')).toBeLessThan(0)
    expect(compareRequestedStorage('1G', '1Gi')).toBeLessThan(0)
    expect(compareRequestedStorage('1024Mi', '1Gi')).toBe(0)
    expect(compareRequestedStorage('1e9', '1G')).toBe(0)
  })

  it('keeps unknown values distinct from actual zero', () => {
    expect(compareRequestedStorage(undefined, '0')).toBeGreaterThan(0)
    expect(compareRequestedStorage('invalid', '0')).toBeGreaterThan(0)
    expect(compareRequestedStorage('0', undefined)).toBeLessThan(0)
    expect(compareRequestedStorage(undefined, undefined)).toBe(0)
    expect(compareRequestedStorage('bad-a', 'bad-b')).toBeLessThan(0)
  })
})
