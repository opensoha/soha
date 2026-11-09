import { describe, it, expect } from 'vitest'
import { trafficSegments, rateText } from './traffic-model'
import type { NetworkProxyTrafficSample } from '@opensoha/contracts/gen/ts/sohaapi'
const sample = (
  n: number,
  extra: Partial<NetworkProxyTrafficSample> = {},
): NetworkProxyTrafficSample => ({
  observedAt: new Date(n * 10000).toISOString(),
  uploadTotal: n,
  downloadTotal: n,
  uploadBytesPerSecond: 1,
  downloadBytesPerSecond: 2,
  processEpoch: 'a',
  sampleSequence: n,
  rateState: 'known',
  ...extra,
})
describe('proxy sampling continuity', () => {
  it('splits epochs, sequence gaps, unknown and long missing intervals', () => {
    expect(
      trafficSegments([
        sample(1),
        sample(2),
        sample(3, { processEpoch: 'b' }),
        sample(4, { rateState: 'gap', uploadBytesPerSecond: 0, downloadBytesPerSecond: 0 }),
        sample(5),
        sample(7),
        sample(20),
      ]).map((x) => x.length),
    ).toEqual([2, 1, 1, 1, 1])
  })
  it('does not turn nullable rate into zero', () => {
    expect(rateText(null, String)).toBe('—')
    expect(rateText(0, String)).toBe('0/s')
    expect(rateText(0, String, 'unknown')).toBe('—')
    expect(rateText(42, String, 'gap')).toBe('—')
    expect(trafficSegments([sample(1, { rateState: 'unknown' })])).toEqual([])
  })
})
