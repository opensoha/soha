import { afterEach, describe, expect, it, vi } from 'vitest'
import { systemIntegrationsApi } from './api'
import { systemIntegrationKeys } from './keys'
import { systemIntegrationQueries } from './queries'

async function executeQuery(options: { queryFn?: unknown }) {
  if (typeof options.queryFn !== 'function') throw new Error('Expected queryFn')
  return options.queryFn({} as never)
}

describe('systemIntegrationQueries', () => {
  afterEach(() => vi.restoreAllMocks())

  it('loads a normalized category list under its hierarchical key', async () => {
    vi.spyOn(systemIntegrationsApi, 'list').mockResolvedValue([])
    const filters = { category: 'source_control' as const, providerType: ' gitlab ' }
    const options = systemIntegrationQueries.list(filters)

    expect(options.queryKey).toEqual(
      systemIntegrationKeys.list({ category: 'source_control', providerType: 'gitlab' }),
    )
    await executeQuery(options)
    expect(systemIntegrationsApi.list).toHaveBeenCalledWith(filters)
  })

  it('does not load a missing detail id', () => {
    expect(systemIntegrationQueries.detail('  ').enabled).toBe(false)
    expect(systemIntegrationQueries.detail('gitlab-main').enabled).toBe(true)
  })
})
