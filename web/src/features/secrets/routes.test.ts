import { describe, expect, it } from 'vitest'
import { secretRoutes } from './routes'

describe('secret route manifest', () => {
  it('registers the canonical settings route with secret.view', () => {
    expect(secretRoutes).toHaveLength(1)
    expect(secretRoutes[0].meta).toMatchObject({
      id: 'settings-secrets',
      path: '/settings/secrets',
      parentId: 'settings',
      permissionKey: 'secret.view',
      workspace: 'system',
    })
  })
})
