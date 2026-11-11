import { describe, expect, it } from 'vitest'
import {
  identityApplicationKeys,
  identityApplicationMutationKeys,
  identityProviderCapabilityKeys,
} from './keys'

describe('identity application query keys', () => {
  it('uses stable hierarchical prefixes', () => {
    expect(identityApplicationKeys.lists()).toEqual(['identity', 'applications', 'list'])
    expect(identityApplicationKeys.list({ query: 'grafana', status: 'enabled' })).toEqual([
      'identity',
      'applications',
      'list',
      { query: 'grafana', status: 'enabled' },
    ])
    expect(identityProviderCapabilityKeys.all).toEqual(['identity', 'provider-capabilities'])
  })

  it('normalizes filters exactly as the request builder does', () => {
    expect(identityApplicationKeys.list({ query: ' grafana ', status: 'enabled' })).toEqual(
      identityApplicationKeys.list({ query: 'grafana', status: 'enabled' }),
    )
    expect(identityApplicationKeys.list({ query: '', status: '' })).toEqual(
      identityApplicationKeys.list({}),
    )
  })

  it('provides stable mutation keys', () => {
    expect(identityApplicationMutationKeys.create).toEqual([
      'identity',
      'applications',
      'mutation',
      'create',
    ])
    expect(identityApplicationMutationKeys.update).toEqual([
      'identity',
      'applications',
      'mutation',
      'update',
    ])
    expect(identityApplicationMutationKeys.remove).toEqual([
      'identity',
      'applications',
      'mutation',
      'delete',
    ])
  })
})
