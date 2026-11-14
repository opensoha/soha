import { describe, expect, it } from 'vitest'
import { manifestKeys, normalizeManifestFilter } from './keys'

describe('manifestKeys', () => {
  it('normalizes optional list filters under one hierarchy', () => {
    const filter = normalizeManifestFilter({
      serviceId: ' api ',
      clusterId: ' dev ',
      namespace: ' team-a ',
      search: ' ingress ',
    })
    expect(filter).toEqual({
      applicationId: undefined,
      serviceId: 'api',
      clusterId: 'dev',
      namespace: 'team-a',
      search: 'ingress',
      page: undefined,
      pageSize: undefined,
      limit: undefined,
    })
    expect(manifestKeys.list(filter).slice(0, 3)).toEqual(['delivery', 'manifests', 'list'])
  })
})
