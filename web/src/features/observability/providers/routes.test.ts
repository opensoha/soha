import { describe, expect, it, vi } from 'vitest'
import { observabilityProviderRoutes } from './routes'

const page = vi.hoisted(() => () => null)
vi.mock('./page', () => ({ ObservabilityProvidersPage: page }))

describe('observability provider route manifest', () => {
  it('loads the signal-neutral provider catalog', async () => {
    expect(observabilityProviderRoutes[0].meta).toMatchObject({
      path: '/monitoring-workbench/providers',
      permissionKey: 'observe.monitoring.view',
      scopeMode: 'passive',
    })
    await expect(observabilityProviderRoutes[0].load()).resolves.toEqual({ default: page })
  })
})
