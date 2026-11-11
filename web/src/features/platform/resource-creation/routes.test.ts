import { describe, expect, it } from 'vitest'
import { resourceCreationRoutes } from './routes'

describe('resource creation routes', () => {
  it('keeps the former page URL as an API-authorized modal redirect', () => {
    expect(resourceCreationRoutes).toHaveLength(1)
    expect(resourceCreationRoutes[0]?.meta).toMatchObject({
      id: 'platform-resource-creation',
      path: '/resource-creation',
      scopeMode: 'namespace',
    })
    expect(resourceCreationRoutes[0]?.permissionExemptReason).toBe(
      'modal redirect; the create API enforces the selected resource permission',
    )
    expect(resourceCreationRoutes[0]?.load).toBeTypeOf('function')
  })
})
