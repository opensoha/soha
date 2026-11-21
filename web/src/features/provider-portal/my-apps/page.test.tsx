/** @vitest-environment jsdom */

import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '@/i18n'
import { useAuthStore } from '@/stores/auth-store'
import { PortalMyHostedAppsPage } from './page'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  patch: vi.fn(),
}))

const permissionState = vi.hoisted(() => ({
  keys: ['workbench.home.view', 'identity.portal.view', 'apphosting.apps.publish', 'observe.ai.chat'],
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))
vi.mock('@/features/auth', () => ({
  hasPermission: (_snapshot: unknown, key?: string) => !key || permissionState.keys.includes(key),
  logoutAuthSession: vi.fn(() => Promise.resolve()),
  usePermissionSnapshot: () => ({
    data: {
      data: {
        permissionKeys: permissionState.keys,
        visibleMenuIds: ['home-workbench'],
        visibleMenus: [],
      },
    },
  }),
}))

const ownerApp = {
  id: 'app-owned',
  name: '季度经营看板',
  description: '经营核心指标',
  kind: 'static' as const,
  dnsLabel: 'sales-a1b2',
  host: 'sales-a1b2.apps.soha.internal',
  url: 'https://sales-a1b2.apps.soha.internal',
  spaFallback: true,
  ownerUserId: 'usr-emp',
  maintainerUserIds: ['usr-collab'],
  maintainers: [{ userId: 'usr-collab', displayName: '李四', teamNames: ['研发部'] }],
  visibility: { mode: 'selected' as const, teamIds: ['team-mkt'] },
  pendingVisibilityApproval: {
    id: 'appr-1',
    appId: 'app-owned',
    appName: '季度经营看板',
    appDnsLabel: 'sales-a1b2',
    requesterUserId: 'usr-emp',
    currentVisibility: { mode: 'selected' as const, teamIds: ['team-mkt'] },
    requestedVisibility: { mode: 'organization' as const },
    baseRouteGeneration: 2,
    policyVersion: 1,
    status: 'pending' as const,
    reason: '全员复盘使用',
    createdAt: '2026-11-21T01:00:00Z',
    updatedAt: '2026-11-21T01:00:00Z',
  },
  status: 'online' as const,
  activeRevisionId: 'rev-2',
  activeRevisionNumber: 2,
  totalBytes: 65536,
  routeGeneration: 2,
  createdAt: '2026-11-20T01:00:00Z',
  updatedAt: '2026-11-21T01:00:00Z',
}

const collaboratorApp = {
  id: 'app-collab',
  name: '排班共享工具',
  description: '客服组排班表',
  kind: 'static' as const,
  dnsLabel: 'shift-c3d4',
  host: 'shift-c3d4.apps.soha.internal',
  url: 'https://shift-c3d4.apps.soha.internal',
  spaFallback: true,
  ownerUserId: 'usr-other-owner',
  ownerProfile: { userId: 'usr-other-owner', displayName: '张组长', teamNames: ['运营部'] },
  maintainerUserIds: ['usr-emp'],
  maintainers: [{ userId: 'usr-emp', displayName: '普通员工', teamNames: ['运营部'] }],
  visibility: { mode: 'selected' as const, teamIds: ['team-ops'] },
  status: 'online' as const,
  activeRevisionId: 'rev-c2',
  activeRevisionNumber: 2,
  routeGeneration: 1,
  createdAt: '2026-11-20T01:00:00Z',
  updatedAt: '2026-11-21T01:00:00Z',
}

const suspendedApp = {
  ...ownerApp,
  id: 'app-suspended',
  name: '异常轮询看板',
  status: 'suspended_by_admin' as const,
  suspendedReason: '高频循环请求触发平台保护',
  pendingVisibilityApproval: undefined,
}

const mountedRoots: Root[] = []

beforeAll(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn(() => ({
      matches: false,
      media: '',
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})

beforeEach(() => {
  vi.clearAllMocks()
  permissionState.keys = [
    'workbench.home.view',
    'identity.portal.view',
    'apphosting.apps.publish',
    'observe.ai.chat',
  ]
  useAuthStore.setState({
    user: {
      userId: 'usr-emp',
      userName: 'emp',
      email: 'emp@example.test',
      roles: ['employee'],
      teams: ['team-ops'],
      projects: [],
      tags: [],
    },
  })
})

afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  useAuthStore.getState().clearAuth()
  document.body.innerHTML = ''
})

async function flushAsync() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}

async function renderMyAppsPage() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={['/portal/my-apps']}>
            <I18nProvider>
              <PortalMyHostedAppsPage />
            </I18nProvider>
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await flushAsync()
  return container
}

