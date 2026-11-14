import { describe, expect, it } from 'vitest'
import { helmKeys } from './keys'

describe('helmKeys', () => {
  it('uses the canonical current manifest key', () => {
    const target = { clusterId: 'cluster-a', namespace: 'team-a', name: 'api' }
    expect(helmKeys.releaseManifest(target)).toEqual([
      ...helmKeys.releases('cluster-a'),
      'team-a',
      'api',
      'manifest',
      'current',
    ])
  })
})
