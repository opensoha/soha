import { describe, expect, it, vi } from 'vitest'
import { identityProviderRoutes } from './routes'

const routePage = vi.hoisted(() => () => null)

vi.mock('./list-page', () => ({ IdentityProvidersPage: routePage }))

describe('identity provider route manifest', () => {
  it('maps the UI route directly to the capability leaf module', async () => {
    expect(identityProviderRoutes).toHaveLength(1)
    const [route] = identityProviderRoutes

    expect(route.meta).toMatchObject({
      id: 'identity-providers',
      path: '/identity/providers',
      permissionKey: 'identity.providers.view',
    })
    expect(route.shell).toBe('app')
    await expect(route.load()).resolves.toEqual({ default: routePage })
  })
})
