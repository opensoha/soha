import { describe, expect, it } from 'vitest'
import {
  identityProviderKeys,
  identityProviderMutationKeys,
  normalizeIdentityProviderFilters,
} from './keys'

describe('identity provider query keys', () => {
  it('uses stable list, detail, and OIDC client hierarchies', () => {
    expect(identityProviderKeys.lists()).toEqual(['identity', 'providers', 'list'])
    expect(identityProviderKeys.list({ applicationId: 'grafana', type: 'oidc' })).toEqual([
      'identity',
      'providers',
      'list',
      { applicationId: 'grafana', type: 'oidc' },
    ])
    expect(identityProviderKeys.detail(' provider/id ')).toEqual([
      'identity',
      'providers',
      'detail',
      'provider/id',
    ])
    expect(identityProviderKeys.oidcClients(' provider/id ')).toEqual([
      'identity',
      'providers',
      'detail',
      'provider/id',
      'oidc-clients',
      'list',
    ])
  })

  it('normalizes filters exactly as the API serializer does', () => {
    expect(
      normalizeIdentityProviderFilters({
        applicationId: ' grafana ',
        type: '',
        status: '',
        limit: 20.8,
        offset: 0.5,
      }),
    ).toEqual({ applicationId: 'grafana', limit: 20 })
    expect(identityProviderKeys.list({ limit: 0.5, offset: -1 })).toEqual(
      identityProviderKeys.list(),
    )
  })

  it('provides stable provider and OIDC mutation keys', () => {
    expect(identityProviderMutationKeys.create).toEqual([
      'identity',
      'providers',
      'mutation',
      'create',
    ])
    expect(identityProviderMutationKeys.updateOIDCClient).toEqual([
      'identity',
      'providers',
      'mutation',
      'oidc-client',
      'update',
    ])
  })
})
