import { afterEach, describe, expect, it, vi } from 'vitest'
import { runtimeConfigurationApi } from './api'
import { runtimeConfigurationKeys } from './keys'
import { runtimeConfigurationQueries } from './queries'

async function executeQuery(options: { queryFn?: unknown }) {
  if (typeof options.queryFn !== 'function') throw new Error('Expected queryFn')
  return options.queryFn({} as never)
}

describe('runtimeConfigurationQueries', () => {
  afterEach(() => vi.restoreAllMocks())

  it('loads the current snapshot under the canonical key', async () => {
    vi.spyOn(runtimeConfigurationApi, 'get').mockResolvedValue({
      version: 2,
      items: [],
      pendingRestart: false,
    })
    const options = runtimeConfigurationQueries.snapshot()

    expect(options.queryKey).toEqual(runtimeConfigurationKeys.snapshot())
    await expect(executeQuery(options)).resolves.toMatchObject({ version: 2 })
  })

  it('polls the service resource snapshot every five seconds', async () => {
    vi.spyOn(runtimeConfigurationApi, 'resources').mockResolvedValue({ uptimeSeconds: 12 } as never)
    const options = runtimeConfigurationQueries.resources()

    expect(options.queryKey).toEqual(runtimeConfigurationKeys.resources())
    expect(options.enabled).toBe(true)
    const interval = options.refetchInterval as (query: unknown) => number | false
    expect(interval({ state: { status: 'success' } })).toBe(5_000)
    expect(interval({ state: { status: 'error' } })).toBe(false)
    await expect(executeQuery(options)).resolves.toMatchObject({ uptimeSeconds: 12 })
    expect(runtimeConfigurationQueries.resources(false).enabled).toBe(false)
  })

  it('polls only while an application is pending or applying', () => {
    const options = runtimeConfigurationQueries.application('application-1')
    const interval = options.refetchInterval
    expect(typeof interval).toBe('function')
    expect(
      (interval as (query: unknown) => number | false)({
        state: { status: 'error', data: { status: 'pending' } },
      }),
    ).toBe(false)
    expect(
      (interval as (query: unknown) => number | false)({ state: { data: { status: 'applying' } } }),
    ).toBe(1_000)
    expect(
      (interval as (query: unknown) => number | false)({ state: { data: { status: 'applied' } } }),
    ).toBe(false)
  })
})
