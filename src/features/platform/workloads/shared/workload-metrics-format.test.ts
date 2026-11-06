import { describe, expect, it } from 'vitest'
import { formatWorkloadMetricValue } from './workload-metrics-format'

describe('Kubernetes workload metric precision', () => {
  it.each([
    [0, '0 mCPU'],
    [0.0000005, '0.0005 mCPU'],
    [0.000008765, '0.00877 mCPU'],
    [0.000499, '0.499 mCPU'],
    [0.001, '1 mCPU'],
    [1.234, '1.23 cores'],
  ])('preserves a CPU sample of %s cores', (value, expected) => {
    expect(formatWorkloadMetricValue(value, 'cores')).toBe(expected)
  })
  it('retains missing values and other units', () => {
    expect(formatWorkloadMetricValue(Number.NaN, 'cores')).toBe('-')
    expect(formatWorkloadMetricValue(0, 'bytes')).toBe('0.0 B')
    expect(formatWorkloadMetricValue(2048, 'bytes/s')).toBe('2.0 KB/s')
  })
})
