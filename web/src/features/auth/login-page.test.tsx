/** @vitest-environment jsdom */

import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from 'antd'
import { LoginPage } from './login-page'
import {
  fetchLoginOptions,
  fetchPermissionSnapshot,
  restoreAuthSession,
} from '@/features/auth/auth-api'
import { authKeys } from '@/features/auth/keys'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import type { User } from '@/types'

vi.mock('@/features/auth/auth-api', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth/auth-api')>(
    '@/features/auth/auth-api',
  )
  return {
    ...actual,
    fetchAuthProviders: vi.fn(async () => []),
    fetchLoginOptions: vi.fn(async () => ({ verification: { sliderEnabled: false } })),
    fetchPermissionSnapshot: vi.fn(async () => ({
      permissionKeys: [],
      visibleMenuIds: [],
      visibleMenus: [],
    })),
    loginWithPassword: vi.fn(),
    restoreAuthSession: vi.fn(async () => 'unauthenticated'),
  }
})

vi.mock('@/stores/preferences-store', () => {
  const state = {
    currentWorkspace: 'resource',
    setThemeMode: vi.fn(),
    themeMode: 'light',
  }
  return {
    usePreferencesStore: Object.assign(
      (selector: (value: typeof state) => unknown) => selector(state),
      { getState: () => state },
    ),
  }
})

vi.mock('@/utils/branding', async () => ({
  ...(await vi.importActual<typeof import('@/utils/branding')>('@/utils/branding')),
  applyBrandingSettings: vi.fn(),
  persistBrandingSettings: vi.fn(),
  readStoredBrandingSettings: () => ({
    appTitle: 'Soha',
    collapsedLogoUrl: '',
    expandedLogoUrl: '',
    faviconUrl: '',
    loginLogoUrl: '',
    sidebarTitle: 'Soha',
    slogan: '让平台协作更简单',
  }),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

const user: User = {
  userId: 'user-1',
  userName: 'opensoha',
  email: 'opensoha@soha.local',
  roles: [],
  teams: [],
  projects: [],
  tags: [],
}

async function flushReact() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

async function renderLoginPage(options: { prefetchLoginOptions?: boolean } = {}) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })

  if (options.prefetchLoginOptions) {
    await queryClient.prefetchQuery({
      queryKey: authKeys.loginOptions(),
      queryFn: fetchLoginOptions,
      staleTime: 60_000,
    })
  }

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <App>
          <MemoryRouter initialEntries={['/login']}>
            <Routes>
              <Route path="/login" element={<LoginPage />} />
              <Route path="/" element={<div>landing page</div>} />
              <Route path="/portal" element={<div>portal landing page</div>} />
            </Routes>
          </MemoryRouter>
        </App>
      </QueryClientProvider>,
    )
  })

  await flushReact()

  return container
}

