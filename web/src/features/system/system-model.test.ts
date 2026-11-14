import dayjs from 'dayjs'
import { describe, expect, it } from 'vitest'
import {
  buildAnnouncementDateRange,
  getMenuDerivedPermissionKeys,
  MENU_WORKBENCH_LABELS,
  MENU_WORKBENCH_ORDER,
  prettifyAction,
} from './system-model'

describe('buildAnnouncementDateRange', () => {
  it('builds the supported quick durations and clears both bounds for permanent announcements', () => {
    const start = dayjs('2026-09-03T10:00:00Z')

    expect(buildAnnouncementDateRange('one-day', start)[1]?.toISOString()).toBe(
      start.add(1, 'day').toISOString(),
    )
    expect(buildAnnouncementDateRange('three-days', start)[1]?.toISOString()).toBe(
      start.add(3, 'day').toISOString(),
    )
    expect(buildAnnouncementDateRange('one-week', start)[1]?.toISOString()).toBe(
      start.add(7, 'day').toISOString(),
    )
    expect(buildAnnouncementDateRange('one-month', start)[1]?.toISOString()).toBe(
      start.add(1, 'month').toISOString(),
    )
    expect(buildAnnouncementDateRange('forever', start)).toEqual([null, null])
  })
})

describe('prettifyAction', () => {
  it('makes common audit actions readable without hiding unknown actions', () => {
    expect(prettifyAction('list')).toBe('查看列表')
    expect(prettifyAction('portal.launch')).toBe('打开工作台')
    expect(prettifyAction('custom_action')).toBe('custom action')
  })
})

describe('menu workbench presentation', () => {
  it('includes all-of prerequisites with the monitoring view alternatives', () => {
    const keys = getMenuDerivedPermissionKeys({
      id: 'resource-monitoring',
      path: '/resource-monitoring',
    })
    expect(keys).toContain('platform.nodes.view')
    expect(keys).toContain('platform.resource-monitoring.cpu.view')
    expect(keys).toContain('platform.resource-monitoring.memory.view')
  })
  it('uses the product order and portal label', () => {
    expect(MENU_WORKBENCH_ORDER.slice(0, 8)).toEqual([
      'home',
      'platform',
      'delivery',
      'monitoring',
      'compute',
      'ai',
      'security',
      'settings',
    ])
    expect(MENU_WORKBENCH_LABELS.home).toBe('门户')
  })
})
