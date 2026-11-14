import { describe, expect, it, vi } from 'vitest'
import { getResourceMonitoring } from './api'
import { monitoringRatio } from './model'
const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api: mocks }))
describe('resource monitoring contracts', () => {
  it('keeps missing values distinct from zero and preserves overcommit', () => {
    expect(monitoringRatio(null, 4)).toBeNull()
    expect(monitoringRatio(0, 4)).toBe(0)
    expect(monitoringRatio(4, 0)).toBeNull()
    expect(monitoringRatio(6, 4)).toBe(150)
  })
  it('uses the fixed view endpoint with cluster scope and cancellation', async () => {
    const signal = new AbortController().signal
    const data = { clusterId: 'cluster-a', view: 'cpu' }
    mocks.get.mockResolvedValue({ data })
    await expect(
      getResourceMonitoring(
        'cluster-a',
        { view: 'cpu', node: 'n', rangeMinutes: 60, stepSeconds: 60 },
        signal,
      ),
    ).resolves.toBe(data)
    expect(mocks.get).toHaveBeenCalledWith(
      '/clusters/cluster-a/resource-monitoring?view=cpu&rangeMinutes=60&stepSeconds=60&node=n',
      { signal },
    )
    expect(mocks.get.mock.calls.at(-1)?.[0]).not.toContain('namespace=')
  })
})