describe('Portal My Hosted Apps page', () => {
  it('allows non-admin employee to manage owned app, withdraw pending approval, and confirm permanent delete', async () => {
    apiMocks.get.mockImplementation(async (path: string) => {
      if (path.startsWith('/app-hosting/apps?scope=mine')) {
        return { data: { items: [ownerApp, collaboratorApp, suspendedApp], total: 3 } }
      }
      if (path === '/app-hosting/apps/app-owned') {
        return { data: ownerApp }
      }
      if (path.startsWith('/app-hosting/apps/app-owned/revisions')) {
        return {
          data: {
            items: [
              {
                id: 'rev-2',
                appId: 'app-owned',
                revisionNumber: 2,
                kind: 'static',
                status: 'ready',
                bundleSha256: 'abcdef1234567890',
                fileCount: 10,
                totalBytes: 65536,
                spaFallback: true,
                createdBy: 'usr-emp',
                createdAt: '2026-11-21T01:00:00Z',
              },
              {
                id: 'rev-1',
                appId: 'app-owned',
                revisionNumber: 1,
                kind: 'static',
                status: 'ready',
                bundleSha256: '1111222233334444',
                fileCount: 8,
                totalBytes: 32768,
                spaFallback: true,
                createdBy: 'usr-emp',
                createdAt: '2026-11-20T01:00:00Z',
              },
            ],
            total: 2,
          },
        }
      }
      if (path.startsWith('/app-hosting/approvals')) {
        return { data: { items: [ownerApp.pendingVisibilityApproval], total: 1 } }
      }
      if (path.startsWith('/app-hosting/audience-candidates')) {
        return { data: { users: [], teams: [{ id: 'team-mkt', name: '市场部' }] } }
      }
      return { data: {} }
    })

    const container = await renderMyAppsPage()
    expect(container.textContent).toContain('季度经营看板')
    expect(container.textContent).toContain('排班共享工具')
    expect(container.textContent).toContain('全公司待审批')
    expect(container.textContent).toContain('管理员封停')

    const manageButtons = Array.from(container.querySelectorAll('button')).filter(
      (b) => b.textContent?.trim() === '管理 / 详情',
    )
    expect(manageButtons.length).toBeGreaterThanOrEqual(3)

    // Open owner app drawer
    await act(async () => {
      manageButtons[0]?.click()
    })
    await flushAsync()

    expect(document.body.textContent).toContain('全公司公开待审批，当前范围继续生效')

    // Withdraw pending visibility approval
    apiMocks.post.mockResolvedValueOnce({
      data: { ...ownerApp, pendingVisibilityApproval: undefined },
    })
    const withdrawBtn = Array.from(document.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === '撤回申请',
    )
    expect(withdrawBtn).toBeDefined()
    await act(async () => {
      withdrawBtn?.click()
    })
    await flushAsync()
    expect(apiMocks.post).toHaveBeenCalledWith(
      '/app-hosting/apps/app-owned/visibility-approval/withdraw',
      {},
    )

    // Switch to Lifecycle & Delete tab (only visible to owner)
    const lifecycleTab = Array.from(document.querySelectorAll<HTMLElement>('[role="tab"]')).find(
      (t) => t.textContent?.trim() === '上下线与删除',
    )
    expect(lifecycleTab).toBeDefined()
    await act(async () => {
      lifecycleTab?.click()
    })
    await flushAsync()

    const deleteBtn = Array.from(document.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === '确认彻底删除',
    )!
    expect(deleteBtn.disabled).toBe(true)

    const confirmInput = document.querySelector<HTMLInputElement>(
      'input[aria-label="确认删除应用名称"]',
    )!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        confirmInput,
        '错误名称',
      )
      confirmInput.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await flushAsync()
    expect(deleteBtn.disabled).toBe(true)

    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        confirmInput,
        '季度经营看板',
      )
      confirmInput.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await flushAsync()
    expect(deleteBtn.disabled).toBe(false)

    apiMocks.post.mockResolvedValueOnce({ data: { appId: 'app-owned', deleted: true } })
    await act(async () => {
      deleteBtn.click()
    })
    await flushAsync()
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/apps/app-owned/delete', {
      confirmationName: '季度经营看板',
    })
  })

  it('enforces collaborator boundary: hides audience/lifecycle/delete writes while allowing confirmed rollback', async () => {
    apiMocks.get.mockImplementation(async (path: string) => {
      if (path.startsWith('/app-hosting/apps?scope=mine')) {
        return { data: { items: [collaboratorApp, suspendedApp], total: 2 } }
      }
      if (path === '/app-hosting/apps/app-collab') {
        return { data: collaboratorApp }
      }
      if (path.startsWith('/app-hosting/apps/app-collab/revisions')) {
        return {
          data: {
            items: [
              {
                id: 'rev-c2',
                appId: 'app-collab',
                revisionNumber: 2,
                kind: 'static',
                status: 'ready',
                bundleSha256: 'c222222222222222',
                fileCount: 5,
                totalBytes: 12000,
                spaFallback: true,
                createdBy: 'usr-emp',
                createdAt: '2026-11-21T02:00:00Z',
              },
              {
                id: 'rev-c1',
                appId: 'app-collab',
                revisionNumber: 1,
                kind: 'static',
                status: 'ready',
                bundleSha256: 'c111111111111111',
                fileCount: 4,
                totalBytes: 10000,
                spaFallback: true,
                createdBy: 'usr-other-owner',
                createdAt: '2026-11-20T02:00:00Z',
              },
            ],
            total: 2,
          },
        }
      }
      if (path === '/app-hosting/apps/app-suspended') {
        return { data: suspendedApp }
      }
      if (path.startsWith('/app-hosting/apps/app-suspended/revisions')) {
        return { data: { items: [], total: 0 } }
      }
      if (path.startsWith('/app-hosting/approvals')) {
        return { data: { items: [], total: 0 } }
      }
      return { data: {} }
    })

    const container = await renderMyAppsPage()
    const manageButtons = Array.from(container.querySelectorAll('button')).filter(
      (b) => b.textContent?.trim() === '管理 / 详情',
    )

    // Open collaborator app drawer
    await act(async () => {
      manageButtons[0]?.click()
    })
    await flushAsync()

    expect(document.body.textContent).toContain('协作维护人只读概览')
    expect(
      Array.from(document.querySelectorAll('button')).find(
        (b) => b.textContent?.trim() === '保存可见范围',
      ),
    ).toBeUndefined()
    expect(
      Array.from(document.querySelectorAll<HTMLElement>('[role="tab"]')).find(
        (t) => t.textContent?.trim() === '上下线与删除',
      ),
    ).toBeUndefined()

    // Collaborator can open Revisions tab and confirm rollback to rev-c1
    const revisionsTab = Array.from(document.querySelectorAll<HTMLElement>('[role="tab"]')).find(
      (t) => t.textContent?.trim() === '版本与回滚',
    )!
    await act(async () => {
      revisionsTab.click()
    })
    await flushAsync()

    const rollbackBtn = Array.from(document.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === '回滚到此版本',
    )!
    expect(rollbackBtn).toBeDefined()
    await act(async () => {
      rollbackBtn.click()
    })
    await flushAsync()

    apiMocks.post.mockResolvedValueOnce({
      data: {
        app: { ...collaboratorApp, activeRevisionId: 'rev-c1', activeRevisionNumber: 1 },
        revision: {
          id: 'rev-c1',
          appId: 'app-collab',
          revisionNumber: 1,
          kind: 'static',
          status: 'ready',
          bundleSha256: 'c111111111111111',
          fileCount: 4,
          totalBytes: 10000,
          spaFallback: true,
          createdBy: 'usr-other-owner',
          createdAt: '2026-11-20T02:00:00Z',
        },
        reusedIdempotencyKey: false,
      },
    })
    const confirmRollbackBtn = Array.from(document.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === '确认回滚',
    )!
    expect(confirmRollbackBtn).toBeDefined()
    await act(async () => {
      confirmRollbackBtn.click()
    })
    await flushAsync()

    expect(apiMocks.post).toHaveBeenCalledWith(
      '/app-hosting/apps/app-collab/rollback',
      expect.objectContaining({
        targetRevisionId: 'rev-c1',
        idempotencyKey: expect.any(String),
      }),
    )

    // Also verify suspended_by_admin disables bringOnline and shows suspension reason
    const closeDrawerBtn = document.querySelector<HTMLButtonElement>('.ant-drawer-close')
    await act(async () => {
      closeDrawerBtn?.click()
    })
    await flushAsync()

    await act(async () => {
      manageButtons[1]?.click()
    })
    await flushAsync()
    expect(document.body.textContent).toContain('高频循环请求触发平台保护')
    const ownerLifecycleTab = Array.from(
      document.querySelectorAll<HTMLElement>('[role="tab"]'),
    ).find((t) => t.textContent?.trim() === '上下线与删除')!
    await act(async () => {
      ownerLifecycleTab.click()
    })
    await flushAsync()
    const disabledSuspendedBtn = Array.from(document.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('已被管理员封停（不可自行恢复上线）'),
    )
    expect(disabledSuspendedBtn?.disabled).toBe(true)
  })

  it('renders empty state CLI and Heya publish guidance when list is empty', async () => {
    apiMocks.get.mockResolvedValue({ data: { items: [], total: 0 } })
    const container = await renderMyAppsPage()
    expect(container.textContent).toContain('soha host deploy ./dist --name "季度看板"')
    expect(container.textContent).toContain('前往 Heya 个人助手一键发布')
  })
})
