import { afterEach, expect, it, vi } from 'vitest'
import type { ApplicationWorkbenchRefreshEnvelope } from '@opensoha/contracts/gen/ts/sohaapi'
import { advanceAuthSession, captureAuthSession } from './auth-session'
import {
  prepareWorkbenchReads,
  takeWorkbenchEntryRead,
  takeWorkbenchPermissionSnapshot,
} from './workbench-startup-reads'

const response = {
  permissionSnapshot: {
    permissionKeys: ['workbench.delivery.view'],
    visibleMenuIds: [],
    visibleMenus: [],
  },
  entry: {
    application: {
      id: 'app-1',
      key: 'app',
      name: 'App',
      group: 'demo',
      language: 'go',
      enabled: true,
      createdAt: '2026-11-23T00:00:00Z',
      updatedAt: '2026-11-23T00:00:00Z',
    },
    environments: [],
    services: [],
  },
} satisfies Pick<ApplicationWorkbenchRefreshEnvelope['data'], 'permissionSnapshot' | 'entry'>

afterEach(() => {
  advanceAuthSession()
  vi.useRealTimers()
})

it('hands off the startup reads once for the authorized application', () => {
  prepareWorkbenchReads(captureAuthSession(), 'app-1', response)
  const { signal } = new AbortController()
  expect(takeWorkbenchEntryRead('another-app', 'services', signal)).toBeUndefined()
  expect(takeWorkbenchEntryRead('app-1', 'services', signal)).toEqual([])
  expect(takeWorkbenchEntryRead('app-1', 'services', signal)).toBeUndefined()
  expect(takeWorkbenchPermissionSnapshot()).toBe(response.permissionSnapshot)
  expect(takeWorkbenchPermissionSnapshot()).toBeUndefined()
})

it('discards pending directory results after the auth owner changes', () => {
  prepareWorkbenchReads(captureAuthSession(), 'app-1', response)
  advanceAuthSession()
  const { signal } = new AbortController()
  expect(takeWorkbenchEntryRead('app-1', 'application', signal)).toBeUndefined()
  expect(takeWorkbenchPermissionSnapshot()).toBeUndefined()
})

it('expires unused startup results', () => {
  vi.useFakeTimers()
  prepareWorkbenchReads(captureAuthSession(), 'app-1', response)
  vi.advanceTimersByTime(30_000)
  const { signal } = new AbortController()
  expect(takeWorkbenchEntryRead('app-1', 'environments', signal)).toBeUndefined()
})
