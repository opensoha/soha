import { describe, expect, it } from 'vitest'
import { resolveRouteDefinitions } from '@/routes/definitions'
import { configurationRoutes } from './routes'

describe('configuration routes', () => {
  it('allows Secret metadata detail through the base view permission', () => {
    const routes = resolveRouteDefinitions(configurationRoutes)
    const permissionFor = (id: string) =>
      routes.find((route) => route.meta.id === id)?.meta.permissionKey

    expect(permissionFor('configuration-configmap-detail')).toBe(
      'platform.configuration.config-maps.view',
    )
    expect(permissionFor('configuration-secret-detail')).toBe('platform.configuration.secrets.view')
  })
})
