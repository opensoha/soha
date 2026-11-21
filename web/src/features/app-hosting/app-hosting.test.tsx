/** @vitest-environment jsdom */

import { act, useState } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '@/i18n'
import {
  AUDIENCE_NARROWING_WARNING,
  AudienceSelector,
  WorkspacePublishModal,
  appHostingApi,
  appHostingKeys,
  formatApprovalInvalidationReason,
  isAudienceNarrowed,
  resolveSafeHostedAppUrl,
  type HostedAppVisibilityInput,
} from './index'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  patch: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

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
})

afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function flushAsync() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 15))
  })
}

describe('app-hosting feature data layer and components', () => {
  it('validates safe URLs and audience narrowing accurately', () => {
    expect(resolveSafeHostedAppUrl('https://demo-a1.apps.soha.internal')).toBe(
      'https://demo-a1.apps.soha.internal/',
    )
    expect(resolveSafeHostedAppUrl('http://localhost:8080/app')).toBe(
      'http://localhost:8080/app',
    )
    expect(resolveSafeHostedAppUrl('javascript:alert(1)')).toBeNull()
    expect(resolveSafeHostedAppUrl('data:text/html,<h1>x</h1>')).toBeNull()
    expect(resolveSafeHostedAppUrl('https://user:secret@demo-a1.apps.soha.internal')).toBeNull()
    expect(resolveSafeHostedAppUrl(`https://demo.apps.soha.internal/${'a'.repeat(4096)}`)).toBeNull()
    expect(resolveSafeHostedAppUrl('')).toBeNull()
    expect(formatApprovalInvalidationReason('route_generation_changed')).toBe(
      '应用版本或路由配置已变更',
    )
    expect(formatApprovalInvalidationReason('app_suspended')).toBe('应用已被管理员强制封停')

    expect(
      isAudienceNarrowed({ mode: 'organization' }, { mode: 'selected', teamIds: ['t1'] }),
    ).toBe(true)
    expect(
      isAudienceNarrowed({ mode: 'selected', teamIds: ['t1'], userIds: ['u1'] }, { mode: 'private' }),
    ).toBe(true)
    expect(
      isAudienceNarrowed(
        { mode: 'selected', teamIds: ['t1', 't2'], userIds: ['u1'] },
        { mode: 'selected', teamIds: ['t1'], userIds: ['u1', 'u2'] },
      ),
    ).toBe(true)
    expect(
      isAudienceNarrowed(
        { mode: 'selected', teamIds: ['t1'], userIds: ['u1'] },
        { mode: 'selected', teamIds: ['t1', 't2'], userIds: ['u1'] },
      ),
    ).toBe(false)

    expect(appHostingKeys.portalList({ query: '  看板 ', kind: 'static' })).toEqual([
      'app-hosting',
      'portal',
      'list',
      { query: '看板', kind: 'static', authorUserId: '', limit: 60, offset: 0 },
    ])
  })

  it('calls canonical app-hosting REST endpoints with encoded paths and parameters', async () => {
    apiMocks.get.mockResolvedValue({ data: { items: [], authors: [], myAppsCount: 0, total: 0 } })
    await appHostingApi.listPortalApps({ query: '排班', kind: 'static', authorUserId: 'usr/1' })
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/app-hosting/portal/apps?query=%E6%8E%92%E7%8F%AD&kind=static&authorUserId=usr%2F1',
    )

    apiMocks.get.mockResolvedValue({ data: { users: [], teams: [] } })
    await appHostingApi.searchAudienceCandidates({ query: '财务', limit: 15 })
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/app-hosting/audience-candidates?query=%E8%B4%A2%E5%8A%A1&limit=15',
    )

    apiMocks.get.mockResolvedValue({ data: { items: [], total: 0 } })
    await appHostingApi.listMyApps({ query: 'sales', status: 'online' })
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/app-hosting/apps?scope=mine&query=sales&status=online',
    )

    apiMocks.post.mockResolvedValue({ data: { appId: 'app-1', deleted: true } })
    await appHostingApi.deleteApp('app-1', { confirmationName: 'Sales Board' })
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/apps/app-1/delete', {
      confirmationName: 'Sales Board',
    })
  })

  it('renders AudienceSelector narrowing risk alert when narrowing from selected to private', async () => {
    apiMocks.get.mockResolvedValue({
      data: {
        teams: [{ id: 'team-fin', name: '财务部' }],
        users: [{ id: 'usr-1', username: 'zhangsan', displayName: '张三', department: '财务部' }],
      },
    })

    function Harness() {
      const [vis, setVis] = useState<HostedAppVisibilityInput>({
        mode: 'selected',
        teamIds: ['team-fin'],
      })
      const [reason, setReason] = useState('')
      return (
        <AudienceSelector
          value={vis}
          onChange={setVis}
          reason={reason}
          onReasonChange={setReason}
          previousVisibility={{ mode: 'selected', teamIds: ['team-fin'] }}
        />
      )
    }

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    mountedRoots.push(root)
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    await act(async () => {
      root.render(
        <AntdApp>
          <QueryClientProvider client={queryClient}>
            <I18nProvider>
              <Harness />
            </I18nProvider>
          </QueryClientProvider>
        </AntdApp>,
      )
    })
    await flushAsync()

    expect(apiMocks.get).toHaveBeenCalledWith('/app-hosting/audience-candidates?limit=25')
    expect(container.textContent).not.toContain(AUDIENCE_NARROWING_WARNING)

    const privateRadio = container.querySelector<HTMLInputElement>('input[type="radio"][value="private"]')
    expect(privateRadio).not.toBeNull()
    await act(async () => {
      privateRadio?.click()
    })
    await flushAsync()

    expect(container.textContent).toContain(AUDIENCE_NARROWING_WARNING)
  })

  it.each(['static', 'dynamic'] as const)(
    'submits %s workspace without claiming a queued build is online',
    async (kind) => {
      const dynamic = kind === 'dynamic'
      apiMocks.get.mockResolvedValue({ data: { items: [], total: 0 } })
      apiMocks.post.mockResolvedValue({
        data: {
          created: true,
          app: {
            id: 'app-new',
            name: '季度看板',
            kind,
            dnsLabel: 'board-a1b2',
            host: 'board-a1b2.apps.soha.internal',
            url: 'https://board-a1b2.apps.soha.internal',
            spaFallback: true,
            ownerUserId: 'usr-1',
            maintainerUserIds: [],
            visibility: { mode: 'private' },
            status: dynamic ? 'draft' : 'online',
            routeGeneration: 1,
            createdAt: '2026-11-21T00:00:00Z',
            updatedAt: '2026-11-21T00:00:00Z',
          },
          revision: {
            id: 'rev-1',
            appId: 'app-new',
            revisionNumber: 1,
            kind,
            status: dynamic ? 'validating' : 'ready',
            bundleSha256: '1234567890abcdef',
            fileCount: 4,
            totalBytes: 2048,
            spaFallback: true,
            createdBy: 'usr-1',
            createdAt: '2026-11-21T00:00:00Z',
          },
        },
      })

      const onPublished = vi.fn()
      const container = document.createElement('div')
      document.body.appendChild(container)
      const root = createRoot(container)
      mountedRoots.push(root)
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

      await act(async () => {
        root.render(
          <AntdApp>
            <QueryClientProvider client={queryClient}>
              <I18nProvider>
                <WorkspacePublishModal
                  open
                  onClose={() => {}}
                  defaultName="季度看板"
                  source={{
                    kind: 'personal_environment',
                    sourcePath: 'dist',
                    assistantId: 'a-1',
                    environmentVersion: 3,
                  }}
                  sourceSummary="个人工作空间 /data/dist"
                  onPublished={onPublished}
                />
              </I18nProvider>
            </QueryClientProvider>
          </AntdApp>,
        )
      })
      await flushAsync()

      if (dynamic) {
        const typeOption = Array.from(document.querySelectorAll<HTMLElement>('.ant-segmented-item'))
          .find((item) => item.textContent?.trim() === '动态 HTTP 应用')
        expect(typeOption).toBeDefined()
        await act(async () => typeOption?.click())
      }
      const submitBtn = Array.from(document.querySelectorAll('button')).find((btn) =>
        btn.textContent?.includes('确认发布'),
      )
      expect(submitBtn).toBeDefined()
      await act(async () => {
        submitBtn?.click()
      })
      await flushAsync()

      expect(apiMocks.post).toHaveBeenCalledWith(
        '/app-hosting/workspace-deployments',
        expect.objectContaining({
          name: '季度看板',
          ...(dynamic ? { kind: 'dynamic' } : { spaFallback: true }),
          visibility: { mode: 'private' },
          source: {
            kind: 'personal_environment',
            sourcePath: 'dist',
            assistantId: 'a-1',
            environmentVersion: 3,
          },
          idempotencyKey: expect.any(String),
        }),
      )
      expect(onPublished).toHaveBeenCalledTimes(dynamic ? 0 : 1)

      const openLink = Array.from(document.querySelectorAll('a')).find((a) =>
        a.textContent?.includes('打开应用'),
      )
      if (dynamic) {
        expect(openLink).toBeUndefined()
        expect(document.body.textContent).toContain('查看版本进度')
        expect(apiMocks.post.mock.calls[0]?.[1]).not.toHaveProperty('spaFallback')
        return
      }
      expect(openLink?.getAttribute('href')).toBe('https://board-a1b2.apps.soha.internal/')
      expect(openLink?.getAttribute('target')).toBe('_blank')
      expect(openLink?.getAttribute('rel')).toBe('noopener noreferrer')
    },
  )

  it('omits unchanged visibility and filters offline apps when updating an existing app', async () => {
    apiMocks.get.mockResolvedValue({
      data: {
        items: [
          {
            id: 'app-offline',
            name: '已下线旧应用',
            kind: 'static',
            dnsLabel: 'off-a1',
            host: 'off-a1.apps.soha.internal',
            url: 'https://off-a1.apps.soha.internal',
            spaFallback: false,
            ownerUserId: 'usr-1',
            maintainerUserIds: [],
            visibility: { mode: 'private' },
            status: 'offline',
            routeGeneration: 2,
            createdAt: '2026-11-21T00:00:00Z',
            updatedAt: '2026-11-21T00:00:00Z',
          },
          {
            id: 'app-live',
            name: '在线运营看板',
            description: '原说明',
            kind: 'static',
            dnsLabel: 'live-b2',
            host: 'live-b2.apps.soha.internal',
            url: 'https://live-b2.apps.soha.internal',
            spaFallback: true,
            ownerUserId: 'usr-1',
            maintainerUserIds: [],
            visibility: { mode: 'organization' },
            status: 'online',
            routeGeneration: 3,
            createdAt: '2026-11-21T00:00:00Z',
            updatedAt: '2026-11-21T00:00:00Z',
          },
        ],
        total: 2,
      },
    })
    apiMocks.post.mockResolvedValue({
      data: {
        created: false,
        app: {
          id: 'app-live',
          name: '在线运营看板',
          kind: 'static',
          dnsLabel: 'live-b2',
          host: 'live-b2.apps.soha.internal',
          url: 'https://live-b2.apps.soha.internal',
          spaFallback: true,
          ownerUserId: 'usr-1',
          maintainerUserIds: [],
          visibility: { mode: 'organization' },
          status: 'online',
          routeGeneration: 4,
          createdAt: '2026-11-21T00:00:00Z',
          updatedAt: '2026-11-21T00:00:00Z',
        },
        revision: {
          id: 'rev-2',
          appId: 'app-live',
          revisionNumber: 2,
          kind: 'static',
          status: 'ready',
          bundleSha256: 'abcdef1234567890',
          fileCount: 2,
          totalBytes: 1024,
          spaFallback: true,
          createdBy: 'usr-1',
          createdAt: '2026-11-21T00:00:00Z',
        },
      },
    })

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    mountedRoots.push(root)
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    await act(async () => {
      root.render(
        <AntdApp>
          <QueryClientProvider client={queryClient}>
            <I18nProvider>
              <WorkspacePublishModal
                open
                onClose={() => {}}
                currentUserId="usr-1"
                source={{ kind: 'personal_environment', sourcePath: 'dist' }}
                sourceSummary="个人工作空间 /data/dist"
              />
            </I18nProvider>
          </QueryClientProvider>
        </AntdApp>,
      )
    })
    await flushAsync()

    const updateSeg = Array.from(document.querySelectorAll<HTMLElement>('.ant-segmented-item')).find(
      (item) => item.textContent === '更新已有应用',
    )
    expect(updateSeg).toBeDefined()
    await act(async () => {
      updateSeg?.click()
    })
    await flushAsync()

    expect(document.body.textContent).toContain('在线运营看板')
    expect(document.body.textContent).not.toContain('已下线旧应用')

    const submitBtn = Array.from(document.querySelectorAll('button')).find((btn) =>
      btn.textContent?.includes('确认发布'),
    )
    await act(async () => {
      submitBtn?.click()
    })
    await flushAsync()

    const [, payload] = apiMocks.post.mock.calls[0]
    expect(payload).toMatchObject({
      appId: 'app-live',
      name: '在线运营看板',
    })
    expect(payload.visibility).toBeUndefined()
  })
})
