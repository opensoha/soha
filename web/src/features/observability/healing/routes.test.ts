import { describe, expect, it, vi } from 'vitest'
import { observabilityHealingRoutes } from './routes'

const page = vi.hoisted(() => () => null)
vi.mock('./page', () => ({ HealingPage: page }))

describe('healing route manifest', () => {
  it('loads the healing leaf with view permission metadata', async () => {
    expect(observabilityHealingRoutes[0].meta).toMatchObject({
      path: '/monitoring-workbench/healing',
      permissionKey: 'observe.healing.view',
    })
    await expect(observabilityHealingRoutes[0].load()).resolves.toEqual({ default: page })
  })
})
