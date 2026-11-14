import { describe, expect, it } from 'vitest'
import { identityPolicyRoutes } from './routes'

describe('identity policy route', () => {
  it('preserves the old URL as a hidden application redirect', () => {
    const [route] = identityPolicyRoutes
    expect(route.meta).toEqual(
      expect.objectContaining({
        id: 'identity-policies',
        path: '/identity/policies',
        navVisible: false,
        tabbar: false,
        permissionKey: 'identity.applications.view',
      }),
    )
    expect(route).toEqual(expect.objectContaining({ redirectTo: '/identity/applications' }))
  })
})
