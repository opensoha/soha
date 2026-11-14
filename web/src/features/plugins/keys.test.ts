import { describe, expect, it } from 'vitest'
import { pluginKeys, pluginMutationKeys } from './keys'

describe('pluginKeys', () => {
  it('normalizes marketplace filters under one canonical hierarchy', () => {
    expect(
      pluginKeys.marketplaceList({
        query: ' agent ',
        publisher: '',
        sourceId: 'catalog-a',
      }),
    ).toEqual(['plugins', 'marketplace', 'list', { query: 'agent', sourceId: 'catalog-a' }])
  })

  it('normalizes identifiers and keeps mutation keys under the plugin root', () => {
    expect(pluginKeys.installedDetail(' plugin-1 ')).toEqual([
      'plugins',
      'installed',
      'detail',
      'plugin-1',
    ])
    expect(pluginKeys.manifest(' plugin-1 ')).toEqual(['plugins', 'manifest', 'plugin-1'])
    expect(pluginMutationKeys.lifecycle('upgrade')).toEqual([
      'plugins',
      'mutation',
      'lifecycle',
      'upgrade',
    ])
  })
})
