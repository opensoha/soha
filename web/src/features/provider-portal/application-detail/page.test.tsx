/** @vitest-environment jsdom */

import { ApiError } from '@/services/api-error'

import { I18nProvider } from '@/i18n'
import { usePreferencesStore } from '@/stores/preferences-store'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { PortalApplicationDetailPage } from './page'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const application = {
  id: 'app/1',
  slug: 'console',
  name: 'Operations Console',
  description: 'Manage production services',
  tags: ['production'],
  launchUrl: 'https://console.example.test',
  providerId: 'provider-1',
  providerType: 'link',
  portalVisible: true,
  featured: true,
  favorite: false,
  sortOrder: 1,
  status: 'enabled',
  metadata: { owner: 'platform' },
  assignments: [
    {
      id: 'assignment-1',
      applicationId: 'app/1',
      subjectType: 'role',
      subjectId: 'platform-admin',
      effect: 'allow',
      createdAt: '2026-01-01T00:00:00Z',
    },
  ],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-02T00:00:00Z',
}

const mountedRoots: Root[] = []

beforeAll(() => {
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
  usePreferencesStore.setState({ localeCode: 'en_US' })
  apiMocks.get.mockResolvedValue({ data: application })
})

afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
  usePreferencesStore.setState({ localeCode: 'zh_CN' })
})

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function renderPage() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={['/portal/applications/app%2F1']}>
            <I18nProvider>
              <Routes>
                <Route
                  path="/portal/applications/:applicationId"
                  element={<PortalApplicationDetailPage />}
                />
              </Routes>
            </I18nProvider>
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await flushAsyncWork()
  return container
}

describe('Provider Portal application detail page', () => {
  it('loads the decoded route identifier through the encoded API wire path', async () => {
    const container = await renderPage()
    expect(apiMocks.get).toHaveBeenCalledWith('/portal/applications/app%2F1')
    expect(container.textContent).toContain('Operations Console')
    expect(container.textContent).toContain('https://console.example.test')
    expect(container.textContent).toContain('owner')
    expect(container.textContent).toContain('platform')
    expect(container.textContent).not.toContain('Access scope')
    expect(container.textContent).not.toContain('platform-admin')
  })

  it.each(['zh_CN', 'en_US'] as const)(
    'localizes detail fields, values and actions in %s',
    async (localeCode) => {
      usePreferencesStore.setState({ localeCode })
      apiMocks.get.mockResolvedValue({ data: { ...application, favorite: true } })
      const container = await renderPage()
      const expected =
        localeCode === 'zh_CN'
          ? [
              '返回门户',
              '已收藏',
              '打开',
              '应用信息',
              '应用标识',
              '状态',
              '可用',
              '链接',
              '推荐',
              '创建时间',
              '更新时间',
              '访问地址',
              '标签',
              '元数据',
            ]
          : [
              'Back to Portal',
              'Favorited',
              'Open',
              'Application',
              'Slug',
              'Status',
              'Available',
              'Link',
              'Featured',
              'Created At',
              'Updated At',
              'Launch URL',
              'Tags',
              'Metadata',
            ]
      for (const label of expected) expect(container.textContent).toContain(label)
      const favoriteAction = localeCode === 'zh_CN' ? '取消收藏' : 'Unfavorite'
      expect(container.querySelector(`button[aria-label="${favoriteAction}"]`)).not.toBeNull()
      expect(container.textContent).toContain('production')
      expect(container.textContent).toContain('Manage production services')
      expect(container.textContent).not.toContain(
        localeCode === 'zh_CN' ? 'Provider type' : '取消收藏',
      )
    },
  )

  it('localizes empty fields and unavailable application messages', async () => {
    usePreferencesStore.setState({ localeCode: 'zh_CN' })
    apiMocks.get.mockResolvedValue({
      data: { ...application, description: '', launchUrl: '', metadata: {}, tags: [] },
    })
    const emptyFields = await renderPage()
    for (const label of ['暂无描述', '未配置应用访问地址', '暂无元数据', '无']) {
      expect(emptyFields.textContent).toContain(label)
    }
    apiMocks.get.mockRejectedValue(new ApiError(404, 'Application not found', { method: 'GET' }))
    const unavailable = await renderPage()
    expect(unavailable.textContent).toContain('应用不可用')
    expect(unavailable.textContent).toContain('该应用已停用、已隐藏或尚未授权给你。')
    expect(unavailable.textContent).toContain('返回门户')
  })
})
