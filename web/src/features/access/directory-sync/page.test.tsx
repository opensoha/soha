/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { beforeAll, beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import type { PermissionSnapshot } from '@/types'
import { DirectorySyncPage } from './page'

const state = vi.hoisted(() => ({
  snapshot: { permissionKeys: [], visibleMenuIds: [], visibleMenus: [] } as PermissionSnapshot,
  connections: [] as unknown[],
  events: [] as unknown[],
  retryEvent: vi.fn(() => Promise.resolve({ id: 'event-1', status: 'queued' })),
  runtimeStatus: undefined as unknown,
}))

vi.mock('@/features/auth/permission-snapshot', () => ({
  usePermissionSnapshot: () => ({ data: { data: state.snapshot }, isLoading: false }),
  hasPermission: (snapshot: PermissionSnapshot | undefined, key: string) =>
    snapshot?.permissionKeys.includes(key) ?? false,
}))

vi.mock('./api', () => ({
  directorySyncApi: {
    listConnections: vi.fn(() => Promise.resolve(state.connections)),
    listConflicts: vi.fn(() => Promise.resolve([])),
    listRuns: vi.fn(() => Promise.resolve([])),
    getRuntimeStatus: vi.fn(() => Promise.resolve(state.runtimeStatus)),
    listEvents: vi.fn(() => Promise.resolve(state.events)),
    retryEvent: state.retryEvent,
    createConnection: vi.fn(),
    updateConnection: vi.fn(),
    validateConnection: vi.fn(),
    preview: vi.fn(),
    startSync: vi.fn(),
    cancelSync: vi.fn(),
    resolveConflict: vi.fn(),
  },
}))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

async function renderPage() {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <DirectorySyncPage />
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await act(async () => {
    await new Promise((resolve) => window.setTimeout(resolve, 0))
  })
}

describe('directory sync page', () => {
  beforeAll(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    )
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn(() => ({
        matches: false,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    })
  })
  beforeEach(() => {
    state.connections = []
    state.events = []
    state.runtimeStatus = undefined
    state.snapshot = { permissionKeys: [], visibleMenuIds: [], visibleMenus: [] }
  })
  afterEach(async () => {
    await act(async () => root?.unmount())
    container?.remove()
    vi.clearAllMocks()
  })

  it('blocks users without directory view permission', async () => {
    await renderPage()
    expect(container.textContent).toContain('当前账号没有查看目录同步的权限。')
  })

  it('shows management actions according to directory permissions', async () => {
    state.snapshot.permissionKeys = ['access.directory.view', 'access.directory.create']
    await renderPage()
    expect(container.textContent).toContain('新增目录连接')
    expect(container.textContent).toContain('关键词')
  })

  it('renders organizations as always enabled and people as disabled by default', async () => {
    state.snapshot.permissionKeys = ['access.directory.view']
    state.connections = [
      {
        id: 'dir-1',
        name: '飞书通讯录',
        providerType: 'feishu',
        enabled: true,
        capabilities: ['organizations'],
        status: 'healthy',
        policy: {
          syncOrganizations: true,
          syncPeople: false,
          mode: 'manual',
          provisionMode: 'review_before_link',
        },
      },
    ]
    await renderPage()
    expect(container.textContent).toContain('飞书通讯录')
    expect(container.textContent).toContain('已开启')
    expect(container.textContent).toContain('未开启')
  })

  it('shows callback, event queue, and reconciliation status for a selected connection', async () => {
    state.snapshot.permissionKeys = ['access.directory.view', 'access.directory.sync']
    state.connections = [
      {
        id: 'dir-1',
        name: '飞书通讯录',
        providerType: 'feishu',
        enabled: true,
        capabilities: ['organizations', 'events'],
        status: 'healthy',
        policy: {
          syncOrganizations: true,
          syncPeople: true,
          mode: 'scheduled_and_realtime',
          schedule: '0 * * * *',
          fullReconcileSchedule: '0 3 * * *',
          provisionMode: 'create_and_link',
          userDisablePolicy: 'managed_only',
          missingObjectPolicy: 'archive',
        },
      },
    ]
    state.runtimeStatus = {
      connectionId: 'dir-1',
      callbackUrl: 'https://soha.example/api/v1/integrations/directory/dir-1/events',
      callbackConfigured: true,
      callbackStatus: 'verified',
      callbackVerifiedAt: '2026-07-28T01:00:00Z',
      lastEventAt: '2026-07-28T02:00:00Z',
      queuedEvents: 2,
      failedEvents: 1,
      needsFullReconcile: false,
      lastIncrementalRun: {
        id: 'run-1',
        connectionId: 'dir-1',
        trigger: 'webhook',
        mode: 'incremental',
        status: 'succeeded',
        finishedAt: '2026-07-28T02:00:00Z',
      },
    }
    state.events = [
      {
        id: 'event-1',
        providerEventId: 'provider-event-1',
        eventType: 'contact.user.updated_v3',
        status: 'failed',
        attempts: 3,
        occurredAt: '2026-07-28T02:00:00Z',
        receivedAt: '2026-07-28T02:00:01Z',
        errorSummary: 'temporary provider error',
      },
    ]

    await renderPage()
    const detailsButton = container.querySelector<HTMLButtonElement>(
      'button[aria-label="查看同步记录"]',
    )
    await act(async () => detailsButton?.click())
    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 0))
    })

    const modalTitle = document.body.querySelector<HTMLElement>('.ant-modal-title')
    expect(modalTitle?.textContent).toBe('飞书通讯录 · 运行与事件')
    expect(modalTitle?.style.position).toBe('absolute')

    const runtimePanel = document.body.querySelector<HTMLElement>(
      '.ant-modal [role="tabpanel"][aria-hidden="false"] > .ant-space',
    )
    expect(runtimePanel?.style.minWidth).toBe('0')
    expect(
      [...(runtimePanel?.children ?? [])].every(
        (item) =>
          (item as HTMLElement).style.width === '100%' &&
          (item as HTMLElement).style.minWidth === '0',
      ),
    ).toBe(true)

    expect(document.body.textContent).toContain('事件回调地址')
    expect(document.body.textContent).toContain('待处理 2')
    expect(document.body.textContent).toContain('失败 1')
    expect(document.body.textContent).toContain('contact.user.updated_v3')
    expect(document.body.textContent).toContain('无需对账')

    const retryButton = [...document.body.querySelectorAll('button')].find(
      (button) => button.textContent === '重试',
    )
    await act(async () => retryButton?.click())
    expect(state.retryEvent).toHaveBeenCalledWith('dir-1', 'event-1')
  })
})
