import { describe, expect, it, vi } from 'vitest'
import { observabilityRuleRoutes } from './routes'

const page = vi.hoisted(() => () => null)
vi.mock('./page', () => ({ AlertRulesPage: page }))

describe('alert rule route manifest', () => {
  it('preserves rule metadata and loads its leaf page', async () => {
    expect(observabilityRuleRoutes[0].meta).toMatchObject({
      path: '/monitoring-workbench/rules',
      permissionKey: 'observe.alert-rules.view',
      menuId: 'monitoring-workbench-rules',
    })
    await expect(observabilityRuleRoutes[0].load()).resolves.toEqual({ default: page })
  })
})
