/** @vitest-environment jsdom */
import { act } from 'react'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ManifestOperationsPanel } from './operations-panel'

const testState = vi.hoisted(() => ({
  put: vi.fn(async () => ({ data: {} })),
  get: vi.fn(async (path: string) => ({
    data: path.endsWith('/bindings')
      ? [
          {
            id: 'binding-1',
            packageId: 'manifest-1',
            applicationEnvironmentId: 'env-1',
            environmentKey: 'test',
            clusterId: 'cluster-1',
            namespace: 'test',
            enabled: true,
            overlay: {},
            driftPolicy: 'report',
            deletionPolicy: 'orphan',
            version: 3,
          },
        ]
      : path.includes('/manifest-deployments')
        ? { items: [], total: 0 }
        : path.endsWith('/source')
          ? {
              mode: 'soha_managed',
              syncPolicy: 'manual',
              generation: 1,
              autoPublish: false,
              autoDeploy: false,
            }
          : [],
  })),
}))
vi.mock('@/features/auth', () => ({
  hasPermission: () => true,
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))
vi.mock('@/services/api-client', () => ({
  api: { get: testState.get, put: testState.put, post: vi.fn(), delete: vi.fn() },
}))

let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
let client: QueryClient
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  testState.put.mockClear()
  window.matchMedia = vi.fn().mockImplementation(() => ({
    matches: false,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})
afterEach(async () => {
  await act(async () => {
    root.unmount()
    // Ant Design Form clears validation with a 10 ms debounce. Let the pending
    // callback finish before Vitest destroys the jsdom window.
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  client.clear()
  container.remove()
  vi.unstubAllGlobals()
})

it('saves the Kustomize entry and immutable image digest, rejecting an invalid digest', async () => {
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <App>
          <ManifestOperationsPanel
            item={{
              id: 'manifest-1',
              name: 'API',
              applicationId: 'app-1',
              renderer: 'kustomize',
              status: 'published',
              currentRevision: 1,
              files: [],
              bindings: [],
              createdAt: '',
              updatedAt: '',
            }}
          />
        </App>
      </QueryClientProvider>,
    )
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  const click = async (label: string) =>
    act(async () => {
      const target = Array.from(
        container.querySelectorAll<HTMLElement>('button,[role="tab"]'),
      ).find((element) => element.textContent?.replace(/\s/g, '') === label)
      expect(target, label).toBeTruthy()
      target!.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
  const fill = async (id: string, value: string) =>
    act(async () => {
      const input = container.querySelector<HTMLInputElement>(`#${id}`)!
      expect(input, id).toBeTruthy()
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
  await click('环境配置')
  await fill('kustomize_entryPath', 'overlays/prod')
  await click('添加镜像映射')
  await fill('kustomize_images_0_name', 'api')
  await fill('kustomize_images_0_newName', 'registry.example.com/team/api')
  await fill('kustomize_images_0_digest', 'latest')
  await click('保存环境配置')
  expect(testState.put).not.toHaveBeenCalled()
  expect(container.textContent).toContain('64 位小写十六进制摘要')
  await fill('kustomize_images_0_digest', `sha256:${'a'.repeat(64)}`)
  await click('保存环境配置')
  expect(testState.put).toHaveBeenCalledWith(
    '/delivery/manifest-bindings/binding-1',
    expect.objectContaining({
      expectedVersion: 3,
      namespace: 'test',
      kustomize: {
        entryPath: 'overlays/prod',
        images: [
          {
            name: 'api',
            newName: 'registry.example.com/team/api',
            digest: `sha256:${'a'.repeat(64)}`,
          },
        ],
      },
    }),
  )
})
