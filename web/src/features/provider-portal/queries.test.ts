import { afterEach, describe, expect, it, vi } from 'vitest'
import { providerPortalApi } from './api'
import { providerPortalKeys } from './keys'
import { providerPortalQueries } from './queries'

async function executeQuery(options: { queryFn?: unknown }) {
  if (typeof options.queryFn !== 'function') throw new Error('Expected a query function')
  return options.queryFn({} as never)
}

describe('providerPortalQueries', () => {
  afterEach(() => vi.restoreAllMocks())

  it('returns unwrapped domain values from all query factories', async () => {
    const bootstrap = { applications: [] }
    const applications = [{ id: 'app-1' }]
    const security = { activeSession: 1 }
    vi.spyOn(providerPortalApi, 'bootstrap').mockResolvedValue(bootstrap as never)
    vi.spyOn(providerPortalApi, 'applications').mockResolvedValue(applications as never)
    vi.spyOn(providerPortalApi, 'security').mockResolvedValue(security as never)

    await expect(executeQuery(providerPortalQueries.bootstrap())).resolves.toBe(bootstrap)
    await expect(executeQuery(providerPortalQueries.applications())).resolves.toBe(applications)
    await expect(executeQuery(providerPortalQueries.security())).resolves.toBe(security)
  })

  it('uses normalized detail IDs and matching recent limits in keys and API calls', async () => {
    const application = vi
      .spyOn(providerPortalApi, 'application')
      .mockResolvedValue({ id: 'app-1' } as never)
    const recent = vi.spyOn(providerPortalApi, 'recent').mockResolvedValue([])
    const detailOptions = providerPortalQueries.application(' app-1 ')
    const recentOptions = providerPortalQueries.recent(6)

    expect(detailOptions.queryKey).toEqual(providerPortalKeys.application('app-1'))
    expect(recentOptions.queryKey).toEqual(providerPortalKeys.recent(6))
    await executeQuery(detailOptions)
    await executeQuery(recentOptions)

    expect(application).toHaveBeenCalledWith('app-1')
    expect(recent).toHaveBeenCalledWith(6)
    expect(providerPortalQueries.application(' ').enabled).toBe(false)
    expect(providerPortalQueries.bootstrap(false).enabled).toBe(false)
  })
})
