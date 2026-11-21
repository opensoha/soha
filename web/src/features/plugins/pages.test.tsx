/** @vitest-environment jsdom */

import { ApiError } from '@/services/api-error'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { InstalledPluginDetailPage } from './installed/detail-page'
import { PluginMarketplaceDetailPage } from './marketplace/detail-page'
import { PluginMarketplacePage } from './marketplace/list-page'
import type { InstalledPlugin, PluginManifest } from './plugin-model'
import { PluginManifestSections } from './shared/manifest-sections'

const apiGetMock = vi.hoisted(() => vi.fn())
const apiPostMock = vi.hoisted(() => vi.fn())
const updatesMock = vi.hoisted(() => vi.fn())
const permissionSnapshotMock = vi.hoisted(() => ({
  permissionKeys: ['plugin.view', 'plugin.install', 'plugin.manage'],
}))

vi.mock('@/services/api-client', () => ({
  api: {
    delete: vi.fn(),
    get: apiGetMock,
    getEnvelope: (path: string) =>
      path.startsWith('/plugins/updates?') ? updatesMock(path) : apiGetMock(path),
    post: apiPostMock,
    postWithSignal: apiPostMock,
    put: vi.fn(),
  },
}))

vi.mock('@/features/auth', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth')>('@/features/auth')
  return {
    ...actual,
    usePermissionSnapshot: () => ({
      data: {
        data: {
          permissionKeys: permissionSnapshotMock.permissionKeys,
          visibleMenuIds: [],
          visibleMenus: [],
        },
      },
    }),
  }
})

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    dataSource,
    title,
  }: {
    dataSource: Array<{ name?: string }>
    title?: ReactNode
  }) => (
    <section>
      <h2>{title}</h2>
      {dataSource.map((item) => (
        <div key={item.name}>{item.name}</div>
      ))}
    </section>
  ),
}))

const mountedRoots: Root[] = []
const repositoryManifest: PluginManifest = {
  id: 'plugin.repository',
  name: 'Repository Plugin',
  publisher: 'Example Publisher',
  type: 'skill',
  version: '2.0.0',
  description: 'Installed without a catalog entry.',
}
const repositoryInstall: InstalledPlugin = {
  ...repositoryManifest,
  source: 'https://github.com/example/plugin',
  status: 'enabled',
  checksumStatus: 'verified',
  installedAt: '2026-11-20T00:00:00Z',
  updatedAt: '2026-11-20T00:00:00Z',
  installedBy: 'operator',
  manifest: repositoryManifest,
}
const localManifest: PluginManifest = {
  ...repositoryManifest,
  id: 'plugin.local',
  name: 'Local Plugin',
  type: 'connector',
}
const localInstall: InstalledPlugin = {
  ...repositoryInstall,
  ...localManifest,
  source: 'direct-manifest',
  status: 'disabled',
  manifest: localManifest,
}
const repositorySourceInfo = {
  kind: 'github-repo' as const,
  canonicalSource: 'https://github.com/example/plugin',
  commitSha: 'a'.repeat(40),
  rawManifestChecksum: `sha256:${'b'.repeat(64)}`,
  official: false as const,
}
const repositoryManifestWithPermissions: PluginManifest = {
  ...repositoryManifest,
  permissions: { required: ['old.read'] },
}
const repositoryInstallWithSource: InstalledPlugin = {
  ...repositoryInstall,
  sourceInfo: repositorySourceInfo,
  requestedPermissions: { required: ['old.read'] },
  manifest: repositoryManifestWithPermissions,
}

beforeAll(() => {
  class ResizeObserverMock {
    observe() {}
    unobserve() {}
    disconnect() {}
  }

  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({
      matches: false,
      media: '',
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  })
  Object.defineProperty(window, 'getComputedStyle', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({ getPropertyValue: vi.fn(() => '') })),
  })
})
beforeEach(() => {
  apiGetMock.mockReset()
  apiPostMock.mockReset()
  updatesMock.mockReset().mockResolvedValue({ items: [] })
  permissionSnapshotMock.permissionKeys = ['plugin.view', 'plugin.install', 'plugin.manage']
})
afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function renderPage(node: ReactNode, route: string) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[route]}>
        <AntdApp>
          <QueryClientProvider client={queryClient}>{node}</QueryClientProvider>
        </AntdApp>
      </MemoryRouter>,
    )
  })
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  return container
}