describe('login page', () => {
  beforeAll(() => {
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

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

    Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
      configurable: true,
      value: vi.fn(() => ({
        arc: vi.fn(),
        beginPath: vi.fn(),
        clearRect: vi.fn(),
        createLinearGradient: vi.fn(() => ({ addColorStop: vi.fn() })),
        fill: vi.fn(),
        fillRect: vi.fn(),
        lineTo: vi.fn(),
        moveTo: vi.fn(),
        restore: vi.fn(),
        save: vi.fn(),
        setTransform: vi.fn(),
        stroke: vi.fn(),
      })),
    })

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
    vi.stubGlobal(
      'requestAnimationFrame',
      vi.fn(() => 1),
    )
    vi.stubGlobal('cancelAnimationFrame', vi.fn())
  })

  beforeEach(() => {
    usePreferencesStore.getState().themeMode = 'light'
    vi.useFakeTimers()
    document.documentElement.dataset.themeMode = 'light'
    vi.mocked(fetchLoginOptions).mockResolvedValue({
      verification: { sliderEnabled: false },
    })
    vi.mocked(fetchPermissionSnapshot).mockResolvedValue({
      permissionKeys: [],
      visibleMenuIds: [],
      visibleMenus: [],
    })
    vi.mocked(restoreAuthSession).mockResolvedValue('unauthenticated')
    useAuthStore.getState().clearAuth()
  })

  afterEach(async () => {
    await act(async () => {
      for (const root of roots) {
        root.unmount()
      }
    })
    roots = []
    for (const container of containers) {
      container.remove()
    }
    containers = []
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.clearAllMocks()
    useAuthStore.getState().clearAuth()
  })

  it('renders copyright on the login page', async () => {
    const container = await renderLoginPage()

    expect(container.querySelector('.soha-auth-copyright')?.textContent).toBe(
      '© 2026 Soha 版权所有，由项目贡献者设计与开发。',
    )
  })

  it('presents the current product capabilities', async () => {
    const container = await renderLoginPage()

    expect(container.querySelector('.soha-auth-flow-title')?.textContent).toBe('让平台协作更简单')
    expect(container.querySelector('.soha-auth-flow-subtitle')).toBeNull()
    expect(container.textContent).not.toContain('统一到一个可协同、可治理的工作平台')
    expect(
      Array.from(container.querySelectorAll('.soha-auth-flow-node__question')).map(
        (node) => node.textContent,
      ),
    ).toEqual([
      '门户',
      'k8s工作台',
      '持续交付工作台',
      '可观测性工作台',
      '计算资源工作台',
      'AI工作台',
      '内网工作台',
      '设置中心',
    ])
    expect(container.textContent).toContain('对话、知识库、Agent、模型网关与评测')
    expect(container.textContent).not.toContain('AI Gateway')
    expect(container.textContent).not.toContain('虚拟化资源')
  })

  it.each(['light', 'dark'] as const)(
    'uses public server branding in %s mode before login',
    async (mode) => {
      usePreferencesStore.getState().themeMode = mode
      vi.mocked(fetchLoginOptions).mockResolvedValue({
        branding: {
          appTitle: 'shanchui',
          collapsedLogoUrl: 'https://cdn.example.com/icon.svg',
          expandedLogoUrl: 'https://cdn.example.com/sidebar-logo.svg',
          darkExpandedLogoUrl: 'https://cdn.example.com/sidebar-dark.svg',
          darkCollapsedLogoUrl: 'https://cdn.example.com/icon-dark.svg',
          faviconUrl: '',
          loginLogoUrl: 'https://cdn.example.com/login-logo.svg',
          sidebarTitle: 'shanchui',
          slogan: 'shanshui',
        },
        verification: { sliderEnabled: false },
      })

      const container = await renderLoginPage({ prefetchLoginOptions: true })

      expect(fetchLoginOptions).toHaveBeenCalledOnce()
      expect(container.querySelector('.soha-auth-flow-title')).toBeNull()
      expect(container.querySelector<HTMLImageElement>('.soha-auth-hero-logo-img')).toMatchObject({
        alt: 'shanchui',
        src:
          mode === 'dark'
            ? 'https://cdn.example.com/sidebar-dark.svg'
            : 'https://cdn.example.com/sidebar-logo.svg',
      })
      expect(container.querySelector<HTMLImageElement>('.soha-auth-brand-logo-img')).toMatchObject({
        alt: '',
        src:
          mode === 'dark'
            ? 'https://cdn.example.com/icon-dark.svg'
            : 'https://cdn.example.com/icon.svg',
      })
      expect(
        container.querySelector('img[src="https://cdn.example.com/login-logo.svg"]'),
      ).toBeNull()
    },
  )

  it('restores an existing browser session from the login page', async () => {
    vi.mocked(restoreAuthSession)
      .mockResolvedValueOnce('unavailable')
      .mockImplementationOnce(async () => {
        useAuthStore.getState().setUser(user)
        useAuthStore.getState().setTokens('new-access-token')
        return 'authenticated'
      })

    const container = await renderLoginPage()

    expect(container.textContent).toContain('恢复登录状态')
    expect(container.textContent).toContain('后端服务暂时不可用')
    expect(container.textContent).not.toContain('登录控制台')

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000)
    })
    await flushReact()

    expect(restoreAuthSession).toHaveBeenCalledTimes(2)
    await vi.waitFor(() => {
      expect(container.textContent).toContain('landing page')
    })
  }, 20_000)

  it('uses the portal as the default landing page when it is accessible', async () => {
    vi.mocked(fetchPermissionSnapshot).mockResolvedValue({
      permissionKeys: ['workbench.home.view', 'identity.portal.view'],
      visibleMenuIds: ['home-workbench'],
      visibleMenus: [{ id: 'home-workbench', path: '/portal' }],
    })
    vi.mocked(restoreAuthSession).mockImplementationOnce(async () => {
      useAuthStore.getState().setUser(user)
      useAuthStore.getState().setTokens('new-access-token')
      return 'authenticated'
    })

    const container = await renderLoginPage()

    await vi.waitFor(() => {
      expect(container.textContent).toContain('portal landing page')
    })
  })
})
