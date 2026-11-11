import { describe, expect, it } from 'vitest'
import { runtimeConfigurationKeys } from './keys'

describe('runtimeConfigurationKeys', () => {
  it('normalizes history filters into a stable hierarchy', () => {
    expect(
      runtimeConfigurationKeys.history({
        keyword: ' AI ',
        applyMode: ' hot ',
        source: ' default ',
      }),
    ).toEqual([
      'settings',
      'runtime-configuration',
      'history',
      { keyword: 'ai', applyMode: 'hot', source: 'default' },
    ])
  })

  it('normalizes application ids', () => {
    expect(runtimeConfigurationKeys.application(' a1 ')).toEqual([
      'settings',
      'runtime-configuration',
      'application',
      'a1',
    ])
  })
})
