import { afterEach, describe, expect, it, vi } from 'vitest'
import { settingsApi } from './api'
import { settingsKeys } from './keys'
import { settingsQueries } from './queries'

async function executeQuery(options: { queryFn?: unknown }) {
  if (typeof options.queryFn !== 'function') throw new Error('Expected queryFn')
  return options.queryFn({} as never)
}

describe('settingsQueries', () => {
  afterEach(() => vi.restoreAllMocks())

  it('normalizes the identity provider response under its canonical key', async () => {
    vi.spyOn(settingsApi.identity, 'get').mockResolvedValue({
      providers: [{ id: 'oidc', name: 'OIDC', enabled: true } as never],
    })
    const options = settingsQueries.identity()

    expect(options.queryKey).toEqual(settingsKeys.identity.detail())
    await expect(executeQuery(options)).resolves.toEqual({
      providers: [expect.objectContaining({ id: 'oidc', type: 'oidc', scopes: [] })],
      defaultProviderId: 'oidc',
      localPasswordLoginEnabled: true,
    })
  })

  it('keeps branding values unwrapped for page consumers', async () => {
    const branding = vi
      .spyOn(settingsApi.branding, 'get')
      .mockResolvedValue({ appTitle: 'OpenSoha' } as never)

    await expect(executeQuery(settingsQueries.branding())).resolves.toEqual({
      appTitle: 'OpenSoha',
    })
    expect(branding).toHaveBeenCalledOnce()
  })

  it('disables protected settings queries when access is denied', () => {
    expect(settingsQueries.identity(false).enabled).toBe(false)
    expect(settingsQueries.branding(false).enabled).toBe(false)
    expect(settingsQueries.ai.detail(false).enabled).toBe(false)
    expect(settingsQueries.ai.skills(false).enabled).toBe(false)
    expect(settingsQueries.ai.dataSources(false).enabled).toBe(false)
    expect(settingsQueries.ai.analysisProfiles(false).enabled).toBe(false)
    expect(settingsQueries.ai.dataSourceCapabilities(false).enabled).toBe(false)
  })
})
