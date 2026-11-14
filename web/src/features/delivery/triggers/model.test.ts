import { expect, it } from 'vitest'
import type { DeliveryTrigger } from '../types'
import { calendarInput, calendarInstant, triggerInput } from './model'

it('interprets calendar times in the configured zone and rejects nonexistent DST minutes', () => {
  expect(calendarInstant('2026-09-14T09:00', 'Asia/Shanghai')).toBe('2026-09-14T01:00:00.000Z')
  expect(calendarInput('2026-09-14T01:00:00Z', 'Asia/Shanghai')).toBe('2026-09-14T09:00')
  expect(() => calendarInstant('2026-03-08T02:30', 'America/New_York')).toThrow('不存在此时间')
  expect(() => calendarInstant('2026-09-14T09:00', 'invalid/zone')).toThrow()
})

it('updates the loaded revision without replaying read-only identity or event fields', () => {
  const item: DeliveryTrigger = {
    id: 'trigger-1',
    name: 'Daily release',
    revision: 4,
    enabled: true,
    targetKind: 'workflow',
    targetId: 'workflow-1',
    workflowVersion: 2,
    type: 'schedule',
    schedule: { timeZone: 'Asia/Shanghai', cron: '0 9 * * *' },
    serviceAccountId: 'service_account:1',
    serviceAccountName: 'Release bot',
    signingSecretConfigured: false,
    createdAt: '2026-09-13T00:00:00Z',
    updatedAt: '2026-09-13T00:00:00Z',
    createdBy: 'user-1',
    updatedBy: 'user-1',
  }
  expect(triggerInput(item, false)).toEqual({
    name: item.name,
    enabled: false,
    expectedRevision: 4,
    targetKind: 'workflow',
    targetId: 'workflow-1',
    workflowVersion: 2,
    type: 'schedule',
    schedule: item.schedule,
  })
})
