/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { deliveryApi } from '../api'
import type { DeliveryTrigger } from '../types'
import { DeliveryTriggerManager } from './manager'

vi.mock('../api', () => ({
  deliveryApi: {
    triggers: {
      list: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      events: vi.fn(),
    },
  },
}))
let permitted = true
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: () => permitted,
}))
let root: Root
let host: HTMLDivElement
let client: QueryClient
let item: DeliveryTrigger
beforeEach(() => {
  ;(globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  window.matchMedia = vi
    .fn()
    .mockReturnValue({ matches: false, addListener: vi.fn(), removeListener: vi.fn() })
  const getStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getStyle(element))
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  permitted = true
  item = {
    id: 'trigger-1',
    name: 'Daily release',
    revision: 4,
    enabled: true,
    targetKind: 'workflow',
    targetId: 'workflow-1',
    workflowVersion: 2,
    type: 'schedule',
    schedule: {
      timeZone: 'Asia/Shanghai',
      runAt: ['2026-09-14T01:00:00Z'],
      excludedDates: ['2026-10-01'],
    },
    serviceAccountId: 'service_account:1',
    serviceAccountName: 'Release bot',
    signingSecretConfigured: false,
    createdAt: '2026-09-13T00:00:00Z',
    updatedAt: '2026-09-13T00:00:00Z',
    createdBy: 'user-1',
    updatedBy: 'user-1',
  }
  vi.mocked(deliveryApi.triggers.list).mockImplementation(async () => [item])
  vi.mocked(deliveryApi.triggers.update).mockImplementation(async (_, input) => {
    item = { ...item, ...input, revision: item.revision + 1 } as DeliveryTrigger
    return item
  })
  vi.mocked(deliveryApi.triggers.events).mockResolvedValue([
    {
      id: 'event-1',
      triggerId: 'trigger-1',
      triggerRevision: 4,
      status: 'succeeded',
      eventId: 'slot-1',
      eventType: 'schedule',
      occurredAt: '2026-09-14T01:00:00Z',
      createdAt: '2026-09-14T01:00:00Z',
      updatedAt: '2026-09-14T01:00:00Z',
      batchId: 'batch-1',
      reason: 'batch_accepted',
      attempts: 1,
    },
  ])
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  host.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.resetAllMocks()
})
async function settle() {
  for (let i = 0; i < 4; i++)
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 15))
    })
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <App>
            <DeliveryTriggerManager
              targetKind="workflow"
              targetId="workflow-1"
              workflowVersion={3}
              webhookRefs={[]}
            />
          </App>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await settle()
}
async function click(label: string) {
  const button = [...document.querySelectorAll('button')].find(
    (b) => b.textContent?.replace(/\s/g, '') === label.replace(/\s/g, ''),
  )
  expect(button, label).toBeDefined()
  await act(async () => button!.click())
  await settle()
}

it('disables with the loaded revision and links trigger acceptance to the actual delivery', async () => {
  await render()
  await act(async () =>
    document.querySelector<HTMLButtonElement>('[aria-label="启用 Daily release"]')!.click(),
  )
  await settle()
  expect(deliveryApi.triggers.update).toHaveBeenCalledWith(
    'trigger-1',
    expect.objectContaining({ enabled: false, expectedRevision: 4, workflowVersion: 2 }),
  )
  await click('记录')
  expect(document.body.textContent).toContain('已创建交付')
  expect(document.body.textContent).not.toContain('部署成功')
  expect(document.querySelector('a[href="/delivery/batches/batch-1"]')).not.toBeNull()
})

it('edits the calendar in its zone, explicitly advances the workflow version and retains credentials', async () => {
  await render()
  await click('配置')
  expect(document.querySelector<HTMLInputElement>('input[type="datetime-local"]')?.value).toBe(
    '2026-09-14T09:00',
  )
  expect(document.querySelector<HTMLInputElement>('input[type="date"]')?.value).toBe('2026-10-01')
  expect(document.querySelector<HTMLInputElement>('input[type="password"]')?.value).toBe('')
  await click('保存触发器')
  expect(deliveryApi.triggers.update).toHaveBeenCalledWith('trigger-1', {
    expectedRevision: 4,
    name: 'Daily release',
    enabled: true,
    targetKind: 'workflow',
    targetId: 'workflow-1',
    workflowVersion: 3,
    type: 'schedule',
    schedule: {
      timeZone: 'Asia/Shanghai',
      runAt: ['2026-09-14T01:00:00.000Z'],
      excludedDates: ['2026-10-01'],
    },
  })
})

it('does not fetch triggers without view permission', async () => {
  permitted = false
  await render()
  expect(document.body.textContent).toContain('无权查看触发器')
  expect(deliveryApi.triggers.list).not.toHaveBeenCalled()
})
