import { describe, expect, it, vi } from 'vitest'
import { observabilityOncallRoutes } from './routes'

const boardPage = vi.hoisted(() => () => null)
const settingsPage = vi.hoisted(() => () => null)
vi.mock('./board-page', () => ({ OnCallBoardPage: boardPage }))
vi.mock('./settings-page', () => ({ OnCallSettingsPage: settingsPage }))

describe('on-call route manifest', () => {
  it('keeps board and settings as separate view-gated leaf loaders', async () => {
    expect(observabilityOncallRoutes.map((route) => route.meta.path)).toEqual([
      '/monitoring-workbench/oncall',
      '/monitoring-workbench/oncall/settings',
    ])
    expect(observabilityOncallRoutes[0].meta.permissionKey).toBe('observe.oncall.view')
    expect(observabilityOncallRoutes[1].meta.permissionKey).toBe('observe.oncall.view')
    expect(observabilityOncallRoutes.map((route) => route.meta.navVisible)).toEqual([true, true])
    expect(observabilityOncallRoutes.map((route) => route.meta.menuId)).toEqual([
      'monitoring-workbench-oncall',
      'monitoring-workbench-oncall-settings',
    ])
    expect(observabilityOncallRoutes[1].meta.parentId).toBe('monitoring-workbench')
    await expect(observabilityOncallRoutes[0].load()).resolves.toEqual({ default: boardPage })
    await expect(observabilityOncallRoutes[1].load()).resolves.toEqual({
      default: settingsPage,
    })
  })
})
