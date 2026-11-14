import { describe, expect, it, vi } from 'vitest'
import { identityOutpostRoutes } from './routes'

const routePage = vi.hoisted(() => () => null)

vi.mock('./list-page', () => ({ IdentityOutpostsPage: routePage }))

describe('identity outpost route manifest', () => {
  it('maps the real UI route directly to the capability leaf module', async () => {
    expect(identityOutpostRoutes).toHaveLength(1)
    const [route] = identityOutpostRoutes

    expect(route.meta).toMatchObject({
      id: 'identity-outposts',
      path: '/identity/outposts',
      permissionKey: 'identity.outposts.view',
    })
    expect(route.shell).toBe('app')
    await expect(route.load()).resolves.toEqual({ default: routePage })
  })
})
