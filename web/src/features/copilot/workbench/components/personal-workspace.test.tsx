import { permissionSnapshotQueryKey } from '@/features/auth'
import { ApiError } from '@/services/api-error'
/** @vitest-environment jsdom */
import type {
  PersonalEnvironment,
  PersonalWorkspaceRuntime,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { PersonalWorkspace } from './personal-workspace'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const state = vi.hoisted(() => ({
  value: undefined as PersonalWorkspaceRuntime | undefined,
  failed: false,
  denied: 0,
  invoke: vi.fn(),
}))
vi.mock('../queries', () => ({
  workbenchQueries: {
    workspaceRuntime: (owner: string, assistant: string, enabled: boolean) => ({
      queryKey: ['workspace', owner, assistant],
      enabled,
      retry: false,
      queryFn: async () => {
        if (state.denied) throw new ApiError(state.denied, 'workspace denied', { method: 'GET' })
        if (state.failed) throw new Error('unavailable')
        return { data: state.value }
      },
    }),
    guestToolsOperation: () => ({ queryKey: ['tools'], enabled: false, queryFn: async () => ({}) }),
  },
}))
vi.mock('../mutations', () => ({
  workbenchMutations: {
    runtime: () => ({ mutationFn: state.invoke, retry: false }),
    guestTools: () => ({ mutationFn: vi.fn() }),
  },
}))
let root: Root
let node: HTMLDivElement
let client: QueryClient
const environment: PersonalEnvironment = {
  assistantId: 'assistant-one',
  state: 'ready',
  version: 7,
  dataInitialized: true,
  expiresAt: '2099-01-01T00:00:00Z',
}
beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn(() => ({
      matches: false,
      media: '',
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
})

beforeEach(() => {
  vi.clearAllMocks()
  state.denied = 0
  state.failed = false
  state.value = { assistantId: environment.assistantId, environmentVersion: 7 }
  state.invoke.mockReset()
  node = document.createElement('div')
  document.body.appendChild(node)
  root = createRoot(node)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  node.remove()
})
function button(label: string) {
  const b = Array.from(document.querySelectorAll('button')).find(
    (v) => v.textContent?.trim() === label,
  )
  expect(b).toBeDefined()
  return b!
}
async function click(element: HTMLElement) {
  await act(async () => {
    element.click()
    await new Promise((r) => setTimeout(r, 25))
  })
  await act(async () => {
    await new Promise((r) => setTimeout(r, 25))
  })
}
async function open() {
  await act(async () => {
    root.render(
      <App>
        <QueryClientProvider client={client}>
          <PersonalWorkspace owner="user-one" environment={environment} onRefresh={() => {}} />
        </QueryClientProvider>
      </App>,
    )
  })
  await click(button('打开个人工作空间'))
}
it('pauses commands after a failed workspace read and never dispatches automatically', async () => {
  state.failed = true
  await open()
  expect(document.body.textContent).toContain('个人工作空间读取失败')
  expect(document.body.textContent).toContain('操作已暂停')
  expect(button('执行命令').disabled).toBe(true)
  expect(button('接通执行能力').disabled).toBe(true)
  expect(state.invoke).not.toHaveBeenCalled()
})
it('keeps an old browser frame visible but rejects pointer input after a generation change', async () => {
  state.value = {
    assistantId: environment.assistantId,
    environmentVersion: 7,
    latest: {
      id: 'original',
      kind: 'browser',
      status: 'succeeded',
      assistantId: environment.assistantId,
      environmentVersion: 7,
      browser: {
        controlId: 'control',
        generation: 1,
        instanceId: 'instance',
        width: 800,
        height: 600,
        jpegBase64: 'AA==',
      },
    },
    browserControl: {
      controlId: 'control',
      generation: 2,
      owner: 'user',
      instanceId: 'instance',
      expiresAt: '2099-01-01T00:00:00Z',
    },
  }
  await open()
  await click(
    Array.from(document.querySelectorAll<HTMLElement>('[role="tab"]')).find(
      (e) => e.textContent === '浏览器',
    )!,
  )
  const pointer = document.querySelector<HTMLButtonElement>(
    'button[aria-label="在浏览器画面中点击"]',
  )!
  expect(pointer).not.toBeNull()
  expect(pointer.disabled).toBe(true)
  expect(document.body.textContent).toContain('保留的观察画面')
  expect(state.invoke).not.toHaveBeenCalled()
})

it.each([401, 403])(
  'hides cached runtime output after access is denied with %s',
  async (status) => {
    state.value = {
      assistantId: environment.assistantId,
      environmentVersion: 7,
      latest: {
        id: 'private-runtime',
        assistantId: environment.assistantId,
        environmentVersion: 7,
        kind: 'execute',
        status: 'succeeded',
        output: 'PRIVATE-RUNTIME-OUTPUT',
      },
    }
    await open()
    expect(document.body.textContent).toContain('PRIVATE-RUNTIME-OUTPUT')
    state.denied = status
    await act(async () => {
      await client.invalidateQueries({
        queryKey: ['workspace', 'user-one', environment.assistantId],
      })
    })
    await vi.waitFor(() =>
      expect(document.body.textContent).not.toContain('PRIVATE-RUNTIME-OUTPUT'),
    )
    expect(button('打开个人工作空间').disabled).toBe(true)
    expect(document.body.textContent).toContain(status === 401 ? '登录状态失效' : '无访问权限')
    expect(state.invoke).not.toHaveBeenCalled()
  },
)

it('shows read-only publish hint without permission and publishes workspace directory when authorized', async () => {
  await open()
  await click(
    Array.from(document.querySelectorAll<HTMLElement>('[role="tab"]')).find(
      (e) => e.textContent === '发布应用',
    )!,
  )
  expect(document.body.textContent).toContain('暂无托管应用发布权限')
  expect(button('配置并发布为托管应用').disabled).toBe(true)

  // Grant publish permission and publish dist directory
  await act(async () => {
    client.setQueryData(permissionSnapshotQueryKey, {
      data: {
        permissionKeys: ['apphosting.apps.publish'],
        visibleMenuIds: [],
        visibleMenus: [],
      },
    })
    await new Promise((r) => setTimeout(r, 25))
  })
  expect(button('配置并发布为托管应用').disabled).toBe(false)

  const pathInput = document.querySelector<HTMLInputElement>('input[aria-label="工作区发布相对路径"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      pathInput,
      'schedule-tool/dist',
    )
    pathInput.dispatchEvent(new Event('input', { bubbles: true }))
  })

  apiMocks.get.mockResolvedValue({ data: { items: [], total: 0 } })
  await click(button('配置并发布为托管应用'))

  const nameInput = document.querySelector<HTMLInputElement>('input[aria-label="托管应用名称"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      nameInput,
      '排班小工具',
    )
    nameInput.dispatchEvent(new Event('input', { bubbles: true }))
  })

  apiMocks.post.mockResolvedValue({
    data: {
      created: true,
      app: {
        id: 'app-ws-1',
        name: '排班小工具',
        kind: 'static',
        dnsLabel: 'schedule-a1b2',
        host: 'schedule-a1b2.apps.soha.internal',
        url: 'https://schedule-a1b2.apps.soha.internal',
        spaFallback: true,
        ownerUserId: 'user-one',
        maintainerUserIds: [],
        visibility: { mode: 'private' },
        status: 'online',
        routeGeneration: 1,
        createdAt: '2026-11-21T00:00:00Z',
        updatedAt: '2026-11-21T00:00:00Z',
      },
      revision: {
        id: 'rev-ws-1',
        appId: 'app-ws-1',
        revisionNumber: 1,
        kind: 'static',
        status: 'ready',
        bundleSha256: '1234567890abcdef',
        fileCount: 3,
        totalBytes: 4096,
        spaFallback: true,
        createdBy: 'user-one',
        createdAt: '2026-11-21T00:00:00Z',
      },
    },
  })

  await click(button('确认发布'))
  expect(apiMocks.post).toHaveBeenCalledWith(
    '/app-hosting/workspace-deployments',
    expect.objectContaining({
      name: '排班小工具',
      source: {
        kind: 'personal_environment',
        sourcePath: 'schedule-tool/dist',
        assistantId: 'assistant-one',
        environmentVersion: 7,
      },
    }),
  )
  await vi.waitFor(() => {
    expect(
      Array.from(document.querySelectorAll('button')).find(
        (v) => v.textContent?.replace(/\s+/g, '') === '完成',
      ),
    ).toBeDefined()
  })
  await click(
    Array.from(document.querySelectorAll('button')).find(
      (v) => v.textContent?.replace(/\s+/g, '') === '完成',
    )!,
  )
  expect(document.body.textContent).toContain('最近发布：排班小工具')
  expect(document.body.textContent).toContain('https://schedule-a1b2.apps.soha.internal')
})
