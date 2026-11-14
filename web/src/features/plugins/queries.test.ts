import { afterEach, describe, expect, it, vi } from 'vitest'
import { pluginApi } from './plugin-api'
import { pluginKeys } from './keys'
import { pluginQueries } from './queries'

async function executeQuery(options: { queryFn?: unknown }) {
  if (typeof options.queryFn !== 'function') throw new Error('Expected a query function')
  return options.queryFn({} as never)
}

describe('pluginQueries', () => {
  afterEach(() => vi.restoreAllMocks())

  it('shares normalized filters between marketplace keys and wire calls', async () => {
    const plugins = [{ id: 'plugin-1' }]
    const marketplace = vi.spyOn(pluginApi, 'marketplace').mockResolvedValue(plugins as never)
    const options = pluginQueries.marketplace({ query: ' agent ', publisher: '' })

    expect(options.queryKey).toEqual(pluginKeys.marketplaceList({ query: 'agent' }))
    await expect(executeQuery(options)).resolves.toBe(plugins)
    expect(marketplace).toHaveBeenCalledWith({ query: 'agent' })
  })

  it('disables identifier queries until their target exists', () => {
    expect(pluginQueries.marketplaceDetail(' ').enabled).toBe(false)
    expect(pluginQueries.installedDetail('').enabled).toBe(false)
    expect(pluginQueries.manifest('', true).enabled).toBe(false)
    expect(pluginQueries.installedDetail('plugin-1').enabled).toBe(true)
    expect(pluginQueries.extensions(' ', false).queryKey).toEqual(pluginKeys.extensions('runtime'))
  })
})