function CurrentSearchParams() {
  return <output data-testid="search-params">{useLocation().search}</output>
}

function buttonWithText(container: ParentNode, label: string) {
  const button = Array.from(container.querySelectorAll('button')).find(
    (candidate) => candidate.textContent?.replace(/\s+/g, '') === label,
  )
  expect(button).toBeDefined()
  return button!
}

function viewChoice(container: ParentNode, label: string) {
  const choice = Array.from(container.querySelectorAll('label'))
    .find((candidate) => candidate.textContent?.trim() === label)
    ?.querySelector<HTMLInputElement>('input[type="radio"]')
  expect(choice).toBeDefined()
  return choice!
}

async function submitKeyword(container: HTMLElement, value: string) {
  const input = container.querySelector<HTMLInputElement>(
    'input[placeholder="搜索 ID、名称、发布者"]',
  )!
  const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!
  await act(async () => {
    setValue.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await act(async () => container.querySelector('form')!.requestSubmit())
}

async function setInputByPlaceholder(container: ParentNode, placeholder: string, value: string) {
  const input = container.querySelector<HTMLInputElement>(`input[placeholder="${placeholder}"]`)!
  const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!
  await act(async () => {
    setValue.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}

describe('plugin list page data boundaries', () => {
  it('shows complete permission requirements and retains non-property configuration schemas', async () => {
    const container = await renderPage(
      <PluginManifestSections
        manifest={{
          id: 'qa-plugin',
          name: 'QA',
          publisher: 'Soha',
          version: '1.0.0',
          type: 'skill',
          permissions: { required: Array.from({ length: 8 }, (_, i) => `qa.permission.${i}`) },
          configSchema: { type: 'string', enum: ['one', 'two'] },
          secrets: {
            required: [
              { name: 'optional-token', required: false, description: 'Optional service token' },
            ],
          },
        }}
      />,
      '/plugins/marketplace/qa-plugin',
    )
    expect(container.textContent).toContain('qa.permission.7')
    expect(container.textContent).toContain('Optional service token')
    expect(container.textContent).toContain('可选')
    const schema = Array.from(container.querySelectorAll('[role="button"]')).find((button) =>
      button.textContent?.includes('配置 Schema'),
    )
    expect(schema).toBeDefined()
    await act(async () => {
      ;(schema as HTMLElement).click()
    })
    expect(container.querySelector('.soha-plugin-json')?.textContent).toContain('"enum"')
    expect(container.textContent).not.toContain('未声明配置 Schema')
  })

  it('loads marketplace data through the feature API', async () => {
    apiGetMock.mockImplementation((path: string) => {
      if (path === '/plugins/installed') return Promise.resolve({ data: [] })
      return Promise.resolve({
        data: [
          {
            id: 'plugin-market',
            name: 'Marketplace Plugin',
            publisher: 'Soha',
            type: 'skill',
            version: '1.0.0',
            source: 'static',
            installed: false,
            manifest: {
              extensionPoints: {
                alerts: {
                  notificationChannels: [
                    { id: 'plugin-market.alerts', label: 'Marketplace Alerts' },
                  ],
                },
              },
            },
          },
        ],
      })
    })

    const container = await renderPage(<PluginMarketplacePage />, '/plugins/marketplace')
    expect(apiGetMock).toHaveBeenCalledWith('/plugins/marketplace')
    expect(apiGetMock).toHaveBeenCalledWith('/plugins/installed')
    expect(container.textContent).toContain('Marketplace Plugin')
    expect(container.querySelector('h1')?.textContent).toBe('插件市场')
    expect(container.querySelector('h2')?.textContent).toBe('Marketplace Plugin')
    expect(container.textContent).not.toContain('alerts.notificationChannels')
    expect(
      container.querySelector('.soha-plugin-market-card-link')?.getAttribute('href'),
    ).toContain('/plugins/marketplace/plugin-market')
    expect(container.querySelector('.soha-plugin-market-card-metrics')?.textContent).toContain(
      '1扩展点',
    )
    expect(container.querySelectorAll('.soha-plugin-market-card')).toHaveLength(1)
    expect(container.querySelector('.soha-plugin-market-card-body')).not.toBeNull()
    expect(container.querySelector('.soha-plugin-market-card-actions')).not.toBeNull()
    expect(
      container.querySelector('.soha-plugin-market-card-actions button')?.textContent,
    ).toContain('安装')
  })

  it('loads non-catalog installs from the Installed deep link and keeps detail actions gated', async () => {
    apiGetMock.mockImplementation(async (path: string) => {
      if (path === '/plugins/installed') return { data: [repositoryInstall, localInstall] }
      if (path === '/plugins/plugin.repository') return { data: repositoryInstall }
      if (path === '/plugins/plugin.repository/manifest') return { data: repositoryManifest }
      if (path === '/extensions/runtime') return { data: [] }
      throw new Error(`Unexpected GET ${path}`)
    })

    const container = await renderPage(
      <Routes>
        <Route path="/plugins/marketplace" element={<PluginMarketplacePage />} />
        <Route path="/plugins/installed/:pluginId" element={<InstalledPluginDetailPage />} />
      </Routes>,
      '/plugins/marketplace?installation=installed',
    )
    expect(apiGetMock.mock.calls).toEqual([['/plugins/installed']])
    expect(container.textContent).toContain('2 个插件')
    expect(container.textContent).toContain('Local Plugin')
    expect(container.textContent).toContain('Enabled')
    expect(container.textContent).toContain('版本 2.0.0')
    const manageLink = container.querySelector<HTMLAnchorElement>(
      'a[aria-label="管理 Repository Plugin"]',
    )!
    expect(manageLink.getAttribute('href')).toBe('/plugins/installed/plugin.repository')
    await act(async () => manageLink.click())
    await vi.waitFor(() => expect(apiGetMock).toHaveBeenCalledWith('/plugins/plugin.repository'))
    await vi.waitFor(() => expect(container.textContent).toContain('安装记录'))
    for (const action of ['停用', '配置', '升级', '移除']) {
      expect(buttonWithText(container, action).disabled).toBe(true)
    }
    expect(apiGetMock.mock.calls.some(([path]) => path.startsWith('/plugins/marketplace'))).toBe(
      false,
    )
  })

  it('filters Installed records locally and preserves independent Market filters and navigation', async () => {
    const marketOnly = { ...repositoryInstall, name: 'Catalog Plugin', installed: false }
    apiGetMock.mockImplementation(async (path: string) => {
      if (path === '/plugins/installed') return { data: [repositoryInstall, localInstall] }
      if (path.startsWith('/plugins/marketplace')) return { data: [marketOnly] }
      throw new Error(`Unexpected GET ${path}`)
    })

    const container = await renderPage(
      <>
        <PluginMarketplacePage />
        <CurrentSearchParams />
      </>,
      '/plugins/marketplace?context=keep',
    )
    await submitKeyword(container, 'Catalog')
    await vi.waitFor(() =>
      expect(apiGetMock).toHaveBeenCalledWith('/plugins/marketplace?q=Catalog'),
    )
    await act(async () => viewChoice(container, '已安装').click())
    expect(container.querySelector('output')?.textContent).toBe(
      '?context=keep&installation=installed',
    )
    expect(container.textContent).toContain('Repository Plugin')
    expect(container.textContent).not.toContain('Catalog Plugin')
    expect(container.querySelector('[placeholder="市场目录地址"]')).toBeNull()
    apiGetMock.mockClear()

    await submitKeyword(container, ' REPOSITORY ')
    expect(container.textContent).toContain('Repository Plugin')
    expect(container.textContent).not.toContain('Local Plugin')
    await act(async () => {
      container
        .querySelector<HTMLInputElement>('input[role="combobox"]')!
        .dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    })
    const connectorOption = document.querySelector<HTMLElement>(
      '.ant-select-item-option[title="Connector"]',
    )!
    await act(async () => connectorOption.click())
    await act(async () => container.querySelector('form')!.requestSubmit())
    expect(container.textContent).toContain('没有匹配插件')
    expect(container.textContent).not.toContain('尚未安装插件')
    expect(apiGetMock).not.toHaveBeenCalled()

    await act(async () => viewChoice(container, '市场').click())
    expect(container.querySelector('output')?.textContent).toBe('?context=keep')
    expect(
      container.querySelector<HTMLInputElement>('input[placeholder="搜索 ID、名称、发布者"]')
        ?.value,
    ).toBe('Catalog')
    await vi.waitFor(() => expect(container.textContent).toContain('Catalog Plugin'))
  })

  it('shows source failures without hiding healthy plugins', async () => {
    apiGetMock.mockImplementation((path: string) => {
      if (path === '/plugins/installed') return Promise.resolve({ data: [] })
      return Promise.resolve({
        items: [
          {
            id: 'healthy',
            name: 'Healthy Plugin',
            publisher: 'Soha',
            type: 'skill',
            version: '1',
            manifest: {},
          },
        ],
        meta: {
          sources: [{ sourceId: 'broken', status: 'unavailable', code: 'source_unavailable' }],
        },
      })
    })
    const container = await renderPage(<PluginMarketplacePage />, '/plugins/marketplace')
    expect(container.textContent).toContain('Healthy Plugin')
    expect(container.textContent).toContain('部分市场来源暂不可用：broken')
    expect(container.textContent).toContain('plugins.marketplace.third_party_urls')
    expect(container.querySelector('a[href="/settings/runtime-configuration"]')).toBeNull()
  })

  it('links to marketplace source settings for a permitted user without a source warning', async () => {
    permissionSnapshotMock.permissionKeys.push('settings.runtime-config.view')
    const container = await renderPage(<PluginMarketplacePage />, '/plugins/marketplace')
    expect(container.textContent).not.toContain('部分市场来源暂不可用')
    expect(container.querySelector('a[href="/settings/runtime-configuration"]')?.textContent).toBe(
      '管理市场来源',
    )
  })

  it('offers retry and opens the Installed view when the marketplace is unavailable', async () => {
    apiGetMock.mockImplementation((path: string) => {
      if (path === '/plugins/installed') return Promise.resolve({ data: [repositoryInstall] })
      return Promise.reject(new Error('network unavailable'))
    })

    const container = await renderPage(<PluginMarketplacePage />, '/plugins/marketplace')

    await vi.waitFor(() => expect(document.body.textContent).toContain('插件市场读取失败'))
    expect(document.body.textContent).toContain('network unavailable')
    expect(
      Array.from(document.body.querySelectorAll('button')).some(
        (button) => button.getAttribute('aria-label') === '重试读取插件市场',
      ),
    ).toBe(true)
    await act(async () => buttonWithText(container, '查看已安装插件').click())
    expect(container.textContent).toContain('Repository Plugin')
    expect(container.textContent).not.toContain('没有匹配插件')
    expect(apiGetMock.mock.calls.filter(([path]) => path === '/plugins/marketplace')).toHaveLength(
      1,
    )
  })

  it('distinguishes Installed loading, retryable failure and a successful empty result', async () => {
    let rejectInitialRead!: (error: Error) => void
    apiGetMock.mockImplementationOnce(
      () =>
        new Promise((_, reject) => {
          rejectInitialRead = reject
        }),
    )
    apiGetMock.mockResolvedValueOnce({ data: [] })

    const container = await renderPage(
      <PluginMarketplacePage />,
      '/plugins/marketplace?installation=installed',
    )
    expect(
      container.querySelector('[role="status"][aria-label="正在加载已安装插件"]'),
    ).not.toBeNull()
    expect(container.textContent).not.toContain('尚未安装插件')
    await act(async () => rejectInitialRead(new Error('install read unavailable')))
    await vi.waitFor(() => expect(document.body.textContent).toContain('已安装插件读取失败'))
    expect(container.textContent).not.toContain('尚未安装插件')
    await act(async () =>
      document.querySelector<HTMLButtonElement>('[aria-label="重试读取已安装插件"]')!.click(),
    )
    await vi.waitFor(() => expect(container.textContent).toContain('尚未安装插件'))
    expect(apiGetMock.mock.calls).toEqual([['/plugins/installed'], ['/plugins/installed']])
  })

  it('labels cached Installed records on transient failure and hides them on access denial', async () => {
    apiGetMock.mockResolvedValueOnce({ data: [repositoryInstall] })
    apiGetMock.mockRejectedValueOnce(new Error('install read unavailable'))
    apiGetMock.mockRejectedValueOnce(new ApiError(403, 'permission revoked'))
    const container = await renderPage(
      <PluginMarketplacePage />,
      '/plugins/marketplace?installation=installed',
    )
    const refresh = container.querySelector<HTMLButtonElement>('[aria-label="刷新插件列表"]')!
    await act(async () => refresh.click())
    await vi.waitFor(() => expect(container.textContent).toContain('1 个插件（未刷新）'))
    expect(container.textContent).toContain('Repository Plugin')
    await act(async () => refresh.click())
    await vi.waitFor(() => expect(container.textContent).toContain('已安装插件无访问权限'))
    expect(container.textContent).not.toContain('Repository Plugin')
    expect(container.textContent).not.toContain('尚未安装插件')
    expect(apiGetMock.mock.calls).toEqual(Array.from({ length: 3 }, () => ['/plugins/installed']))
  })

  it('previews and installs a repository independently of the marketplace, without enabling it', async () => {
    const preview = {
      manifest: { ...repositoryManifest, permissions: { required: ['repo.read'] } },
      sourceInfo: {
        ...repositorySourceInfo,
        pinnedRef: 'v2.0.0',
      },
      checksumStatus: 'verified',
      signatureStatus: 'unverified',
      previewToken: 'repository-preview-token',
      expiresAt: new Date(Date.now() + 10 * 60_000).toISOString(),
    }
    const installed = {
      ...repositoryInstallWithSource,
      status: 'disabled' as const,
      version: '2.0.0',
    }
    apiGetMock.mockImplementation(async (path: string) => {
      if (path === '/plugins/installed') return { data: [] }
      if (path.startsWith('/plugins/marketplace')) throw new Error('directory unavailable')
      throw new Error(`Unexpected GET ${path}`)
    })
    apiPostMock.mockImplementation(async (path: string) => {
      if (path === '/plugins/repository/preview') return { data: preview }
      if (path === '/plugins/repository/install') return { data: installed }
      throw new Error(`Unexpected POST ${path}`)
    })

    await renderPage(<PluginMarketplacePage />, '/plugins/marketplace')
    await vi.waitFor(() => expect(document.body.textContent).toContain('插件市场读取失败'))
    await act(async () => buttonWithText(document.body, '从仓库安装').click())
    await vi.waitFor(() => expect(document.body.textContent).toContain('从 GitHub 仓库安装'))

    await setInputByPlaceholder(
      document.body,
      'https://github.com/owner/repository',
      'https://github.com/example/plugin',
    )
    await setInputByPlaceholder(document.body, '分支、标签或提交（可选）', 'v2.0.0')
    await act(async () => buttonWithText(document.body, '预览').click())
    await vi.waitFor(() =>
      expect(apiPostMock).toHaveBeenCalledWith('/plugins/repository/preview', {
        repositoryUrl: 'https://github.com/example/plugin',
        ref: 'v2.0.0',
      }),
    )
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain(repositorySourceInfo.commitSha),
    )
    expect(document.body.textContent).toContain('GitHub 仓库')
    expect(document.body.textContent).toContain('官方收录否')
    expect(document.body.textContent).toContain(repositorySourceInfo.rawManifestChecksum)
    expect(document.body.textContent).toContain('repo.read')

    await act(async () => buttonWithText(document.body, '确认安装').click())
    await vi.waitFor(() =>
      expect(apiPostMock).toHaveBeenCalledWith('/plugins/repository/install', {
        previewToken: 'repository-preview-token',
      }),
    )
    await vi.waitFor(() => expect(document.body.textContent).toContain('安装记录已保存'))
    expect(document.body.textContent).toContain('未启用')
    expect(apiPostMock.mock.calls.some(([path]) => path === '/plugins/install')).toBe(false)
  })

  it('stops repository preview failure without falling back to the legacy install route', async () => {
    apiGetMock.mockImplementation(async (path: string) => {
      if (path === '/plugins/installed') return { data: [] }
      if (path.startsWith('/plugins/marketplace')) return { data: [] }
      throw new Error(`Unexpected GET ${path}`)
    })
    apiPostMock.mockRejectedValue(new ApiError(404, 'repository endpoint unavailable'))

    await renderPage(<PluginMarketplacePage />, '/plugins/marketplace?installation=installed')
    await act(async () => buttonWithText(document.body, '从仓库安装').click())
    await setInputByPlaceholder(
      document.body,
      'https://github.com/owner/repository',
      'https://github.com/example/plugin',
    )
    await act(async () => buttonWithText(document.body, '预览').click())
    await vi.waitFor(() => expect(document.body.textContent).toContain('未回退到旧安装或升级入口'))
    expect(apiPostMock).toHaveBeenCalledTimes(1)
    expect(apiPostMock).toHaveBeenCalledWith('/plugins/repository/preview', {
      repositoryUrl: 'https://github.com/example/plugin',
    })
    expect(apiPostMock.mock.calls.some(([path]) => path === '/plugins/install')).toBe(false)
  })

  it('loads release history only on demand and previews an older release with the market source binding', async () => {
    const repositoryUrl = repositorySourceInfo.canonicalSource
    const marketPlugin = {
      ...repositoryInstall,
      sourceId: 'official',
      installed: false,
      repositoryUrl,
    }
    const releaseUrl = `${repositoryUrl}/releases/tag/v1.0.0`
    const release = { name: 'v1.0.0', publishedAt: '2026-10-01T00:00:00Z', releaseUrl }
    apiGetMock.mockResolvedValue({ data: marketPlugin })
    apiPostMock.mockImplementation(async (path: string) => {
      if (path === '/plugins/repository/refs') {
        return {
          data: {
            refs: [
              release,
              { name: 'v0.2.0', releaseUrl: 'https://github.com.example/releases/tag/v0.2.0' },
              {
                name: 'v0.1.0',
                releaseUrl: 'http://github.com/example/plugin/releases/tag/v0.1.0',
              },
            ],
            hasMore: false,
          },
        }
      }
      if (path === '/plugins/repository/preview')
        return {
          data: {
            manifest: { ...repositoryManifest, version: '1.0.0' },
            sourceInfo: { ...repositorySourceInfo, official: true },
            previewToken: 'historical-release-preview',
            expiresAt: new Date(Date.now() + 10 * 60_000).toISOString(),
          },
        }
      if (path === '/plugins/repository/install') return { data: repositoryInstall }
      throw new Error(`Unexpected POST ${path}`)
    })
    const container = await renderPage(
      <Routes>
        <Route path="/plugins/marketplace/:pluginId" element={<PluginMarketplaceDetailPage />} />
      </Routes>,
      '/plugins/marketplace/plugin.repository?sourceId=official',
    )

    expect(apiPostMock).not.toHaveBeenCalled()
    expect(updatesMock).not.toHaveBeenCalled()
    await act(async () => buttonWithText(container, '查看发布历史').click())
    await vi.waitFor(() =>
      expect(container.querySelector(`a[href="${releaseUrl}"]`)).not.toBeNull(),
    )
    expect(container.querySelectorAll('a[target="_blank"]')).toHaveLength(1)
    expect(container.querySelector('time')?.dateTime).toBe(release.publishedAt)
    expect(apiPostMock).toHaveBeenCalledWith(
      '/plugins/repository/refs',
      {
        repositoryUrl,
        kind: 'release',
        page: 1,
      },
      expect.any(AbortSignal),
    )
    await act(async () =>
      container.querySelector<HTMLButtonElement>('[aria-label="预览安装 v1.0.0"]')!.click(),
    )
    await act(async () => buttonWithText(document.body, '预览').click())
    await vi.waitFor(() =>
      expect(apiPostMock).toHaveBeenCalledWith('/plugins/repository/preview', {
        repositoryUrl,
        pluginId: 'plugin.repository',
        marketplaceSourceId: 'official',
        ref: 'refs/tags/v1.0.0',
      }),
    )
    await vi.waitFor(() => expect(buttonWithText(document.body, '确认安装').disabled).toBe(false))
    await act(async () => buttonWithText(document.body, '确认安装').click())
    expect(apiPostMock).toHaveBeenCalledWith('/plugins/repository/install', {
      previewToken: 'historical-release-preview',
    })
  })

  it('checks updates only in Installed and requires permission-increase confirmation for a bound market upgrade', async () => {
    permissionSnapshotMock.permissionKeys = [
      'plugin.view',
      'plugin.install',
      'plugin.upgrade',
      'plugin.manage',
    ]
    const preview = {
      manifest: {
        ...repositoryManifestWithPermissions,
        version: '3.0.0',
        permissions: { required: ['old.read', 'new.write'], domain: ['new.domain'] },
      },
      sourceInfo: {
        ...repositorySourceInfo,
        commitSha: 'c'.repeat(40),
        rawManifestChecksum: `sha256:${'d'.repeat(64)}`,
      },
      checksumStatus: 'verified',
      signatureStatus: 'unverified',
      previewToken: 'upgrade-preview-token',
      expiresAt: new Date(Date.now() + 10 * 60_000).toISOString(),
    }
    const marketInstall: InstalledPlugin = {
      ...repositoryInstallWithSource,
      sourceInfo: {
        ...repositorySourceInfo,
        kind: 'marketplace',
        canonicalSource: 'https://catalog.example/plugins.json',
        sourceId: 'other',
        repositoryUrl: repositorySourceInfo.canonicalSource,
      },
    }
    const upgraded = {
      ...marketInstall,
      version: '3.0.0',
      manifest: preview.manifest,
      requestedPermissions: preview.manifest.permissions,
    }
    const earlierPlugins = Array.from({ length: 24 }, (_, index) => ({
      ...marketInstall,
      id: `plugin.earlier-${index}`,
    }))
    const updatePath = '/plugins/updates?pluginId=plugin.repository'
    let confirmed = false
    updatesMock.mockImplementation(async (path: string) => ({
      items:
        confirmed || path !== updatePath
          ? []
          : [
              {
                pluginId: marketInstall.id,
                repositoryUrl: repositorySourceInfo.canonicalSource,
                installedVersion: marketInstall.version,
                targetVersion: '3.0.0',
                ref: 'refs/tags/v3.0.0',
                marketplaceSourceId: 'other',
              },
            ],
    }))
    apiGetMock.mockImplementation(async (path: string) => {
      if (path === '/plugins/installed') {
        return { data: [...earlierPlugins, confirmed ? upgraded : marketInstall] }
      }
      if (path === '/plugins/marketplace') return { items: [] }
      if (path === '/plugins/plugin.repository') return { data: marketInstall }
      throw new Error(`Unexpected GET ${path}`)
    })
    apiPostMock.mockImplementation(async (path: string) => {
      if (path === '/plugins/repository/preview') return { data: preview }
      if (path === '/plugins/plugin.repository/repository/upgrade') {
        confirmed = true
        return { data: upgraded }
      }
      throw new Error(`Unexpected POST ${path}`)
    })

    const container = await renderPage(<PluginMarketplacePage />, '/plugins/marketplace')
    expect(updatesMock).not.toHaveBeenCalled()
    await act(async () => viewChoice(container, '已安装').click())
    await vi.waitFor(() => expect(updatesMock).toHaveBeenCalledOnce())
    expect(updatesMock).not.toHaveBeenCalledWith(updatePath)
    await act(async () => container.querySelector<HTMLElement>('li[title="3"]')!.click())
    await vi.waitFor(() => expect(container.textContent).toContain('2.0.0 → 3.0.0'))
    expect(updatesMock).toHaveBeenCalledWith(updatePath)
    await act(async () => buttonWithText(container, '升级').click())
    await vi.waitFor(() => expect(document.body.textContent).toContain('升级 Repository Plugin'))
    await act(async () => buttonWithText(document.body, '预览升级').click())
    await vi.waitFor(() =>
      expect(apiPostMock).toHaveBeenCalledWith('/plugins/repository/preview', {
        repositoryUrl: 'https://github.com/example/plugin',
        ref: 'refs/tags/v3.0.0',
        pluginId: 'plugin.repository',
        marketplaceSourceId: 'other',
      }),
    )
    await vi.waitFor(() => expect(document.body.textContent).toContain('new.write'))
    expect(document.body.textContent).toContain('新增 Gateway')
    expect(document.body.textContent).toContain('new.domain')
    expect(buttonWithText(document.body, '确认升级').disabled).toBe(true)

    await act(async () =>
      document.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click(),
    )
    expect(buttonWithText(document.body, '确认升级').disabled).toBe(false)
    await act(async () => buttonWithText(document.body, '确认升级').click())
    await vi.waitFor(() =>
      expect(apiPostMock).toHaveBeenCalledWith('/plugins/plugin.repository/repository/upgrade', {
        previewToken: 'upgrade-preview-token',
        confirmPermissionIncrease: true,
      }),
    )
    expect(
      apiPostMock.mock.calls.some(([path]) => path === '/plugins/plugin.repository/upgrade'),
    ).toBe(false)
    await vi.waitFor(() => expect(container.textContent).not.toContain('2.0.0 → 3.0.0'))
  })
})
